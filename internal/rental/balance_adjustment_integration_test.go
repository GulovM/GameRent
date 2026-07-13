package rental_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"rent_game_accs/internal/payment"
	pkg_postgres_pool "rent_game_accs/internal/pkg/repository/postgres/pool"
	repo_postgres "rent_game_accs/internal/repository/postgres"
	shared_authorization "rent_game_accs/internal/shared/authorization"
	"rent_game_accs/internal/user"
)

type financialAuthorizationSnapshot struct {
	balance       int64
	ledgerCount   int64
	refundCount   int64
	holdStatus    int16
	auditCount    int64
	securityCount int64
}

func TestAdminSteamSyncPersistenceRevalidatesCurrentAdmin(t *testing.T) {
	actorStates := []struct {
		name   string
		mutate string
	}{
		{name: "demoted", mutate: `UPDATE users SET role='RENT' WHERE id=$1`},
		{name: "blocked", mutate: `UPDATE users SET is_blocked=true WHERE id=$1`},
		{name: "deleted", mutate: `UPDATE users SET deleted_at=NOW() WHERE id=$1`},
	}
	for _, actorState := range actorStates {
		t.Run(actorState.name, func(t *testing.T) {
			pool, _ := setupTestDB(t)
			const accountID int64 = 97001
			if _, err := pool.Exec(context.Background(), `
				INSERT INTO accounts (id, login, encrypted_password, status, steam_guard_enabled, inventory_verified, hourly_price, deposit_amount, steam_id64)
				VALUES ($1, 'admin_sync_guard', $2, 2, true, true, 100, 0, '765611980097001')`, accountID, []byte("enc-pass")); err != nil {
				t.Fatalf("seed sync account: %v", err)
			}
			if _, err := pool.Exec(context.Background(), actorState.mutate, integrationAdminID); err != nil {
				t.Fatalf("mutate admin state: %v", err)
			}

			repo := repo_postgres.NewRepository(&pkg_postgres_pool.ConnectionPool{Pool: pool})
			err := repo.SyncAccountGamesAsCurrentAdmin(context.Background(), integrationAdminID, accountID, []repo_postgres.AccountGameSyncInfo{{StoreGameID: "97001", Name: "Forbidden Sync", PlaytimeMinutes: 1}})
			if !errors.Is(err, shared_authorization.ErrCurrentAdminRequired) {
				t.Fatalf("expected stale sync rejection, got %v", err)
			}
			err = repo.DisableAccountIfIdleAsCurrentAdmin(context.Background(), integrationAdminID, accountID)
			if !errors.Is(err, shared_authorization.ErrCurrentAdminRequired) {
				t.Fatalf("expected stale ban rejection, got %v", err)
			}

			var gameCount, relationCount int64
			var status int16
			var deleted bool
			if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM games WHERE steam_app_id=97001`).Scan(&gameCount); err != nil {
				t.Fatalf("count forbidden sync games: %v", err)
			}
			if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM account_games WHERE account_id=$1`, accountID).Scan(&relationCount); err != nil {
				t.Fatalf("count forbidden sync relations: %v", err)
			}
			if err := pool.QueryRow(context.Background(), `SELECT status, deleted_at IS NOT NULL FROM accounts WHERE id=$1`, accountID).Scan(&status, &deleted); err != nil {
				t.Fatalf("read forbidden sync account: %v", err)
			}
			if gameCount != 0 || relationCount != 0 || status != 2 || deleted {
				t.Fatalf("stale sync mutated state: games=%d relations=%d status=%d deleted=%t", gameCount, relationCount, status, deleted)
			}
		})
	}
}

func captureFinancialAuthorizationSnapshot(t *testing.T, pool *pgxpool.Pool, userID, rentalID int64) financialAuthorizationSnapshot {
	t.Helper()
	var snapshot financialAuthorizationSnapshot
	if err := pool.QueryRow(context.Background(), `
		SELECT
			u.balance,
			(SELECT COUNT(*) FROM financial_ledger_entries fle WHERE fle.user_id = u.id),
			(SELECT COUNT(*) FROM refunds r WHERE r.user_id = u.id),
			COALESCE((SELECT dh.status FROM deposit_holds dh WHERE dh.rental_id = $2), 0),
			(SELECT COUNT(*) FROM audit_logs),
			(SELECT COUNT(*) FROM security_events)
		FROM users u WHERE u.id = $1`, userID, rentalID).Scan(
		&snapshot.balance,
		&snapshot.ledgerCount,
		&snapshot.refundCount,
		&snapshot.holdStatus,
		&snapshot.auditCount,
		&snapshot.securityCount,
	); err != nil {
		t.Fatalf("capture financial authorization snapshot: %v", err)
	}
	return snapshot
}

func seedBalanceAdjustmentUsers(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (int64, int64) {
	t.Helper()
	var actorID, targetID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role, balance) VALUES ('balance-admin@example.com', 'hash', 'ADMIN', 0) RETURNING id`).Scan(&actorID); err != nil {
		t.Fatalf("seed balance adjustment admin: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role, balance) VALUES ('balance-target@example.com', 'hash', 'RENT', 1000) RETURNING id`).Scan(&targetID); err != nil {
		t.Fatalf("seed balance adjustment target: %v", err)
	}
	return actorID, targetID
}

func integrationAdjustmentInput(amount int64, key string) payment.AdminBalanceAdjustmentInput {
	return payment.AdminBalanceAdjustmentInput{
		Amount:         amount,
		Currency:       "USD",
		ReasonCode:     "MANUAL_COMPENSATION",
		Comment:        "integration test adjustment",
		IdempotencyKey: key,
	}
}

func TestAdminBalanceAdjustment_ConcurrentAdjustmentsAndReplay(t *testing.T) {
	pool, _ := setupTestDB(t)
	ctx := context.Background()
	actorID, targetID := seedBalanceAdjustmentUsers(t, ctx, pool)
	service := payment.NewPaymentService(payment.NewPostgresRepository(pool))

	type outcome struct {
		result *payment.AdminBalanceAdjustmentResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	var wg sync.WaitGroup
	for _, tc := range []struct {
		amount int64
		key    string
	}{{500, "integration-credit-001"}, {-300, "integration-debit-001"}} {
		wg.Add(1)
		go func(amount int64, key string) {
			defer wg.Done()
			result, err := service.AdjustAdminBalance(ctx, actorID, "ADMIN", targetID, integrationAdjustmentInput(amount, key), "127.0.0.1", "integration", time.Now())
			outcomes <- outcome{result: result, err: err}
		}(tc.amount, tc.key)
	}
	wg.Wait()
	close(outcomes)
	for item := range outcomes {
		if item.err != nil {
			t.Fatalf("concurrent adjustment failed: %v", item.err)
		}
		if item.result == nil || item.result.IdempotentReplay {
			t.Fatalf("unexpected concurrent result: %+v", item.result)
		}
	}

	var balance, ledgerCount, auditCount, securityCount int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM users WHERE id=$1`, targetID).Scan(&balance); err != nil {
		t.Fatalf("read final balance: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM financial_ledger_entries WHERE user_id=$1 AND entry_type IN (8,9)`, targetID).Scan(&ledgerCount); err != nil {
		t.Fatalf("count adjustment ledger entries: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE entity_type='user_balance' AND entity_id=$1 AND action='admin_balance_adjustment'`, targetID).Scan(&auditCount); err != nil {
		t.Fatalf("count adjustment audit logs: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM security_events WHERE user_id=$1 AND event_type=15`, targetID).Scan(&securityCount); err != nil {
		t.Fatalf("count adjustment security events: %v", err)
	}
	if balance != 1200 || ledgerCount != 2 || auditCount != 2 || securityCount != 2 {
		t.Fatalf("unexpected concurrent state balance=%d ledger=%d audit=%d security=%d", balance, ledgerCount, auditCount, securityCount)
	}

	input := integrationAdjustmentInput(200, "integration-replay-001")
	first, err := service.AdjustAdminBalance(ctx, actorID, "ADMIN", targetID, input, "127.0.0.1", "integration", time.Now())
	if err != nil {
		t.Fatalf("first replay candidate failed: %v", err)
	}
	replay, err := service.AdjustAdminBalance(ctx, actorID, "ADMIN", targetID, input, "127.0.0.1", "integration", time.Now())
	if err != nil {
		t.Fatalf("idempotent replay failed: %v", err)
	}
	if !replay.IdempotentReplay || replay.LedgerEntryID != first.LedgerEntryID || replay.NewBalance != first.NewBalance {
		t.Fatalf("unexpected replay first=%+v replay=%+v", first, replay)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM financial_ledger_entries WHERE idempotency_key='admin:balance-adjustment:integration-replay-001'`).Scan(&ledgerCount); err != nil {
		t.Fatalf("count replay ledger: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("replay created %d ledger entries", ledgerCount)
	}

	start := make(chan struct{})
	sameKeyOutcomes := make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			result, err := service.AdjustAdminBalance(ctx, actorID, "ADMIN", targetID, integrationAdjustmentInput(50, "integration-concurrent-replay-001"), "127.0.0.1", "integration", time.Now())
			sameKeyOutcomes <- outcome{result: result, err: err}
		}()
	}
	close(start)
	replayCount := 0
	for i := 0; i < 2; i++ {
		item := <-sameKeyOutcomes
		if item.err != nil {
			t.Fatalf("concurrent same-key adjustment failed: %v", item.err)
		}
		if item.result.IdempotentReplay {
			replayCount++
		}
	}
	if replayCount != 1 {
		t.Fatalf("expected one concurrent replay, got %d", replayCount)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM financial_ledger_entries WHERE idempotency_key='admin:balance-adjustment:integration-concurrent-replay-001'`).Scan(&ledgerCount); err != nil {
		t.Fatalf("count concurrent replay ledger: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("concurrent replay created %d ledger entries", ledgerCount)
	}
}

func TestAdminBalanceAdjustment_WaitsForUserRowLock(t *testing.T) {
	pool, _ := setupTestDB(t)
	ctx := context.Background()
	actorID, targetID := seedBalanceAdjustmentUsers(t, ctx, pool)
	service := payment.NewPaymentService(payment.NewPostgresRepository(pool))

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `SELECT balance FROM users WHERE id=$1 FOR UPDATE`, targetID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("lock target user: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := service.AdjustAdminBalance(ctx, actorID, "ADMIN", targetID, integrationAdjustmentInput(100, "integration-lock-001"), "", "", time.Now())
		done <- err
	}()

	select {
	case err := <-done:
		_ = tx.Rollback(ctx)
		t.Fatalf("adjustment completed before row lock released: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit lock transaction: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("adjustment failed after lock release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("adjustment remained blocked after row lock release")
	}
}

func TestAdminBalanceAdjustment_RejectsStaleAdminClaims(t *testing.T) {
	tests := []struct {
		name   string
		mutate string
	}{
		{name: "demoted", mutate: `UPDATE users SET role='RENT' WHERE id=$1`},
		{name: "blocked", mutate: `UPDATE users SET is_blocked=true WHERE id=$1`},
		{name: "deleted", mutate: `UPDATE users SET deleted_at=NOW() WHERE id=$1`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pool, _ := setupTestDB(t)
			ctx := context.Background()
			actorID, targetID := seedBalanceAdjustmentUsers(t, ctx, pool)
			if _, err := pool.Exec(ctx, tc.mutate, actorID); err != nil {
				t.Fatalf("mutate admin state: %v", err)
			}

			service := payment.NewPaymentService(payment.NewPostgresRepository(pool))
			_, err := service.AdjustAdminBalance(ctx, actorID, "ADMIN", targetID, integrationAdjustmentInput(100, "integration-stale-admin-"+tc.name), "", "", time.Now())
			if !errors.Is(err, payment.ErrAdminRequired) {
				t.Fatalf("expected current database role rejection, got %v", err)
			}

			var balance, ledgerCount int64
			if err := pool.QueryRow(ctx, `SELECT balance FROM users WHERE id=$1`, targetID).Scan(&balance); err != nil {
				t.Fatalf("read target balance: %v", err)
			}
			if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM financial_ledger_entries WHERE user_id=$1 AND entry_type IN (8,9)`, targetID).Scan(&ledgerCount); err != nil {
				t.Fatalf("count adjustment ledger entries: %v", err)
			}
			if balance != 1000 || ledgerCount != 0 {
				t.Fatalf("stale admin claim mutated state balance=%d ledger=%d", balance, ledgerCount)
			}
		})
	}
}

func TestAdminBalanceAdjustment_RollbackAndProfileStaleWriteProtection(t *testing.T) {
	pool, _ := setupTestDB(t)
	ctx := context.Background()
	actorID, targetID := seedBalanceAdjustmentUsers(t, ctx, pool)
	service := payment.NewPaymentService(payment.NewPostgresRepository(pool))

	if _, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION fail_balance_adjustment_audit() RETURNS trigger AS $$
		BEGIN
			IF NEW.action = 'admin_balance_adjustment' THEN
				RAISE EXCEPTION 'forced balance adjustment audit failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`); err != nil {
		t.Fatalf("create audit failure function: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TRIGGER trg_test_fail_balance_adjustment_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION fail_balance_adjustment_audit()`); err != nil {
		t.Fatalf("create audit failure trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS trg_test_fail_balance_adjustment_audit ON audit_logs`)
		_, _ = pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS fail_balance_adjustment_audit()`)
	})

	_, err := service.AdjustAdminBalance(ctx, actorID, "ADMIN", targetID, integrationAdjustmentInput(500, "integration-rollback-001"), "", "", time.Now())
	if err == nil {
		t.Fatal("expected forced audit failure")
	}
	var balance, ledgerCount, securityCount int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM users WHERE id=$1`, targetID).Scan(&balance); err != nil {
		t.Fatalf("read rollback balance: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM financial_ledger_entries WHERE idempotency_key='admin:balance-adjustment:integration-rollback-001'`).Scan(&ledgerCount); err != nil {
		t.Fatalf("count rollback ledger: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM security_events WHERE user_id=$1 AND event_type=15`, targetID).Scan(&securityCount); err != nil {
		t.Fatalf("count rollback security events: %v", err)
	}
	if balance != 1000 || ledgerCount != 0 || securityCount != 0 {
		t.Fatalf("rollback left partial state balance=%d ledger=%d security=%d", balance, ledgerCount, securityCount)
	}

	if _, err := pool.Exec(ctx, `DROP TRIGGER trg_test_fail_balance_adjustment_audit ON audit_logs`); err != nil {
		t.Fatalf("drop audit failure trigger: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP FUNCTION fail_balance_adjustment_audit()`); err != nil {
		t.Fatalf("drop audit failure function: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION fail_balance_adjustment_security() RETURNS trigger AS $$
		BEGIN
			IF NEW.event_type = 15 THEN
				RAISE EXCEPTION 'forced balance adjustment security failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`); err != nil {
		t.Fatalf("create security failure function: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TRIGGER trg_test_fail_balance_adjustment_security BEFORE INSERT ON security_events FOR EACH ROW EXECUTE FUNCTION fail_balance_adjustment_security()`); err != nil {
		t.Fatalf("create security failure trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS trg_test_fail_balance_adjustment_security ON security_events`)
		_, _ = pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS fail_balance_adjustment_security()`)
	})

	_, err = service.AdjustAdminBalance(ctx, actorID, "ADMIN", targetID, integrationAdjustmentInput(400, "integration-security-rollback-001"), "", "", time.Now())
	if err == nil {
		t.Fatal("expected forced security event failure")
	}
	var auditCount int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM users WHERE id=$1`, targetID).Scan(&balance); err != nil {
		t.Fatalf("read security rollback balance: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM financial_ledger_entries WHERE idempotency_key='admin:balance-adjustment:integration-security-rollback-001'`).Scan(&ledgerCount); err != nil {
		t.Fatalf("count security rollback ledger: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE entity_type='user_balance' AND entity_id=$1 AND action='admin_balance_adjustment'`, targetID).Scan(&auditCount); err != nil {
		t.Fatalf("count security rollback audit logs: %v", err)
	}
	if balance != 1000 || ledgerCount != 0 || auditCount != 0 {
		t.Fatalf("security failure rollback left partial state balance=%d ledger=%d audit=%d", balance, ledgerCount, auditCount)
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER trg_test_fail_balance_adjustment_security ON security_events`); err != nil {
		t.Fatalf("drop security failure trigger: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP FUNCTION fail_balance_adjustment_security()`); err != nil {
		t.Fatalf("drop security failure function: %v", err)
	}

	userRepo := user.NewPostgresRepository(pool)
	stale, err := userRepo.GetUser(ctx, targetID)
	if err != nil {
		t.Fatalf("load stale profile: %v", err)
	}
	if _, err := service.AdjustAdminBalance(ctx, actorID, "ADMIN", targetID, integrationAdjustmentInput(500, "integration-profile-001"), "", "", time.Now()); err != nil {
		t.Fatalf("financial operation before stale profile update: %v", err)
	}
	stale.FirstName = "Updated"
	stale.LastName = "Profile"
	stale.UpdatedAt = time.Now()
	if err := userRepo.UpdateUser(ctx, stale); err != nil {
		t.Fatalf("update stale profile: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT balance FROM users WHERE id=$1`, targetID).Scan(&balance); err != nil {
		t.Fatalf("read balance after profile update: %v", err)
	}
	if balance != 1500 {
		t.Fatalf("stale profile update overwrote financial balance: %d", balance)
	}
}

func TestAdminFinancialOperations_RejectStaleCurrentAdminWithoutMutation(t *testing.T) {
	var forfeitEvidenceReference string
	type operation struct {
		name  string
		setup func(*testing.T, *pgxpool.Pool) (int64, int64)
		call  func(*payment.PaymentService, int64, int64, string) error
	}
	operations := []operation{
		{
			name: "balance adjustment",
			setup: func(t *testing.T, pool *pgxpool.Pool) (int64, int64) {
				const userID int64 = 780001
				if _, err := pool.Exec(context.Background(), `INSERT INTO users (id, email, password_hash, role, balance) VALUES ($1, 'stale-adjust-target@example.com', 'hash', 'RENT', 1000)`, userID); err != nil {
					t.Fatalf("seed adjustment target: %v", err)
				}
				return userID, 0
			},
			call: func(service *payment.PaymentService, userID, _ int64, key string) error {
				_, err := service.AdjustAdminBalance(context.Background(), integrationAdminID, "ADMIN", userID, integrationAdjustmentInput(100, key), "", "", time.Now())
				return err
			},
		},
		{
			name: "wallet refund",
			setup: func(t *testing.T, pool *pgxpool.Pool) (int64, int64) {
				const userID, accountID, rentalID, paymentID int64 = 780101, 780102, 780103, 780104
				seedWalletPaidRefundableRental(t, pool, userID, accountID, rentalID, paymentID, 10000, 500, 500)
				return userID, rentalID
			},
			call: func(service *payment.PaymentService, _, rentalID int64, _ string) error {
				_, err := service.RefundWalletPayment(context.Background(), integrationAdminID, "ADMIN", rentalID, "SERVICE_UNAVAILABLE", time.Now())
				return err
			},
		},
		{
			name: "deposit release",
			setup: func(t *testing.T, pool *pgxpool.Pool) (int64, int64) {
				const userID, accountID, rentalID, paymentID int64 = 780201, 780202, 780203, 780204
				seedHeldDepositSettlementRental(t, pool, userID, accountID, rentalID, paymentID, 500, 500)
				return userID, rentalID
			},
			call: func(service *payment.PaymentService, _, rentalID int64, _ string) error {
				_, err := service.ReleaseDeposit(context.Background(), integrationAdminID, "ADMIN", rentalID, time.Now())
				return err
			},
		},
		{
			name: "deposit forfeit",
			setup: func(t *testing.T, pool *pgxpool.Pool) (int64, int64) {
				const userID, accountID, rentalID, paymentID int64 = 780301, 780302, 780303, 780304
				seedHeldDepositSettlementRental(t, pool, userID, accountID, rentalID, paymentID, 500, 500)
				forfeitEvidenceReference = settlementEvidenceReference(t, pool, rentalID)
				return userID, rentalID
			},
			call: func(service *payment.PaymentService, _, rentalID int64, _ string) error {
				_, err := service.ForfeitDeposit(context.Background(), integrationAdminID, "ADMIN", rentalID, "DAMAGE_CONFIRMED", forfeitEvidenceReference, time.Now())
				return err
			},
		},
	}
	actorStates := []struct {
		name   string
		mutate string
	}{
		{name: "demoted", mutate: `UPDATE users SET role='RENT' WHERE id=$1`},
		{name: "blocked", mutate: `UPDATE users SET is_blocked=true WHERE id=$1`},
		{name: "deleted", mutate: `UPDATE users SET deleted_at=NOW() WHERE id=$1`},
	}

	for _, operation := range operations {
		for _, actorState := range actorStates {
			t.Run(operation.name+"/first attempt/"+actorState.name, func(t *testing.T) {
				pool, _ := setupTestDB(t)
				userID, rentalID := operation.setup(t, pool)
				before := captureFinancialAuthorizationSnapshot(t, pool, userID, rentalID)
				if _, err := pool.Exec(context.Background(), actorState.mutate, integrationAdminID); err != nil {
					t.Fatalf("mutate current admin: %v", err)
				}
				err := operation.call(payment.NewPaymentService(payment.NewPostgresRepository(pool)), userID, rentalID, "stale-first-"+actorState.name)
				if !errors.Is(err, payment.ErrAdminRequired) {
					t.Fatalf("expected stale current-admin rejection, got %v", err)
				}
				after := captureFinancialAuthorizationSnapshot(t, pool, userID, rentalID)
				if after != before {
					t.Fatalf("authorization failure mutated financial state: before=%+v after=%+v", before, after)
				}
			})

			t.Run(operation.name+"/replay/"+actorState.name, func(t *testing.T) {
				pool, _ := setupTestDB(t)
				userID, rentalID := operation.setup(t, pool)
				service := payment.NewPaymentService(payment.NewPostgresRepository(pool))
				key := fmt.Sprintf("stale-replay-%s-%s", strings.ReplaceAll(operation.name, " ", "-"), actorState.name)
				if err := operation.call(service, userID, rentalID, key); err != nil {
					t.Fatalf("initial privileged operation failed: %v", err)
				}
				beforeReplay := captureFinancialAuthorizationSnapshot(t, pool, userID, rentalID)
				if _, err := pool.Exec(context.Background(), actorState.mutate, integrationAdminID); err != nil {
					t.Fatalf("mutate current admin before replay: %v", err)
				}
				err := operation.call(service, userID, rentalID, key)
				if !errors.Is(err, payment.ErrAdminRequired) {
					t.Fatalf("expected stale replay rejection, got %v", err)
				}
				afterReplay := captureFinancialAuthorizationSnapshot(t, pool, userID, rentalID)
				if afterReplay != beforeReplay {
					t.Fatalf("stale replay duplicated financial state: before=%+v after=%+v", beforeReplay, afterReplay)
				}
			})
		}
	}
}
