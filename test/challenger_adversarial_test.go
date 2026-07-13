package test_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"rent_game_accs/internal/account"
	"rent_game_accs/internal/auth"
	"rent_game_accs/internal/payment"
	pkg_http_server "rent_game_accs/internal/pkg/transport/http/server"
	"rent_game_accs/internal/rental"
	shared_logger "rent_game_accs/internal/shared/logger"
	shared_middleware "rent_game_accs/internal/shared/middleware"
	"rent_game_accs/internal/user"
)

func TestAdversarial_CredentialEligibilityFix(t *testing.T) {
	ctx := context.Background()

	pool, txManager := setupE2ETestDB(t)

	t.Setenv("ENCRYPTION_KEY", "super-secret-32-byte-key-for-aes")
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "e2e-test-webhook-secret-at-least-32-bytes")

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
	apiHandler := newAPIHandlerForE2E(pool, txManager, rentalService, paymentService, accountRepo, userRepo)

	router := pkg_http_server.NewAPIVersionRouter(pkg_http_server.ApiVersion1)
	rateLimiter := shared_middleware.NewRateLimiter(100.0, 200.0)
	router.RegisterRoutes(authHandler.Routes(jwtSecret, rateLimiter, sLogger, pool)...)
	router.RegisterRoutes(paymentHandler.Routes()...)
	router.RegisterRoutes(apiHandler.Routes(jwtSecret, sLogger)...)

	mux := http.NewServeMux()
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", router))

	ts := httptest.NewServer(mux)
	t.Cleanup(func() { ts.Close() })

	userID, token := registerAndLoginE2EUser(t, ts, "adversarial@example.com", "pass-123-adv", "Adversarial", "Tester")

	_, err := pool.Exec(ctx, `UPDATE users SET balance = $1 WHERE id = $2`, 10000, userID)
	if err != nil {
		t.Fatalf("failed to top up user balance: %v", err)
	}

	gameID := int64(9995)
	_, err = pool.Exec(ctx, `
		INSERT INTO games (id, name, steam_app_id, short_description, header_image, developers, publishers, genres)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		gameID, "Adversarial Test Game", 1091505, "", "", []byte("[]"), []byte("[]"), []byte("[]"),
	)
	if err != nil {
		t.Fatalf("failed to insert game: %v", err)
	}

	encPassword, _ := accountRepo.Encrypt("adver-secret-pass")
	steamCreds, _ := account.NewSteamCredentials("adver_login", encPassword, "76561198000000002")

	price, _ := account.NewMoney(100, "USD")
	deposit, _ := account.NewMoney(500, "USD")

	accEntity, err := account.NewAccount(steamCreds, price, deposit, time.Now().UTC())
	accEntity.ID = 8885
	accEntity.MarkSecurityChecked(true, true, time.Now().UTC())
	accEntity.SyncLibrary([]account.AccountGame{{
		Game:            account.Game{ID: gameID, SteamAppID: 1091505, Name: "Adversarial Test Game"},
		PlaytimeMinutes: 100,
	}}, time.Now().UTC())
	_ = accEntity.Publish(time.Now().UTC())
	_ = accountRepo.CreateAccount(ctx, accEntity)

	rent, err := rentalService.RentAccount(ctx, userID, accEntity.ID, 3*time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatalf("failed to rent: %v", err)
	}
	rentalID := rent.ID

	var paymentID int64
	_ = pool.QueryRow(ctx, "SELECT id FROM payments WHERE rental_id = $1", rentalID).Scan(&paymentID)

	_, _ = pool.Exec(ctx, "UPDATE payments SET status = 2, external_transaction_id = 'ext-tx-adv' WHERE id = $1", paymentID)
	_, _ = pool.Exec(ctx, "UPDATE rentals SET status = 2 WHERE id = $1", rentalID)
	_, _ = pool.Exec(ctx, "UPDATE accounts SET status = 4 WHERE id = $1", accEntity.ID)

	client := &http.Client{}

	_, _ = pool.Exec(ctx, "UPDATE rentals SET payment_expires_at = $1 WHERE id = $2", time.Now().UTC().Add(-1*time.Hour), rentalID)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/me/rentals/"+strconv.FormatInt(rentalID, 10)+"/credentials", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "Adversarial-Agent")

	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to execute request: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected credentials retrieval to succeed (200), got %d: %s", res.StatusCode, string(body))
	}

	if cc := res.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("expected Cache-Control: no-store, got %q", cc)
	}
	if pragma := res.Header.Get("Pragma"); pragma != "no-cache" {
		t.Errorf("expected Pragma: no-cache, got %q", pragma)
	}
	if expires := res.Header.Get("Expires"); expires != "0" {
		t.Errorf("expected Expires: 0, got %q", expires)
	}

	var credsResp struct {
		Success bool `json:"success"`
		Data    struct {
			Login    string `json:"login"`
			Password string `json:"password"`
		} `json:"data"`
	}
	_ = json.NewDecoder(res.Body).Decode(&credsResp)
	if credsResp.Data.Login != "adver_login" || credsResp.Data.Password != "adver-secret-pass" {
		t.Fatalf("incorrect credentials returned: %+v", credsResp)
	}

	var loggedUA, loggedIP string
	var loggedAccID int64
	var loggedSuccess bool
	err = pool.QueryRow(ctx, "SELECT user_agent, ip_address::text, account_id, success FROM security_events WHERE rental_id = $1 AND event_type = 7", rentalID).Scan(&loggedUA, &loggedIP, &loggedAccID, &loggedSuccess)
	if err != nil {
		t.Fatalf("failed to find security event: %v", err)
	}
	if loggedUA != "Adversarial-Agent" {
		t.Errorf("expected logged User-Agent 'Adversarial-Agent', got %q", loggedUA)
	}
	if !strings.HasPrefix(loggedIP, "127.0.0.1") {
		t.Errorf("expected logged IP to start with '127.0.0.1', got %q", loggedIP)
	}
	if loggedAccID != accEntity.ID {
		t.Errorf("expected logged Account ID %d, got %d", accEntity.ID, loggedAccID)
	}
	if !loggedSuccess {
		t.Errorf("expected logged Success true, got false")
	}

	reqErr, _ := http.NewRequest("GET", ts.URL+"/api/v1/me/rentals/"+strconv.FormatInt(rentalID, 10)+"/credentials", nil)
	reqErr.Header.Set("Authorization", "Bearer "+token)
	reqErr.Header.Set("User-Agent", "Adversarial-Agent")
	reqErr.Header.Set("X-Forwarded-For", "not-a-valid-ip-address-syntax")

	resErr, errErr := client.Do(reqErr)
	if errErr != nil {
		t.Fatalf("failed to execute request: %v", errErr)
	}
	defer resErr.Body.Close()

	if resErr.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(resErr.Body)
		t.Errorf("expected audit event failure to block and return 500 Internal Server Error, got %d: %s", resErr.StatusCode, string(body))
	}

	_, _ = pool.Exec(ctx, "UPDATE rentals SET status = 3 WHERE id = $1", rentalID)
	resExp, errExp := client.Do(req)
	if errExp != nil {
		t.Fatalf("failed to execute request for expired rental: %v", errExp)
	}
	defer resExp.Body.Close()
	if resExp.StatusCode != http.StatusNotFound {
		t.Errorf("expected expired rental to return 404, got %d", resExp.StatusCode)
	}

	_, _ = pool.Exec(ctx, "UPDATE rentals SET status = 5 WHERE id = $1", rentalID)
	resCan, errCan := client.Do(req)
	if errCan != nil {
		t.Fatalf("failed to execute request for cancelled rental: %v", errCan)
	}
	defer resCan.Body.Close()
	if resCan.StatusCode != http.StatusNotFound {
		t.Errorf("expected cancelled rental to return 404, got %d", resCan.StatusCode)
	}

	_, _ = pool.Exec(ctx, "UPDATE rentals SET status = 2 WHERE id = $1", rentalID)
	_, _ = pool.Exec(ctx, "UPDATE payments SET status = 3 WHERE id = $1", paymentID)
	resRef, errRef := client.Do(req)
	if errRef != nil {
		t.Fatalf("failed to execute request for refunded/failed rental: %v", errRef)
	}
	defer resRef.Body.Close()
	if resRef.StatusCode != http.StatusNotFound {
		t.Errorf("expected rental with failed/refunded payment to return 404, got %d", resRef.StatusCode)
	}
}
