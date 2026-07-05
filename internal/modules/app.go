package modules

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"rent_game_accs/migrations"

	"rent_game_accs/internal/account"
	"rent_game_accs/internal/api"
	"rent_game_accs/internal/auth"
	"rent_game_accs/internal/game"
	"rent_game_accs/internal/payment"
	pkg_logger "rent_game_accs/internal/pkg/logger"
	"rent_game_accs/internal/pkg/monitoring"
	pkg_postgres_pool "rent_game_accs/internal/pkg/repository/postgres/pool"
	pkg_redis "rent_game_accs/internal/pkg/repository/redis"
	pkg_http_middleware "rent_game_accs/internal/pkg/transport/http/middleware"
	pkg_http_server "rent_game_accs/internal/pkg/transport/http/server"
	"rent_game_accs/internal/rental"
	repo_postgres "rent_game_accs/internal/repository/postgres"
	"rent_game_accs/internal/shared/clock"
	"rent_game_accs/internal/shared/database"
	shared_logger "rent_game_accs/internal/shared/logger"
	shared_middleware "rent_game_accs/internal/shared/middleware"
	"rent_game_accs/internal/shared/scheduler"
	"rent_game_accs/internal/user"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

func Run() {
	signalCtx, cancelSignal := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT, syscall.SIGTERM,
	)
	defer cancelSignal()

	logger, err := pkg_logger.NewLogger(pkg_logger.NewConfigMust())
	if err != nil {
		fmt.Println("failed to init application logger:", err)
		os.Exit(1)
	}
	defer logger.Close()

	logger.Debug("initializing postgres connection pool")

	pool, db, err := pkg_postgres_pool.NewConnectionPool(
		signalCtx,
		pkg_postgres_pool.NewPostgresConfigMust(),
	)
	if err != nil {
		logger.Fatal("failed to init postgres connection pool", zap.Error(err))
	}
	defer pool.Close()
	defer db.Close()

	runMigrations(db)

	rdb, err := pkg_redis.InitRedis(signalCtx, pkg_redis.NewRedisConfigMust())
	if err != nil {
		logger.Fatal("failed to init redis connection", zap.Error(err))
	}
	defer rdb.Close()

	repo := repo_postgres.NewRepository(pool)

	clk := clock.NewRealClock()

	bgScheduler := scheduler.New(logger.Logger)

	bgScheduler.Register(
		"expired_cleanup",
		30*time.Second,
		scheduler.NewExpiredCleanupWorker(repo, clk, logger.Logger),
	)

	var steamClient game.SteamClient
	steamKey := os.Getenv("STEAM_API_KEY")
	if steamKey == "" {
		logger.Warn("STEAM_API_KEY is not set. Falling back to FakeSteamClient for local testing.")
		steamClient = scheduler.NewFakeSteamClient()
	} else {
		steamBaseURL := os.Getenv("STEAM_BASE_URL")
		if steamBaseURL == "" {
			steamBaseURL = "https://api.steampowered.com"
		}
		steamClient = game.NewSteamClient(game.SteamConfig{
			APIKey:  steamKey,
			BaseURL: steamBaseURL,
		}, logger.Logger)
	}

	bgScheduler.Register(
		"steam_sync",
		5*time.Minute,
		scheduler.NewSteamSyncWorker(repo, steamClient, logger.Logger),
	)

	bgScheduler.Start(signalCtx)

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		logger.Fatal("JWT_SECRET is required")
	}

	jwtTTL := 24 * time.Hour
	if rawTTL := os.Getenv("JWT_TTL"); rawTTL != "" {
		parsedTTL, err := time.ParseDuration(rawTTL)
		if err != nil {
			logger.Fatal("invalid JWT_TTL", zap.String("value", rawTTL), zap.Error(err))
		}
		jwtTTL = parsedTTL
	}

	txManager := database.NewTxManager(pool.Pool)
	authRepo := auth.NewPostgresRepository(pool.Pool)
	userRepo := user.NewPostgresRepository(pool.Pool)
	gameRepo := game.NewPostgresRepository(pool.Pool)
	accountRepo := account.NewPostgresRepository(pool.Pool, os.Getenv("ENCRYPTION_KEY"))
	rentalRepo := rental.NewPostgresRepository(pool.Pool)

	authService := auth.NewPostgresService(authRepo, txManager, jwtSecret, jwtTTL)
	userService := user.NewPostgresService(userRepo)
	gameService := game.NewPostgresService(gameRepo)
	accountService := account.NewPostgresService(accountRepo)
	rentalService := rental.NewService(rentalRepo, accountRepo, userRepo, txManager)

	paymentRepo := payment.NewPostgresRepository(pool.Pool)
	paymentService := payment.NewPaymentService(paymentRepo)
	paymentHandler := payment.NewHandler(paymentService, logger.Logger)

	authHandler := auth.NewHandler(authService, logger.Logger)
	userHandler := user.NewHandler(userService, logger.Logger)
	gameHandler := game.NewHandler(gameService, logger.Logger)
	accountHandler := account.NewHandler(accountService, logger.Logger)
	apiHandler := api.NewHandler(pool.Pool, rentalService, accountRepo)

	sLogger := &shared_logger.Logger{Logger: logger.Logger}
	rateLimiter := shared_middleware.NewRateLimiter(5.0, 10.0)

	logger.Debug("Initialize HTTP server...")
	httpServer := pkg_http_server.NewHTTPServer(
		pkg_http_server.NewConfigMust(),
		logger,
		pkg_http_middleware.RequestID(),
		pkg_http_middleware.Metrics(),
		pkg_http_middleware.Logger(logger),
		pkg_http_middleware.Trace(),
		pkg_http_middleware.Panic(),
	)

	apiVersionRouter := pkg_http_server.NewAPIVersionRouter(pkg_http_server.ApiVersion1)

	apiVersionRouter.RegisterRoutes(authHandler.Routes(jwtSecret, rateLimiter, sLogger)...)
	apiVersionRouter.RegisterRoutes(userHandler.Routes(jwtSecret, sLogger)...)
	apiVersionRouter.RegisterRoutes(gameHandler.Routes()...)
	apiVersionRouter.RegisterRoutes(accountHandler.Routes()...)
	apiVersionRouter.RegisterRoutes(paymentHandler.Routes()...)
	apiVersionRouter.RegisterRoutes(apiHandler.Routes(jwtSecret, sLogger)...)

	httpServer.RegisterAPIRouters(apiVersionRouter)

	monitoring.RegisterActiveRentalsGauge(func() (float64, error) {
		var count int
		err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM rentals WHERE status = $1", int16(2)).Scan(&count)
		return float64(count), err
	})

	httpServer.RegisterHandler("/metrics", promhttp.Handler())

	httpServer.RegisterHandler("/healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"UP"}`))
	}))

	httpServer.RegisterHandler("/health/live", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"UP"}`))
	}))

	httpServer.RegisterHandler("/health/ready", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if signalCtx.Err() != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"DOWN","reason":"shutting down"}`))
			return
		}

		pingCtx, pingCancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer pingCancel()

		if err := pool.Ping(pingCtx); err != nil {
			logger.Error("Readiness check failed: postgres is down", zap.Error(err))
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"DOWN","reason":"postgres unavailable"}`))
			return
		}

		if err := rdb.Ping(pingCtx).Err(); err != nil {
			logger.Error("Readiness check failed: redis is down", zap.Error(err))
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"DOWN","reason":"redis unavailable"}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"UP"}`))
	}))

	serverCtx, cancelServer := context.WithCancel(context.Background())
	defer cancelServer()

	go func() {
		<-signalCtx.Done()
		logger.Warn("Shutdown signal received, initiating graceful shutdown...")

		logger.Info("Cool-off period: waiting for load balancer to remove instance...")
		time.Sleep(3 * time.Second)

		logger.Info("Stopping HTTP server...")
		cancelServer()
	}()

	if err := httpServer.Run(serverCtx); err != nil {
		logger.Error("HTTP server run error", zap.Error(err))
	}

	logger.Info("Stopping background scheduler...")
	bgScheduler.Stop()

	logger.Info("Graceful shutdown completed. Cleaning up connection pools.")
}

func runMigrations(db *sql.DB) {
	goose.SetBaseFS(migrations.EmbedMigrations)

	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("Не удалось установить диалект для goose: %v", err)
	}

	log.Println("Проверка встроенных миграций...")
	if err := goose.Up(db, "."); err != nil {
		log.Fatalf("Ошибка выполнения миграций goose: %v", err)
	}

	log.Println("Все миграции успешно применены!")
}
