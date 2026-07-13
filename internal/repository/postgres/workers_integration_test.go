package repo_postgres

import (
	"context"
	"database/sql"
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
			RentalID:              strconv.FormatInt(rentalID, 10),
			ExternalTransactionID: "cleanup-race-ext",
			Provider:              "internal",
			Amount:                1000,
			Currency:              "USD",
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
	pool, txManager := setupWorkersTestDB(t)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	endAt := now.Add(-time.Minute)
	userID, accountID, rentalID, paymentID := seedActiveRental(t, pool, endAt)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO deposit_holds (
			rental_id, user_id, payment_id, amount, currency, status,
			held_at, idempotency_key, created_at, updated_at
		) VALUES ($1, $2, $3, 500, 'USD', 1, $4, $5, $4, $4)`,
		rentalID, userID, paymentID, endAt.Add(-time.Hour), "expire-held-"+strconv.FormatInt(rentalID, 10)); err != nil {
		t.Fatalf("failed to insert held deposit: %v", err)
	}
	repo := NewRepository(pool)

	changed, err := repo.ExpireRental(context.Background(), rentalID, accountID, now)
	if err != nil {
		t.Fatalf("ExpireRental failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected active rental to expire")
	}

	var rentalStatus, paymentStatus, accountStatus int16
	var actualFinishedAt, reviewDeadline *time.Time
	if err := pool.QueryRow(context.Background(), `
		SELECT status, actual_finished_at, deposit_review_deadline_at
		FROM rentals
		WHERE id = $1`, rentalID).Scan(&rentalStatus, &actualFinishedAt, &reviewDeadline); err != nil {
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
	if actualFinishedAt == nil || !actualFinishedAt.Equal(endAt) {
		t.Fatalf("expected actual_finished_at=%v, got %v", endAt, actualFinishedAt)
	}
	expectedDeadline := endAt.Add(24 * time.Hour)
	if reviewDeadline == nil || !reviewDeadline.Equal(expectedDeadline) {
		t.Fatalf("expected deposit review deadline=%v, got %v", expectedDeadline, reviewDeadline)
	}

	credentialsRepo := rental.NewPostgresRepository(pool.Pool)
	accountRepo := account.NewPostgresRepository(pool.Pool, "super-secret-32-byte-key-for-aes")
	service := rental.NewService(credentialsRepo, accountRepo, nil, nil, txManager)
	creds, err := service.GetRentalCredentials(context.Background(), userID, rentalID, rental.CredentialRequestContext{}, now)
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

func TestGetExpiredRentals_IncludesExactEndBoundary(t *testing.T) {
	pool, _ := setupWorkersTestDB(t)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	_, _, rentalID, _ := seedActiveRental(t, pool, now)
	repo := NewRepository(pool)

	items, err := repo.GetExpiredRentals(context.Background(), now)
	if err != nil {
		t.Fatalf("GetExpiredRentals failed: %v", err)
	}
	for _, item := range items {
		if item.ID == rentalID {
			return
		}
	}
	t.Fatalf("expected rental ending exactly at boundary to be selected: rental_id=%d items=%+v", rentalID, items)
}

func TestExpireRental_InconsistentPositiveDepositFailsClosed(t *testing.T) {
	pool, _ := setupWorkersTestDB(t)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	_, accountID, rentalID, _ := seedActiveRental(t, pool, now)
	repo := NewRepository(pool)

	changed, err := repo.ExpireRental(context.Background(), rentalID, accountID, now)
	if err != nil || !changed {
		t.Fatalf("ExpireRental at boundary failed: changed=%v err=%v", changed, err)
	}

	var status int16
	var reviewDeadline *time.Time
	if err := pool.QueryRow(context.Background(), `
		SELECT status, deposit_review_deadline_at
		FROM rentals
		WHERE id = $1`, rentalID).Scan(&status, &reviewDeadline); err != nil {
		t.Fatalf("load expired rental: %v", err)
	}
	if status != int16(rental.StatusExpired) {
		t.Fatalf("expected rental EXPIRED, got %d", status)
	}
	if reviewDeadline != nil {
		t.Fatalf("missing positive-deposit hold must fail closed with NULL deadline, got %v", reviewDeadline)
	}
}

func TestRentalCompletionMigration_BackfillConstraintsAndDown(t *testing.T) {
	pool, _ := setupWorkersTestDB(t)
	ctx := context.Background()

	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5433"
	}
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost"
	}
	dsn := (&pkg_postgres_pool.PostgresConfig{
		Host: host, Port: port, User: "postgres", Password: "postgres", Database: "game_rental",
	}).PostgresDSN()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open migration validation db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	goose.SetBaseFS(migrations.EmbedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("set migration dialect: %v", err)
	}
	if err := goose.Down(db, "."); err != nil {
		t.Fatalf("downgrade completion migration for fixture setup: %v", err)
	}
	t.Cleanup(func() {
		goose.SetBaseFS(migrations.EmbedMigrations)
		_ = goose.SetDialect("postgres")
		_ = goose.Up(db, ".")
	})

	base := atomic.AddInt64(&workersTestCounter, 20)
	userID := base
	completedAccountID := base + 1
	heldAccountID := base + 2
	inconsistentAccountID := base + 3
	completedRentalID := base + 4
	heldRentalID := base + 5
	inconsistentRentalID := base + 6
	heldPaymentID := base + 7
	inconsistentPaymentID := base + 8
	completedAt := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Microsecond)
	usageEndedAt := completedAt.Add(12 * time.Hour)

	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, balance)
		VALUES ($1, $2, 'hash', 0)`, userID, "migration-"+strconv.FormatInt(base, 10)+"@example.com"); err != nil {
		t.Fatalf("seed migration user: %v", err)
	}
	for _, accountID := range []int64{completedAccountID, heldAccountID, inconsistentAccountID} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO accounts (
				id, login, encrypted_password, status, steam_guard_enabled,
				inventory_verified, hourly_price, deposit_amount, steam_id64, created_at, updated_at
			) VALUES ($1, $2, $3, 2, true, true, 100, 500, $4, $5, $5)`,
			accountID,
			"migration-account-"+strconv.FormatInt(accountID, 10),
			[]byte("enc-pass"),
			strconv.FormatInt(76561198000000000+accountID, 10),
			completedAt,
		); err != nil {
			t.Fatalf("seed migration account %d: %v", accountID, err)
		}
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO rentals (
			id, user_id, account_id, status, start_at, end_at, rental_price,
			deposit_amount, payment_expires_at, actual_finished_at, created_at, updated_at
		) VALUES
			($1, $4, $5, 4, $7, $8, 500, 0, $9, $8, $7, $6),
			($2, $4, $10, 3, $7, $8, 500, 500, $9, $8, $7, $8),
			($3, $4, $11, 3, $7, $8, 500, 500, $9, $8, $7, $8)`,
		completedRentalID, heldRentalID, inconsistentRentalID, userID,
		completedAccountID, completedAt, completedAt.Add(-2*time.Hour), usageEndedAt,
		usageEndedAt.Add(time.Hour), heldAccountID, inconsistentAccountID,
	); err != nil {
		t.Fatalf("seed legacy rentals: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO payments (id, rental_id, user_id, payment_type, provider, status, amount, currency, processed_at)
		VALUES
			($1, $3, $5, 1, 'internal', 2, 1000, 'USD', $6),
			($2, $4, $5, 1, 'internal', 2, 1000, 'USD', $6)`,
		heldPaymentID, inconsistentPaymentID, heldRentalID, inconsistentRentalID, userID, usageEndedAt,
	); err != nil {
		t.Fatalf("seed legacy payments: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO deposit_holds (
			rental_id, user_id, payment_id, amount, currency, status,
			held_at, idempotency_key, created_at, updated_at
		) VALUES ($1, $2, $3, 500, 'USD', 1, $4, $5, $4, $4)`,
		heldRentalID, userID, heldPaymentID, usageEndedAt.Add(-time.Hour),
		"migration-held-"+strconv.FormatInt(heldRentalID, 10),
	); err != nil {
		t.Fatalf("seed legacy held deposit: %v", err)
	}

	beforeUp := time.Now().UTC()
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("apply completion migration: %v", err)
	}
	afterUp := time.Now().UTC()

	var backfilledCompletedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT completed_at FROM rentals WHERE id=$1`, completedRentalID).Scan(&backfilledCompletedAt); err != nil {
		t.Fatalf("load completed_at backfill: %v", err)
	}
	if !backfilledCompletedAt.Equal(completedAt) {
		t.Fatalf("completed_at=%v want legacy updated_at=%v", backfilledCompletedAt, completedAt)
	}

	var heldDeadline, inconsistentDeadline *time.Time
	if err := pool.QueryRow(ctx, `SELECT deposit_review_deadline_at FROM rentals WHERE id=$1`, heldRentalID).Scan(&heldDeadline); err != nil {
		t.Fatalf("load held review deadline: %v", err)
	}
	if heldDeadline == nil || heldDeadline.Before(beforeUp.Add(24*time.Hour)) || heldDeadline.After(afterUp.Add(24*time.Hour)) {
		t.Fatalf("held review deadline must be a fresh rollout window: before=%v after=%v got=%v", beforeUp, afterUp, heldDeadline)
	}
	if err := pool.QueryRow(ctx, `SELECT deposit_review_deadline_at FROM rentals WHERE id=$1`, inconsistentRentalID).Scan(&inconsistentDeadline); err != nil {
		t.Fatalf("load inconsistent review deadline: %v", err)
	}
	if inconsistentDeadline != nil {
		t.Fatalf("inconsistent positive deposit must remain fail closed, got deadline=%v", inconsistentDeadline)
	}

	for _, name := range []string{"idx_rentals_active_expiry_queue", "idx_rentals_expired_finalization_queue"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname=current_schema() AND indexname=$1)`, name).Scan(&exists); err != nil || !exists {
			t.Fatalf("expected migration index %s: exists=%v err=%v", name, exists, err)
		}
	}

	if err := goose.Down(db, "."); err != nil {
		t.Fatalf("downgrade completion migration: %v", err)
	}
	var newColumnCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema=current_schema()
		  AND ((table_name='rentals' AND column_name IN ('completed_at','deposit_review_deadline_at'))
		    OR (table_name='deposit_holds' AND column_name IN ('settlement_source','settled_by_user_id','settlement_reason_code','settlement_evidence_ref')))`,
	).Scan(&newColumnCount); err != nil {
		t.Fatalf("inspect downgraded columns: %v", err)
	}
	if newColumnCount != 0 {
		t.Fatalf("expected completion migration down to remove all new columns, found %d", newColumnCount)
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

func TestDisableAccountIfIdle_PreservesExclusiveRentalConsistency(t *testing.T) {
	pool, _ := setupWorkersTestDB(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	repo := NewRepository(pool)

	_, reservedAccountID, waitingRentalID, _ := seedWaitingPaymentReservation(t, pool, now.Add(time.Hour))
	if err := repo.DisableAccountIfIdle(context.Background(), reservedAccountID); !errors.Is(err, ErrAccountLifecycleConflict) {
		t.Fatalf("reserved account disable err=%v want=%v", err, ErrAccountLifecycleConflict)
	}
	assertRentalAccountStatuses(t, pool, waitingRentalID, reservedAccountID, int16(rental.StatusWaitingPayment), int16(account.StatusReserved))

	_, rentedAccountID, activeRentalID, _ := seedActiveRental(t, pool, now.Add(time.Hour))
	if err := repo.DisableAccountIfIdle(context.Background(), rentedAccountID); !errors.Is(err, ErrAccountLifecycleConflict) {
		t.Fatalf("rented account disable err=%v want=%v", err, ErrAccountLifecycleConflict)
	}
	assertRentalAccountStatuses(t, pool, activeRentalID, rentedAccountID, int16(rental.StatusActive), int16(account.StatusRented))

	idleAccountID := atomic.AddInt64(&workersTestCounter, 10)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO accounts (id, login, encrypted_password, status, steam_guard_enabled, inventory_verified, hourly_price, deposit_amount, steam_id64, created_at, updated_at)
		VALUES ($1, 'idle-disable', $2, 2, true, true, 100, 0, $3, $4, $4)`, idleAccountID, []byte("enc-pass"), strconv.FormatInt(76561198000000000+idleAccountID, 10), now); err != nil {
		t.Fatalf("seed idle account: %v", err)
	}
	if err := repo.DisableAccountIfIdle(context.Background(), idleAccountID); err != nil {
		t.Fatalf("disable idle account: %v", err)
	}
	var disabledStatus int16
	var deletedAt *time.Time
	if err := pool.QueryRow(context.Background(), `SELECT status, deleted_at FROM accounts WHERE id=$1`, idleAccountID).Scan(&disabledStatus, &deletedAt); err != nil {
		t.Fatalf("load disabled idle account: %v", err)
	}
	if disabledStatus != int16(account.StatusDisabled) || deletedAt == nil {
		t.Fatalf("idle account not disabled: status=%d deleted_at=%v", disabledStatus, deletedAt)
	}
}

func assertRentalAccountStatuses(t *testing.T, pool *pkg_postgres_pool.ConnectionPool, rentalID, accountID int64, wantRental, wantAccount int16) {
	t.Helper()
	var rentalStatus, accountStatus int16
	if err := pool.QueryRow(context.Background(), `SELECT status FROM rentals WHERE id=$1`, rentalID).Scan(&rentalStatus); err != nil {
		t.Fatalf("load rental status: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT status FROM accounts WHERE id=$1`, accountID).Scan(&accountStatus); err != nil {
		t.Fatalf("load account status: %v", err)
	}
	if rentalStatus != wantRental || accountStatus != wantAccount {
		t.Fatalf("inconsistent lifecycle tuple: rental=%d want=%d account=%d want=%d", rentalStatus, wantRental, accountStatus, wantAccount)
	}
}

func TestExpireVsCredentialsRequest_DeniesAfterExpiration(t *testing.T) {
	pool, txManager := setupWorkersTestDB(t)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	userID, accountID, rentalID, _ := seedActiveRental(t, pool, now.Add(-time.Minute))
	repo := NewRepository(pool)

	changed, err := repo.ExpireRental(context.Background(), rentalID, accountID, now)
	if err != nil || !changed {
		t.Fatalf("expire failed: changed=%v err=%v", changed, err)
	}

	credentialsRepo := rental.NewPostgresRepository(pool.Pool)
	accountRepo := account.NewPostgresRepository(pool.Pool, "super-secret-32-byte-key-for-aes")
	service := rental.NewService(credentialsRepo, accountRepo, nil, nil, txManager)
	creds, err := service.GetRentalCredentials(context.Background(), userID, rentalID, rental.CredentialRequestContext{}, now.Add(time.Second))
	if !errors.Is(err, rental.ErrCredentialsNotAvailable) {
		t.Fatalf("expected credentials denial after committed expiration, got creds=%+v err=%v", creds, err)
	}
}

type blockingCredentialRepository struct {
	inner         rental.Repository
	eventInserted chan struct{}
	release       chan struct{}
	once          sync.Once
}

func (r *blockingCredentialRepository) CreateRental(ctx context.Context, value *rental.Rental) error {
	return r.inner.CreateRental(ctx, value)
}

func (r *blockingCredentialRepository) GetRental(ctx context.Context, id int64) (*rental.Rental, error) {
	return r.inner.GetRental(ctx, id)
}

func (r *blockingCredentialRepository) GetRentalCredentials(ctx context.Context, rentalID, userID int64, now time.Time) (*rental.RentalCredentialsRecord, error) {
	return r.inner.GetRentalCredentials(ctx, rentalID, userID, now)
}

func (r *blockingCredentialRepository) RecordCredentialIssued(ctx context.Context, event rental.CredentialIssueEvent) error {
	if err := r.inner.RecordCredentialIssued(ctx, event); err != nil {
		return err
	}
	r.once.Do(func() { close(r.eventInserted) })
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *blockingCredentialRepository) CancelWaitingPaymentRental(ctx context.Context, rentalID, userID int64, reason string, now time.Time) (bool, error) {
	return r.inner.CancelWaitingPaymentRental(ctx, rentalID, userID, reason, now)
}

func prepareEncryptedCredentials(t *testing.T, pool *pkg_postgres_pool.ConnectionPool, accountID int64) *account.PostgresRepository {
	t.Helper()
	repo := account.NewPostgresRepository(pool.Pool, "super-secret-32-byte-key-for-aes")
	encrypted, err := repo.Encrypt("credential-race-password")
	if err != nil {
		t.Fatalf("encrypt race credential: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "UPDATE accounts SET encrypted_password = $1 WHERE id = $2", encrypted, accountID); err != nil {
		t.Fatalf("store encrypted race credential: %v", err)
	}
	return repo
}

func TestCredentialIssuanceBeforeCleanupIsSerialized(t *testing.T) {
	pool, txManager := setupWorkersTestDB(t)
	boundary := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	userID, accountID, rentalID, _ := seedActiveRental(t, pool, boundary)
	accountRepo := prepareEncryptedCredentials(t, pool, accountID)
	inner := rental.NewPostgresRepository(pool.Pool)
	blockingRepo := &blockingCredentialRepository{
		inner:         inner,
		eventInserted: make(chan struct{}),
		release:       make(chan struct{}),
	}
	service := rental.NewService(blockingRepo, accountRepo, nil, nil, txManager)
	cleanupRepo := NewRepository(pool)

	credentialResult := make(chan *rental.RentalCredentials, 1)
	credentialErr := make(chan error, 1)
	go func() {
		creds, err := service.GetRentalCredentials(context.Background(), userID, rentalID, rental.CredentialRequestContext{}, boundary.Add(-time.Second))
		credentialResult <- creds
		credentialErr <- err
	}()

	select {
	case <-blockingRepo.eventInserted:
	case err := <-credentialErr:
		t.Fatalf("credential issuance failed before transactional audit insert: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("credential issuance did not reach transactional audit insert")
	}

	changed, err := cleanupRepo.ExpireRental(context.Background(), rentalID, accountID, boundary.Add(time.Second))
	if err != nil {
		t.Fatalf("cleanup while credential transaction holds locks failed: %v", err)
	}
	if changed {
		t.Fatal("cleanup bypassed credential transaction row locks")
	}

	close(blockingRepo.release)
	if err := <-credentialErr; err != nil {
		t.Fatalf("credential issuance failed: %v", err)
	}
	if creds := <-credentialResult; creds == nil || creds.Password != "credential-race-password" {
		t.Fatalf("unexpected credential result: %+v", creds)
	}

	changed, err = cleanupRepo.ExpireRental(context.Background(), rentalID, accountID, boundary.Add(time.Second))
	if err != nil || !changed {
		t.Fatalf("cleanup did not expire rental after credential commit: changed=%v err=%v", changed, err)
	}

	var issuedEvents, expiredEvents int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FILTER (WHERE event_type = 7), COUNT(*) FILTER (WHERE event_type = 10) FROM security_events WHERE rental_id = $1`, rentalID).Scan(&issuedEvents, &expiredEvents); err != nil {
		t.Fatalf("load serialized security events: %v", err)
	}
	if issuedEvents != 1 || expiredEvents != 1 {
		t.Fatalf("unexpected serialized security events: issued=%d expired=%d", issuedEvents, expiredEvents)
	}
}

func TestConcurrentCleanupVersusCredentialIssuanceHasNoStaleDisclosure(t *testing.T) {
	pool, txManager := setupWorkersTestDB(t)
	boundary := time.Date(2026, 7, 13, 13, 0, 0, 0, time.UTC)
	userID, accountID, rentalID, _ := seedActiveRental(t, pool, boundary)
	accountRepo := prepareEncryptedCredentials(t, pool, accountID)
	service := rental.NewService(rental.NewPostgresRepository(pool.Pool), accountRepo, nil, nil, txManager)
	cleanupRepo := NewRepository(pool)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var creds *rental.RentalCredentials
	var credentialErr error
	var cleanupChanged bool
	var cleanupErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		creds, credentialErr = service.GetRentalCredentials(context.Background(), userID, rentalID, rental.CredentialRequestContext{}, boundary.Add(-time.Second))
	}()
	go func() {
		defer wg.Done()
		<-start
		cleanupChanged, cleanupErr = cleanupRepo.ExpireRental(context.Background(), rentalID, accountID, boundary.Add(time.Second))
	}()
	close(start)
	wg.Wait()

	if cleanupErr != nil {
		t.Fatalf("concurrent cleanup failed: %v", cleanupErr)
	}
	if cleanupChanged {
		if !errors.Is(credentialErr, rental.ErrCredentialsNotAvailable) || creds != nil {
			t.Fatalf("cleanup committed first but credentials were disclosed: creds=%+v err=%v", creds, credentialErr)
		}
	} else {
		if credentialErr != nil || creds == nil {
			t.Fatalf("credential transaction won locks but did not issue consistently: creds=%+v err=%v", creds, credentialErr)
		}
		changed, err := cleanupRepo.ExpireRental(context.Background(), rentalID, accountID, boundary.Add(time.Second))
		if err != nil || !changed {
			t.Fatalf("follow-up cleanup failed after credential commit: changed=%v err=%v", changed, err)
		}
	}

	var rentalStatus int16
	var issuedEvents int
	if err := pool.QueryRow(context.Background(), "SELECT status FROM rentals WHERE id = $1", rentalID).Scan(&rentalStatus); err != nil {
		t.Fatalf("load final rental status: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM security_events WHERE rental_id = $1 AND event_type = 7", rentalID).Scan(&issuedEvents); err != nil {
		t.Fatalf("load credential event count: %v", err)
	}
	if rentalStatus != int16(rental.StatusExpired) {
		t.Fatalf("expected final expired rental, got %d", rentalStatus)
	}
	if (creds == nil && issuedEvents != 0) || (creds != nil && issuedEvents != 1) {
		t.Fatalf("credential disclosure and audit disagree: creds=%+v issued_events=%d", creds, issuedEvents)
	}
}

func TestCredentialAuditInsertFailureRollsBackAndDisclosesNothing(t *testing.T) {
	pool, txManager := setupWorkersTestDB(t)
	now := time.Date(2026, 7, 13, 14, 0, 0, 0, time.UTC)
	userID, accountID, rentalID, _ := seedActiveRental(t, pool, now.Add(time.Hour))
	accountRepo := prepareEncryptedCredentials(t, pool, accountID)

	if _, err := pool.Exec(context.Background(), `
		CREATE OR REPLACE FUNCTION reject_credential_issue_event() RETURNS trigger AS $$
		BEGIN
			IF NEW.event_type = 7 THEN
				RAISE EXCEPTION 'credential audit rejected for test';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`); err != nil {
		t.Fatalf("install credential audit failure function: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DROP TRIGGER IF EXISTS trg_reject_credential_issue_event ON security_events"); err != nil {
		t.Fatalf("drop stale credential audit failure trigger: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `CREATE TRIGGER trg_reject_credential_issue_event
		BEFORE INSERT ON security_events
		FOR EACH ROW EXECUTE FUNCTION reject_credential_issue_event()`); err != nil {
		t.Fatalf("install credential audit failure trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP TRIGGER IF EXISTS trg_reject_credential_issue_event ON security_events")
		_, _ = pool.Exec(context.Background(), "DROP FUNCTION IF EXISTS reject_credential_issue_event()")
	})

	service := rental.NewService(rental.NewPostgresRepository(pool.Pool), accountRepo, nil, nil, txManager)
	creds, err := service.GetRentalCredentials(context.Background(), userID, rentalID, rental.CredentialRequestContext{}, now)
	if err == nil {
		t.Fatalf("expected credential audit failure, got credentials=%+v", creds)
	}
	if creds != nil {
		t.Fatalf("credential audit failure disclosed credentials: %+v", creds)
	}

	var eventCount int
	var rentalStatus int16
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM security_events WHERE rental_id = $1 AND event_type = 7", rentalID).Scan(&eventCount); err != nil {
		t.Fatalf("load credential audit events: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT status FROM rentals WHERE id = $1", rentalID).Scan(&rentalStatus); err != nil {
		t.Fatalf("load rental after audit rollback: %v", err)
	}
	if eventCount != 0 || rentalStatus != int16(rental.StatusActive) {
		t.Fatalf("audit failure did not roll back cleanly: events=%d rental_status=%d", eventCount, rentalStatus)
	}
}

func containsSecret(metadata string) bool {
	return strings.Contains(metadata, "password") || strings.Contains(metadata, "steam_id64") || strings.Contains(metadata, "token") || strings.Contains(metadata, "key")
}
