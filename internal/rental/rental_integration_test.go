package rental_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	"rent_game_accs/internal/account"
	"rent_game_accs/internal/payment"
	pkg_postgres_pool "rent_game_accs/internal/pkg/repository/postgres/pool"
	"rent_game_accs/internal/rental"
	"rent_game_accs/internal/shared/database"
	"rent_game_accs/internal/user"
	"rent_game_accs/migrations"
)

func setupTestDB(t *testing.T) (*pgxpool.Pool, database.TxManager) {
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
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM account_games")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM accounts")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM games")
	_, _ = poolConn.Pool.Exec(ctx, "DELETE FROM users")

	txManager := database.NewTxManager(poolConn.Pool)
	return poolConn.Pool, txManager
}

func TestRentalService_RentAccount_Concurrency(t *testing.T) {
	pool, txManager := setupTestDB(t)
	ctx := context.Background()

	gameID := int64(123)
	_, err := pool.Exec(ctx, `INSERT INTO games (
		id, name, steam_app_id, 
		short_description, header_image, developers, publishers, genres
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		gameID, "Half-Life 3", 12345,
		"", "", []byte("[]"), []byte("[]"), []byte("[]"))
	if err != nil {
		t.Fatalf("failed to insert test game: %v", err)
	}

	const numRequests = 10
	userIDs := make([]int64, numRequests)
	for i := 0; i < numRequests; i++ {
		userIDs[i] = int64(900 + i)
		_, err = pool.Exec(ctx, "INSERT INTO users (id, email, password_hash, balance) VALUES ($1, $2, $3, 10000)",
			userIDs[i], fmt.Sprintf("user%d@example.com", i), "hash")
		if err != nil {
			t.Fatalf("failed to insert test user %d: %v", i, err)
		}
	}

	accountRepo := account.NewPostgresRepository(pool, "super-secret-32-byte-key-for-aes")
	creds, err := account.NewSteamCredentials("steam_login_1", []byte("enc_pass_1"), "12345678901234567")
	if err != nil {
		t.Fatalf("failed to create credentials: %v", err)
	}

	price, err := account.NewMoney(200, "USD")
	if err != nil {
		t.Fatalf("failed to create money: %v", err)
	}

	deposit, err := account.NewMoney(1000, "USD")
	if err != nil {
		t.Fatalf("failed to create money: %v", err)
	}

	acc, err := account.NewAccount(creds, price, deposit, time.Now())
	if err != nil {
		t.Fatalf("failed to create account entity: %v", err)
	}
	acc.ID = 555
	acc.MarkSecurityChecked(true, true, time.Now())
	acc.SyncLibrary([]account.AccountGame{
		{
			Game: account.Game{
				ID:         gameID,
				SteamAppID: 12345,
				Name:       "Half-Life 3",
			},
			PlaytimeMinutes: 100,
		},
	}, time.Now())

	if err := acc.Publish(time.Now()); err != nil {
		t.Fatalf("failed to publish account: %v", err)
	}

	if err := accountRepo.CreateAccount(ctx, acc); err != nil {
		t.Fatalf("failed to insert game account: %v", err)
	}

	rentalRepo := rental.NewPostgresRepository(pool)
	paymentRepo := payment.NewPostgresRepository(pool)
	userRepo := user.NewPostgresRepository(pool)
	rentalService := rental.NewService(rentalRepo, accountRepo, userRepo, paymentRepo, txManager)

	var wg sync.WaitGroup
	startChan := make(chan struct{})

	var successCount int
	var failureCount int
	var countMu sync.Mutex
	errChan := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(uid int64) {
			defer wg.Done()

			<-startChan

			_, rErr := rentalService.RentAccount(context.Background(), uid, acc.ID, 2*time.Hour, time.Now())

			countMu.Lock()
			defer countMu.Unlock()
			if rErr != nil {
				failureCount++
				errChan <- rErr
			} else {
				successCount++
			}
		}(userIDs[i])
	}

	close(startChan)

	wg.Wait()
	close(errChan)

	t.Logf("Total requests: %d, Successes: %d, Failures: %d", numRequests, successCount, failureCount)

	if successCount != 1 {
		t.Errorf("expected exactly 1 successful rental, got %d", successCount)
	}

	if failureCount != numRequests-1 {
		t.Errorf("expected %d failed rentals, got %d", numRequests-1, failureCount)
	}

	for rErr := range errChan {
		if !errors.Is(rErr, rental.ErrAccountNotAvailable) {
			t.Errorf("expected error %v, got %v", rental.ErrAccountNotAvailable, rErr)
		}
	}

	_, err = rentalService.RentAccount(ctx, userIDs[0], acc.ID, 2*time.Hour, time.Now())
	if !errors.Is(err, rental.ErrAccountNotAvailable) {
		t.Fatalf("expected sequential second reservation to fail with availability conflict, got %v", err)
	}

	updatedAcc, err := accountRepo.GetAccount(ctx, acc.ID)
	if err != nil {
		t.Fatalf("failed to fetch updated account status: %v", err)
	}

	if updatedAcc.Status != account.StatusReserved {
		t.Errorf("expected account status to be Reserved (%v), got %v", account.StatusReserved, updatedAcc.Status)
	}

	rows, err := pool.Query(ctx, "SELECT id, user_id, status FROM rentals")
	if err != nil {
		t.Fatalf("failed to query rentals: %v", err)
	}
	defer rows.Close()

	var dbRentalCount int
	var rentalStatus int16
	for rows.Next() {
		var id, uid int64
		if err := rows.Scan(&id, &uid, &rentalStatus); err != nil {
			t.Fatalf("failed to scan rental row: %v", err)
		}
		dbRentalCount++
	}
	if dbRentalCount != 1 {
		t.Errorf("expected exactly 1 rental in database, got %d", dbRentalCount)
	}
	if rentalStatus != 1 {
		t.Errorf("expected rental status 1 (WaitingPayment), got %d", rentalStatus)
	}

	var paymentCount int
	var paymentStatus int16
	err = pool.QueryRow(ctx, "SELECT COUNT(*), MIN(status) FROM payments").Scan(&paymentCount, &paymentStatus)
	if err != nil {
		t.Fatalf("failed to query payments: %v", err)
	}
	if paymentCount != 1 {
		t.Errorf("expected exactly 1 payment in database, got %d", paymentCount)
	}
	if paymentStatus != 1 {
		t.Errorf("expected payment status 1 (Pending/Waiting), got %d", paymentStatus)
	}
}

type failingPaymentRepo struct{}

func (f failingPaymentRepo) CreatePendingPayment(ctx context.Context, rentalID, userID int64, amount int64, currency string) (int64, error) {
	return 0, errors.New("payment insert failed")
}

func TestRentalService_RentAccount_PaymentFailureRollback(t *testing.T) {
	pool, txManager := setupTestDB(t)
	ctx := context.Background()

	gameID := int64(777)
	_, err := pool.Exec(ctx, `INSERT INTO games (
		id, name, steam_app_id, short_description, header_image, developers, publishers, genres
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		gameID, "Portal 3", 77777, "", "", []byte("[]"), []byte("[]"), []byte("[]"))
	if err != nil {
		t.Fatalf("failed to insert test game: %v", err)
	}

	_, err = pool.Exec(ctx, "INSERT INTO users (id, email, password_hash, balance) VALUES ($1, $2, $3, 10000)", 7001, "rollback@example.com", "hash")
	if err != nil {
		t.Fatalf("failed to insert test user: %v", err)
	}

	accountRepo := account.NewPostgresRepository(pool, "super-secret-32-byte-key-for-aes")
	creds, err := account.NewSteamCredentials("steam_login_rb", []byte("enc_pass_rb"), "12345678901234568")
	if err != nil {
		t.Fatalf("failed to create credentials: %v", err)
	}
	price, _ := account.NewMoney(250, "USD")
	deposit, _ := account.NewMoney(500, "USD")
	acc, err := account.NewAccount(creds, price, deposit, time.Now())
	if err != nil {
		t.Fatalf("failed to create account entity: %v", err)
	}
	acc.ID = 8888
	acc.MarkSecurityChecked(true, true, time.Now())
	acc.SyncLibrary([]account.AccountGame{{Game: account.Game{ID: gameID, SteamAppID: 77777, Name: "Portal 3"}, PlaytimeMinutes: 10}}, time.Now())
	if err := acc.Publish(time.Now()); err != nil {
		t.Fatalf("failed to publish account: %v", err)
	}
	if err := accountRepo.CreateAccount(ctx, acc); err != nil {
		t.Fatalf("failed to save account: %v", err)
	}

	rentalRepo := rental.NewPostgresRepository(pool)
	userRepo := user.NewPostgresRepository(pool)
	rentalService := rental.NewService(rentalRepo, accountRepo, userRepo, failingPaymentRepo{}, txManager)

	_, err = rentalService.RentAccount(ctx, 7001, acc.ID, 2*time.Hour, time.Now())
	if err == nil {
		t.Fatalf("expected payment failure to bubble up")
	}

	var rentalCount, paymentCount int
	var accountStatus int16
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM rentals").Scan(&rentalCount)
	if err != nil {
		t.Fatalf("failed to query rentals count: %v", err)
	}
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM payments").Scan(&paymentCount)
	if err != nil {
		t.Fatalf("failed to query payments count: %v", err)
	}
	err = pool.QueryRow(ctx, "SELECT status FROM accounts WHERE id = $1", acc.ID).Scan(&accountStatus)
	if err != nil {
		t.Fatalf("failed to query account status: %v", err)
	}

	if rentalCount != 0 || paymentCount != 0 {
		t.Fatalf("expected rollback to leave zero rentals/payments, got rentals=%d payments=%d", rentalCount, paymentCount)
	}
	if accountStatus != int16(account.StatusAvailable) {
		t.Fatalf("expected account status to remain Available, got %d", accountStatus)
	}
}

func TestRentalService_RentAccount_ActiveRentalBlocksNewReservation(t *testing.T) {
	pool, txManager := setupTestDB(t)
	ctx := context.Background()

	gameID := int64(900)
	_, err := pool.Exec(ctx, `INSERT INTO games (
		id, name, steam_app_id, short_description, header_image, developers, publishers, genres
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		gameID, "Dota 3", 90000, "", "", []byte("[]"), []byte("[]"), []byte("[]"))
	if err != nil {
		t.Fatalf("failed to insert test game: %v", err)
	}

	_, err = pool.Exec(ctx, "INSERT INTO users (id, email, password_hash, balance) VALUES ($1, $2, $3, 10000)", 8001, "active@example.com", "hash")
	if err != nil {
		t.Fatalf("failed to insert test user: %v", err)
	}

	accountRepo := account.NewPostgresRepository(pool, "super-secret-32-byte-key-for-aes")
	creds, err := account.NewSteamCredentials("steam_login_active", []byte("enc_pass_active"), "12345678901234569")
	if err != nil {
		t.Fatalf("failed to create credentials: %v", err)
	}
	price, _ := account.NewMoney(250, "USD")
	deposit, _ := account.NewMoney(500, "USD")
	acc, err := account.NewAccount(creds, price, deposit, time.Now())
	if err != nil {
		t.Fatalf("failed to create account entity: %v", err)
	}
	acc.ID = 9999
	acc.MarkSecurityChecked(true, true, time.Now())
	acc.SyncLibrary([]account.AccountGame{{Game: account.Game{ID: gameID, SteamAppID: 90000, Name: "Dota 3"}, PlaytimeMinutes: 10}}, time.Now())
	if err := acc.Publish(time.Now()); err != nil {
		t.Fatalf("failed to publish account: %v", err)
	}
	if err := accountRepo.CreateAccount(ctx, acc); err != nil {
		t.Fatalf("failed to save account: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO rentals (id, user_id, account_id, status, start_at, end_at, rental_price, deposit_amount)
		VALUES ($1, $2, $3, 2, NOW(), NOW() + INTERVAL '2 hours', 500, 500)`, 1, 8001, acc.ID)
	if err != nil {
		t.Fatalf("failed to seed active rental: %v", err)
	}
	_, err = pool.Exec(ctx, "UPDATE accounts SET status = 4 WHERE id = $1", acc.ID)
	if err != nil {
		t.Fatalf("failed to mark account rented: %v", err)
	}

	rentalRepo := rental.NewPostgresRepository(pool)
	paymentRepo := payment.NewPostgresRepository(pool)
	userRepo := user.NewPostgresRepository(pool)
	rentalService := rental.NewService(rentalRepo, accountRepo, userRepo, paymentRepo, txManager)

	_, err = rentalService.RentAccount(ctx, 8001, acc.ID, 2*time.Hour, time.Now())
	if !errors.Is(err, rental.ErrAccountNotAvailable) {
		t.Fatalf("expected active rental to block new reservation, got %v", err)
	}
}

func TestRentalService_RentAccount_WaitingPaymentRentalBlocksNewReservation(t *testing.T) {
	pool, txManager := setupTestDB(t)
	ctx := context.Background()

	gameID := int64(901)
	_, err := pool.Exec(ctx, `INSERT INTO games (
		id, name, steam_app_id, short_description, header_image, developers, publishers, genres
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		gameID, "Left 4 Dead 3", 90100, "", "", []byte("[]"), []byte("[]"), []byte("[]"))
	if err != nil {
		t.Fatalf("failed to insert test game: %v", err)
	}

	_, err = pool.Exec(ctx, "INSERT INTO users (id, email, password_hash, balance) VALUES ($1, $2, $3, 10000)", 8002, "waiting@example.com", "hash")
	if err != nil {
		t.Fatalf("failed to insert test user: %v", err)
	}

	accountRepo := account.NewPostgresRepository(pool, "super-secret-32-byte-key-for-aes")
	creds, err := account.NewSteamCredentials("steam_login_waiting", []byte("enc_pass_waiting"), "12345678901234570")
	if err != nil {
		t.Fatalf("failed to create credentials: %v", err)
	}
	price, _ := account.NewMoney(250, "USD")
	deposit, _ := account.NewMoney(500, "USD")
	acc, err := account.NewAccount(creds, price, deposit, time.Now())
	if err != nil {
		t.Fatalf("failed to create account entity: %v", err)
	}
	acc.ID = 10001
	acc.MarkSecurityChecked(true, true, time.Now())
	acc.SyncLibrary([]account.AccountGame{{Game: account.Game{ID: gameID, SteamAppID: 90100, Name: "Left 4 Dead 3"}, PlaytimeMinutes: 10}}, time.Now())
	if err := acc.Publish(time.Now()); err != nil {
		t.Fatalf("failed to publish account: %v", err)
	}
	if err := accountRepo.CreateAccount(ctx, acc); err != nil {
		t.Fatalf("failed to save account: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO rentals (id, user_id, account_id, status, start_at, end_at, rental_price, deposit_amount)
		VALUES ($1, $2, $3, 1, NOW(), NOW() + INTERVAL '2 hours', 500, 500)`, 2, 8002, acc.ID)
	if err != nil {
		t.Fatalf("failed to seed waiting rental: %v", err)
	}

	rentalRepo := rental.NewPostgresRepository(pool)
	paymentRepo := payment.NewPostgresRepository(pool)
	userRepo := user.NewPostgresRepository(pool)
	rentalService := rental.NewService(rentalRepo, accountRepo, userRepo, paymentRepo, txManager)

	_, err = rentalService.RentAccount(ctx, 8002, acc.ID, 2*time.Hour, time.Now())
	if !errors.Is(err, rental.ErrAccountNotAvailable) {
		t.Fatalf("expected waiting payment rental to block new reservation, got %v", err)
	}
}

func seedWaitingPaymentRental(t *testing.T, pool *pgxpool.Pool, userID, accountID, rentalID, paymentID int64) {
	t.Helper()
	seedWaitingPaymentRentalWithAmounts(t, pool, userID, accountID, rentalID, paymentID, 500, 500)
}

func seedWaitingPaymentRentalWithAmounts(t *testing.T, pool *pgxpool.Pool, userID, accountID, rentalID, paymentID int64, rentalPrice, depositAmount int64) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	_, err := pool.Exec(ctx, "INSERT INTO users (id, email, password_hash, balance) VALUES ($1, $2, $3, 10000)", userID, fmt.Sprintf("cancel-%d@example.com", userID), "hash")
	if err != nil {
		t.Fatalf("failed to insert user: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, login, encrypted_password, status, steam_guard_enabled, inventory_verified, hourly_price, deposit_amount, steam_id64, created_at, updated_at)
		VALUES ($1, $2, $3, 3, true, true, 250, $4, $5, $6, $6)`,
		accountID, fmt.Sprintf("cancel_login_%d", accountID), []byte("enc-pass"), depositAmount, fmt.Sprintf("76561198000%d", accountID), now)
	if err != nil {
		t.Fatalf("failed to insert account: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO rentals (id, user_id, account_id, status, start_at, end_at, rental_price, deposit_amount, payment_expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, 1, $4, $5, $6, $7, $8, $4, $4)`,
		rentalID, userID, accountID, now, now.Add(2*time.Hour), rentalPrice, depositAmount, now.Add(30*time.Minute))
	if err != nil {
		t.Fatalf("failed to insert rental: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO payments (id, rental_id, user_id, payment_type, provider, status, amount, currency)
		VALUES ($1, $2, $3, 1, 'internal', 1, $4, 'USD')`,
		paymentID, rentalID, userID, rentalPrice+depositAmount)
	if err != nil {
		t.Fatalf("failed to insert payment: %v", err)
	}
}

func seedWaitingPaymentRentalWithBalance(t *testing.T, pool *pgxpool.Pool, userID, accountID, rentalID, paymentID int64, balance, rentalPrice, depositAmount int64) {
	t.Helper()
	seedWaitingPaymentRentalWithAmounts(t, pool, userID, accountID, rentalID, paymentID, rentalPrice, depositAmount)
	if _, err := pool.Exec(context.Background(), "UPDATE users SET balance = $1 WHERE id = $2", balance, userID); err != nil {
		t.Fatalf("failed to update user balance: %v", err)
	}
}

func TestRentalService_CancelWaitingPaymentLifecycle(t *testing.T) {
	pool, txManager := setupTestDB(t)
	userID, accountID, rentalID, paymentID := int64(9101), int64(9102), int64(9103), int64(9104)
	seedWaitingPaymentRental(t, pool, userID, accountID, rentalID, paymentID)

	accountRepo := account.NewPostgresRepository(pool, "super-secret-32-byte-key-for-aes")
	rentalRepo := rental.NewPostgresRepository(pool)
	userRepo := user.NewPostgresRepository(pool)
	paymentRepo := payment.NewPostgresRepository(pool)
	service := rental.NewService(rentalRepo, accountRepo, userRepo, paymentRepo, txManager)

	result, err := service.CancelRental(context.Background(), userID, rentalID, "buyer changed mind", time.Date(2026, 7, 5, 12, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CancelRental failed: %v", err)
	}
	if result == nil || !result.Changed {
		t.Fatalf("expected first cancel to change state, got %+v", result)
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
	if rentalStatus != int16(rental.StatusCancelled) {
		t.Fatalf("expected rental CANCELLED, got %d", rentalStatus)
	}
	if paymentStatus != 3 {
		t.Fatalf("expected pending payment FAILED, got %d", paymentStatus)
	}
	if accountStatus != int16(account.StatusAvailable) {
		t.Fatalf("expected account AVAILABLE, got %d", accountStatus)
	}

	creds, err := service.GetRentalCredentials(context.Background(), userID, rentalID, time.Date(2026, 7, 5, 12, 6, 0, 0, time.UTC))
	if !errors.Is(err, rental.ErrCredentialsNotAvailable) {
		t.Fatalf("expected credentials denial after cancel, got creds=%+v err=%v", creds, err)
	}

	result, err = service.CancelRental(context.Background(), userID, rentalID, "buyer changed mind", time.Date(2026, 7, 5, 12, 7, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("repeated CancelRental should be no-op: %v", err)
	}
	if result == nil || result.Changed {
		t.Fatalf("expected repeated cancel no-op, got %+v", result)
	}
	var eventCount int
	err = pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM security_events WHERE rental_id = $1", rentalID).Scan(&eventCount)
	if err != nil {
		t.Fatalf("failed to count security events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected one cancel security event, got %d", eventCount)
	}
}

func TestRentalService_CancelVsWebhookRaceStaysConsistent(t *testing.T) {
	pool, txManager := setupTestDB(t)
	userID, accountID, rentalID, paymentID := int64(9201), int64(9202), int64(9203), int64(9204)
	seedWaitingPaymentRental(t, pool, userID, accountID, rentalID, paymentID)

	accountRepo := account.NewPostgresRepository(pool, "super-secret-32-byte-key-for-aes")
	rentalRepo := rental.NewPostgresRepository(pool)
	userRepo := user.NewPostgresRepository(pool)
	paymentRepo := payment.NewPostgresRepository(pool)
	rentalService := rental.NewService(rentalRepo, accountRepo, userRepo, paymentRepo, txManager)
	paymentService := payment.NewPaymentService(paymentRepo)

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	var cancelErr, webhookErr error
	go func() {
		defer wg.Done()
		<-start
		_, cancelErr = rentalService.CancelRental(context.Background(), userID, rentalID, "race cancel", time.Now())
	}()
	go func() {
		defer wg.Done()
		<-start
		_, webhookErr = paymentService.ProcessWebhook(context.Background(), payment.WebhookRequest{
			PaymentID:             fmt.Sprintf("%d", paymentID),
			ExternalTransactionID: "cancel-race-ext",
			Status:                "success",
		}, "127.0.0.1", "test")
	}()
	close(start)
	wg.Wait()

	if cancelErr != nil && !errors.Is(cancelErr, rental.ErrCannotCancel) {
		t.Fatalf("unexpected cancel error: %v", cancelErr)
	}
	if webhookErr != nil && !errors.Is(webhookErr, payment.ErrWebhookInvalidTransition) && !errors.Is(webhookErr, payment.ErrWebhookNotSuccessful) {
		t.Fatalf("unexpected webhook error: %v", webhookErr)
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

	consistentCancel := rentalStatus == int16(rental.StatusCancelled) && paymentStatus == 3 && accountStatus == int16(account.StatusAvailable)
	consistentWebhook := rentalStatus == int16(rental.StatusActive) && paymentStatus == 2 && accountStatus == int16(account.StatusRented)
	if !consistentCancel && !consistentWebhook {
		t.Fatalf("inconsistent final state: rental=%d payment=%d account=%d cancelErr=%v webhookErr=%v", rentalStatus, paymentStatus, accountStatus, cancelErr, webhookErr)
	}
}

func TestPaymentWebhookCreatesLedgerAndDepositHold(t *testing.T) {
	pool, _ := setupTestDB(t)
	userID, accountID, rentalID, paymentID := int64(9401), int64(9402), int64(9403), int64(9404)
	seedWaitingPaymentRental(t, pool, userID, accountID, rentalID, paymentID)

	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))
	_, err := paymentService.ProcessWebhook(context.Background(), payment.WebhookRequest{
		PaymentID:             fmt.Sprintf("%d", paymentID),
		ExternalTransactionID: "ledger-ext-1",
		Status:                "success",
	}, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("ProcessWebhook failed: %v", err)
	}

	var providerEntries, depositEntries, depositHolds int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM financial_ledger_entries WHERE payment_id = $1 AND entry_type = 1`, paymentID).Scan(&providerEntries); err != nil {
		t.Fatalf("failed to count provider ledger entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM financial_ledger_entries WHERE payment_id = $1 AND entry_type = 2`, paymentID).Scan(&depositEntries); err != nil {
		t.Fatalf("failed to count deposit ledger entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM deposit_holds WHERE payment_id = $1 AND status = 1`, paymentID).Scan(&depositHolds); err != nil {
		t.Fatalf("failed to count deposit holds: %v", err)
	}
	if providerEntries != 1 || depositEntries != 1 || depositHolds != 1 {
		t.Fatalf("expected one provider entry, one deposit entry and one hold, got provider=%d deposit=%d holds=%d", providerEntries, depositEntries, depositHolds)
	}

	var amount int64
	var provider, externalTx, idempotencyKey string
	if err := pool.QueryRow(context.Background(), `
		SELECT amount, provider, external_transaction_id, idempotency_key
		FROM financial_ledger_entries
		WHERE payment_id = $1 AND entry_type = 1`, paymentID).Scan(&amount, &provider, &externalTx, &idempotencyKey); err != nil {
		t.Fatalf("failed to load provider ledger entry: %v", err)
	}
	if amount != 1000 || provider != "internal" || externalTx != "ledger-ext-1" || idempotencyKey != "payment:webhook:internal:ledger-ext-1" {
		t.Fatalf("unexpected provider ledger entry amount=%d provider=%q ext=%q key=%q", amount, provider, externalTx, idempotencyKey)
	}

	if _, err = paymentService.ProcessWebhook(context.Background(), payment.WebhookRequest{
		PaymentID:             fmt.Sprintf("%d", paymentID),
		ExternalTransactionID: "ledger-ext-1",
		Status:                "success",
	}, "127.0.0.1", "test"); err != nil {
		t.Fatalf("duplicate webhook should be idempotent: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM financial_ledger_entries WHERE payment_id = $1`, paymentID).Scan(&providerEntries); err != nil {
		t.Fatalf("failed to count ledger entries after replay: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM deposit_holds WHERE payment_id = $1`, paymentID).Scan(&depositHolds); err != nil {
		t.Fatalf("failed to count deposit holds after replay: %v", err)
	}
	if providerEntries != 2 || depositHolds != 1 {
		t.Fatalf("expected replay to avoid duplicates, got ledger=%d holds=%d", providerEntries, depositHolds)
	}
}

func TestPaymentCardinalityRejectsSecondPaymentForRental(t *testing.T) {
	pool, _ := setupTestDB(t)
	userID, accountID, rentalID, paymentID := int64(9411), int64(9412), int64(9413), int64(9414)
	seedWaitingPaymentRental(t, pool, userID, accountID, rentalID, paymentID)

	_, err := pool.Exec(context.Background(), `
		INSERT INTO payments (rental_id, user_id, payment_type, provider, status, amount, currency)
		VALUES ($1, $2, 1, 'internal', 1, 1000, 'USD')`,
		rentalID, userID,
	)
	if err == nil {
		t.Fatalf("expected second payment insert to fail")
	}

	var paymentCount int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM payments WHERE rental_id = $1`, rentalID).Scan(&paymentCount); err != nil {
		t.Fatalf("failed to count payments: %v", err)
	}
	if paymentCount != 1 {
		t.Fatalf("expected exactly one payment for rental, got %d", paymentCount)
	}
}

func TestPaymentWebhookUnknownExternalTransactionDoesNotMutateState(t *testing.T) {
	pool, _ := setupTestDB(t)
	userID, accountID, rentalID, paymentID := int64(9421), int64(9422), int64(9423), int64(9424)
	seedWaitingPaymentRental(t, pool, userID, accountID, rentalID, paymentID)

	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))
	_, err := paymentService.ProcessWebhook(context.Background(), payment.WebhookRequest{
		ExternalTransactionID: "missing-external-tx",
		Status:                "success",
	}, "127.0.0.1", "test")
	if !errors.Is(err, payment.ErrPaymentNotFound) {
		t.Fatalf("expected unknown external transaction to return ErrPaymentNotFound, got %v", err)
	}

	var rentalStatus, paymentStatus, accountStatus int16
	var ledgerCount, holdCount int
	if err := pool.QueryRow(context.Background(), `SELECT status FROM rentals WHERE id = $1`, rentalID).Scan(&rentalStatus); err != nil {
		t.Fatalf("failed to read rental status: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT status FROM payments WHERE id = $1`, paymentID).Scan(&paymentStatus); err != nil {
		t.Fatalf("failed to read payment status: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT status FROM accounts WHERE id = $1`, accountID).Scan(&accountStatus); err != nil {
		t.Fatalf("failed to read account status: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1`, rentalID).Scan(&ledgerCount); err != nil {
		t.Fatalf("failed to count ledger rows: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM deposit_holds WHERE rental_id = $1`, rentalID).Scan(&holdCount); err != nil {
		t.Fatalf("failed to count deposit holds: %v", err)
	}
	if rentalStatus != int16(rental.StatusWaitingPayment) || paymentStatus != 1 || accountStatus != int16(account.StatusReserved) {
		t.Fatalf("unknown external transaction mutated state rental=%d payment=%d account=%d", rentalStatus, paymentStatus, accountStatus)
	}
	if ledgerCount != 0 || holdCount != 0 {
		t.Fatalf("unknown external transaction created financial side effects ledger=%d holds=%d", ledgerCount, holdCount)
	}
}

func TestPaymentWebhookExternalTransactionLookupDoesNotTouchWalletPaidRental(t *testing.T) {
	pool, _ := setupTestDB(t)
	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))
	userID, accountID, rentalID, paymentID := int64(9431), int64(9432), int64(9433), int64(9434)
	seedWaitingPaymentRentalWithBalance(t, pool, userID, accountID, rentalID, paymentID, 10000, 500, 500)

	if _, err := paymentService.PayRentalWithBalance(context.Background(), userID, rentalID, "127.0.0.1", "test", time.Now().UTC()); err != nil {
		t.Fatalf("wallet payment failed: %v", err)
	}

	_, err := paymentService.ProcessWebhook(context.Background(), payment.WebhookRequest{
		ExternalTransactionID: "provider-should-not-match-wallet",
		Status:                "success",
	}, "127.0.0.1", "test")
	if !errors.Is(err, payment.ErrPaymentNotFound) {
		t.Fatalf("expected provider lookup to ignore wallet-paid rental, got %v", err)
	}

	var balance int64
	var provider string
	var providerLedgerCount, balanceLedgerCount, depositHoldCount int
	if err := pool.QueryRow(context.Background(), `SELECT balance FROM users WHERE id = $1`, userID).Scan(&balance); err != nil {
		t.Fatalf("failed to read user balance: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT provider FROM payments WHERE id = $1`, paymentID).Scan(&provider); err != nil {
		t.Fatalf("failed to read payment provider: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 1`, rentalID).Scan(&providerLedgerCount); err != nil {
		t.Fatalf("failed to count provider ledger rows: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 5`, rentalID).Scan(&balanceLedgerCount); err != nil {
		t.Fatalf("failed to count balance ledger rows: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM deposit_holds WHERE rental_id = $1`, rentalID).Scan(&depositHoldCount); err != nil {
		t.Fatalf("failed to count deposit holds: %v", err)
	}
	if balance != 9000 || provider != "balance" {
		t.Fatalf("wallet-paid rental was mutated by provider lookup balance=%d provider=%q", balance, provider)
	}
	if providerLedgerCount != 0 || balanceLedgerCount != 1 || depositHoldCount != 1 {
		t.Fatalf("wallet-paid rental side effects changed unexpectedly providerLedger=%d balanceLedger=%d holds=%d", providerLedgerCount, balanceLedgerCount, depositHoldCount)
	}
}

func TestPaymentWebhookZeroDepositSkipsDepositRecords(t *testing.T) {
	pool, _ := setupTestDB(t)
	userID, accountID, rentalID, paymentID := int64(9501), int64(9502), int64(9503), int64(9504)
	seedWaitingPaymentRentalWithAmounts(t, pool, userID, accountID, rentalID, paymentID, 500, 0)

	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))
	_, err := paymentService.ProcessWebhook(context.Background(), payment.WebhookRequest{
		PaymentID:             fmt.Sprintf("%d", paymentID),
		ExternalTransactionID: "zero-deposit-ext",
		Status:                "success",
	}, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("ProcessWebhook failed: %v", err)
	}

	var providerEntries, depositEntries, depositHolds int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM financial_ledger_entries WHERE payment_id = $1 AND entry_type = 1`, paymentID).Scan(&providerEntries); err != nil {
		t.Fatalf("failed to count provider ledger entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM financial_ledger_entries WHERE payment_id = $1 AND entry_type = 2`, paymentID).Scan(&depositEntries); err != nil {
		t.Fatalf("failed to count deposit ledger entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM deposit_holds WHERE payment_id = $1`, paymentID).Scan(&depositHolds); err != nil {
		t.Fatalf("failed to count deposit holds: %v", err)
	}
	if providerEntries != 1 || depositEntries != 0 || depositHolds != 0 {
		t.Fatalf("expected provider only for zero deposit, got provider=%d deposit=%d holds=%d", providerEntries, depositEntries, depositHolds)
	}
}

func TestFinancialLedgerIsImmutable(t *testing.T) {
	pool, _ := setupTestDB(t)
	userID, accountID, rentalID, paymentID := int64(9601), int64(9602), int64(9603), int64(9604)
	seedWaitingPaymentRental(t, pool, userID, accountID, rentalID, paymentID)

	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))
	_, err := paymentService.ProcessWebhook(context.Background(), payment.WebhookRequest{
		PaymentID:             fmt.Sprintf("%d", paymentID),
		ExternalTransactionID: "immutable-ext",
		Status:                "success",
	}, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("ProcessWebhook failed: %v", err)
	}

	var ledgerID int64
	if err := pool.QueryRow(context.Background(), `SELECT id FROM financial_ledger_entries WHERE payment_id = $1 LIMIT 1`, paymentID).Scan(&ledgerID); err != nil {
		t.Fatalf("failed to load ledger entry: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE financial_ledger_entries SET amount = amount + 1 WHERE id = $1`, ledgerID); err == nil {
		t.Fatalf("expected ledger UPDATE to fail")
	}
	if _, err := pool.Exec(context.Background(), `DELETE FROM financial_ledger_entries WHERE id = $1`, ledgerID); err == nil {
		t.Fatalf("expected ledger DELETE to fail")
	}
}

func TestFinancialLedgerMetadataIsSanitized(t *testing.T) {
	pool, _ := setupTestDB(t)
	userID, accountID, rentalID, paymentID := int64(9701), int64(9702), int64(9703), int64(9704)
	seedWaitingPaymentRental(t, pool, userID, accountID, rentalID, paymentID)

	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))
	_, err := paymentService.ProcessWebhook(context.Background(), payment.WebhookRequest{
		PaymentID:             fmt.Sprintf("%d", paymentID),
		ExternalTransactionID: "metadata-ext",
		Status:                "success",
	}, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("ProcessWebhook failed: %v", err)
	}

	rows, err := pool.Query(context.Background(), `SELECT metadata::text FROM financial_ledger_entries WHERE payment_id = $1`, paymentID)
	if err != nil {
		t.Fatalf("failed to query metadata: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var metadata string
		if err := rows.Scan(&metadata); err != nil {
			t.Fatalf("failed to scan metadata: %v", err)
		}
		metadata = strings.ToLower(metadata)
		for _, forbidden := range []string{"credential", "token", "password", "secret", "authorization", "key"} {
			if strings.Contains(metadata, forbidden) {
				t.Fatalf("metadata contains forbidden term %q: %s", forbidden, metadata)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("metadata rows failed: %v", err)
	}
}

func seedHeldDepositSettlementRental(t *testing.T, pool *pgxpool.Pool, userID, accountID, rentalID, paymentID int64, rentalPrice, depositAmount int64) {
	t.Helper()
	seedWaitingPaymentRentalWithAmounts(t, pool, userID, accountID, rentalID, paymentID, rentalPrice, depositAmount)

	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))
	if _, err := paymentService.ProcessWebhook(context.Background(), payment.WebhookRequest{
		PaymentID:             fmt.Sprintf("%d", paymentID),
		ExternalTransactionID: fmt.Sprintf("settlement-ext-%d", paymentID),
		Status:                "success",
	}, "127.0.0.1", "test"); err != nil {
		t.Fatalf("failed to activate rental for settlement: %v", err)
	}

	if _, err := pool.Exec(context.Background(), "UPDATE rentals SET status = 3, actual_finished_at = NOW(), updated_at = NOW() WHERE id = $1", rentalID); err != nil {
		t.Fatalf("failed to expire rental for settlement: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "UPDATE accounts SET status = 2, updated_at = NOW() WHERE id = $1", accountID); err != nil {
		t.Fatalf("failed to mark account available for settlement: %v", err)
	}
}

func seedWalletPaidRefundableRental(t *testing.T, pool *pgxpool.Pool, userID, accountID, rentalID, paymentID int64, balance, rentalPrice, depositAmount int64) {
	t.Helper()
	seedWaitingPaymentRentalWithBalance(t, pool, userID, accountID, rentalID, paymentID, balance, rentalPrice, depositAmount)

	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))
	if _, err := paymentService.PayRentalWithBalance(context.Background(), userID, rentalID, "127.0.0.1", "test", time.Now().UTC()); err != nil {
		t.Fatalf("failed to wallet-pay refundable rental: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "UPDATE rentals SET status = 3, actual_finished_at = NOW(), updated_at = NOW() WHERE id = $1", rentalID); err != nil {
		t.Fatalf("failed to expire wallet-paid rental: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "UPDATE accounts SET status = 2, updated_at = NOW() WHERE id = $1", accountID); err != nil {
		t.Fatalf("failed to mark wallet-paid account available: %v", err)
	}
}

func TestDepositReleaseCreditsBalanceOnce(t *testing.T) {
	pool, _ := setupTestDB(t)
	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))
	userID, accountID, rentalID, paymentID := int64(9801), int64(9802), int64(9803), int64(9804)
	seedHeldDepositSettlementRental(t, pool, userID, accountID, rentalID, paymentID, 500, 500)

	var beforeBalance int64
	if err := pool.QueryRow(context.Background(), "SELECT balance FROM users WHERE id = $1", userID).Scan(&beforeBalance); err != nil {
		t.Fatalf("failed to read initial balance: %v", err)
	}

	result, err := paymentService.ReleaseDeposit(context.Background(), 7001, "ADMIN", rentalID, time.Now().UTC())
	if err != nil {
		t.Fatalf("ReleaseDeposit failed: %v", err)
	}
	if result == nil || !result.Changed || result.Status != "RELEASED" {
		t.Fatalf("unexpected release result: %+v", result)
	}

	var holdStatus int16
	var afterBalance int64
	var ledgerCount, securityCount, auditCount int
	if err := pool.QueryRow(context.Background(), "SELECT status FROM deposit_holds WHERE rental_id = $1", rentalID).Scan(&holdStatus); err != nil {
		t.Fatalf("failed to read deposit hold status: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT balance FROM users WHERE id = $1", userID).Scan(&afterBalance); err != nil {
		t.Fatalf("failed to read balance after release: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 3", rentalID).Scan(&ledgerCount); err != nil {
		t.Fatalf("failed to count release ledger entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM security_events WHERE rental_id = $1 AND event_type = 11", rentalID).Scan(&securityCount); err != nil {
		t.Fatalf("failed to count release security events: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM audit_logs WHERE entity_type = 'deposit_hold' AND action = 'deposit_release'").Scan(&auditCount); err != nil {
		t.Fatalf("failed to count release audit logs: %v", err)
	}
	if holdStatus != 2 {
		t.Fatalf("expected hold RELEASED, got %d", holdStatus)
	}
	if afterBalance != beforeBalance+500 {
		t.Fatalf("expected balance credit by 500, got before=%d after=%d", beforeBalance, afterBalance)
	}
	if ledgerCount != 1 || securityCount != 1 || auditCount != 1 {
		t.Fatalf("expected one release ledger/security/audit entry, got ledger=%d security=%d audit=%d", ledgerCount, securityCount, auditCount)
	}

	result, err = paymentService.ReleaseDeposit(context.Background(), 7001, "ADMIN", rentalID, time.Now().UTC())
	if err != nil {
		t.Fatalf("repeated ReleaseDeposit should be no-op: %v", err)
	}
	if result == nil || result.Changed {
		t.Fatalf("expected repeated release no-op, got %+v", result)
	}
	if err := pool.QueryRow(context.Background(), "SELECT balance FROM users WHERE id = $1", userID).Scan(&afterBalance); err != nil {
		t.Fatalf("failed to read balance after replay: %v", err)
	}
	if afterBalance != beforeBalance+500 {
		t.Fatalf("balance changed twice on release replay: before=%d after=%d", beforeBalance, afterBalance)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 3", rentalID).Scan(&ledgerCount); err != nil {
		t.Fatalf("failed to count replay release ledger entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM security_events WHERE rental_id = $1 AND event_type = 11", rentalID).Scan(&securityCount); err != nil {
		t.Fatalf("failed to count replay release security events: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM audit_logs WHERE entity_type = 'deposit_hold' AND action = 'deposit_release'").Scan(&auditCount); err != nil {
		t.Fatalf("failed to count replay release audit logs: %v", err)
	}
	if ledgerCount != 1 || securityCount != 1 || auditCount != 1 {
		t.Fatalf("expected release replay to avoid duplicates, got ledger=%d security=%d audit=%d", ledgerCount, securityCount, auditCount)
	}
}

func TestDepositForfeitDoesNotCreditBalance(t *testing.T) {
	pool, _ := setupTestDB(t)
	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))
	userID, accountID, rentalID, paymentID := int64(9811), int64(9812), int64(9813), int64(9814)
	seedHeldDepositSettlementRental(t, pool, userID, accountID, rentalID, paymentID, 500, 500)

	var beforeBalance int64
	if err := pool.QueryRow(context.Background(), "SELECT balance FROM users WHERE id = $1", userID).Scan(&beforeBalance); err != nil {
		t.Fatalf("failed to read initial balance: %v", err)
	}

	result, err := paymentService.ForfeitDeposit(context.Background(), 7002, "ADMIN", rentalID, "damage_confirmed", time.Now().UTC())
	if err != nil {
		t.Fatalf("ForfeitDeposit failed: %v", err)
	}
	if result == nil || !result.Changed || result.Status != "FORFEITED" {
		t.Fatalf("unexpected forfeit result: %+v", result)
	}

	var holdStatus int16
	var afterBalance int64
	var ledgerCount, securityCount, auditCount int
	if err := pool.QueryRow(context.Background(), "SELECT status FROM deposit_holds WHERE rental_id = $1", rentalID).Scan(&holdStatus); err != nil {
		t.Fatalf("failed to read deposit hold status: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT balance FROM users WHERE id = $1", userID).Scan(&afterBalance); err != nil {
		t.Fatalf("failed to read balance after forfeit: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 4", rentalID).Scan(&ledgerCount); err != nil {
		t.Fatalf("failed to count forfeit ledger entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM security_events WHERE rental_id = $1 AND event_type = 12", rentalID).Scan(&securityCount); err != nil {
		t.Fatalf("failed to count forfeit security events: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM audit_logs WHERE entity_type = 'deposit_hold' AND action = 'deposit_forfeit'").Scan(&auditCount); err != nil {
		t.Fatalf("failed to count forfeit audit logs: %v", err)
	}
	if holdStatus != 3 {
		t.Fatalf("expected hold FORFEITED, got %d", holdStatus)
	}
	if afterBalance != beforeBalance {
		t.Fatalf("forfeit must not credit balance: before=%d after=%d", beforeBalance, afterBalance)
	}
	if ledgerCount != 1 || securityCount != 1 || auditCount != 1 {
		t.Fatalf("expected one forfeit ledger/security/audit entry, got ledger=%d security=%d audit=%d", ledgerCount, securityCount, auditCount)
	}

	result, err = paymentService.ForfeitDeposit(context.Background(), 7002, "ADMIN", rentalID, "damage_confirmed", time.Now().UTC())
	if err != nil {
		t.Fatalf("repeated ForfeitDeposit should be no-op: %v", err)
	}
	if result == nil || result.Changed {
		t.Fatalf("expected repeated forfeit no-op, got %+v", result)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 4", rentalID).Scan(&ledgerCount); err != nil {
		t.Fatalf("failed to count replay forfeit ledger entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM security_events WHERE rental_id = $1 AND event_type = 12", rentalID).Scan(&securityCount); err != nil {
		t.Fatalf("failed to count replay forfeit security events: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM audit_logs WHERE entity_type = 'deposit_hold' AND action = 'deposit_forfeit'").Scan(&auditCount); err != nil {
		t.Fatalf("failed to count replay forfeit audit logs: %v", err)
	}
	if ledgerCount != 1 || securityCount != 1 || auditCount != 1 {
		t.Fatalf("expected forfeit replay to avoid duplicates, got ledger=%d security=%d audit=%d", ledgerCount, securityCount, auditCount)
	}
}

func TestDepositSettlementRejectsInvalidStatesAndRoles(t *testing.T) {
	pool, _ := setupTestDB(t)
	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))

	userID, accountID, rentalID, paymentID := int64(9821), int64(9822), int64(9823), int64(9824)
	seedWaitingPaymentRentalWithAmounts(t, pool, userID, accountID, rentalID, paymentID, 500, 500)
	if _, err := paymentService.ReleaseDeposit(context.Background(), 7003, "ADMIN", rentalID, time.Now().UTC()); !errors.Is(err, payment.ErrDepositSettlementNotAllowed) {
		t.Fatalf("expected WAITING_PAYMENT release rejection, got %v", err)
	}
	if _, err := paymentService.ForfeitDeposit(context.Background(), 7003, "ADMIN", rentalID, "damage_confirmed", time.Now().UTC()); !errors.Is(err, payment.ErrDepositSettlementNotAllowed) {
		t.Fatalf("expected WAITING_PAYMENT forfeit rejection, got %v", err)
	}

	userID2, accountID2, rentalID2, paymentID2 := int64(9831), int64(9832), int64(9833), int64(9834)
	seedWaitingPaymentRentalWithAmounts(t, pool, userID2, accountID2, rentalID2, paymentID2, 500, 500)
	if _, err := payment.NewPaymentService(payment.NewPostgresRepository(pool)).ProcessWebhook(context.Background(), payment.WebhookRequest{
		PaymentID:             fmt.Sprintf("%d", paymentID2),
		ExternalTransactionID: "active-settlement-reject",
		Status:                "success",
	}, "127.0.0.1", "test"); err != nil {
		t.Fatalf("failed to activate rental for ACTIVE rejection test: %v", err)
	}
	if _, err := paymentService.ReleaseDeposit(context.Background(), 7003, "ADMIN", rentalID2, time.Now().UTC()); !errors.Is(err, payment.ErrDepositSettlementNotAllowed) {
		t.Fatalf("expected ACTIVE release rejection, got %v", err)
	}
	if _, err := paymentService.ForfeitDeposit(context.Background(), 7003, "ADMIN", rentalID2, "damage_confirmed", time.Now().UTC()); !errors.Is(err, payment.ErrDepositSettlementNotAllowed) {
		t.Fatalf("expected ACTIVE forfeit rejection, got %v", err)
	}

	userID3, accountID3, rentalID3, paymentID3 := int64(9841), int64(9842), int64(9843), int64(9844)
	seedHeldDepositSettlementRental(t, pool, userID3, accountID3, rentalID3, paymentID3, 500, 500)
	if _, err := pool.Exec(context.Background(), "UPDATE rentals SET status = 5 WHERE id = $1", rentalID3); err != nil {
		t.Fatalf("failed to cancel expired rental for test: %v", err)
	}
	if _, err := paymentService.ReleaseDeposit(context.Background(), 7003, "ADMIN", rentalID3, time.Now().UTC()); !errors.Is(err, payment.ErrDepositSettlementNotAllowed) {
		t.Fatalf("expected CANCELLED release rejection, got %v", err)
	}

	userID4, accountID4, rentalID4, paymentID4 := int64(9851), int64(9852), int64(9853), int64(9854)
	seedHeldDepositSettlementRental(t, pool, userID4, accountID4, rentalID4, paymentID4, 500, 500)
	if _, err := pool.Exec(context.Background(), "UPDATE payments SET status = 3 WHERE id = $1", paymentID4); err != nil {
		t.Fatalf("failed to fail payment for test: %v", err)
	}
	if _, err := paymentService.ReleaseDeposit(context.Background(), 7003, "ADMIN", rentalID4, time.Now().UTC()); !errors.Is(err, payment.ErrDepositSettlementNotAllowed) {
		t.Fatalf("expected non-success payment release rejection, got %v", err)
	}

	userID5, accountID5, rentalID5, paymentID5 := int64(9861), int64(9862), int64(9863), int64(9864)
	seedHeldDepositSettlementRental(t, pool, userID5, accountID5, rentalID5, paymentID5, 500, 0)
	if _, err := paymentService.ReleaseDeposit(context.Background(), 7003, "ADMIN", rentalID5, time.Now().UTC()); !errors.Is(err, payment.ErrDepositSettlementNotAllowed) {
		t.Fatalf("expected zero-deposit release rejection, got %v", err)
	}

	if _, err := paymentService.ForfeitDeposit(context.Background(), 7003, "RENT", rentalID2, "damage_confirmed", time.Now().UTC()); !errors.Is(err, payment.ErrAdminRequired) {
		t.Fatalf("expected non-admin forfeit rejection, got %v", err)
	}
}

func TestDepositSettlementConcurrentReleaseVsForfeit(t *testing.T) {
	pool, _ := setupTestDB(t)
	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))
	userID, accountID, rentalID, paymentID := int64(9861), int64(9862), int64(9863), int64(9864)
	seedHeldDepositSettlementRental(t, pool, userID, accountID, rentalID, paymentID, 500, 500)

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var releaseErr, forfeitErr error

	go func() {
		defer wg.Done()
		<-start
		_, releaseErr = paymentService.ReleaseDeposit(context.Background(), 7004, "ADMIN", rentalID, time.Now().UTC())
	}()
	go func() {
		defer wg.Done()
		<-start
		_, forfeitErr = paymentService.ForfeitDeposit(context.Background(), 7005, "ADMIN", rentalID, "damage_confirmed", time.Now().UTC())
	}()

	close(start)
	wg.Wait()

	if releaseErr != nil && !errors.Is(releaseErr, payment.ErrDepositAlreadySettled) {
		t.Fatalf("unexpected release error: %v", releaseErr)
	}
	if forfeitErr != nil && !errors.Is(forfeitErr, payment.ErrDepositAlreadySettled) {
		t.Fatalf("unexpected forfeit error: %v", forfeitErr)
	}

	var holdStatus int16
	var balance int64
	var releaseLedgerCount, forfeitLedgerCount int
	if err := pool.QueryRow(context.Background(), "SELECT status FROM deposit_holds WHERE rental_id = $1", rentalID).Scan(&holdStatus); err != nil {
		t.Fatalf("failed to read concurrent hold status: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT balance FROM users WHERE id = $1", userID).Scan(&balance); err != nil {
		t.Fatalf("failed to read concurrent balance: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 3", rentalID).Scan(&releaseLedgerCount); err != nil {
		t.Fatalf("failed to count concurrent release ledger entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 4", rentalID).Scan(&forfeitLedgerCount); err != nil {
		t.Fatalf("failed to count concurrent forfeit ledger entries: %v", err)
	}

	if holdStatus == 2 {
		if releaseLedgerCount != 1 || forfeitLedgerCount != 0 || balance != 10500 {
			t.Fatalf("inconsistent release-won state: hold=%d releaseLedger=%d forfeitLedger=%d balance=%d", holdStatus, releaseLedgerCount, forfeitLedgerCount, balance)
		}
		return
	}
	if holdStatus == 3 {
		if releaseLedgerCount != 0 || forfeitLedgerCount != 1 || balance != 10000 {
			t.Fatalf("inconsistent forfeit-won state: hold=%d releaseLedger=%d forfeitLedger=%d balance=%d", holdStatus, releaseLedgerCount, forfeitLedgerCount, balance)
		}
		return
	}
	t.Fatalf("unexpected concurrent hold status: %d", holdStatus)
}

func TestWalletPaymentWithBalance_Success(t *testing.T) {
	pool, _ := setupTestDB(t)
	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))
	userID, accountID, rentalID, paymentID := int64(9901), int64(9902), int64(9903), int64(9904)
	seedWaitingPaymentRentalWithBalance(t, pool, userID, accountID, rentalID, paymentID, 10000, 500, 500)

	result, err := paymentService.PayRentalWithBalance(context.Background(), userID, rentalID, "127.0.0.1", "test", time.Now().UTC())
	if err != nil {
		t.Fatalf("PayRentalWithBalance failed: %v", err)
	}
	if result == nil || !result.Changed || result.Idempotent {
		t.Fatalf("unexpected wallet payment result: %+v", result)
	}

	var balance int64
	var paymentStatus, rentalStatus, accountStatus int16
	var paymentProvider string
	var balanceDebitEntries, depositEntries, depositHolds, securityCount, auditCount int
	if err := pool.QueryRow(context.Background(), "SELECT balance FROM users WHERE id = $1", userID).Scan(&balance); err != nil {
		t.Fatalf("failed to read user balance: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT status, provider FROM payments WHERE id = $1", paymentID).Scan(&paymentStatus, &paymentProvider); err != nil {
		t.Fatalf("failed to read payment state: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT status FROM rentals WHERE id = $1", rentalID).Scan(&rentalStatus); err != nil {
		t.Fatalf("failed to read rental state: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT status FROM accounts WHERE id = $1", accountID).Scan(&accountStatus); err != nil {
		t.Fatalf("failed to read account state: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 5", rentalID).Scan(&balanceDebitEntries); err != nil {
		t.Fatalf("failed to count balance debit entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 2", rentalID).Scan(&depositEntries); err != nil {
		t.Fatalf("failed to count deposit entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM deposit_holds WHERE rental_id = $1 AND status = 1", rentalID).Scan(&depositHolds); err != nil {
		t.Fatalf("failed to count deposit holds: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM security_events WHERE rental_id = $1 AND event_type = 13", rentalID).Scan(&securityCount); err != nil {
		t.Fatalf("failed to count wallet security events: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM audit_logs WHERE entity_type = 'payment' AND entity_id = $1 AND action = 'wallet_payment_success'", paymentID).Scan(&auditCount); err != nil {
		t.Fatalf("failed to count wallet audit logs: %v", err)
	}
	if balance != 9000 || paymentStatus != 2 || rentalStatus != 2 || accountStatus != 4 || paymentProvider != "balance" {
		t.Fatalf("unexpected wallet payment state balance=%d payment=%d provider=%q rental=%d account=%d", balance, paymentStatus, paymentProvider, rentalStatus, accountStatus)
	}
	if balanceDebitEntries != 1 || depositEntries != 1 || depositHolds != 1 || securityCount != 1 || auditCount != 1 {
		t.Fatalf("unexpected wallet side effects debit=%d depositEntries=%d holds=%d security=%d audit=%d", balanceDebitEntries, depositEntries, depositHolds, securityCount, auditCount)
	}
}

func TestWalletPaymentWithBalance_ZeroDeposit(t *testing.T) {
	pool, _ := setupTestDB(t)
	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))
	userID, accountID, rentalID, paymentID := int64(9911), int64(9912), int64(9913), int64(9914)
	seedWaitingPaymentRentalWithBalance(t, pool, userID, accountID, rentalID, paymentID, 10000, 500, 0)

	if _, err := paymentService.PayRentalWithBalance(context.Background(), userID, rentalID, "127.0.0.1", "test", time.Now().UTC()); err != nil {
		t.Fatalf("PayRentalWithBalance failed: %v", err)
	}

	var balanceDebitEntries, depositEntries, depositHolds int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 5", rentalID).Scan(&balanceDebitEntries); err != nil {
		t.Fatalf("failed to count balance debit entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 2", rentalID).Scan(&depositEntries); err != nil {
		t.Fatalf("failed to count deposit entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM deposit_holds WHERE rental_id = $1", rentalID).Scan(&depositHolds); err != nil {
		t.Fatalf("failed to count deposit holds: %v", err)
	}
	if balanceDebitEntries != 1 || depositEntries != 0 || depositHolds != 0 {
		t.Fatalf("unexpected zero-deposit wallet results debit=%d depositEntries=%d holds=%d", balanceDebitEntries, depositEntries, depositHolds)
	}
}

func TestWalletPaymentWithBalance_RejectionsAndReplay(t *testing.T) {
	pool, _ := setupTestDB(t)
	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))

	userID, accountID, rentalID, paymentID := int64(9921), int64(9922), int64(9923), int64(9924)
	seedWaitingPaymentRentalWithBalance(t, pool, userID, accountID, rentalID, paymentID, 900, 500, 500)
	if _, err := paymentService.PayRentalWithBalance(context.Background(), userID, rentalID, "127.0.0.1", "test", time.Now().UTC()); !errors.Is(err, payment.ErrWalletInsufficientBalance) {
		t.Fatalf("expected insufficient balance, got %v", err)
	}

	var balance int64
	var paymentStatus, rentalStatus, accountStatus int16
	if err := pool.QueryRow(context.Background(), "SELECT balance FROM users WHERE id = $1", userID).Scan(&balance); err != nil {
		t.Fatalf("failed to read balance: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT status FROM payments WHERE id = $1", paymentID).Scan(&paymentStatus); err != nil {
		t.Fatalf("failed to read payment status: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT status FROM rentals WHERE id = $1", rentalID).Scan(&rentalStatus); err != nil {
		t.Fatalf("failed to read rental status: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT status FROM accounts WHERE id = $1", accountID).Scan(&accountStatus); err != nil {
		t.Fatalf("failed to read account status: %v", err)
	}
	if balance != 900 || paymentStatus != 1 || rentalStatus != 1 || accountStatus != 3 {
		t.Fatalf("insufficient balance mutated state balance=%d payment=%d rental=%d account=%d", balance, paymentStatus, rentalStatus, accountStatus)
	}

	userID2, accountID2, rentalID2, paymentID2 := int64(9931), int64(9932), int64(9933), int64(9934)
	seedWaitingPaymentRentalWithBalance(t, pool, userID2, accountID2, rentalID2, paymentID2, 10000, 500, 500)
	if _, err := pool.Exec(context.Background(), "UPDATE rentals SET payment_expires_at = created_at + INTERVAL '1 second' WHERE id = $1", rentalID2); err != nil {
		t.Fatalf("failed to expire waiting payment window: %v", err)
	}
	if _, err := paymentService.PayRentalWithBalance(context.Background(), userID2, rentalID2, "127.0.0.1", "test", time.Now().UTC().Add(2*time.Second)); !errors.Is(err, payment.ErrWalletPaymentExpired) {
		t.Fatalf("expected expired wallet payment rejection, got %v", err)
	}

	userID3, accountID3, rentalID3, paymentID3 := int64(9941), int64(9942), int64(9943), int64(9944)
	seedWaitingPaymentRentalWithBalance(t, pool, userID3, accountID3, rentalID3, paymentID3, 10000, 500, 500)
	if _, err := paymentService.PayRentalWithBalance(context.Background(), userID3+999, rentalID3, "127.0.0.1", "test", time.Now().UTC()); !errors.Is(err, payment.ErrWalletPaymentNotFound) {
		t.Fatalf("expected non-owner rejection, got %v", err)
	}

	userID4, accountID4, rentalID4, paymentID4 := int64(9951), int64(9952), int64(9953), int64(9954)
	seedWaitingPaymentRentalWithBalance(t, pool, userID4, accountID4, rentalID4, paymentID4, 10000, 500, 500)
	if _, err := paymentService.PayRentalWithBalance(context.Background(), userID4, rentalID4, "127.0.0.1", "test", time.Now().UTC()); err != nil {
		t.Fatalf("first wallet payment failed: %v", err)
	}
	result, err := paymentService.PayRentalWithBalance(context.Background(), userID4, rentalID4, "127.0.0.1", "test", time.Now().UTC())
	if err != nil {
		t.Fatalf("replayed wallet payment failed: %v", err)
	}
	if result == nil || !result.Idempotent || result.Changed {
		t.Fatalf("expected idempotent wallet replay result, got %+v", result)
	}
	var debitCount, holdCount int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 5", rentalID4).Scan(&debitCount); err != nil {
		t.Fatalf("failed to count wallet debit entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM deposit_holds WHERE rental_id = $1", rentalID4).Scan(&holdCount); err != nil {
		t.Fatalf("failed to count wallet deposit holds: %v", err)
	}
	if debitCount != 1 || holdCount != 1 {
		t.Fatalf("wallet replay created duplicates debit=%d holds=%d", debitCount, holdCount)
	}
}

func TestWalletPaymentWithBalance_ConcurrentAndWebhookRace(t *testing.T) {
	pool, _ := setupTestDB(t)
	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))

	userID, accountID, rentalID, paymentID := int64(9961), int64(9962), int64(9963), int64(9964)
	seedWaitingPaymentRentalWithBalance(t, pool, userID, accountID, rentalID, paymentID, 10000, 500, 500)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var errOne, errTwo error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, errOne = paymentService.PayRentalWithBalance(context.Background(), userID, rentalID, "127.0.0.1", "test", time.Now().UTC())
	}()
	go func() {
		defer wg.Done()
		<-start
		_, errTwo = paymentService.PayRentalWithBalance(context.Background(), userID, rentalID, "127.0.0.1", "test", time.Now().UTC())
	}()
	close(start)
	wg.Wait()
	if errOne != nil {
		t.Fatalf("unexpected first concurrent wallet error: %v", errOne)
	}
	if errTwo != nil {
		t.Fatalf("unexpected second concurrent wallet error: %v", errTwo)
	}

	var balance int64
	var debitCount int
	if err := pool.QueryRow(context.Background(), "SELECT balance FROM users WHERE id = $1", userID).Scan(&balance); err != nil {
		t.Fatalf("failed to read concurrent balance: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 5", rentalID).Scan(&debitCount); err != nil {
		t.Fatalf("failed to count concurrent debit entries: %v", err)
	}
	if balance != 9000 || debitCount != 1 {
		t.Fatalf("concurrent wallet payment produced double debit balance=%d debitCount=%d", balance, debitCount)
	}

	userID2, accountID2, rentalID2, paymentID2 := int64(9971), int64(9972), int64(9973), int64(9974)
	seedWaitingPaymentRentalWithBalance(t, pool, userID2, accountID2, rentalID2, paymentID2, 10000, 500, 500)

	start = make(chan struct{})
	wg = sync.WaitGroup{}
	var walletErr, webhookErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, walletErr = paymentService.PayRentalWithBalance(context.Background(), userID2, rentalID2, "127.0.0.1", "test", time.Now().UTC())
	}()
	go func() {
		defer wg.Done()
		<-start
		_, webhookErr = paymentService.ProcessWebhook(context.Background(), payment.WebhookRequest{
			PaymentID:             fmt.Sprintf("%d", paymentID2),
			ExternalTransactionID: "wallet-webhook-race",
			Status:                "success",
		}, "127.0.0.1", "test")
	}()
	close(start)
	wg.Wait()
	if walletErr != nil {
		t.Fatalf("unexpected wallet/webhook wallet error: %v", walletErr)
	}
	if webhookErr != nil {
		t.Fatalf("unexpected wallet/webhook webhook error: %v", webhookErr)
	}

	var finalBalance int64
	var finalProvider string
	var providerLedgerCount, balanceLedgerCount, holdCount, depositEntries int
	if err := pool.QueryRow(context.Background(), "SELECT balance FROM users WHERE id = $1", userID2).Scan(&finalBalance); err != nil {
		t.Fatalf("failed to read race balance: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT provider FROM payments WHERE id = $1", paymentID2).Scan(&finalProvider); err != nil {
		t.Fatalf("failed to read race provider: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 1", rentalID2).Scan(&providerLedgerCount); err != nil {
		t.Fatalf("failed to count provider ledger entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 5", rentalID2).Scan(&balanceLedgerCount); err != nil {
		t.Fatalf("failed to count balance ledger entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM deposit_holds WHERE rental_id = $1", rentalID2).Scan(&holdCount); err != nil {
		t.Fatalf("failed to count deposit holds in race: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 2", rentalID2).Scan(&depositEntries); err != nil {
		t.Fatalf("failed to count deposit entries in race: %v", err)
	}
	if holdCount != 1 || depositEntries != 1 {
		t.Fatalf("wallet/webhook race left inconsistent deposit records holds=%d depositEntries=%d", holdCount, depositEntries)
	}
	switch finalProvider {
	case "balance":
		if finalBalance != 9000 || balanceLedgerCount != 1 || providerLedgerCount != 0 {
			t.Fatalf("wallet won race but final state is inconsistent balance=%d balanceLedger=%d providerLedger=%d", finalBalance, balanceLedgerCount, providerLedgerCount)
		}
	case "internal":
		if finalBalance != 10000 || balanceLedgerCount != 0 || providerLedgerCount != 1 {
			t.Fatalf("webhook won race but final state is inconsistent balance=%d balanceLedger=%d providerLedger=%d", finalBalance, balanceLedgerCount, providerLedgerCount)
		}
	default:
		t.Fatalf("unexpected final payment provider in race: %q", finalProvider)
	}
}

func TestWalletRefundCreditsBalanceAndDepositOnce(t *testing.T) {
	pool, _ := setupTestDB(t)
	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))
	userID, accountID, rentalID, paymentID := int64(9981), int64(9982), int64(9983), int64(9984)
	seedWalletPaidRefundableRental(t, pool, userID, accountID, rentalID, paymentID, 10000, 500, 500)

	var beforeBalance int64
	if err := pool.QueryRow(context.Background(), "SELECT balance FROM users WHERE id = $1", userID).Scan(&beforeBalance); err != nil {
		t.Fatalf("failed to read balance before refund: %v", err)
	}

	result, err := paymentService.RefundWalletPayment(context.Background(), userID, "ADMIN", rentalID, "SERVICE_UNAVAILABLE", time.Now().UTC())
	if err != nil {
		t.Fatalf("RefundWalletPayment failed: %v", err)
	}
	if result == nil || !result.Changed || result.Idempotent || result.TotalAmount != 1000 || result.DepositStatus != "REFUNDED" {
		t.Fatalf("unexpected refund result: %+v", result)
	}

	var afterBalance int64
	var holdStatus int16
	var refundStatus int16
	var principalLedgerCount, depositLedgerCount, auditCount, securityCount int
	if err := pool.QueryRow(context.Background(), "SELECT balance FROM users WHERE id = $1", userID).Scan(&afterBalance); err != nil {
		t.Fatalf("failed to read balance after refund: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT status FROM deposit_holds WHERE rental_id = $1", rentalID).Scan(&holdStatus); err != nil {
		t.Fatalf("failed to read hold after refund: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT status FROM refunds WHERE rental_id = $1", rentalID).Scan(&refundStatus); err != nil {
		t.Fatalf("failed to read refund row: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 6", rentalID).Scan(&principalLedgerCount); err != nil {
		t.Fatalf("failed to count principal refund entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM financial_ledger_entries WHERE rental_id = $1 AND entry_type = 7", rentalID).Scan(&depositLedgerCount); err != nil {
		t.Fatalf("failed to count deposit refund entries: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM audit_logs WHERE entity_type = 'refund' AND action = 'wallet_refund_completed'").Scan(&auditCount); err != nil {
		t.Fatalf("failed to count refund audit logs: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM security_events WHERE rental_id = $1 AND event_type = 14", rentalID).Scan(&securityCount); err != nil {
		t.Fatalf("failed to count refund security events: %v", err)
	}
	if afterBalance != beforeBalance+1000 || holdStatus != 4 || refundStatus != 2 {
		t.Fatalf("unexpected refund state balance=%d before=%d hold=%d refund=%d", afterBalance, beforeBalance, holdStatus, refundStatus)
	}
	if principalLedgerCount != 1 || depositLedgerCount != 1 || auditCount != 1 || securityCount != 1 {
		t.Fatalf("unexpected refund side effects principal=%d deposit=%d audit=%d security=%d", principalLedgerCount, depositLedgerCount, auditCount, securityCount)
	}

	result, err = paymentService.RefundWalletPayment(context.Background(), userID, "ADMIN", rentalID, "SERVICE_UNAVAILABLE", time.Now().UTC())
	if err != nil {
		t.Fatalf("refund replay failed: %v", err)
	}
	if result == nil || !result.Idempotent || result.Changed {
		t.Fatalf("expected idempotent refund replay, got %+v", result)
	}
	if err := pool.QueryRow(context.Background(), "SELECT balance FROM users WHERE id = $1", userID).Scan(&afterBalance); err != nil {
		t.Fatalf("failed to read balance after replay: %v", err)
	}
	if afterBalance != beforeBalance+1000 {
		t.Fatalf("balance changed twice on refund replay: before=%d after=%d", beforeBalance, afterBalance)
	}
}

func TestWalletRefundPrincipalOnlyScenarios(t *testing.T) {
	pool, _ := setupTestDB(t)
	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))

	userID, accountID, rentalID, paymentID := int64(9991), int64(9992), int64(9993), int64(9994)
	seedWalletPaidRefundableRental(t, pool, userID, accountID, rentalID, paymentID, 10000, 500, 0)
	result, err := paymentService.RefundWalletPayment(context.Background(), userID, "ADMIN", rentalID, "SERVICE_UNAVAILABLE", time.Now().UTC())
	if err != nil {
		t.Fatalf("zero-deposit refund failed: %v", err)
	}
	if result.TotalAmount != 500 || result.DepositAmount != 0 || result.DepositStatus != "NONE" {
		t.Fatalf("unexpected zero-deposit refund result: %+v", result)
	}

	userID2, accountID2, rentalID2, paymentID2 := int64(9995), int64(9996), int64(9997), int64(9998)
	seedWalletPaidRefundableRental(t, pool, userID2, accountID2, rentalID2, paymentID2, 10000, 500, 500)
	if _, err := paymentService.ReleaseDeposit(context.Background(), 7103, "ADMIN", rentalID2, time.Now().UTC()); err != nil {
		t.Fatalf("failed to release deposit before refund test: %v", err)
	}
	result, err = paymentService.RefundWalletPayment(context.Background(), userID2, "ADMIN", rentalID2, "SERVICE_UNAVAILABLE", time.Now().UTC())
	if err != nil {
		t.Fatalf("released-hold principal refund failed: %v", err)
	}
	if result.TotalAmount != 500 || result.DepositAmount != 0 || result.DepositStatus != "RELEASED" {
		t.Fatalf("unexpected released-hold refund result: %+v", result)
	}

	userID3, accountID3, rentalID3, paymentID3 := int64(10001), int64(10002), int64(10003), int64(10004)
	seedWalletPaidRefundableRental(t, pool, userID3, accountID3, rentalID3, paymentID3, 10000, 500, 500)
	if _, err := paymentService.ForfeitDeposit(context.Background(), 7104, "ADMIN", rentalID3, "damage_confirmed", time.Now().UTC()); err != nil {
		t.Fatalf("failed to forfeit deposit before refund test: %v", err)
	}
	result, err = paymentService.RefundWalletPayment(context.Background(), userID3, "ADMIN", rentalID3, "SERVICE_UNAVAILABLE", time.Now().UTC())
	if err != nil {
		t.Fatalf("forfeited-hold principal refund failed: %v", err)
	}
	if result.TotalAmount != 500 || result.DepositAmount != 0 || result.DepositStatus != "FORFEITED" {
		t.Fatalf("unexpected forfeited-hold refund result: %+v", result)
	}
}

func TestWalletRefundRejectsInvalidEligibilityAndRoles(t *testing.T) {
	pool, _ := setupTestDB(t)
	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))

	userID, accountID, rentalID, paymentID := int64(10011), int64(10012), int64(10013), int64(10014)
	seedWaitingPaymentRentalWithBalance(t, pool, userID, accountID, rentalID, paymentID, 10000, 500, 500)
	if _, err := paymentService.RefundWalletPayment(context.Background(), userID, "ADMIN", rentalID, "SERVICE_UNAVAILABLE", time.Now().UTC()); !errors.Is(err, payment.ErrWalletRefundNotAllowed) {
		t.Fatalf("expected WAITING_PAYMENT wallet refund rejection, got %v", err)
	}

	userID2, accountID2, rentalID2, paymentID2 := int64(10021), int64(10022), int64(10023), int64(10024)
	seedWaitingPaymentRentalWithBalance(t, pool, userID2, accountID2, rentalID2, paymentID2, 10000, 500, 500)
	if _, err := paymentService.PayRentalWithBalance(context.Background(), userID2, rentalID2, "127.0.0.1", "test", time.Now().UTC()); err != nil {
		t.Fatalf("wallet payment failed for ACTIVE refund rejection test: %v", err)
	}
	if _, err := paymentService.RefundWalletPayment(context.Background(), userID2, "ADMIN", rentalID2, "SERVICE_UNAVAILABLE", time.Now().UTC()); !errors.Is(err, payment.ErrWalletRefundNotAllowed) {
		t.Fatalf("expected ACTIVE wallet refund rejection, got %v", err)
	}

	userID3, accountID3, rentalID3, paymentID3 := int64(10031), int64(10032), int64(10033), int64(10034)
	seedHeldDepositSettlementRental(t, pool, userID3, accountID3, rentalID3, paymentID3, 500, 500)
	if _, err := paymentService.RefundWalletPayment(context.Background(), userID3, "ADMIN", rentalID3, "SERVICE_UNAVAILABLE", time.Now().UTC()); !errors.Is(err, payment.ErrWalletRefundNotAllowed) {
		t.Fatalf("expected provider-paid wallet refund rejection, got %v", err)
	}

	if _, err := paymentService.RefundWalletPayment(context.Background(), userID3, "RENT", rentalID3, "SERVICE_UNAVAILABLE", time.Now().UTC()); !errors.Is(err, payment.ErrAdminRequired) {
		t.Fatalf("expected non-admin wallet refund rejection, got %v", err)
	}
}

func TestWalletRefundConcurrentWithDepositSettlement(t *testing.T) {
	pool, _ := setupTestDB(t)
	paymentService := payment.NewPaymentService(payment.NewPostgresRepository(pool))

	userID, accountID, rentalID, paymentID := int64(10041), int64(10042), int64(10043), int64(10044)
	seedWalletPaidRefundableRental(t, pool, userID, accountID, rentalID, paymentID, 10000, 500, 500)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var refundErr, releaseErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, refundErr = paymentService.RefundWalletPayment(context.Background(), userID, "ADMIN", rentalID, "SERVICE_UNAVAILABLE", time.Now().UTC())
	}()
	go func() {
		defer wg.Done()
		<-start
		_, releaseErr = paymentService.ReleaseDeposit(context.Background(), 7203, "ADMIN", rentalID, time.Now().UTC())
	}()
	close(start)
	wg.Wait()

	if refundErr != nil && !errors.Is(refundErr, payment.ErrDepositAlreadySettled) && !errors.Is(refundErr, payment.ErrWalletRefundNotAllowed) {
		t.Fatalf("unexpected refund/release refund error: %v", refundErr)
	}
	if releaseErr != nil && !errors.Is(releaseErr, payment.ErrDepositAlreadySettled) {
		t.Fatalf("unexpected refund/release deposit error: %v", releaseErr)
	}

	var holdStatus int16
	var balance int64
	if err := pool.QueryRow(context.Background(), "SELECT status FROM deposit_holds WHERE rental_id = $1", rentalID).Scan(&holdStatus); err != nil {
		t.Fatalf("failed to read hold status after refund/release race: %v", err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT balance FROM users WHERE id = $1", userID).Scan(&balance); err != nil {
		t.Fatalf("failed to read balance after refund/release race: %v", err)
	}
	if holdStatus != 2 && holdStatus != 4 {
		t.Fatalf("unexpected hold status after refund/release race: %d", holdStatus)
	}
	if balance != 10000 {
		t.Fatalf("unexpected balance after refund/release race: %d", balance)
	}

	userID2, accountID2, rentalID2, paymentID2 := int64(10051), int64(10052), int64(10053), int64(10054)
	seedWalletPaidRefundableRental(t, pool, userID2, accountID2, rentalID2, paymentID2, 10000, 500, 500)
	start = make(chan struct{})
	wg = sync.WaitGroup{}
	var refundErr2, forfeitErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, refundErr2 = paymentService.RefundWalletPayment(context.Background(), userID2, "ADMIN", rentalID2, "SERVICE_UNAVAILABLE", time.Now().UTC())
	}()
	go func() {
		defer wg.Done()
		<-start
		_, forfeitErr = paymentService.ForfeitDeposit(context.Background(), 7205, "ADMIN", rentalID2, "damage_confirmed", time.Now().UTC())
	}()
	close(start)
	wg.Wait()

	if refundErr2 != nil && !errors.Is(refundErr2, payment.ErrDepositAlreadySettled) && !errors.Is(refundErr2, payment.ErrWalletRefundNotAllowed) {
		t.Fatalf("unexpected refund/forfeit refund error: %v", refundErr2)
	}
	if forfeitErr != nil && !errors.Is(forfeitErr, payment.ErrDepositAlreadySettled) {
		t.Fatalf("unexpected refund/forfeit deposit error: %v", forfeitErr)
	}

	if err := pool.QueryRow(context.Background(), "SELECT status FROM deposit_holds WHERE rental_id = $1", rentalID2).Scan(&holdStatus); err != nil {
		t.Fatalf("failed to read hold status after refund/forfeit race: %v", err)
	}
	if holdStatus != 3 && holdStatus != 4 {
		t.Fatalf("unexpected hold status after refund/forfeit race: %d", holdStatus)
	}
	if err := pool.QueryRow(context.Background(), "SELECT balance FROM users WHERE id = $1", userID2).Scan(&balance); err != nil {
		t.Fatalf("failed to read balance after refund/forfeit race: %v", err)
	}
	if balance != 9500 && balance != 10000 {
		t.Fatalf("unexpected balance after refund/forfeit race: %d", balance)
	}
}

func TestRentalService_CancelDoesNotReleaseActiveRental(t *testing.T) {
	pool, txManager := setupTestDB(t)
	userID, accountID, rentalID, paymentID := int64(9301), int64(9302), int64(9303), int64(9304)
	seedWaitingPaymentRental(t, pool, userID, accountID, rentalID, paymentID)
	_, err := pool.Exec(context.Background(), "UPDATE rentals SET status = 2 WHERE id = $1", rentalID)
	if err != nil {
		t.Fatalf("failed to make rental active: %v", err)
	}
	_, err = pool.Exec(context.Background(), "UPDATE payments SET status = 2 WHERE id = $1", paymentID)
	if err != nil {
		t.Fatalf("failed to make payment success: %v", err)
	}
	_, err = pool.Exec(context.Background(), "UPDATE accounts SET status = 4 WHERE id = $1", accountID)
	if err != nil {
		t.Fatalf("failed to make account rented: %v", err)
	}

	service := rental.NewService(
		rental.NewPostgresRepository(pool),
		account.NewPostgresRepository(pool, "super-secret-32-byte-key-for-aes"),
		user.NewPostgresRepository(pool),
		payment.NewPostgresRepository(pool),
		txManager,
	)

	_, err = service.CancelRental(context.Background(), userID, rentalID, "too late", time.Now())
	if !errors.Is(err, rental.ErrCannotCancel) {
		t.Fatalf("expected ErrCannotCancel for ACTIVE rental, got %v", err)
	}
	var accountStatus int16
	if err := pool.QueryRow(context.Background(), "SELECT status FROM accounts WHERE id = $1", accountID).Scan(&accountStatus); err != nil {
		t.Fatalf("failed to query account status: %v", err)
	}
	if accountStatus != int16(account.StatusRented) {
		t.Fatalf("expected account to remain RENTED, got %d", accountStatus)
	}
}
