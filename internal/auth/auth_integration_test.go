package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	pkg_postgres_pool "rent_game_accs/internal/pkg/repository/postgres/pool"
	"rent_game_accs/internal/shared/database"
	shared_middleware "rent_game_accs/internal/shared/middleware"
	"rent_game_accs/migrations"
)

func setupAuthIntegrationDB(t *testing.T) (*pgxpool.Pool, database.TxManager) {
	t.Helper()
	if os.Getenv("RUN_INTEGRATION_TESTS") != "1" {
		t.Skip("set RUN_INTEGRATION_TESTS=1 and start PostgreSQL to run integration tests")
	}

	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5433"
	}
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost"
	}
	poolConn, db, err := pkg_postgres_pool.NewConnectionPool(context.Background(), pkg_postgres_pool.PostgresConfig{
		Host: host, Port: port, User: "postgres", Password: "postgres", Database: "game_rental", Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("connect auth integration database: %v", err)
	}
	lockConn, err := poolConn.Pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire integration lock: %v", err)
	}
	if _, err := lockConn.Exec(context.Background(), "SELECT pg_advisory_lock($1)", int64(915202607)); err != nil {
		t.Fatalf("lock integration database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = lockConn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", int64(915202607))
		lockConn.Release()
		poolConn.Close()
		_ = db.Close()
	})

	goose.SetBaseFS(migrations.EmbedMigrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("set goose dialect: %v", err)
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("run auth integration migrations: %v", err)
	}
	if _, err := poolConn.Pool.Exec(context.Background(), "TRUNCATE users RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("reset auth integration database: %v", err)
	}
	return poolConn.Pool, database.NewTxManager(poolConn.Pool)
}

func TestAuthPrivilegeAndSessionLifecycleIntegration(t *testing.T) {
	pool, txManager := setupAuthIntegrationDB(t)
	ctx := context.Background()
	const secret = "auth-integration-secret"
	service := NewPostgresService(NewPostgresRepository(pool), txManager, secret, 15*time.Minute)

	t.Setenv("ADMIN_EMAILS", " admin@example.com ")
	adminCandidate, staleRentAccess, _, err := service.Register(ctx, "admin@example.com", "super-secure-pass-123", "Admin", "Candidate")
	if err != nil {
		t.Fatalf("register admin candidate: %v", err)
	}
	if adminCandidate.Role != RoleRent {
		t.Fatalf("public registration granted %s, want RENT", adminCandidate.Role)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET role='ADMIN' WHERE id=$1`, adminCandidate.ID); err != nil {
		t.Fatalf("provision admin: %v", err)
	}

	adminEndpoint := shared_middleware.Auth(secret, nil, pool)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !shared_middleware.IsAdmin(r.Context()) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	callAdmin := func(token string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res := httptest.NewRecorder()
		adminEndpoint.ServeHTTP(res, req)
		return res.Code
	}
	if status := callAdmin(staleRentAccess); status != http.StatusForbidden {
		t.Fatalf("pre-promotion RENT token gained admin access: %d", status)
	}
	freshAdminAccess, _, err := service.Login(ctx, adminCandidate.Email, "super-secure-pass-123")
	if err != nil {
		t.Fatalf("login provisioned admin: %v", err)
	}
	if status := callAdmin(freshAdminAccess); status != http.StatusNoContent {
		t.Fatalf("fresh provisioned admin denied: %d", status)
	}

	refreshUser, _, originalRefresh, err := service.Register(ctx, "refresh@example.com", "super-secure-pass-456", "Refresh", "User")
	if err != nil {
		t.Fatalf("register refresh user: %v", err)
	}
	const workers = 12
	start := make(chan struct{})
	results := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, successor, refreshErr := service.Refresh(ctx, originalRefresh)
			if refreshErr != nil {
				errs <- refreshErr
				return
			}
			results <- successor
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	if len(results) != 1 || len(errs) != workers-1 {
		t.Fatalf("concurrent refresh successes=%d failures=%d, want 1/%d", len(results), len(errs), workers-1)
	}
	successor := <-results
	var activeTokens int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM refresh_tokens WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>NOW()`, refreshUser.ID).Scan(&activeTokens); err != nil {
		t.Fatalf("count active refresh tokens: %v", err)
	}
	if activeTokens != 1 {
		t.Fatalf("active successor tokens=%d, want 1", activeTokens)
	}
	if err := service.Logout(ctx, successor); err != nil {
		t.Fatalf("logout successor: %v", err)
	}
	if _, _, err := service.Refresh(ctx, successor); err == nil {
		t.Fatal("logged-out refresh token remained usable")
	}

	if _, err := pool.Exec(ctx, `UPDATE users SET is_blocked=true WHERE id=$1`, adminCandidate.ID); err != nil {
		t.Fatalf("block admin: %v", err)
	}
	if status := callAdmin(freshAdminAccess); status != http.StatusUnauthorized {
		t.Fatalf("blocked access token status=%d, want 401", status)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET is_blocked=false, role='RENT' WHERE id=$1`, adminCandidate.ID); err != nil {
		t.Fatalf("demote admin: %v", err)
	}
	if status := callAdmin(freshAdminAccess); status != http.StatusForbidden {
		t.Fatalf("demoted ADMIN token status=%d, want 403", status)
	}
}
