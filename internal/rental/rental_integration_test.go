package rental_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	"rent_game_accs/internal/account"
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
	userRepo := user.NewPostgresRepository(pool)
	rentalService := rental.NewService(rentalRepo, accountRepo, userRepo, txManager)

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
		if rErr != rental.ErrAccountNotAvailable {
			t.Errorf("expected error %v, got %v", rental.ErrAccountNotAvailable, rErr)
		}
	}

	updatedAcc, err := accountRepo.GetAccount(ctx, acc.ID)
	if err != nil {
		t.Fatalf("failed to fetch updated account status: %v", err)
	}

	if updatedAcc.Status != account.StatusRented {
		t.Errorf("expected account status to be Rented (%v), got %v", account.StatusRented, updatedAcc.Status)
	}

	rows, err := pool.Query(ctx, "SELECT id, user_id FROM rentals")
	if err != nil {
		t.Fatalf("failed to query rentals: %v", err)
	}
	defer rows.Close()

	var dbRentalCount int
	for rows.Next() {
		dbRentalCount++
	}
	if dbRentalCount != 1 {
		t.Errorf("expected exactly 1 rental in database, got %d", dbRentalCount)
	}
}
