package test_test

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"rent_game_accs/internal/account"
	"rent_game_accs/internal/api"
	"rent_game_accs/internal/game"
	"rent_game_accs/internal/notification"
	"rent_game_accs/internal/payment"
	"rent_game_accs/internal/rental"
	repo_postgres "rent_game_accs/internal/repository/postgres"
	"rent_game_accs/internal/review"
	"rent_game_accs/internal/shared/database"
	shared_logger "rent_game_accs/internal/shared/logger"
	shared_middleware "rent_game_accs/internal/shared/middleware"
	"rent_game_accs/internal/shared/scheduler"
	"rent_game_accs/internal/user"
)

type e2ePostgresPool struct {
	*pgxpool.Pool
}

func (e2ePostgresPool) OpTimeout() time.Duration { return 10 * time.Second }

func newAPIHandlerForE2E(
	pool *pgxpool.Pool,
	txManager database.TxManager,
	rentalService *rental.Service,
	paymentService payment.Service,
	accountRepo *account.PostgresRepository,
	userRepo *user.PostgresRepository,
) *api.Handler {
	authMiddleware := func(secret string, log *shared_logger.Logger) func(http.Handler) http.Handler {
		return shared_middleware.Auth(secret, log, pool)
	}

	var steamClient game.SteamClient = scheduler.NewFakeSteamClient()
	return api.NewHandler(
		authMiddleware,
		rentalService,
		paymentService,
		account.NewAdminService(accountRepo, txManager),
		user.NewAdminService(userRepo, txManager),
		steamClient,
		repo_postgres.NewRepository(e2ePostgresPool{Pool: pool}),
		review.NewService(review.NewPostgresRepository(pool)),
		notification.NewService(notification.NewPostgresRepository(pool)),
	)
}
