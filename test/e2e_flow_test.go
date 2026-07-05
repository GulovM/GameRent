package test_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"go.uber.org/zap"

	"rent_game_accs/internal/account"
	"rent_game_accs/internal/auth"
	"rent_game_accs/internal/payment"
	pkg_postgres_pool "rent_game_accs/internal/pkg/repository/postgres/pool"
	pkg_http_server "rent_game_accs/internal/pkg/transport/http/server"
	"rent_game_accs/internal/rental"
	"rent_game_accs/internal/shared/database"
	shared_logger "rent_game_accs/internal/shared/logger"
	shared_middleware "rent_game_accs/internal/shared/middleware"
	"rent_game_accs/internal/user"
	"rent_game_accs/migrations"
)

func setupE2ETestDB(t *testing.T) (*pgxpool.Pool, database.TxManager) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "1" {
		t.Skip("set RUN_INTEGRATION_TESTS=1 and start PostgreSQL to run e2e tests")
	}

	ctx := context.Background()

	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5433"
	}
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost"
	}

	cfg := pkg_postgres_pool.PostgresConfig{
		Host:     host,
		Port:     port,
		User:     "postgres",
		Password: "postgres",
		Database: "game_rental",
		Timeout:  10 * time.Second,
	}

	poolConn, db, err := pkg_postgres_pool.NewConnectionPool(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to connect to test db: %v", err)
	}

	t.Cleanup(func() {
		poolConn.Close()
		_ = db.Close()
	})

	goose.SetBaseFS(migrations.EmbedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("failed to set goose dialect: %v", err)
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM payments")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM security_events")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM rentals")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM account_games")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM accounts")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM games")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM users")

	txManager := database.NewTxManager(poolConn.Pool)
	return poolConn.Pool, txManager
}

func TestE2E_RegistrationToRentalActivationFlow(t *testing.T) {
	ctx := context.Background()

	pool, txManager := setupE2ETestDB(t)

	t.Setenv("ENCRYPTION_KEY", "super-secret-32-byte-key-for-aes")
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "e2e-test-webhook-secret-12345")

	logger, _ := zap.NewDevelopment()
	defer logger.Sync()
	sLogger := &shared_logger.Logger{Logger: logger}

	jwtSecret := "e2e-jwt-secret-key-1234567890123"
	authRepo := auth.NewPostgresRepository(pool)
	authService := auth.NewPostgresService(authRepo, txManager, jwtSecret, 1*time.Hour)
	authHandler := auth.NewHandler(authService, logger)

	paymentRepo := payment.NewPostgresRepository(pool)
	paymentService := payment.NewPaymentService(paymentRepo)
	paymentHandler := payment.NewHandler(paymentService, logger)

	accountRepo := account.NewPostgresRepository(pool, "super-secret-32-byte-key-for-aes")
	userRepo := user.NewPostgresRepository(pool)
	rentalRepo := rental.NewPostgresRepository(pool)
	rentalService := rental.NewService(rentalRepo, accountRepo, userRepo, txManager)

	router := pkg_http_server.NewAPIVersionRouter(pkg_http_server.ApiVersion1)
	rateLimiter := shared_middleware.NewRateLimiter(100.0, 200.0)
	router.RegisterRoutes(authHandler.Routes(jwtSecret, rateLimiter, sLogger)...)
	router.RegisterRoutes(paymentHandler.Routes()...)

	mux := http.NewServeMux()
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", router))

	ts := httptest.NewServer(mux)
	defer ts.Close()

	regReq := auth.RegisterRequest{
		Email:     "buyer@example.com",
		Password:  "super-secure-pass-123",
		FirstName: "Buyer",
		LastName:  "User",
	}
	regReqBytes, _ := json.Marshal(regReq)

	res, err := http.Post(ts.URL+"/api/v1/auth/register", "application/json", bytes.NewBuffer(regReqBytes))
	if err != nil {
		t.Fatalf("failed to request register: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201 Created, got %d", res.StatusCode)
	}

	var regResp struct {
		Success bool                  `json:"success"`
		Data    auth.RegisterResponse `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&regResp); err != nil {
		t.Fatalf("failed to decode register response: %v", err)
	}

	if !regResp.Success {
		t.Fatalf("register response was not successful")
	}

	userID := regResp.Data.User.ID
	if userID == 0 {
		t.Fatalf("expected non-zero user ID")
	}

	_, err = pool.Exec(ctx, `UPDATE users SET balance = $1 WHERE id = $2`, 10000, userID)
	if err != nil {
		t.Fatalf("failed to top up user balance: %v", err)
	}

	loginReq := auth.LoginRequest{
		Email:    "buyer@example.com",
		Password: "super-secure-pass-123",
	}
	loginReqBytes, _ := json.Marshal(loginReq)

	res, err = http.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(loginReqBytes))
	if err != nil {
		t.Fatalf("failed to request login: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 OK, got %d", res.StatusCode)
	}

	var loginResp struct {
		Success bool               `json:"success"`
		Data    auth.LoginResponse `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&loginResp); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	if loginResp.Data.AccessToken == "" {
		t.Errorf("expected non-empty access token")
	}

	gameID := int64(9991)
	_, err = pool.Exec(ctx, `
		INSERT INTO games (id, name, steam_app_id, short_description, header_image, developers, publishers, genres)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		gameID, "Cyberpunk 2077", 1091500, "", "", []byte("[]"), []byte("[]"), []byte("[]"),
	)
	if err != nil {
		t.Fatalf("failed to insert game: %v", err)
	}

	encPassword, err := accountRepo.Encrypt("steam_secure_password_99")
	if err != nil {
		t.Fatalf("failed to encrypt password: %v", err)
	}

	creds, err := account.NewSteamCredentials("steam_buyer_e2e", encPassword, "76561198000000001")
	if err != nil {
		t.Fatalf("failed to create steam credentials: %v", err)
	}

	hourlyPrice, _ := account.NewMoney(150, "USD")
	depositAmount, _ := account.NewMoney(500, "USD")
	accEntity, err := account.NewAccount(creds, hourlyPrice, depositAmount, time.Now())
	if err != nil {
		t.Fatalf("failed to instantiate account: %v", err)
	}
	accEntity.ID = 8881
	accEntity.MarkSecurityChecked(true, true, time.Now())
	accEntity.SyncLibrary([]account.AccountGame{{
		Game:            account.Game{ID: gameID, SteamAppID: 1091500, Name: "Cyberpunk 2077"},
		PlaytimeMinutes: 240,
	}}, time.Now())

	if err := accEntity.Publish(time.Now()); err != nil {
		t.Fatalf("failed to publish account: %v", err)
	}

	if err := accountRepo.CreateAccount(ctx, accEntity); err != nil {
		t.Fatalf("failed to save account to database: %v", err)
	}

	rent, err := rentalService.RentAccount(ctx, userID, accEntity.ID, 3*time.Hour, time.Now())
	if err != nil {
		t.Fatalf("failed to rent account: %v", err)
	}
	if rent.ID == 0 {
		t.Fatalf("expected non-zero rental ID")
	}

	paymentID := int64(7771)
	rentalID := rent.ID
	_, err = pool.Exec(ctx, `
		INSERT INTO payments (id, rental_id, user_id, payment_type, status, amount, currency)
		VALUES ($1, $2, $3, 1, 1, $4, $5)`,
		paymentID, rentalID, userID, 950, "USD",
	)
	if err != nil {
		t.Fatalf("failed to insert payment record: %v", err)
	}

	webhookReq := payment.WebhookRequest{
		PaymentID:             strconv.FormatInt(paymentID, 10),
		ExternalTransactionID: "ext-tx-e2e-8877",
		Status:                "success",
	}
	webhookReqBytes, _ := json.Marshal(webhookReq)

	mac := hmac.New(sha256.New, []byte("e2e-test-webhook-secret-12345"))
	mac.Write(webhookReqBytes)
	signature := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest("POST", ts.URL+"/api/v1/payments/webhook", bytes.NewBuffer(webhookReqBytes))
	if err != nil {
		t.Fatalf("failed to create webhook request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Payment-Signature", signature)
	req.Header.Set("User-Agent", "E2E-Test-Client")

	client := &http.Client{}
	webhookRes, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to perform webhook request: %v", err)
	}
	defer webhookRes.Body.Close()

	if webhookRes.StatusCode != http.StatusOK {
		t.Fatalf("expected webhook status 200 OK, got %d", webhookRes.StatusCode)
	}

	var webhookResp struct {
		Success bool                    `json:"success"`
		Data    payment.WebhookResponse `json:"data"`
	}
	if err := json.NewDecoder(webhookRes.Body).Decode(&webhookResp); err != nil {
		t.Fatalf("failed to decode webhook response: %v", err)
	}

	if !webhookResp.Success {
		t.Fatalf("expected webhook response success to be true")
	}

	credsPayload := webhookResp.Data.Credentials
	if credsPayload == nil {
		t.Fatalf("expected credentials payload in webhook response, got nil")
	}
	if credsPayload.Login != "steam_buyer_e2e" {
		t.Errorf("expected steam login 'steam_buyer_e2e', got %q", credsPayload.Login)
	}
	if credsPayload.Password != "steam_secure_password_99" {
		t.Errorf("expected steam password 'steam_secure_password_99', got %q", credsPayload.Password)
	}
	if credsPayload.SteamID64 != "76561198000000001" {
		t.Errorf("expected steam_id64 '76561198000000001', got %q", credsPayload.SteamID64)
	}

	var payStatus int16
	var extTxID string
	err = pool.QueryRow(ctx, "SELECT status, external_transaction_id FROM payments WHERE id = $1", paymentID).Scan(&payStatus, &extTxID)
	if err != nil {
		t.Fatalf("failed to query payment status: %v", err)
	}
	if payStatus != 2 {
		t.Errorf("expected payment status 2, got %d", payStatus)
	}
	if extTxID != "ext-tx-e2e-8877" {
		t.Errorf("expected external tx ID 'ext-tx-e2e-8877', got %q", extTxID)
	}

	var rentalStatus int16
	err = pool.QueryRow(ctx, "SELECT status FROM rentals WHERE id = $1", rentalID).Scan(&rentalStatus)
	if err != nil {
		t.Fatalf("failed to query rental status: %v", err)
	}
	if rentalStatus != 2 {
		t.Errorf("expected rental status 2 (Active), got %d", rentalStatus)
	}

	var accStatus int16
	err = pool.QueryRow(ctx, "SELECT status FROM accounts WHERE id = $1", accEntity.ID).Scan(&accStatus)
	if err != nil {
		t.Fatalf("failed to query game account status: %v", err)
	}
	if accStatus != 4 {
		t.Errorf("expected game account status 4 (Rented), got %d", accStatus)
	}

	var countEvents int
	var loggedEventType int16
	var loggedClientIP string
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(MIN(event_type), 0), COALESCE(MIN(ip_address::text), '')
		FROM security_events 
		WHERE rental_id = $1`, rentalID,
	).Scan(&countEvents, &loggedEventType, &loggedClientIP)
	if err != nil {
		t.Fatalf("failed to query security events: %v", err)
	}
	if countEvents != 1 {
		t.Errorf("expected exactly 1 security event logged, got %d", countEvents)
	}
	if loggedEventType != 2 {
		t.Errorf("expected logged event_type = 2, got %d", loggedEventType)
	}

	if loggedClientIP != "127.0.0.1" && loggedClientIP != "127.0.0.1/32" && loggedClientIP != "::1" && loggedClientIP != "" {
		t.Errorf("expected client IP to be localhost, got %q", loggedClientIP)
	}
}
