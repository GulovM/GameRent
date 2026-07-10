package test_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	"rent_game_accs/internal/api"
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

	lockConn, err := poolConn.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire integration test lock connection: %v", err)
	}
	if _, err := lockConn.Exec(ctx, "SELECT pg_advisory_lock($1)", int64(915202607)); err != nil {
		t.Fatalf("failed to acquire integration test lock: %v", err)
	}

	t.Cleanup(func() {
		_, _ = lockConn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", int64(915202607))
		lockConn.Release()
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

	_, _ = poolConn.Pool.Exec(ctx, "TRUNCATE refunds, financial_ledger_entries, deposit_holds")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM payments")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM security_events")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM audit_logs")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM rentals")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM account_games")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM accounts")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM games")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM users")

	txManager := database.NewTxManager(poolConn.Pool)
	return poolConn.Pool, txManager
}

func registerAndLoginE2EUser(t *testing.T, ts *httptest.Server, email, password, firstName, lastName string) (int64, string) {
	t.Helper()

	regReqBytes, _ := json.Marshal(auth.RegisterRequest{
		Email:     email,
		Password:  password,
		FirstName: firstName,
		LastName:  lastName,
	})

	res, err := http.Post(ts.URL+"/api/v1/auth/register", "application/json", bytes.NewBuffer(regReqBytes))
	if err != nil {
		t.Fatalf("failed to register user: %v", err)
	}
	defer res.Body.Close()

	var regResp struct {
		Success bool                  `json:"success"`
		Data    auth.RegisterResponse `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&regResp); err != nil {
		t.Fatalf("failed to decode register response: %v", err)
	}
	if !regResp.Success || regResp.Data.User.ID == 0 {
		t.Fatalf("unexpected register response: %+v", regResp)
	}

	loginReqBytes, _ := json.Marshal(auth.LoginRequest{Email: email, Password: password})
	res, err = http.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(loginReqBytes))
	if err != nil {
		t.Fatalf("failed to login user: %v", err)
	}
	defer res.Body.Close()

	var loginResp struct {
		Success bool               `json:"success"`
		Data    auth.LoginResponse `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&loginResp); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	if !loginResp.Success || loginResp.Data.AccessToken == "" {
		t.Fatalf("unexpected login response: %+v", loginResp)
	}

	return regResp.Data.User.ID, loginResp.Data.AccessToken
}

func loginE2EUser(t *testing.T, ts *httptest.Server, email, password string) string {
	t.Helper()

	loginReqBytes, _ := json.Marshal(auth.LoginRequest{Email: email, Password: password})
	res, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(loginReqBytes))
	if err != nil {
		t.Fatalf("failed to login user: %v", err)
	}
	defer res.Body.Close()

	var loginResp struct {
		Success bool               `json:"success"`
		Data    auth.LoginResponse `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&loginResp); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	if !loginResp.Success || loginResp.Data.AccessToken == "" {
		t.Fatalf("unexpected login response: %+v", loginResp)
	}
	return loginResp.Data.AccessToken
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
	rentalService := rental.NewService(rentalRepo, accountRepo, userRepo, paymentRepo, txManager)
	apiHandler := api.NewHandler(pool, rentalService, paymentService, accountRepo, nil, nil)

	router := pkg_http_server.NewAPIVersionRouter(pkg_http_server.ApiVersion1)
	rateLimiter := shared_middleware.NewRateLimiter(100.0, 200.0)
	router.RegisterRoutes(authHandler.Routes(jwtSecret, rateLimiter, sLogger)...)
	router.RegisterRoutes(paymentHandler.Routes()...)
	router.RegisterRoutes(apiHandler.Routes(jwtSecret, sLogger)...)

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

	rentalID := rent.ID
	var paymentID int64
	err = pool.QueryRow(ctx, `SELECT id FROM payments WHERE rental_id = $1`, rentalID).Scan(&paymentID)
	if err != nil {
		t.Fatalf("failed to load created payment record: %v", err)
	}

	var prePayStatus, preRentalStatus, preAccountStatus int16
	var preExpiresAt time.Time
	err = pool.QueryRow(ctx, `
		SELECT p.status, r.status, a.status, r.payment_expires_at
		FROM payments p
		JOIN rentals r ON r.id = p.rental_id
		JOIN accounts a ON a.id = r.account_id
		WHERE p.id = $1`,
		paymentID,
	).Scan(&prePayStatus, &preRentalStatus, &preAccountStatus, &preExpiresAt)
	if err != nil {
		t.Fatalf("failed to inspect pre-webhook state: %v", err)
	}
	if prePayStatus != 1 || preRentalStatus != 1 || preAccountStatus != 3 {
		t.Fatalf("unexpected pre-webhook state: payment=%d rental=%d account=%d expires_at=%s", prePayStatus, preRentalStatus, preAccountStatus, preExpiresAt.UTC().Format(time.RFC3339))
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
		Success bool                       `json:"success"`
		Data    map[string]json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(webhookRes.Body).Decode(&webhookResp); err != nil {
		t.Fatalf("failed to decode webhook response: %v", err)
	}

	if !webhookResp.Success {
		t.Fatalf("expected webhook response success to be true")
	}
	if _, ok := webhookResp.Data["credentials"]; ok {
		t.Fatalf("expected webhook response to omit credentials")
	}

	replayReq, err := http.NewRequest("POST", ts.URL+"/api/v1/payments/webhook", bytes.NewBuffer(webhookReqBytes))
	if err != nil {
		t.Fatalf("failed to create replay webhook request: %v", err)
	}
	replayReq.Header.Set("Content-Type", "application/json")
	replayReq.Header.Set("X-Payment-Signature", signature)
	replayReq.Header.Set("User-Agent", "E2E-Test-Client")

	replayRes, err := client.Do(replayReq)
	if err != nil {
		t.Fatalf("failed to perform replay webhook request: %v", err)
	}
	defer replayRes.Body.Close()

	if replayRes.StatusCode != http.StatusOK {
		t.Fatalf("expected replay webhook status 200 OK, got %d", replayRes.StatusCode)
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

	var balance int64
	err = pool.QueryRow(ctx, "SELECT balance FROM users WHERE id = $1", userID).Scan(&balance)
	if err != nil {
		t.Fatalf("failed to query balance: %v", err)
	}
	if balance != 10000 {
		t.Fatalf("expected balance to remain unchanged, got %d", balance)
	}

	var paymentCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM payments WHERE rental_id = $1", rentalID).Scan(&paymentCount)
	if err != nil {
		t.Fatalf("failed to count payments for rental: %v", err)
	}
	if paymentCount != 1 {
		t.Fatalf("expected webhook replay to keep a single payment record, got %d", paymentCount)
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

	credsReq, err := http.NewRequest("GET", ts.URL+"/api/v1/me/rentals/"+strconv.FormatInt(rentalID, 10)+"/credentials", nil)
	if err != nil {
		t.Fatalf("failed to create credentials request: %v", err)
	}
	credsReq.Header.Set("Authorization", "Bearer "+loginResp.Data.AccessToken)

	credsRes, err := client.Do(credsReq)
	if err != nil {
		t.Fatalf("failed to perform credentials request: %v", err)
	}
	defer credsRes.Body.Close()

	if credsRes.StatusCode != http.StatusOK {
		t.Fatalf("expected credentials status 200 OK, got %d", credsRes.StatusCode)
	}

	var credsResp struct {
		Success bool `json:"success"`
		Data    struct {
			Login    string `json:"login"`
			Password string `json:"password"`
		} `json:"data"`
	}
	credsBody, err := io.ReadAll(credsRes.Body)
	if err != nil {
		t.Fatalf("failed to read credentials response: %v", err)
	}
	if bytes.Contains(credsBody, []byte("steam_id64")) || bytes.Contains(credsBody, []byte("token")) || bytes.Contains(credsBody, []byte("key")) {
		t.Fatalf("credentials response leaked unexpected fields: %s", string(credsBody))
	}
	if err := json.Unmarshal(credsBody, &credsResp); err != nil {
		t.Fatalf("failed to decode credentials response: %v", err)
	}
	if !credsResp.Success || credsResp.Data.Login != "steam_buyer_e2e" || credsResp.Data.Password != "steam_secure_password_99" {
		t.Fatalf("unexpected credentials payload: %+v", credsResp)
	}

	unauthReq, err := http.NewRequest("GET", ts.URL+"/api/v1/me/rentals/"+strconv.FormatInt(rentalID, 10)+"/credentials", nil)
	if err != nil {
		t.Fatalf("failed to create unauthenticated request: %v", err)
	}
	unauthRes, err := client.Do(unauthReq)
	if err != nil {
		t.Fatalf("failed to perform unauthenticated request: %v", err)
	}
	defer unauthRes.Body.Close()
	if unauthRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated credentials request to be 401, got %d", unauthRes.StatusCode)
	}

	otherRegReq := auth.RegisterRequest{
		Email:     "other@example.com",
		Password:  "other-secure-pass-123",
		FirstName: "Other",
		LastName:  "User",
	}
	otherRegReqBytes, _ := json.Marshal(otherRegReq)
	otherRes, err := http.Post(ts.URL+"/api/v1/auth/register", "application/json", bytes.NewBuffer(otherRegReqBytes))
	if err != nil {
		t.Fatalf("failed to register other user: %v", err)
	}
	defer otherRes.Body.Close()
	var otherRegResp struct {
		Success bool                  `json:"success"`
		Data    auth.RegisterResponse `json:"data"`
	}
	if err := json.NewDecoder(otherRes.Body).Decode(&otherRegResp); err != nil {
		t.Fatalf("failed to decode other register response: %v", err)
	}
	otherLoginReq := auth.LoginRequest{Email: "other@example.com", Password: "other-secure-pass-123"}
	otherLoginReqBytes, _ := json.Marshal(otherLoginReq)
	otherLoginRes, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(otherLoginReqBytes))
	if err != nil {
		t.Fatalf("failed to login other user: %v", err)
	}
	defer otherLoginRes.Body.Close()
	var otherLoginResp struct {
		Success bool               `json:"success"`
		Data    auth.LoginResponse `json:"data"`
	}
	if err := json.NewDecoder(otherLoginRes.Body).Decode(&otherLoginResp); err != nil {
		t.Fatalf("failed to decode other login response: %v", err)
	}
	otherReq, err := http.NewRequest("GET", ts.URL+"/api/v1/me/rentals/"+strconv.FormatInt(rentalID, 10)+"/credentials", nil)
	if err != nil {
		t.Fatalf("failed to create other-user credentials request: %v", err)
	}
	otherReq.Header.Set("Authorization", "Bearer "+otherLoginResp.Data.AccessToken)
	otherCredsRes, err := client.Do(otherReq)
	if err != nil {
		t.Fatalf("failed to perform other-user credentials request: %v", err)
	}
	defer otherCredsRes.Body.Close()
	if otherCredsRes.StatusCode != http.StatusNotFound {
		t.Fatalf("expected other user credentials request to be 404, got %d", otherCredsRes.StatusCode)
	}

	adminRegReq := auth.RegisterRequest{
		Email:     "admin@example.com",
		Password:  "admin-secure-pass-123",
		FirstName: "Admin",
		LastName:  "User",
	}
	adminRegReqBytes, _ := json.Marshal(adminRegReq)
	adminRes, err := http.Post(ts.URL+"/api/v1/auth/register", "application/json", bytes.NewBuffer(adminRegReqBytes))
	if err != nil {
		t.Fatalf("failed to register admin user: %v", err)
	}
	defer adminRes.Body.Close()
	var adminRegResp struct {
		Success bool                  `json:"success"`
		Data    auth.RegisterResponse `json:"data"`
	}
	if err := json.NewDecoder(adminRes.Body).Decode(&adminRegResp); err != nil {
		t.Fatalf("failed to decode admin register response: %v", err)
	}
	_, err = pool.Exec(ctx, `UPDATE users SET role = 'ADMIN' WHERE id = $1`, adminRegResp.Data.User.ID)
	if err != nil {
		t.Fatalf("failed to promote admin user: %v", err)
	}
	adminLoginReq := auth.LoginRequest{Email: "admin@example.com", Password: "admin-secure-pass-123"}
	adminLoginReqBytes, _ := json.Marshal(adminLoginReq)
	adminLoginRes, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(adminLoginReqBytes))
	if err != nil {
		t.Fatalf("failed to login admin user: %v", err)
	}
	defer adminLoginRes.Body.Close()
	var adminLoginResp struct {
		Success bool               `json:"success"`
		Data    auth.LoginResponse `json:"data"`
	}
	if err := json.NewDecoder(adminLoginRes.Body).Decode(&adminLoginResp); err != nil {
		t.Fatalf("failed to decode admin login response: %v", err)
	}
	adminReq, err := http.NewRequest("GET", ts.URL+"/api/v1/me/rentals/"+strconv.FormatInt(rentalID, 10)+"/credentials", nil)
	if err != nil {
		t.Fatalf("failed to create admin credentials request: %v", err)
	}
	adminReq.Header.Set("Authorization", "Bearer "+adminLoginResp.Data.AccessToken)
	adminCredsRes, err := client.Do(adminReq)
	if err != nil {
		t.Fatalf("failed to perform admin credentials request: %v", err)
	}
	defer adminCredsRes.Body.Close()
	if adminCredsRes.StatusCode != http.StatusNotFound {
		t.Fatalf("expected admin credentials request for another rental to be 404, got %d", adminCredsRes.StatusCode)
	}

	rentalDetailsReq, err := http.NewRequest("GET", ts.URL+"/api/v1/rentals/"+strconv.FormatInt(rentalID, 10), nil)
	if err != nil {
		t.Fatalf("failed to create rental details request: %v", err)
	}
	rentalDetailsReq.Header.Set("Authorization", "Bearer "+loginResp.Data.AccessToken)
	rentalDetailsRes, err := client.Do(rentalDetailsReq)
	if err != nil {
		t.Fatalf("failed to perform rental details request: %v", err)
	}
	defer rentalDetailsRes.Body.Close()
	rentalDetailsBody, err := io.ReadAll(rentalDetailsRes.Body)
	if err != nil {
		t.Fatalf("failed to read rental details response: %v", err)
	}
	if bytes.Contains(rentalDetailsBody, []byte("password")) || bytes.Contains(rentalDetailsBody, []byte("steam_id64")) {
		t.Fatalf("rental details response leaked credential fields: %s", string(rentalDetailsBody))
	}

	rows, err := pool.Query(ctx, `
		SELECT event_type, COALESCE(metadata::text, '')
		FROM security_events
		WHERE rental_id = $1
		ORDER BY event_type`, rentalID)
	if err != nil {
		t.Fatalf("failed to query security events: %v", err)
	}
	defer rows.Close()

	var countEvents int
	var eventTypes []int16
	for rows.Next() {
		var eventType int16
		var metadata string
		if err := rows.Scan(&eventType, &metadata); err != nil {
			t.Fatalf("failed to scan security event: %v", err)
		}
		countEvents++
		eventTypes = append(eventTypes, eventType)
		if eventType == 7 && (bytes.Contains([]byte(metadata), []byte("password")) || bytes.Contains([]byte(metadata), []byte("steam_id64"))) {
			t.Errorf("expected credential issuance metadata to avoid secrets, got %s", metadata)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to iterate security events: %v", err)
	}
	if countEvents != 2 {
		t.Errorf("expected exactly 2 security events logged, got %d", countEvents)
	}
	if len(eventTypes) != 2 || eventTypes[0] != 2 || eventTypes[1] != 7 {
		t.Errorf("expected event types [2 7], got %v", eventTypes)
	}
}

func TestE2E_ReadOnlyFinancialEndpoints(t *testing.T) {
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
	rentalService := rental.NewService(rentalRepo, accountRepo, userRepo, paymentRepo, txManager)
	apiHandler := api.NewHandler(pool, rentalService, paymentService, accountRepo, nil, nil)

	router := pkg_http_server.NewAPIVersionRouter(pkg_http_server.ApiVersion1)
	rateLimiter := shared_middleware.NewRateLimiter(100.0, 200.0)
	router.RegisterRoutes(authHandler.Routes(jwtSecret, rateLimiter, sLogger)...)
	router.RegisterRoutes(paymentHandler.Routes()...)
	router.RegisterRoutes(apiHandler.Routes(jwtSecret, sLogger)...)

	mux := http.NewServeMux()
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", router))

	ts := httptest.NewServer(mux)
	defer ts.Close()

	userID, accessToken := registerAndLoginE2EUser(t, ts, "finance-user@example.com", "super-secure-pass-123", "Finance", "User")
	otherUserID, otherAccessToken := registerAndLoginE2EUser(t, ts, "finance-other@example.com", "super-secure-pass-456", "Other", "User")

	if _, err := pool.Exec(ctx, `UPDATE users SET balance = $1 WHERE id = $2`, 22222, userID); err != nil {
		t.Fatalf("failed to set user balance: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET balance = $1 WHERE id = $2`, 99999, otherUserID); err != nil {
		t.Fatalf("failed to set other user balance: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	insertAccount := func(accountID int64, status int16, depositAmount int64) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			INSERT INTO accounts (id, login, encrypted_password, status, steam_guard_enabled, inventory_verified, hourly_price, deposit_amount, steam_id64, created_at, updated_at)
			VALUES ($1, $2, $3, $4, true, true, 250, $5, $6, $7, $7)`,
			accountID, fmt.Sprintf("financial_login_%d", accountID), []byte("enc-pass"), status, depositAmount, fmt.Sprintf("76561198000%d", accountID), now); err != nil {
			t.Fatalf("failed to insert account %d: %v", accountID, err)
		}
	}

	insertAccount(7101, 4, 500)
	insertAccount(7102, 2, 500)
	insertAccount(7103, 2, 500)
	insertAccount(7104, 3, 0)
	insertAccount(7105, 4, 500)
	insertAccount(7106, 2, 500)

	if _, err := pool.Exec(ctx, `
		INSERT INTO rentals (id, user_id, account_id, status, start_at, end_at, rental_price, deposit_amount, payment_expires_at, created_at, updated_at)
		VALUES
			(7201, $1, 7101, 2, $3, $4, 500, 500, $5, $3, $3),
			(7202, $1, 7102, 3, $3, $4, 500, 500, $5, $3, $3),
			(7203, $1, 7103, 4, $3, $4, 500, 500, $5, $3, $3),
			(7204, $1, 7104, 1, $3, $4, 500, 0, $5, $3, $3),
			(7205, $2, 7105, 2, $3, $4, 500, 500, $5, $3, $3),
			(7206, $1, 7106, 4, $3, $4, 500, 500, $5, $3, $3)`,
		userID, otherUserID, now.Add(-2*time.Hour), now.Add(2*time.Hour), now.Add(30*time.Minute)); err != nil {
		t.Fatalf("failed to insert rentals: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO payments (id, rental_id, user_id, payment_type, provider, status, amount, currency, external_transaction_id, created_at, processed_at)
		VALUES
			(7301, 7201, $1, 1, 'internal', 2, 1000, 'USD', 'tx-7201', $3, $3),
			(7302, 7202, $1, 1, 'internal', 2, 1000, 'USD', 'tx-7202', $3, $3),
			(7303, 7203, $1, 1, 'internal', 2, 1000, 'USD', 'tx-7203', $3, $3),
			(7304, 7204, $1, 1, 'internal', 1, 500, 'USD', NULL, $3, NULL),
			(7305, 7205, $2, 1, 'internal', 2, 1000, 'USD', 'tx-7205', $3, $3),
			(7306, 7206, $1, 1, 'balance', 2, 1000, 'USD', NULL, $3, $3)`,
		userID, otherUserID, now.Add(-90*time.Minute)); err != nil {
		t.Fatalf("failed to insert payments: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO deposit_holds (id, rental_id, user_id, payment_id, amount, currency, status, held_at, released_at, forfeited_at, idempotency_key, created_at, updated_at)
		VALUES
			(7401, 7201, $1, 7301, 500, 'USD', 1, $2, NULL, NULL, 'deposit:hold:rental:7201', $2, $2),
			(7402, 7202, $1, 7302, 500, 'USD', 2, $2, $3, NULL, 'deposit:hold:rental:7202', $2, $3),
			(7403, 7203, $1, 7303, 500, 'USD', 3, $2, NULL, $3, 'deposit:hold:rental:7203', $2, $3),
			(7405, 7205, $4, 7305, 500, 'USD', 1, $2, NULL, NULL, 'deposit:hold:rental:7205', $2, $2),
			(7406, 7206, $1, 7306, 500, 'USD', 4, $2, NULL, NULL, 'deposit:hold:rental:7206', $2, $3)`,
		userID, now.Add(-80*time.Minute), now.Add(-30*time.Minute), otherUserID); err != nil {
		t.Fatalf("failed to insert deposit holds: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO refunds (
			id, payment_id, rental_id, user_id, account_id, source_type, refund_kind, status, reason_code,
			requested_by_user_id, requested_by_role, amount_principal, amount_deposit, amount_total, currency,
			idempotency_key, correlation_id, metadata, processed_at, created_at, updated_at
		)
		VALUES
			(7501, 7302, 7202, $1, 7102, 1, 1, 2, 'SERVICE_UNAVAILABLE', NULL, 'SYSTEM', 500, 0, 500, 'USD', 'refund:wallet:full:rental:7202', 'refund:wallet:full:rental:7202', '{}'::jsonb, $2, $3, $3),
			(7502, 7306, 7206, $1, 7106, 1, 1, 2, 'SERVICE_UNAVAILABLE', NULL, 'SYSTEM', 500, 500, 1000, 'USD', 'refund:wallet:full:rental:7206', 'refund:wallet:full:rental:7206', '{}'::jsonb, $3, $4, $4),
			(7503, 7305, 7205, $5, 7105, 1, 1, 2, 'SERVICE_UNAVAILABLE', NULL, 'SYSTEM', 500, 500, 1000, 'USD', 'refund:wallet:full:rental:7205', 'refund:wallet:full:rental:7205', '{}'::jsonb, $4, $4, $4)`,
		userID, now.Add(-25*time.Minute), now.Add(-26*time.Minute), now.Add(-5*time.Minute), otherUserID); err != nil {
		t.Fatalf("failed to insert refunds: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO financial_ledger_entries (entry_type, user_id, rental_id, payment_id, account_id, amount, currency, provider, external_transaction_id, idempotency_key, correlation_id, metadata, created_at)
		VALUES
			(1, $1, 7201, 7301, 7101, 1000, 'USD', 'internal', 'tx-7201', 'payment:webhook:internal:tx-7201', 'corr-7201', '{"event":"provider_payment_received"}', $2),
			(3, $1, 7202, 7302, 7102, 500, 'USD', 'internal', 'tx-7202', 'deposit:release:rental:7202', 'corr-7202', '{"event":"deposit_released_to_balance"}', $3),
			(4, $1, 7203, 7303, 7103, 500, 'USD', 'internal', 'tx-7203', 'deposit:forfeit:rental:7203', 'corr-7203', '{"event":"deposit_forfeited"}', $4),
			(2, $5, 7205, 7305, 7105, 500, 'USD', 'internal', 'tx-7205', 'deposit:hold:rental:7205', 'corr-7205', '{"event":"deposit_held"}', $6)`,
		userID, now.Add(-10*time.Minute), now.Add(-20*time.Minute), now.Add(-30*time.Minute), otherUserID, now.Add(-40*time.Minute)); err != nil {
		t.Fatalf("failed to insert financial ledger entries: %v", err)
	}

	balanceReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/me/balance", nil)
	balanceReq.Header.Set("Authorization", "Bearer "+accessToken)
	balanceRes, err := http.DefaultClient.Do(balanceReq)
	if err != nil {
		t.Fatalf("failed to request balance: %v", err)
	}
	defer balanceRes.Body.Close()
	if balanceRes.StatusCode != http.StatusOK {
		t.Fatalf("expected balance status 200, got %d", balanceRes.StatusCode)
	}
	var balanceResp struct {
		Success bool `json:"success"`
		Data    struct {
			AvailableBalance int64  `json:"available_balance"`
			Currency         string `json:"currency"`
		} `json:"data"`
	}
	if err := json.NewDecoder(balanceRes.Body).Decode(&balanceResp); err != nil {
		t.Fatalf("failed to decode balance response: %v", err)
	}
	if !balanceResp.Success || balanceResp.Data.AvailableBalance != 22222 || balanceResp.Data.Currency != "USD" {
		t.Fatalf("unexpected balance response: %+v", balanceResp)
	}

	ledgerReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/me/ledger?page=1&page_size=2", nil)
	ledgerReq.Header.Set("Authorization", "Bearer "+accessToken)
	ledgerRes, err := http.DefaultClient.Do(ledgerReq)
	if err != nil {
		t.Fatalf("failed to request ledger: %v", err)
	}
	defer ledgerRes.Body.Close()
	if ledgerRes.StatusCode != http.StatusOK {
		t.Fatalf("expected ledger status 200, got %d", ledgerRes.StatusCode)
	}
	ledgerBody, _ := io.ReadAll(ledgerRes.Body)
	var ledgerResp struct {
		Success bool `json:"success"`
		Data    struct {
			Entries    []map[string]any `json:"entries"`
			Pagination struct {
				Page       int `json:"page"`
				PageSize   int `json:"page_size"`
				TotalItems int `json:"total_items"`
				TotalPages int `json:"total_pages"`
			} `json:"pagination"`
		} `json:"data"`
	}
	if err := json.Unmarshal(ledgerBody, &ledgerResp); err != nil {
		t.Fatalf("failed to decode ledger response: %v", err)
	}
	if !ledgerResp.Success || len(ledgerResp.Data.Entries) != 2 {
		t.Fatalf("unexpected ledger response: %s", string(ledgerBody))
	}
	if ledgerResp.Data.Pagination.Page != 1 || ledgerResp.Data.Pagination.PageSize != 2 || ledgerResp.Data.Pagination.TotalItems != 3 || ledgerResp.Data.Pagination.TotalPages != 2 {
		t.Fatalf("unexpected ledger pagination: %+v", ledgerResp.Data.Pagination)
	}
	firstAmount, _ := ledgerResp.Data.Entries[0]["amount"].(float64)
	secondAmount, _ := ledgerResp.Data.Entries[1]["amount"].(float64)
	if int64(firstAmount) != 1000 || int64(secondAmount) != 500 {
		t.Fatalf("expected newest-first user ledger entries, got %s", string(ledgerBody))
	}
	for _, entry := range ledgerResp.Data.Entries {
		for _, forbidden := range []string{"idempotency_key", "correlation_id", "external_transaction_id", "metadata", "credentials", "secret"} {
			if _, exists := entry[forbidden]; exists {
				t.Fatalf("ledger DTO leaked forbidden field %q: %s", forbidden, string(ledgerBody))
			}
		}
	}

	ledgerSecondPageReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/me/ledger?page=2&page_size=2", nil)
	ledgerSecondPageReq.Header.Set("Authorization", "Bearer "+accessToken)
	ledgerSecondPageRes, err := http.DefaultClient.Do(ledgerSecondPageReq)
	if err != nil {
		t.Fatalf("failed to request ledger second page: %v", err)
	}
	defer ledgerSecondPageRes.Body.Close()
	var ledgerSecondPage struct {
		Success bool `json:"success"`
		Data    struct {
			Entries []map[string]any `json:"entries"`
		} `json:"data"`
	}
	if err := json.NewDecoder(ledgerSecondPageRes.Body).Decode(&ledgerSecondPage); err != nil {
		t.Fatalf("failed to decode second ledger page: %v", err)
	}
	if !ledgerSecondPage.Success || len(ledgerSecondPage.Data.Entries) != 1 {
		t.Fatalf("expected exactly one entry on second page, got %+v", ledgerSecondPage)
	}

	ledgerInvalidReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/me/ledger?page=-1&page_size=0", nil)
	ledgerInvalidReq.Header.Set("Authorization", "Bearer "+accessToken)
	ledgerInvalidRes, err := http.DefaultClient.Do(ledgerInvalidReq)
	if err != nil {
		t.Fatalf("failed to request ledger with invalid pagination: %v", err)
	}
	defer ledgerInvalidRes.Body.Close()
	if ledgerInvalidRes.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected invalid ledger pagination to return 422, got %d", ledgerInvalidRes.StatusCode)
	}

	rentalsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/me/rentals", nil)
	rentalsReq.Header.Set("Authorization", "Bearer "+accessToken)
	rentalsRes, err := http.DefaultClient.Do(rentalsReq)
	if err != nil {
		t.Fatalf("failed to request rentals: %v", err)
	}
	defer rentalsRes.Body.Close()
	if rentalsRes.StatusCode != http.StatusOK {
		t.Fatalf("expected rentals status 200, got %d", rentalsRes.StatusCode)
	}
	var rentalsResp struct {
		Success bool `json:"success"`
		Data    struct {
			Rentals []struct {
				ID                int64  `json:"id"`
				DepositStatus     string `json:"deposit_status"`
				HasRefund         bool   `json:"has_refund"`
				RefundStatus      string `json:"refund_status"`
				RefundTotalAmount struct {
					Amount int64 `json:"amount"`
				} `json:"refund_total_amount"`
			} `json:"rentals"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rentalsRes.Body).Decode(&rentalsResp); err != nil {
		t.Fatalf("failed to decode rentals response: %v", err)
	}
	if !rentalsResp.Success || len(rentalsResp.Data.Rentals) != 5 {
		t.Fatalf("unexpected rentals response: %+v", rentalsResp)
	}
	depositStatuses := map[int64]string{}
	refundStatuses := map[int64]string{}
	refundFlags := map[int64]bool{}
	refundTotals := map[int64]int64{}
	for _, item := range rentalsResp.Data.Rentals {
		depositStatuses[item.ID] = item.DepositStatus
		refundStatuses[item.ID] = item.RefundStatus
		refundFlags[item.ID] = item.HasRefund
		refundTotals[item.ID] = item.RefundTotalAmount.Amount
	}
	if depositStatuses[7201] != "HELD" || depositStatuses[7202] != "RELEASED" || depositStatuses[7203] != "FORFEITED" || depositStatuses[7204] != "NONE" || depositStatuses[7206] != "REFUNDED" {
		t.Fatalf("unexpected deposit statuses: %+v", depositStatuses)
	}
	if refundFlags[7201] || refundStatuses[7201] != "NONE" || refundTotals[7201] != 0 {
		t.Fatalf("expected no refund summary for rental 7201, got flags=%+v statuses=%+v totals=%+v", refundFlags, refundStatuses, refundTotals)
	}
	if !refundFlags[7202] || refundStatuses[7202] != "COMPLETED" || refundTotals[7202] != 500 {
		t.Fatalf("expected principal-only refund summary for rental 7202, got flags=%+v statuses=%+v totals=%+v", refundFlags, refundStatuses, refundTotals)
	}
	if !refundFlags[7206] || refundStatuses[7206] != "COMPLETED" || refundTotals[7206] != 1000 {
		t.Fatalf("expected principal+deposit refund summary for rental 7206, got flags=%+v statuses=%+v totals=%+v", refundFlags, refundStatuses, refundTotals)
	}

	rentalDetailReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/rentals/7203", nil)
	rentalDetailReq.Header.Set("Authorization", "Bearer "+accessToken)
	rentalDetailRes, err := http.DefaultClient.Do(rentalDetailReq)
	if err != nil {
		t.Fatalf("failed to request rental detail: %v", err)
	}
	defer rentalDetailRes.Body.Close()
	var rentalDetailResp struct {
		Success bool `json:"success"`
		Data    struct {
			DepositStatus string `json:"deposit_status"`
			HasRefund     bool   `json:"has_refund"`
			RefundStatus  string `json:"refund_status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rentalDetailRes.Body).Decode(&rentalDetailResp); err != nil {
		t.Fatalf("failed to decode rental detail response: %v", err)
	}
	if !rentalDetailResp.Success || rentalDetailResp.Data.DepositStatus != "FORFEITED" || rentalDetailResp.Data.HasRefund || rentalDetailResp.Data.RefundStatus != "NONE" {
		t.Fatalf("unexpected rental detail response: %+v", rentalDetailResp)
	}

	rentalRefundDetailReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/rentals/7206", nil)
	rentalRefundDetailReq.Header.Set("Authorization", "Bearer "+accessToken)
	rentalRefundDetailRes, err := http.DefaultClient.Do(rentalRefundDetailReq)
	if err != nil {
		t.Fatalf("failed to request refunded rental detail: %v", err)
	}
	defer rentalRefundDetailRes.Body.Close()
	var rentalRefundDetailResp struct {
		Success bool `json:"success"`
		Data    struct {
			DepositStatus     string `json:"deposit_status"`
			HasRefund         bool   `json:"has_refund"`
			RefundStatus      string `json:"refund_status"`
			ProcessedAt       string `json:"processed_at"`
			RefundTotalAmount struct {
				Amount int64 `json:"amount"`
			} `json:"refund_total_amount"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rentalRefundDetailRes.Body).Decode(&rentalRefundDetailResp); err != nil {
		t.Fatalf("failed to decode refunded rental detail response: %v", err)
	}
	if !rentalRefundDetailResp.Success || rentalRefundDetailResp.Data.DepositStatus != "REFUNDED" || !rentalRefundDetailResp.Data.HasRefund || rentalRefundDetailResp.Data.RefundStatus != "COMPLETED" || rentalRefundDetailResp.Data.RefundTotalAmount.Amount != 1000 || rentalRefundDetailResp.Data.ProcessedAt == "" {
		t.Fatalf("unexpected refunded rental detail response: %+v", rentalRefundDetailResp)
	}

	otherLedgerReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/me/ledger?page=1&page_size=10", nil)
	otherLedgerReq.Header.Set("Authorization", "Bearer "+otherAccessToken)
	otherLedgerRes, err := http.DefaultClient.Do(otherLedgerReq)
	if err != nil {
		t.Fatalf("failed to request other user ledger: %v", err)
	}
	defer otherLedgerRes.Body.Close()
	var otherLedgerResp struct {
		Success bool `json:"success"`
		Data    struct {
			Entries []map[string]any `json:"entries"`
		} `json:"data"`
	}
	if err := json.NewDecoder(otherLedgerRes.Body).Decode(&otherLedgerResp); err != nil {
		t.Fatalf("failed to decode other user ledger response: %v", err)
	}
	if !otherLedgerResp.Success || len(otherLedgerResp.Data.Entries) != 1 {
		t.Fatalf("expected other user to see only own ledger entry, got %+v", otherLedgerResp)
	}
	if rentalID, ok := otherLedgerResp.Data.Entries[0]["rental_id"].(float64); !ok || int64(rentalID) != 7205 {
		t.Fatalf("other user saw non-owned ledger entries: %+v", otherLedgerResp.Data.Entries)
	}

	refundsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/me/refunds?page=1&page_size=1", nil)
	refundsReq.Header.Set("Authorization", "Bearer "+accessToken)
	refundsRes, err := http.DefaultClient.Do(refundsReq)
	if err != nil {
		t.Fatalf("failed to request refunds: %v", err)
	}
	defer refundsRes.Body.Close()
	if refundsRes.StatusCode != http.StatusOK {
		t.Fatalf("expected refunds status 200, got %d", refundsRes.StatusCode)
	}
	refundsBody, _ := io.ReadAll(refundsRes.Body)
	var refundsResp struct {
		Success bool `json:"success"`
		Data    struct {
			Refunds    []map[string]any `json:"refunds"`
			Pagination struct {
				Page       int `json:"page"`
				PageSize   int `json:"page_size"`
				TotalItems int `json:"total_items"`
				TotalPages int `json:"total_pages"`
			} `json:"pagination"`
		} `json:"data"`
	}
	if err := json.Unmarshal(refundsBody, &refundsResp); err != nil {
		t.Fatalf("failed to decode refunds response: %v", err)
	}
	if !refundsResp.Success || len(refundsResp.Data.Refunds) != 1 {
		t.Fatalf("unexpected refunds response: %s", string(refundsBody))
	}
	if refundsResp.Data.Pagination.Page != 1 || refundsResp.Data.Pagination.PageSize != 1 || refundsResp.Data.Pagination.TotalItems != 2 || refundsResp.Data.Pagination.TotalPages != 2 {
		t.Fatalf("unexpected refunds pagination: %+v", refundsResp.Data.Pagination)
	}
	if rentalID, ok := refundsResp.Data.Refunds[0]["rental_id"].(float64); !ok || int64(rentalID) != 7206 {
		t.Fatalf("expected newest refund first for rental 7206, got %s", string(refundsBody))
	}
	for _, forbidden := range []string{"idempotency_key", "metadata", "correlation_id", "requested_by_user_id", "requested_by_role", "credentials", "token", "provider"} {
		if _, exists := refundsResp.Data.Refunds[0][forbidden]; exists {
			t.Fatalf("refund DTO leaked forbidden field %q: %s", forbidden, string(refundsBody))
		}
	}
	if _, ok := refundsResp.Data.Refunds[0]["reason_code"].(string); !ok {
		t.Fatalf("expected safe reason_code in refund DTO, got %s", string(refundsBody))
	}

	refundsSecondPageReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/me/refunds?page=2&page_size=1", nil)
	refundsSecondPageReq.Header.Set("Authorization", "Bearer "+accessToken)
	refundsSecondPageRes, err := http.DefaultClient.Do(refundsSecondPageReq)
	if err != nil {
		t.Fatalf("failed to request refunds second page: %v", err)
	}
	defer refundsSecondPageRes.Body.Close()
	var refundsSecondPage struct {
		Success bool `json:"success"`
		Data    struct {
			Refunds []map[string]any `json:"refunds"`
		} `json:"data"`
	}
	if err := json.NewDecoder(refundsSecondPageRes.Body).Decode(&refundsSecondPage); err != nil {
		t.Fatalf("failed to decode refunds second page: %v", err)
	}
	if !refundsSecondPage.Success || len(refundsSecondPage.Data.Refunds) != 1 {
		t.Fatalf("expected exactly one refund on second page, got %+v", refundsSecondPage)
	}
	if rentalID, ok := refundsSecondPage.Data.Refunds[0]["rental_id"].(float64); !ok || int64(rentalID) != 7202 {
		t.Fatalf("expected second refund page to contain rental 7202, got %+v", refundsSecondPage.Data.Refunds)
	}

	refundsInvalidReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/me/refunds?page=0&page_size=101", nil)
	refundsInvalidReq.Header.Set("Authorization", "Bearer "+accessToken)
	refundsInvalidRes, err := http.DefaultClient.Do(refundsInvalidReq)
	if err != nil {
		t.Fatalf("failed to request refunds with invalid pagination: %v", err)
	}
	defer refundsInvalidRes.Body.Close()
	if refundsInvalidRes.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected invalid refunds pagination to return 422, got %d", refundsInvalidRes.StatusCode)
	}

	otherRefundsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/me/refunds?page=1&page_size=10", nil)
	otherRefundsReq.Header.Set("Authorization", "Bearer "+otherAccessToken)
	otherRefundsRes, err := http.DefaultClient.Do(otherRefundsReq)
	if err != nil {
		t.Fatalf("failed to request other user refunds: %v", err)
	}
	defer otherRefundsRes.Body.Close()
	var otherRefundsResp struct {
		Success bool `json:"success"`
		Data    struct {
			Refunds []map[string]any `json:"refunds"`
		} `json:"data"`
	}
	if err := json.NewDecoder(otherRefundsRes.Body).Decode(&otherRefundsResp); err != nil {
		t.Fatalf("failed to decode other user refunds response: %v", err)
	}
	if !otherRefundsResp.Success || len(otherRefundsResp.Data.Refunds) != 1 {
		t.Fatalf("expected other user to see only own refund entry, got %+v", otherRefundsResp)
	}
	if rentalID, ok := otherRefundsResp.Data.Refunds[0]["rental_id"].(float64); !ok || int64(rentalID) != 7205 {
		t.Fatalf("other user saw non-owned refund entries: %+v", otherRefundsResp.Data.Refunds)
	}

	unauthorizedBalanceRes, err := http.Get(ts.URL + "/api/v1/me/balance")
	if err != nil {
		t.Fatalf("failed to request unauthorized balance: %v", err)
	}
	defer unauthorizedBalanceRes.Body.Close()
	if unauthorizedBalanceRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized balance status 401, got %d", unauthorizedBalanceRes.StatusCode)
	}

	unauthorizedRefundsRes, err := http.Get(ts.URL + "/api/v1/me/refunds")
	if err != nil {
		t.Fatalf("failed to request unauthorized refunds: %v", err)
	}
	defer unauthorizedRefundsRes.Body.Close()
	if unauthorizedRefundsRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized refunds status 401, got %d", unauthorizedRefundsRes.StatusCode)
	}
}

func TestE2E_PayRentalWithBalanceEndpoint(t *testing.T) {
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
	rentalService := rental.NewService(rentalRepo, accountRepo, userRepo, paymentRepo, txManager)
	apiHandler := api.NewHandler(pool, rentalService, paymentService, accountRepo, nil, nil)

	router := pkg_http_server.NewAPIVersionRouter(pkg_http_server.ApiVersion1)
	rateLimiter := shared_middleware.NewRateLimiter(100.0, 200.0)
	router.RegisterRoutes(authHandler.Routes(jwtSecret, rateLimiter, sLogger)...)
	router.RegisterRoutes(paymentHandler.Routes()...)
	router.RegisterRoutes(apiHandler.Routes(jwtSecret, sLogger)...)

	mux := http.NewServeMux()
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", router))

	ts := httptest.NewServer(mux)
	defer ts.Close()

	userID, accessToken := registerAndLoginE2EUser(t, ts, "wallet-user@example.com", "super-secure-pass-123", "Wallet", "User")
	otherUserID, otherAccessToken := registerAndLoginE2EUser(t, ts, "wallet-other@example.com", "super-secure-pass-456", "Other", "User")

	if _, err := pool.Exec(ctx, `UPDATE users SET balance = $1 WHERE id = $2`, 10000, userID); err != nil {
		t.Fatalf("failed to set owner balance: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET balance = $1 WHERE id = $2`, 10000, otherUserID); err != nil {
		t.Fatalf("failed to set other balance: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, login, encrypted_password, status, steam_guard_enabled, inventory_verified, hourly_price, deposit_amount, steam_id64, created_at, updated_at)
		VALUES
			(8101, 'wallet_login_1', $1, 3, true, true, 250, 500, '765611980008101', $2, $2),
			(8102, 'wallet_login_2', $1, 3, true, true, 250, 500, '765611980008102', $2, $2)`,
		[]byte("enc-pass"), now); err != nil {
		t.Fatalf("failed to insert accounts: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO rentals (id, user_id, account_id, status, start_at, end_at, rental_price, deposit_amount, payment_expires_at, created_at, updated_at)
		VALUES
			(8201, $1, 8101, 1, $3, $4, 500, 500, $5, $3, $3),
			(8202, $2, 8102, 1, $3, $4, 500, 500, $5, $3, $3)`,
		userID, otherUserID, now, now.Add(2*time.Hour), now.Add(30*time.Minute)); err != nil {
		t.Fatalf("failed to insert rentals: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO payments (id, rental_id, user_id, payment_type, provider, status, amount, currency, created_at)
		VALUES
			(8301, 8201, $1, 1, 'internal', 1, 1000, 'USD', $3),
			(8302, 8202, $2, 1, 'internal', 1, 1000, 'USD', $3)`,
		userID, otherUserID, now); err != nil {
		t.Fatalf("failed to insert payments: %v", err)
	}

	unauthorizedRes, err := http.Post(ts.URL+"/api/v1/me/rentals/8201/pay-with-balance", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to request unauthorized wallet payment: %v", err)
	}
	defer unauthorizedRes.Body.Close()
	if unauthorizedRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized wallet payment status 401, got %d", unauthorizedRes.StatusCode)
	}

	nonOwnerReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/me/rentals/8201/pay-with-balance", nil)
	nonOwnerReq.Header.Set("Authorization", "Bearer "+otherAccessToken)
	nonOwnerRes, err := http.DefaultClient.Do(nonOwnerReq)
	if err != nil {
		t.Fatalf("failed to request non-owner wallet payment: %v", err)
	}
	defer nonOwnerRes.Body.Close()
	if nonOwnerRes.StatusCode != http.StatusNotFound {
		t.Fatalf("expected non-owner wallet payment status 404, got %d", nonOwnerRes.StatusCode)
	}

	ownerReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/me/rentals/8201/pay-with-balance", nil)
	ownerReq.Header.Set("Authorization", "Bearer "+accessToken)
	ownerRes, err := http.DefaultClient.Do(ownerReq)
	if err != nil {
		t.Fatalf("failed to request owner wallet payment: %v", err)
	}
	defer ownerRes.Body.Close()
	if ownerRes.StatusCode != http.StatusOK {
		t.Fatalf("expected owner wallet payment status 200, got %d", ownerRes.StatusCode)
	}
	bodyBytes, _ := io.ReadAll(ownerRes.Body)
	var ownerResp struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &ownerResp); err != nil {
		t.Fatalf("failed to decode owner wallet payment response: %v", err)
	}
	if !ownerResp.Success {
		t.Fatalf("unexpected wallet payment response: %s", string(bodyBytes))
	}
	for _, forbidden := range []string{"login", "password", "credentials", "metadata", "idempotency_key"} {
		if _, exists := ownerResp.Data[forbidden]; exists {
			t.Fatalf("wallet payment response leaked forbidden field %q: %s", forbidden, string(bodyBytes))
		}
	}

	var balance int64
	var paymentStatus, rentalStatus, accountStatus int16
	var provider string
	if err := pool.QueryRow(ctx, "SELECT balance FROM users WHERE id = $1", userID).Scan(&balance); err != nil {
		t.Fatalf("failed to read owner balance after wallet payment: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT status, provider FROM payments WHERE id = 8301").Scan(&paymentStatus, &provider); err != nil {
		t.Fatalf("failed to read payment after wallet payment: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT status FROM rentals WHERE id = 8201").Scan(&rentalStatus); err != nil {
		t.Fatalf("failed to read rental after wallet payment: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT status FROM accounts WHERE id = 8101").Scan(&accountStatus); err != nil {
		t.Fatalf("failed to read account after wallet payment: %v", err)
	}
	if balance != 9000 || paymentStatus != 2 || rentalStatus != 2 || accountStatus != 4 || provider != "balance" {
		t.Fatalf("unexpected owner wallet payment state balance=%d payment=%d provider=%q rental=%d account=%d", balance, paymentStatus, provider, rentalStatus, accountStatus)
	}
}

func TestE2E_AdminWalletRefundEndpoint(t *testing.T) {
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
	rentalService := rental.NewService(rentalRepo, accountRepo, userRepo, paymentRepo, txManager)
	apiHandler := api.NewHandler(pool, rentalService, paymentService, accountRepo, nil, nil)

	router := pkg_http_server.NewAPIVersionRouter(pkg_http_server.ApiVersion1)
	rateLimiter := shared_middleware.NewRateLimiter(100.0, 200.0)
	router.RegisterRoutes(authHandler.Routes(jwtSecret, rateLimiter, sLogger)...)
	router.RegisterRoutes(paymentHandler.Routes()...)
	router.RegisterRoutes(apiHandler.Routes(jwtSecret, sLogger)...)

	mux := http.NewServeMux()
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", router))

	ts := httptest.NewServer(mux)
	defer ts.Close()

	renterID, renterToken := registerAndLoginE2EUser(t, ts, "refund-renter@example.com", "super-secure-pass-123", "Refund", "Renter")
	adminID, _ := registerAndLoginE2EUser(t, ts, "refund-admin@example.com", "super-secure-pass-456", "Refund", "Admin")
	if _, err := pool.Exec(ctx, `UPDATE users SET role = 'ADMIN' WHERE id = $1`, adminID); err != nil {
		t.Fatalf("failed to grant admin role: %v", err)
	}
	adminToken := loginE2EUser(t, ts, "refund-admin@example.com", "super-secure-pass-456")

	if _, err := pool.Exec(ctx, `UPDATE users SET balance = $1 WHERE id = $2`, 10000, renterID); err != nil {
		t.Fatalf("failed to set renter balance: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, login, encrypted_password, status, steam_guard_enabled, inventory_verified, hourly_price, deposit_amount, steam_id64, created_at, updated_at)
		VALUES (8401, 'refund_login_1', $1, 3, true, true, 250, 500, '765611980008401', $2, $2)`,
		[]byte("enc-pass"), now); err != nil {
		t.Fatalf("failed to insert refund account: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO rentals (id, user_id, account_id, status, start_at, end_at, rental_price, deposit_amount, payment_expires_at, created_at, updated_at)
		VALUES (8501, $1, 8401, 1, $2, $3, 500, 500, $4, $2, $2)`,
		renterID, now, now.Add(2*time.Hour), now.Add(30*time.Minute)); err != nil {
		t.Fatalf("failed to insert refund rental: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO payments (id, rental_id, user_id, payment_type, provider, status, amount, currency, created_at)
		VALUES (8601, 8501, $1, 1, 'internal', 1, 1000, 'USD', $2)`,
		renterID, now); err != nil {
		t.Fatalf("failed to insert refund payment: %v", err)
	}
	if _, err := paymentService.PayRentalWithBalance(context.Background(), renterID, 8501, "127.0.0.1", "test", time.Now().UTC()); err != nil {
		t.Fatalf("failed to wallet pay refund rental: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE rentals SET status = 3, actual_finished_at = NOW(), updated_at = NOW() WHERE id = 8501`); err != nil {
		t.Fatalf("failed to expire refund rental: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET status = 2, updated_at = NOW() WHERE id = 8401`); err != nil {
		t.Fatalf("failed to release refund account: %v", err)
	}

	adminListUnauthorizedReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/admin/rentals?page=1&page_size=10", nil)
	adminListUnauthorizedReq.Header.Set("Authorization", "Bearer "+renterToken)
	adminListUnauthorizedRes, err := http.DefaultClient.Do(adminListUnauthorizedReq)
	if err != nil {
		t.Fatalf("failed to call non-admin rentals admin endpoint: %v", err)
	}
	defer adminListUnauthorizedRes.Body.Close()
	if adminListUnauthorizedRes.StatusCode != http.StatusForbidden {
		t.Fatalf("expected non-admin admin rentals status 403, got %d", adminListUnauthorizedRes.StatusCode)
	}

	adminListReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/admin/rentals?page=1&page_size=10", nil)
	adminListReq.Header.Set("Authorization", "Bearer "+adminToken)
	adminListRes, err := http.DefaultClient.Do(adminListReq)
	if err != nil {
		t.Fatalf("failed to call admin rentals endpoint: %v", err)
	}
	defer adminListRes.Body.Close()
	if adminListRes.StatusCode != http.StatusOK {
		t.Fatalf("expected admin rentals status 200, got %d", adminListRes.StatusCode)
	}
	adminListBody, _ := io.ReadAll(adminListRes.Body)
	var adminListResp struct {
		Success bool `json:"success"`
		Data    struct {
			Rentals    []map[string]any `json:"rentals"`
			Summary    map[string]any   `json:"summary"`
			Pagination struct {
				Page       int `json:"page"`
				PageSize   int `json:"page_size"`
				TotalItems int `json:"total_items"`
				TotalPages int `json:"total_pages"`
			} `json:"pagination"`
		} `json:"data"`
	}
	if err := json.Unmarshal(adminListBody, &adminListResp); err != nil {
		t.Fatalf("failed to decode admin rentals response: %v", err)
	}
	if !adminListResp.Success || len(adminListResp.Data.Rentals) == 0 {
		t.Fatalf("unexpected admin rentals response: %s", string(adminListBody))
	}
	if adminListResp.Data.Pagination.Page != 1 || adminListResp.Data.Pagination.PageSize != 10 {
		t.Fatalf("unexpected admin rentals pagination: %+v", adminListResp.Data.Pagination)
	}
	for _, required := range []string{"total_count", "eligible_wallet_refund_count", "rental_status_counts", "payment_status_counts", "refund_status_counts"} {
		if _, exists := adminListResp.Data.Summary[required]; !exists {
			t.Fatalf("admin rentals summary missed %q: %s", required, string(adminListBody))
		}
	}
	row := adminListResp.Data.Rentals[0]
	for _, required := range []string{"id", "user_id", "account_id", "status", "payment_status", "payment_provider", "rental_price", "security_deposit", "deposit_status", "has_refund", "refund_status", "refund_total_amount"} {
		if _, exists := row[required]; !exists {
			t.Fatalf("admin rentals DTO missed %q: %s", required, string(adminListBody))
		}
	}
	for _, forbidden := range []string{"login", "password", "credentials", "metadata", "idempotency_key", "correlation_id", "provider_payload", "token", "secret"} {
		if _, exists := row[forbidden]; exists {
			t.Fatalf("admin rentals DTO leaked forbidden field %q: %s", forbidden, string(adminListBody))
		}
	}

	reasonCodesReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/admin/refund-reason-codes", nil)
	reasonCodesReq.Header.Set("Authorization", "Bearer "+adminToken)
	reasonCodesRes, err := http.DefaultClient.Do(reasonCodesReq)
	if err != nil {
		t.Fatalf("failed to call admin refund reason codes endpoint: %v", err)
	}
	defer reasonCodesRes.Body.Close()
	if reasonCodesRes.StatusCode != http.StatusOK {
		t.Fatalf("expected admin refund reason codes status 200, got %d", reasonCodesRes.StatusCode)
	}
	reasonCodesBody, _ := io.ReadAll(reasonCodesRes.Body)
	var reasonCodesResp struct {
		Success bool `json:"success"`
		Data    struct {
			ReasonCodes []map[string]any `json:"reason_codes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(reasonCodesBody, &reasonCodesResp); err != nil {
		t.Fatalf("failed to decode admin refund reason codes response: %v", err)
	}
	if !reasonCodesResp.Success || len(reasonCodesResp.Data.ReasonCodes) == 0 {
		t.Fatalf("unexpected admin refund reason codes response: %s", string(reasonCodesBody))
	}
	for _, required := range []string{"code", "label"} {
		if _, exists := reasonCodesResp.Data.ReasonCodes[0][required]; !exists {
			t.Fatalf("admin refund reason code DTO missed %q: %s", required, string(reasonCodesBody))
		}
	}
	for _, forbidden := range []string{"metadata", "token", "secret", "credentials"} {
		if _, exists := reasonCodesResp.Data.ReasonCodes[0][forbidden]; exists {
			t.Fatalf("admin refund reason codes leaked forbidden field %q: %s", forbidden, string(reasonCodesBody))
		}
	}

	unauthorizedReqBody := bytes.NewBufferString(`{"reason_code":"SERVICE_UNAVAILABLE"}`)
	unauthorizedReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/admin/rentals/8501/wallet-refund", unauthorizedReqBody)
	unauthorizedReq.Header.Set("Authorization", "Bearer "+renterToken)
	unauthorizedReq.Header.Set("Content-Type", "application/json")
	unauthorizedRes, err := http.DefaultClient.Do(unauthorizedReq)
	if err != nil {
		t.Fatalf("failed to call non-admin wallet refund endpoint: %v", err)
	}
	defer unauthorizedRes.Body.Close()
	if unauthorizedRes.StatusCode != http.StatusForbidden {
		t.Fatalf("expected non-admin wallet refund status 403, got %d", unauthorizedRes.StatusCode)
	}

	adminReqBody := bytes.NewBufferString(`{"reason_code":"SERVICE_UNAVAILABLE"}`)
	adminReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/admin/rentals/8501/wallet-refund", adminReqBody)
	adminReq.Header.Set("Authorization", "Bearer "+adminToken)
	adminReq.Header.Set("Content-Type", "application/json")
	adminRes, err := http.DefaultClient.Do(adminReq)
	if err != nil {
		t.Fatalf("failed to call admin wallet refund endpoint: %v", err)
	}
	defer adminRes.Body.Close()
	if adminRes.StatusCode != http.StatusOK {
		t.Fatalf("expected admin wallet refund status 200, got %d", adminRes.StatusCode)
	}
	bodyBytes, _ := io.ReadAll(adminRes.Body)
	var adminResp struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &adminResp); err != nil {
		t.Fatalf("failed to decode admin wallet refund response: %v", err)
	}
	if !adminResp.Success {
		t.Fatalf("unexpected admin wallet refund response: %s", string(bodyBytes))
	}
	for _, forbidden := range []string{"metadata", "idempotency_key", "correlation_id", "external_transaction_id", "credentials", "token", "secret"} {
		if _, exists := adminResp.Data[forbidden]; exists {
			t.Fatalf("wallet refund response leaked forbidden field %q: %s", forbidden, string(bodyBytes))
		}
	}

	var balance int64
	var holdStatus int16
	var refundCount int
	if err := pool.QueryRow(ctx, "SELECT balance FROM users WHERE id = $1", renterID).Scan(&balance); err != nil {
		t.Fatalf("failed to read balance after refund: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT status FROM deposit_holds WHERE rental_id = 8501").Scan(&holdStatus); err != nil {
		t.Fatalf("failed to read hold after refund: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM refunds WHERE rental_id = 8501").Scan(&refundCount); err != nil {
		t.Fatalf("failed to read refund count: %v", err)
	}
	if balance != 10000 || holdStatus != 4 || refundCount != 1 {
		t.Fatalf("unexpected admin refund final state balance=%d hold=%d refunds=%d", balance, holdStatus, refundCount)
	}
}

func TestE2E_AdminRentalsFiltersEndpoint(t *testing.T) {
	ctx := context.Background()

	pool, txManager := setupE2ETestDB(t)

	t.Setenv("ENCRYPTION_KEY", "super-secret-32-byte-key-for-aes")
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "e2e-test-webhook-secret-12345")

	logger, _ := zap.NewDevelopment()
	defer logger.Sync()
	sLogger := &shared_logger.Logger{Logger: logger}

	jwtSecret := "e2e-jwt-secret-key-1234567890123"
	authRepo := auth.NewPostgresRepository(pool)
	authService := auth.NewPostgresService(authRepo, txManager, jwtSecret, time.Hour)
	authHandler := auth.NewHandler(authService, logger)

	paymentRepo := payment.NewPostgresRepository(pool)
	paymentService := payment.NewPaymentService(paymentRepo)
	paymentHandler := payment.NewHandler(paymentService, logger)

	accountRepo := account.NewPostgresRepository(pool, "super-secret-32-byte-key-for-aes")
	userRepo := user.NewPostgresRepository(pool)
	rentalRepo := rental.NewPostgresRepository(pool)
	rentalService := rental.NewService(rentalRepo, accountRepo, userRepo, paymentRepo, txManager)
	apiHandler := api.NewHandler(pool, rentalService, paymentService, accountRepo, nil, nil)

	router := pkg_http_server.NewAPIVersionRouter(pkg_http_server.ApiVersion1)
	rateLimiter := shared_middleware.NewRateLimiter(100.0, 200.0)
	router.RegisterRoutes(authHandler.Routes(jwtSecret, rateLimiter, sLogger)...)
	router.RegisterRoutes(paymentHandler.Routes()...)
	router.RegisterRoutes(apiHandler.Routes(jwtSecret, sLogger)...)

	mux := http.NewServeMux()
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", router))

	ts := httptest.NewServer(mux)
	defer ts.Close()

	renterOneID, renterOneToken := registerAndLoginE2EUser(t, ts, "filters-renter-1@example.com", "super-secure-pass-123", "Filters", "One")
	renterTwoID, _ := registerAndLoginE2EUser(t, ts, "filters-renter-2@example.com", "super-secure-pass-234", "Filters", "Two")
	adminID, _ := registerAndLoginE2EUser(t, ts, "filters-admin@example.com", "super-secure-pass-456", "Filters", "Admin")
	if _, err := pool.Exec(ctx, `UPDATE users SET role = 'ADMIN' WHERE id = $1`, adminID); err != nil {
		t.Fatalf("failed to grant admin role: %v", err)
	}
	adminToken := loginE2EUser(t, ts, "filters-admin@example.com", "super-secure-pass-456")

	now := time.Now().UTC().Truncate(time.Second)
	for _, query := range []struct {
		sql  string
		args []any
	}{
		{
			sql: `INSERT INTO accounts (id, login, encrypted_password, status, steam_guard_enabled, inventory_verified, hourly_price, deposit_amount, steam_id64, created_at, updated_at)
				VALUES (9401, 'filter_login_1', $1, 2, true, true, 250, 700, '765611980009401', $2, $2)`,
			args: []any{[]byte("enc-pass"), now},
		},
		{
			sql: `INSERT INTO accounts (id, login, encrypted_password, status, steam_guard_enabled, inventory_verified, hourly_price, deposit_amount, steam_id64, created_at, updated_at)
				VALUES (9402, 'filter_login_2', $1, 3, true, true, 250, 0, '765611980009402', $2, $2)`,
			args: []any{[]byte("enc-pass"), now},
		},
		{
			sql: `INSERT INTO accounts (id, login, encrypted_password, status, steam_guard_enabled, inventory_verified, hourly_price, deposit_amount, steam_id64, created_at, updated_at)
				VALUES (9403, 'filter_login_3', $1, 4, true, true, 250, 500, '765611980009403', $2, $2)`,
			args: []any{[]byte("enc-pass"), now},
		},
		{
			sql: `INSERT INTO rentals (id, user_id, account_id, status, start_at, end_at, rental_price, deposit_amount, payment_expires_at, created_at, updated_at, actual_finished_at)
				VALUES (9501, $1, 9401, 3, $3, $4, 500, 700, $5, $2, $2, $2)`,
			args: []any{renterOneID, now, now.Add(-2 * time.Hour), now.Add(-time.Hour), now.Add(15 * time.Minute)},
		},
		{
			sql: `INSERT INTO rentals (id, user_id, account_id, status, start_at, end_at, rental_price, deposit_amount, payment_expires_at, created_at, updated_at)
				VALUES (9502, $1, 9402, 1, $2, $3, 500, 0, $4, $2, $2)`,
			args: []any{renterOneID, now, now.Add(2 * time.Hour), now.Add(15 * time.Minute)},
		},
		{
			sql: `INSERT INTO rentals (id, user_id, account_id, status, start_at, end_at, rental_price, deposit_amount, payment_expires_at, created_at, updated_at)
				VALUES (9503, $1, 9403, 2, $2, $3, 500, 500, $4, $2, $2)`,
			args: []any{renterTwoID, now, now.Add(2 * time.Hour), now.Add(15 * time.Minute)},
		},
		{
			sql: `INSERT INTO payments (id, rental_id, user_id, payment_type, provider, status, amount, currency, created_at)
				VALUES (9601, 9501, $1, 1, 'balance', 2, 1200, 'USD', $2)`,
			args: []any{renterOneID, now},
		},
		{
			sql: `INSERT INTO payments (id, rental_id, user_id, payment_type, provider, status, amount, currency, created_at)
				VALUES (9602, 9502, $1, 1, 'internal', 1, 500, 'USD', $2)`,
			args: []any{renterOneID, now},
		},
		{
			sql: `INSERT INTO payments (id, rental_id, user_id, payment_type, provider, status, amount, currency, created_at)
				VALUES (9603, 9503, $1, 1, 'balance', 2, 1000, 'USD', $2)`,
			args: []any{renterTwoID, now},
		},
		{
			sql:  `INSERT INTO deposit_holds (id, user_id, rental_id, payment_id, amount, currency, status, held_at, created_at, updated_at, idempotency_key) VALUES (9701, $1, 9501, 9601, 700, 'USD', 1, $2, $2, $2, 'hold-9501')`,
			args: []any{renterOneID, now},
		},
	} {
		if _, err := pool.Exec(ctx, query.sql, query.args...); err != nil {
			t.Fatalf("failed to seed admin rentals filter data: %v", err)
		}
	}

	doAdminRentalsRequest := func(token string, query string) (int, []byte) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/admin/rentals?"+query, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("failed to call admin rentals endpoint: %v", err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, body
	}

	status, _ := doAdminRentalsRequest(renterOneToken, "page=1&page_size=10")
	if status != http.StatusForbidden {
		t.Fatalf("expected non-admin admin rentals status 403, got %d", status)
	}

	expiredStatus, expiredBody := doAdminRentalsRequest(adminToken, "page=1&page_size=10&rental_status=EXPIRED")
	if expiredStatus != http.StatusOK {
		t.Fatalf("expected EXPIRED filter status 200, got %d body=%s", expiredStatus, string(expiredBody))
	}
	var expiredResp struct {
		Success bool `json:"success"`
		Data    struct {
			Rentals []map[string]any `json:"rentals"`
			Summary struct {
				TotalCount                float64            `json:"total_count"`
				EligibleWalletRefundCount float64            `json:"eligible_wallet_refund_count"`
				RentalStatusCounts        map[string]float64 `json:"rental_status_counts"`
				PaymentStatusCounts       map[string]float64 `json:"payment_status_counts"`
				RefundStatusCounts        map[string]float64 `json:"refund_status_counts"`
			} `json:"summary"`
			Pagination struct {
				TotalItems float64 `json:"total_items"`
			} `json:"pagination"`
		} `json:"data"`
	}
	if err := json.Unmarshal(expiredBody, &expiredResp); err != nil {
		t.Fatalf("failed to decode EXPIRED filter response: %v", err)
	}
	if !expiredResp.Success || len(expiredResp.Data.Rentals) != 1 || int64(expiredResp.Data.Rentals[0]["id"].(float64)) != 9501 {
		t.Fatalf("unexpected EXPIRED filter payload: %s", string(expiredBody))
	}
	if expiredResp.Data.Summary.TotalCount != 1 || expiredResp.Data.Pagination.TotalItems != 1 {
		t.Fatalf("expected filtered totals to equal 1, got summary=%v pagination=%v", expiredResp.Data.Summary.TotalCount, expiredResp.Data.Pagination.TotalItems)
	}
	if expiredResp.Data.Summary.EligibleWalletRefundCount != 1 || expiredResp.Data.Summary.RentalStatusCounts["EXPIRED"] != 1 || expiredResp.Data.Summary.PaymentStatusCounts["SUCCESS"] != 1 || expiredResp.Data.Summary.RefundStatusCounts["NONE"] != 1 {
		t.Fatalf("unexpected filtered summary counts: %s", string(expiredBody))
	}

	balanceStatus, balanceBody := doAdminRentalsRequest(adminToken, "page=1&page_size=10&payment_provider=balance")
	if balanceStatus != http.StatusOK {
		t.Fatalf("expected balance filter status 200, got %d body=%s", balanceStatus, string(balanceBody))
	}
	var balanceResp struct {
		Data struct {
			Rentals []map[string]any `json:"rentals"`
		} `json:"data"`
	}
	if err := json.Unmarshal(balanceBody, &balanceResp); err != nil {
		t.Fatalf("failed to decode payment_provider response: %v", err)
	}
	if len(balanceResp.Data.Rentals) != 2 {
		t.Fatalf("expected 2 balance rentals, got %d body=%s", len(balanceResp.Data.Rentals), string(balanceBody))
	}
	for _, row := range balanceResp.Data.Rentals {
		if row["payment_provider"] != "balance" {
			t.Fatalf("expected only balance provider rows, got %s", string(balanceBody))
		}
	}

	eligibleStatus, eligibleBody := doAdminRentalsRequest(adminToken, "page=1&page_size=10&eligible_wallet_refund=true")
	if eligibleStatus != http.StatusOK {
		t.Fatalf("expected eligible filter status 200, got %d body=%s", eligibleStatus, string(eligibleBody))
	}
	var eligibleResp struct {
		Data struct {
			Rentals []map[string]any `json:"rentals"`
		} `json:"data"`
	}
	if err := json.Unmarshal(eligibleBody, &eligibleResp); err != nil {
		t.Fatalf("failed to decode eligible filter response: %v", err)
	}
	if len(eligibleResp.Data.Rentals) != 1 || int64(eligibleResp.Data.Rentals[0]["id"].(float64)) != 9501 {
		t.Fatalf("expected only eligible rental 9501, got %s", string(eligibleBody))
	}

	userStatus, userBody := doAdminRentalsRequest(adminToken, fmt.Sprintf("page=1&page_size=10&user_id=%d", renterTwoID))
	if userStatus != http.StatusOK {
		t.Fatalf("expected user_id filter status 200, got %d body=%s", userStatus, string(userBody))
	}
	var userResp struct {
		Data struct {
			Rentals []map[string]any `json:"rentals"`
		} `json:"data"`
	}
	if err := json.Unmarshal(userBody, &userResp); err != nil {
		t.Fatalf("failed to decode user_id filter response: %v", err)
	}
	if len(userResp.Data.Rentals) != 1 || int64(userResp.Data.Rentals[0]["user_id"].(float64)) != renterTwoID {
		t.Fatalf("expected exact user_id match, got %s", string(userBody))
	}

	rentalStatus, rentalBody := doAdminRentalsRequest(adminToken, "page=1&page_size=10&rental_id=9502")
	if rentalStatus != http.StatusOK {
		t.Fatalf("expected rental_id filter status 200, got %d body=%s", rentalStatus, string(rentalBody))
	}
	var rentalResp struct {
		Data struct {
			Rentals []map[string]any `json:"rentals"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rentalBody, &rentalResp); err != nil {
		t.Fatalf("failed to decode rental_id filter response: %v", err)
	}
	if len(rentalResp.Data.Rentals) != 1 || int64(rentalResp.Data.Rentals[0]["id"].(float64)) != 9502 {
		t.Fatalf("expected exact rental_id match, got %s", string(rentalBody))
	}
	for _, forbidden := range []string{"login", "password", "credentials", "metadata", "idempotency_key", "correlation_id", "provider_payload", "token", "secret"} {
		if _, exists := rentalResp.Data.Rentals[0][forbidden]; exists {
			t.Fatalf("admin rentals DTO leaked forbidden field %q: %s", forbidden, string(rentalBody))
		}
	}

	invalidEnumStatus, _ := doAdminRentalsRequest(adminToken, "page=1&page_size=10&payment_status=BROKEN")
	if invalidEnumStatus != http.StatusUnprocessableEntity {
		t.Fatalf("expected invalid enum status 422, got %d", invalidEnumStatus)
	}

	invalidPageStatus, _ := doAdminRentalsRequest(adminToken, "page=0&page_size=101")
	if invalidPageStatus != http.StatusUnprocessableEntity {
		t.Fatalf("expected invalid pagination status 422, got %d", invalidPageStatus)
	}
}
