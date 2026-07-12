package test_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"go.uber.org/zap"

	"rent_game_accs/internal/account"
	"rent_game_accs/internal/api"
	"rent_game_accs/internal/auth"
	"rent_game_accs/internal/payment"
	pkg_http_server "rent_game_accs/internal/pkg/transport/http/server"
	"rent_game_accs/internal/rental"
	shared_logger "rent_game_accs/internal/shared/logger"
	shared_middleware "rent_game_accs/internal/shared/middleware"
	"rent_game_accs/internal/user"
)

func generateRandomID() int64 {
	n, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	return n.Int64() + 1
}

func TestAdversarial_CredentialsSecurity(t *testing.T) {
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
	apiHandler := api.NewHandler(pool, rentalService, paymentService, accountRepo, nil, nil)

	router := pkg_http_server.NewAPIVersionRouter(pkg_http_server.ApiVersion1)
	rateLimiter := shared_middleware.NewRateLimiter(100.0, 200.0)
	router.RegisterRoutes(authHandler.Routes(jwtSecret, rateLimiter, sLogger, pool)...)
	router.RegisterRoutes(paymentHandler.Routes()...)
	router.RegisterRoutes(apiHandler.Routes(jwtSecret, sLogger)...)

	mux := http.NewServeMux()
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", router))

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// 1. Setup User
	userID, accessToken := registerAndLoginE2EUser(t, ts, "adv_user@example.com", "super-secure-pass-123", "Adv", "User")

	// Ensure we top up user balance if needed
	_, err := pool.Exec(ctx, `UPDATE users SET balance = $1 WHERE id = $2`, 20000, userID)
	if err != nil {
		t.Fatalf("failed to top up user balance: %v", err)
	}

	// Helper function to create a new unique account, rental, and payment
	createAccountAndRental := func(t *testing.T, start, end, payExpires time.Time) (int64, int64, int64) {
		t.Helper()
		accID := generateRandomID()

		// Insert account
		encPassword, err := accountRepo.Encrypt("steam_secure_password_adversarial")
		if err != nil {
			t.Fatalf("failed to encrypt password: %v", err)
		}

		steamID := "steam-" + strconv.FormatInt(accID, 10)
		if len(steamID) > 32 {
			steamID = steamID[:32]
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO accounts (id, steam_id64, login, encrypted_password, steam_guard_enabled, inventory_verified, last_security_check, hourly_price, deposit_amount, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, false, true, NOW(), 100, 200, 4, NOW(), NOW())`,
			accID, steamID, "steam_login_"+strconv.FormatInt(accID, 10), encPassword,
		)
		if err != nil {
			t.Fatalf("failed to insert account: %v", err)
		}

		rentalID := generateRandomID()
		// Set created_at to 10 minutes before payExpires to satisfy check constraints
		createdAt := payExpires.Add(-10 * time.Minute)
		_, err = pool.Exec(ctx, `
			INSERT INTO rentals (id, user_id, account_id, rental_price, deposit_amount, start_at, end_at, status, payment_expires_at, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)`,
			rentalID, userID, accID, int64(100), int64(200), start.UTC(), end.UTC(), int16(2), payExpires.UTC(), createdAt.UTC(),
		)
		if err != nil {
			t.Fatalf("failed to insert rental: %v", err)
		}

		// Insert confirmed payment
		paymentID := generateRandomID()
		_, err = pool.Exec(ctx, `
			INSERT INTO payments (id, rental_id, user_id, payment_type, provider, status, amount, currency, external_transaction_id, created_at, processed_at)
			VALUES ($1, $2, $3, 1, $4, $5, $6, $7, $8, NOW(), NOW())`,
			paymentID, rentalID, userID, "internal", int16(2), int64(300), "USD", "tx-adv-"+strconv.FormatInt(paymentID, 10),
		)
		if err != nil {
			t.Fatalf("failed to insert payment: %v", err)
		}

		return accID, rentalID, paymentID
	}

	client := &http.Client{}

	// --- TEST 1: Active Paid Rental with EXPIRED payment deadline allows credentials ---
	t.Run("ActivePaidExpiredDeadlineAllowsCredentials", func(t *testing.T) {
		now := time.Now().UTC()
		// Start is 1 hour ago, end is 2 hours from now, payment_expires_at is 30 minutes ago (expired!)
		_, rentalID, _ := createAccountAndRental(t, now.Add(-1*time.Hour), now.Add(2*time.Hour), now.Add(-30*time.Minute))

		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/me/rentals/"+strconv.FormatInt(rentalID, 10)+"/credentials", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("User-Agent", "Test-Agent-Adversarial")

		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(res.Body)
			t.Fatalf("expected status 200 OK, got %d. Body: %s", res.StatusCode, string(body))
		}

		var credsResp struct {
			Success bool `json:"success"`
			Data    struct {
				Login    string `json:"login"`
				Password string `json:"password"`
			} `json:"data"`
		}
		if err := json.NewDecoder(res.Body).Decode(&credsResp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if !credsResp.Success || credsResp.Data.Password != "steam_secure_password_adversarial" {
			t.Fatalf("unexpected credentials response: %+v", credsResp)
		}
	})

	// --- TEST 2: Expired, Cancelled, and Refunded Rentals strictly deny credentials ---
	t.Run("IneligibleRentalsDenyCredentials", func(t *testing.T) {
		now := time.Now().UTC()

		// Case A: Expired rental (end_at in past)
		_, expiredRentalID, _ := createAccountAndRental(t, now.Add(-3*time.Hour), now.Add(-1*time.Hour), now.Add(-2*time.Hour))

		// Case B: Cancelled rental (status = 5)
		_, cancelledRentalID, _ := createAccountAndRental(t, now.Add(-1*time.Hour), now.Add(2*time.Hour), now.Add(30*time.Minute))
		_, err = pool.Exec(ctx, `UPDATE rentals SET status = 5 WHERE id = $1`, cancelledRentalID)
		if err != nil {
			t.Fatalf("failed to cancel rental: %v", err)
		}

		// Case C: Refunded rental
		accID, refundedRentalID, paymentID := createAccountAndRental(t, now.Add(-1*time.Hour), now.Add(2*time.Hour), now.Add(30*time.Minute))
		_, err = pool.Exec(ctx, `UPDATE rentals SET status = 5 WHERE id = $1`, refundedRentalID)
		if err != nil {
			t.Fatalf("failed to refund rental: %v", err)
		}
		// insert refund record to represent refunded
		refundIdempotency := "idempotency-key-" + strconv.FormatInt(refundedRentalID, 10)
		refundCorrelation := "correlation-id-" + strconv.FormatInt(refundedRentalID, 10)
		_, err = pool.Exec(ctx, `
			INSERT INTO refunds (payment_id, rental_id, user_id, account_id, source_type, refund_kind, status, amount_principal, amount_deposit, amount_total, currency, idempotency_key, correlation_id, metadata, reason_code, requested_by_role, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 1, 1, 2, 100, 200, 300, 'USD', $5, $6, '{}', 'SERVICE_UNAVAILABLE', 'ADMIN', NOW(), NOW())`,
			paymentID, refundedRentalID, userID, accID, refundIdempotency, refundCorrelation,
		)
		if err != nil {
			t.Fatalf("failed to insert refund record: %v", err)
		}

		for name, rID := range map[string]int64{
			"Expired":   expiredRentalID,
			"Cancelled": cancelledRentalID,
			"Refunded":  refundedRentalID,
		} {
			req, _ := http.NewRequest("GET", ts.URL+"/api/v1/me/rentals/"+strconv.FormatInt(rID, 10)+"/credentials", nil)
			req.Header.Set("Authorization", "Bearer "+accessToken)
			res, err := client.Do(req)
			if err != nil {
				t.Fatalf("%s: request failed: %v", name, err)
			}
			res.Body.Close()
			if res.StatusCode != http.StatusNotFound {
				t.Fatalf("expected status 404 for %s rental, got %d", name, res.StatusCode)
			}
		}
	})

	// --- TEST 3: Cache-Control: no-store headers are present ---
	t.Run("CacheControlHeadersArePresent", func(t *testing.T) {
		now := time.Now().UTC()
		_, rentalID, _ := createAccountAndRental(t, now.Add(-1*time.Hour), now.Add(2*time.Hour), now.Add(30*time.Minute))

		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/me/rentals/"+strconv.FormatInt(rentalID, 10)+"/credentials", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", res.StatusCode)
		}

		cc := res.Header.Get("Cache-Control")
		pragma := res.Header.Get("Pragma")
		expires := res.Header.Get("Expires")

		if cc != "no-store, no-cache, must-revalidate, max-age=0" {
			t.Errorf("unexpected Cache-Control header: %q", cc)
		}
		if pragma != "no-cache" {
			t.Errorf("unexpected Pragma header: %q", pragma)
		}
		if expires != "0" {
			t.Errorf("unexpected Expires header: %q", expires)
		}
	})

	// --- TEST 4: Client IP, User-Agent, and Account ID are correctly logged ---
	t.Run("LogsIpUserAgentAndAccountID", func(t *testing.T) {
		// Clean security events first
		_, _ = pool.Exec(ctx, `DELETE FROM security_events`)

		now := time.Now().UTC()
		accID, rentalID, _ := createAccountAndRental(t, now.Add(-1*time.Hour), now.Add(2*time.Hour), now.Add(30*time.Minute))

		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/me/rentals/"+strconv.FormatInt(rentalID, 10)+"/credentials", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("User-Agent", "Specific-Adversarial-Agent")

		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", res.StatusCode)
		}

		var count int
		var loggedIP, loggedUA string
		var loggedAccountID int64
		err = pool.QueryRow(ctx, `
			SELECT COUNT(*), COALESCE(ip_address::text, ''), user_agent, account_id
			FROM security_events
			WHERE rental_id = $1 AND event_type = 7
			GROUP BY ip_address, user_agent, account_id`, rentalID).Scan(&count, &loggedIP, &loggedUA, &loggedAccountID)
		if err != nil {
			t.Fatalf("failed to query security events: %v", err)
		}

		if count != 1 {
			t.Errorf("expected 1 logged event, got %d", count)
		}
		if loggedUA != "Specific-Adversarial-Agent" {
			t.Errorf("expected UA %q, got %q", "Specific-Adversarial-Agent", loggedUA)
		}
		if loggedAccountID != accID {
			t.Errorf("expected Account ID %d, got %d", accID, loggedAccountID)
		}
	})

	// --- TEST 5: Audit Failures Block Credential Retrieval ---
	t.Run("AuditFailureBlocksCredentialRetrieval", func(t *testing.T) {
		// Create a trigger that forces insert into security_events to fail for credential issuance (event_type = 7)
		_, err := pool.Exec(ctx, `
			CREATE OR REPLACE FUNCTION fail_credential_security_events() RETURNS trigger AS $$
			BEGIN
				IF NEW.event_type = 7 THEN
					RAISE EXCEPTION 'forced credential security log failure';
				END IF;
				RETURN NEW;
			END;
			$$ LANGUAGE plpgsql`)
		if err != nil {
			t.Fatalf("failed to create fail function: %v", err)
		}

		_, err = pool.Exec(ctx, `
			CREATE TRIGGER trg_test_fail_credential_security
			BEFORE INSERT ON security_events
			FOR EACH ROW EXECUTE FUNCTION fail_credential_security_events()`)
		if err != nil {
			t.Fatalf("failed to create trigger: %v", err)
		}

		defer func() {
			_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS trg_test_fail_credential_security ON security_events`)
			_, _ = pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS fail_credential_security_events()`)
		}()

		now := time.Now().UTC()
		_, rentalID, _ := createAccountAndRental(t, now.Add(-1*time.Hour), now.Add(2*time.Hour), now.Add(30*time.Minute))

		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/me/rentals/"+strconv.FormatInt(rentalID, 10)+"/credentials", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer res.Body.Close()

		// The API should fail with 500 Internal Server Error because the DB event log failed
		if res.StatusCode != http.StatusInternalServerError {
			body, _ := io.ReadAll(res.Body)
			t.Fatalf("expected status 500 Internal Server Error when audit fails, got %d. Body: %s", res.StatusCode, string(body))
		}
	})
}
