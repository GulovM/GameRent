package repo_postgres

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"rent_game_accs/internal/account"
	"rent_game_accs/internal/payment"
	pkg_postgres_pool "rent_game_accs/internal/pkg/repository/postgres/pool"
	"rent_game_accs/internal/rental"
	"rent_game_accs/internal/shared/database"
	"rent_game_accs/migrations"
)

var workersTestCounter int64 = 9100

func setupWorkersTestDB(t *testing.T) (*pkg_postgres_pool.ConnectionPool, database.TxManager) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "1" {
		t.Skip("set RUN_INTEGRATION_TESTS=1 and start PostgreSQL to run integration tests")
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
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM accounts")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM users")

	return poolConn, database.NewTxManager(poolConn.Pool)
}

func seedWaitingPaymentReservation(t *testing.T, pool *pkg_postgres_pool.ConnectionPool, paymentExpiresAt time.Time) (userID, accountID, rentalID, paymentID int64) {
	ctx := context.Background()

	base := atomic.AddInt64(&workersTestCounter, 10)
	userID = base
	accountID = base + 1
	rentalID = base + 2
	paymentID = base + 3
	createdAt := paymentExpiresAt.Add(-2 * time.Hour)

	userEmail := "cleanup-" + strconv.FormatInt(base, 10) + "@example.com"
	accountLogin := "cleanup_steam_login_" + strconv.FormatInt(base, 10)
	steamID64 := "7656119800000" + strconv.FormatInt(base, 10)

	_, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, balance) VALUES ($1, $2, $3, 10000)`, userID, userEmail, "hash")
	if err != nil {
		t.Fatalf("failed to insert user: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, login, encrypted_password, status, steam_guard_enabled, inventory_verified, hourly_price, deposit_amount, steam_id64, created_at, updated_at)
		VALUES ($1, $2, $3, 3, true, true, 250, 500, $4, NOW(), NOW())`,
		accountID, accountLogin, []byte("enc-pass"), steamID64)
	if err != nil {
		t.Fatalf("failed to insert account: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO rentals (id, user_id, account_id, status, start_at, end_at, rental_price, deposit_amount, payment_expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, 1, $4, $5, 500, 500, $6, $7, $7)`,
		rentalID, userID, accountID, createdAt, createdAt.Add(2*time.Hour), paymentExpiresAt, createdAt)
	if err != nil {
		t.Fatalf("failed to insert rental: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO payments (id, rental_id, user_id, payment_type, provider, status, amount, currency)
		VALUES ($1, $2, $3, 1, 'internal', 1, 1000, 'USD')`,
		paymentID, rentalID, userID)
	if err != nil {
		t.Fatalf("failed to insert payment: %v", err)
	}

	return userID, accountID, rentalID, paymentID
}

func TestExpireWaitingPaymentReservation_TransitionsToTerminalStates(t *testing.T) {
	pool, _ := setupWorkersTestDB(t)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	_, accountID, rentalID, paymentID := seedWaitingPaymentReservation(t, pool, now.Add(-time.Minute))
	repo := NewRepository(pool)

	changed, err := repo.ExpireWaitingPaymentReservation(context.Background(), paymentID, now)
	if err != nil {
		t.Fatalf("ExpireWaitingPaymentReservation failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected reservation to be processed")
	}

	var rentalStatus, paymentStatus, accountStatus int16
	err = pool.QueryRow(context.Background(), "SELECT status FROM rentals WHERE id = $1", rentalID).Scan(&rentalStatus)
	if err != nil {
		t.Fatalf("failed to query rental status: %v", err)
	}
	err = pool.QueryRow(context.Background(), "SELECT status FROM payments WHERE id = $1", paymentID).Scan(&paymentStatus)
	if err != nil {
		t.Fatalf("failed to query payment status: %v", err)
	}
	err = pool.QueryRow(context.Background(), "SELECT status FROM accounts WHERE id = $1", accountID).Scan(&accountStatus)
	if err != nil {
		t.Fatalf("failed to query account status: %v", err)
	}

	if rentalStatus != int16(rental.StatusExpired) {
		t.Fatalf("expected rental to become EXPIRED, got %d", rentalStatus)
	}
	if paymentStatus != 3 {
		t.Fatalf("expected payment to become FAILED, got %d", paymentStatus)
	}
	if accountStatus != int16(2) {
		t.Fatalf("expected account to become AVAILABLE, got %d", accountStatus)
	}

	var eventCount int
	var eventType int16
	var metadata string
	err = pool.QueryRow(context.Background(), `
		SELECT COUNT(*), COALESCE(MIN(event_type), 0), COALESCE(MIN(metadata::text), '')
		FROM security_events
		WHERE rental_id = $1`, rentalID).Scan(&eventCount, &eventType, &metadata)
	if err != nil {
		t.Fatalf("failed to query security events: %v", err)
	}
	if eventCount != 1 || eventType != 8 {
		t.Fatalf("expected one cleanup security event with type 8, got count=%d type=%d", eventCount, eventType)
	}
	if containsSecret(metadata) {
		t.Fatalf("expected cleanup metadata to avoid secrets, got %s", metadata)
	}
}

func TestExpireWaitingPaymentReservation_IdempotentOnReplay(t *testing.T) {
	pool, _ := setupWorkersTestDB(t)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	_, _, rentalID, paymentID := seedWaitingPaymentReservation(t, pool, now.Add(-time.Minute))
	repo := NewRepository(pool)

	changed, err := repo.ExpireWaitingPaymentReservation(context.Background(), paymentID, now)
	if err != nil || !changed {
		t.Fatalf("first cleanup failed: changed=%v err=%v", changed, err)
	}

	changed, err = repo.ExpireWaitingPaymentReservation(context.Background(), paymentID, now)
	if err != nil {
		t.Fatalf("replay cleanup failed: %v", err)
	}
	if changed {
		t.Fatalf("expected replay cleanup to be a no-op")
	}

	var eventCount int
	err = pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM security_events WHERE rental_id = $1", rentalID).Scan(&eventCount)
	if err != nil {
		t.Fatalf("failed to count security events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected exactly one security event after replay, got %d", eventCount)
	}
}

func TestExpireWaitingPaymentReservation_SkipsActiveRentalAndFutureExpiry(t *testing.T) {
	pool, _ := setupWorkersTestDB(t)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)

	_, accountID, rentalID, paymentID := seedWaitingPaymentReservation(t, pool, now.Add(time.Hour))
	repo := NewRepository(pool)

	changed, err := repo.ExpireWaitingPaymentReservation(context.Background(), paymentID, now)
	if err != nil {
		t.Fatalf("expected future expiry cleanup to be harmless: %v", err)
	}
	if changed {
		t.Fatalf("expected future expiry reservation to remain untouched")
	}

	_, err = pool.Exec(context.Background(), "UPDATE rentals SET status = 2 WHERE id = $1", rentalID)
	if err != nil {
		t.Fatalf("failed to promote rental to active: %v", err)
	}
	_, err = pool.Exec(context.Background(), "UPDATE accounts SET status = 4 WHERE id = $1", accountID)
	if err != nil {
		t.Fatalf("failed to promote account to rented: %v", err)
	}

	changed, err = repo.ExpireWaitingPaymentReservation(context.Background(), paymentID, now)
	if err != nil {
		t.Fatalf("active rental cleanup should not fail: %v", err)
	}
	if changed {
		t.Fatalf("expected active rental to stay untouched")
	}
}

func TestCleanupAndWebhookRaceStayConsistent(t *testing.T) {
	pool, _ := setupWorkersTestDB(t)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	_, accountID, rentalID, paymentID := seedWaitingPaymentReservation(t, pool, now.Add(-time.Minute))

	repo := NewRepository(pool)
	paymentRepo := payment.NewPostgresRepository(pool.Pool)
	service := payment.NewPaymentService(paymentRepo)

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	var cleanupChanged bool
	var cleanupErr error
	go func() {
		defer wg.Done()
		<-start
		cleanupChanged, cleanupErr = repo.ExpireWaitingPaymentReservation(context.Background(), paymentID, now)
	}()

	var webhookErr error
	go func() {
		defer wg.Done()
		<-start
		_, webhookErr = service.ProcessWebhook(context.Background(), payment.WebhookRequest{
			PaymentID:             strconv.FormatInt(paymentID, 10),
			ExternalTransactionID: "cleanup-race-ext",
			Status:                "success",
		}, "127.0.0.1", "test")
	}()

	close(start)
	wg.Wait()

	if cleanupErr != nil && webhookErr == nil {
		t.Fatalf("cleanup errored while webhook succeeded: %v", cleanupErr)
	}

	var rentalStatus, paymentStatus, accountStatus int16
	err := pool.QueryRow(context.Background(), "SELECT status FROM rentals WHERE id = $1", rentalID).Scan(&rentalStatus)
	if err != nil {
		t.Fatalf("failed to query rental status: %v", err)
	}
	err = pool.QueryRow(context.Background(), "SELECT status FROM payments WHERE id = $1", paymentID).Scan(&paymentStatus)
	if err != nil {
		t.Fatalf("failed to query payment status: %v", err)
	}
	err = pool.QueryRow(context.Background(), "SELECT status FROM accounts WHERE id = $1", accountID).Scan(&accountStatus)
	if err != nil {
		t.Fatalf("failed to query account status: %v", err)
	}

	consistentCleanup := rentalStatus == int16(rental.StatusExpired) && paymentStatus == 3 && accountStatus == int16(2)
	consistentWebhook := rentalStatus == int16(rental.StatusActive) && paymentStatus == 2 && accountStatus == int16(4)
	if !consistentCleanup && !consistentWebhook {
		t.Fatalf("expected race to finish in a consistent terminal state, got rental=%d payment=%d account=%d cleanup_changed=%v cleanup_err=%v webhook_err=%v", rentalStatus, paymentStatus, accountStatus, cleanupChanged, cleanupErr, webhookErr)
	}

	var eventCount int
	err = pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM security_events WHERE rental_id = $1", rentalID).Scan(&eventCount)
	if err != nil {
		t.Fatalf("failed to query security events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected exactly one security event after race, got %d", eventCount)
	}
}

func seedActiveRental(t *testing.T, pool *pkg_postgres_pool.ConnectionPool, endAt time.Time) (userID, accountID, rentalID, paymentID int64) {
	t.Helper()
	ctx := context.Background()

	base := atomic.AddInt64(&workersTestCounter, 10)
	userID = base
	accountID = base + 1
	rentalID = base + 2
	paymentID = base + 3
	createdAt := endAt.Add(-2 * time.Hour)

	_, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, balance) VALUES ($1, $2, $3, 10000)`, userID, "active-expire-"+strconv.FormatInt(base, 10)+"@example.com", "hash")
	if err != nil {
		t.Fatalf("failed to insert user: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, login, encrypted_password, status, steam_guard_enabled, inventory_verified, hourly_price, deposit_amount, steam_id64, created_at, updated_at)
		VALUES ($1, $2, $3, 4, true, true, 250, 500, $4, NOW(), NOW())`,
		accountID, "active_expire_login_"+strconv.FormatInt(base, 10), []byte("enc-pass"), "7656119810000"+strconv.FormatInt(base, 10))
	if err != nil {
		t.Fatalf("failed to insert account: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO rentals (id, user_id, account_id, status, start_at, end_at, rental_price, deposit_amount, payment_expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, 2, $4, $5, 500, 500, $6, $4, $4)`,
		rentalID, userID, accountID, createdAt, endAt, endAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("failed to insert rental: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO payments (id, rental_id, user_id, payment_type, provider, status, amount, currency, processed_at)
		VALUES ($1, $2, $3, 1, 'internal', 2, 1000, 'USD', $4)`,
		paymentID, rentalID, userID, createdAt)
	if err != nil {
		t.Fatalf("failed to insert payment: %v", err)
	}

	return userID, accountID, rentalID, paymentID
}

func TestExpireRental_ActiveRentalLifecycle(t *testing.T) {
	pool, _ := setupWorkersTestDB(t)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	userID, accountID, rentalID, paymentID := seedActiveRental(t, pool, now.Add(-time.Minute))
	repo := NewRepository(pool)

	changed, err := repo.ExpireRental(context.Background(), rentalID, accountID, now)
	if err != nil {
		t.Fatalf("ExpireRental failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected active rental to expire")
	}

	var rentalStatus, paymentStatus, accountStatus int16
	if err := pool.QueryRow(context.Background(), "SELECT status FROM rentals WHERE id = $1", rentalID).Scan(&rentalStatus); err != nil {
		t.Fatalf("failed to query rental status: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT status FROM payments WHERE id = $1", paymentID).Scan(&paymentStatus); err != nil {
		t.Fatalf("failed to query payment status: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT status FROM accounts WHERE id = $1", accountID).Scan(&accountStatus); err != nil {
		t.Fatalf("failed to query account status: %v", err)
	}
	if rentalStatus != int16(rental.StatusExpired) {
		t.Fatalf("expected rental EXPIRED, got %d", rentalStatus)
	}
	if paymentStatus != 2 {
		t.Fatalf("expected successful payment to remain SUCCESS, got %d", paymentStatus)
	}
	if accountStatus != int16(2) {
		t.Fatalf("expected account AVAILABLE, got %d", accountStatus)
	}

	credentialsRepo := rental.NewPostgresRepository(pool.Pool)
	accountRepo := account.NewPostgresRepository(pool.Pool, "super-secret-32-byte-key-for-aes")
	service := rental.NewService(credentialsRepo, accountRepo, nil, nil, nil)
	creds, err := service.GetRentalCredentials(context.Background(), userID, rentalID, now)
	if !errors.Is(err, rental.ErrCredentialsNotAvailable) {
		t.Fatalf("expected credentials denial after expire, got creds=%+v err=%v", creds, err)
	}

	var eventCount int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM security_events WHERE rental_id = $1", rentalID).Scan(&eventCount); err != nil {
		t.Fatalf("failed to count security events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected one expire security event, got %d", eventCount)
	}
}

func TestExpireRental_IdempotentOnReplay(t *testing.T) {
	pool, _ := setupWorkersTestDB(t)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	_, accountID, rentalID, _ := seedActiveRental(t, pool, now.Add(-time.Minute))
	repo := NewRepository(pool)

	changed, err := repo.ExpireRental(context.Background(), rentalID, accountID, now)
	if err != nil || !changed {
		t.Fatalf("first expire failed: changed=%v err=%v", changed, err)
	}
	changed, err = repo.ExpireRental(context.Background(), rentalID, accountID, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("replay expire failed: %v", err)
	}
	if changed {
		t.Fatalf("expected replay expire to be no-op")
	}

	var eventCount int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM security_events WHERE rental_id = $1", rentalID).Scan(&eventCount); err != nil {
		t.Fatalf("failed to count security events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected one expire security event after replay, got %d", eventCount)
	}
}

func TestExpireVsCredentialsRequest_DeniesAfterExpiration(t *testing.T) {
	pool, _ := setupWorkersTestDB(t)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	userID, accountID, rentalID, _ := seedActiveRental(t, pool, now.Add(-time.Minute))
	repo := NewRepository(pool)

	changed, err := repo.ExpireRental(context.Background(), rentalID, accountID, now)
	if err != nil || !changed {
		t.Fatalf("expire failed: changed=%v err=%v", changed, err)
	}

	credentialsRepo := rental.NewPostgresRepository(pool.Pool)
	accountRepo := account.NewPostgresRepository(pool.Pool, "super-secret-32-byte-key-for-aes")
	service := rental.NewService(credentialsRepo, accountRepo, nil, nil, nil)
	creds, err := service.GetRentalCredentials(context.Background(), userID, rentalID, now.Add(time.Second))
	if !errors.Is(err, rental.ErrCredentialsNotAvailable) {
		t.Fatalf("expected credentials denial after committed expiration, got creds=%+v err=%v", creds, err)
	}
}

func containsSecret(metadata string) bool {
	return strings.Contains(metadata, "password") || strings.Contains(metadata, "steam_id64") || strings.Contains(metadata, "token") || strings.Contains(metadata, "key")
}
