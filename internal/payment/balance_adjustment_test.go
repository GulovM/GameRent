package payment

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func validAdminBalanceAdjustmentInput(amount int64, key string) AdminBalanceAdjustmentInput {
	return AdminBalanceAdjustmentInput{
		Amount:         amount,
		Currency:       "USD",
		ReasonCode:     "MANUAL_COMPENSATION",
		Comment:        "Support-approved balance correction",
		IdempotencyKey: key,
	}
}

func TestPaymentService_AdjustAdminBalance_PositiveAndNegative(t *testing.T) {
	repo := newMockRepository(nil)
	repo.adjustmentBalances[42] = 1000
	service := NewPaymentService(repo)
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)

	credit, err := service.AdjustAdminBalance(context.Background(), 7, "ADMIN", 42, validAdminBalanceAdjustmentInput(500, "adjust-credit-001"), "127.0.0.1", "test", now)
	if err != nil {
		t.Fatalf("positive adjustment failed: %v", err)
	}
	if credit.PreviousBalance != 1000 || credit.NewBalance != 1500 || credit.Amount != 500 || credit.IdempotentReplay {
		t.Fatalf("unexpected credit result: %+v", credit)
	}

	debit, err := service.AdjustAdminBalance(context.Background(), 7, "ADMIN", 42, validAdminBalanceAdjustmentInput(-300, "adjust-debit-001"), "127.0.0.1", "test", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("negative adjustment failed: %v", err)
	}
	if debit.PreviousBalance != 1500 || debit.NewBalance != 1200 || debit.Amount != -300 {
		t.Fatalf("unexpected debit result: %+v", debit)
	}
	if repo.adjustmentBalances[42] != 1200 || len(repo.adjustmentLedgerRecords) != 2 || repo.auditCalls != 2 || repo.securityEventCalls != 2 {
		t.Fatalf("unexpected persisted mock state: balance=%d ledger=%d audit=%d security=%d", repo.adjustmentBalances[42], len(repo.adjustmentLedgerRecords), repo.auditCalls, repo.securityEventCalls)
	}
}

func TestPaymentService_AdjustAdminBalance_ValidationAndAuthorization(t *testing.T) {
	tests := []struct {
		name        string
		role        string
		input       AdminBalanceAdjustmentInput
		expectedErr error
	}{
		{name: "non admin", role: "RENT", input: validAdminBalanceAdjustmentInput(100, "adjust-auth-001"), expectedErr: ErrAdminRequired},
		{name: "zero amount", role: "ADMIN", input: validAdminBalanceAdjustmentInput(0, "adjust-zero-001"), expectedErr: ErrBalanceAdjustmentAmountRequired},
		{name: "missing reason", role: "ADMIN", input: AdminBalanceAdjustmentInput{Amount: 100, Currency: "USD", IdempotencyKey: "adjust-reason-001"}, expectedErr: ErrBalanceAdjustmentReasonRequired},
		{name: "unsupported currency", role: "ADMIN", input: AdminBalanceAdjustmentInput{Amount: 100, Currency: "TJS", ReasonCode: "MANUAL", IdempotencyKey: "adjust-currency-001"}, expectedErr: ErrBalanceAdjustmentCurrencyUnsupported},
		{name: "missing idempotency", role: "ADMIN", input: AdminBalanceAdjustmentInput{Amount: 100, Currency: "USD", ReasonCode: "MANUAL"}, expectedErr: ErrBalanceAdjustmentIdempotencyRequired},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMockRepository(nil)
			repo.adjustmentBalances[42] = 1000
			_, err := NewPaymentService(repo).AdjustAdminBalance(context.Background(), 7, tc.role, 42, tc.input, "", "", time.Now())
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("expected %v, got %v", tc.expectedErr, err)
			}
			if repo.adjustmentBalances[42] != 1000 || len(repo.adjustmentLedgerRecords) != 0 {
				t.Fatalf("validation failure mutated state")
			}
		})
	}
}

func TestPaymentService_AdjustAdminBalance_RejectsMissingUserAndNegativeResult(t *testing.T) {
	repo := newMockRepository(nil)
	service := NewPaymentService(repo)

	_, err := service.AdjustAdminBalance(context.Background(), 7, "ADMIN", 404, validAdminBalanceAdjustmentInput(100, "adjust-missing-001"), "", "", time.Now())
	if !errors.Is(err, ErrFinancialUserNotFound) {
		t.Fatalf("expected missing user, got %v", err)
	}

	repo.adjustmentBalances[42] = 100
	_, err = service.AdjustAdminBalance(context.Background(), 7, "ADMIN", 42, validAdminBalanceAdjustmentInput(-101, "adjust-negative-001"), "", "", time.Now())
	if !errors.Is(err, ErrBalanceAdjustmentWouldBeNegative) {
		t.Fatalf("expected negative balance rejection, got %v", err)
	}
	if repo.adjustmentBalances[42] != 100 || len(repo.adjustmentLedgerRecords) != 0 {
		t.Fatalf("negative balance rejection mutated state")
	}
}

func TestPaymentService_AdjustAdminBalance_RejectsIntegerOverflow(t *testing.T) {
	repo := newMockRepository(nil)
	repo.adjustmentBalances[42] = math.MaxInt64

	_, err := NewPaymentService(repo).AdjustAdminBalance(context.Background(), 7, "ADMIN", 42, validAdminBalanceAdjustmentInput(1, "adjust-overflow-001"), "", "", time.Now())
	if !errors.Is(err, ErrBalanceAdjustmentOverflow) {
		t.Fatalf("expected overflow rejection, got %v", err)
	}
	if repo.adjustmentBalances[42] != math.MaxInt64 || len(repo.adjustmentLedgerRecords) != 0 {
		t.Fatal("overflow rejection mutated state")
	}
}

func TestPaymentService_AdjustAdminBalance_ReplayAndConflict(t *testing.T) {
	repo := newMockRepository(nil)
	repo.adjustmentBalances[42] = 1000
	service := NewPaymentService(repo)
	input := validAdminBalanceAdjustmentInput(250, "adjust-replay-001")

	first, err := service.AdjustAdminBalance(context.Background(), 7, "ADMIN", 42, input, "", "", time.Now())
	if err != nil {
		t.Fatalf("first adjustment failed: %v", err)
	}
	replay, err := service.AdjustAdminBalance(context.Background(), 7, "ADMIN", 42, input, "", "", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("replay failed: %v", err)
	}
	if !replay.IdempotentReplay || replay.AdjustmentID != first.AdjustmentID || replay.NewBalance != first.NewBalance {
		t.Fatalf("unexpected replay: first=%+v replay=%+v", first, replay)
	}
	if repo.adjustmentBalances[42] != 1250 || len(repo.adjustmentLedgerRecords) != 1 || repo.auditCalls != 1 || repo.securityEventCalls != 1 {
		t.Fatalf("replay duplicated mutation")
	}

	conflictInput := input
	conflictInput.Amount = 300
	_, err = service.AdjustAdminBalance(context.Background(), 7, "ADMIN", 42, conflictInput, "", "", time.Now())
	if !errors.Is(err, ErrBalanceAdjustmentIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}

func TestPaymentService_AdjustAdminBalance_RollsBackOnAuditFailure(t *testing.T) {
	repo := newMockRepository(nil)
	repo.adjustmentBalances[42] = 1000
	repo.auditErr = errors.New("audit unavailable")

	_, err := NewPaymentService(repo).AdjustAdminBalance(context.Background(), 7, "ADMIN", 42, validAdminBalanceAdjustmentInput(500, "adjust-rollback-001"), "", "", time.Now())
	if err == nil {
		t.Fatal("expected audit failure")
	}
	if repo.adjustmentBalances[42] != 1000 || len(repo.adjustmentLedgerRecords) != 0 || len(repo.adjustmentsByKey) != 0 || repo.securityEventCalls != 0 {
		t.Fatalf("rollback left partial state: balance=%d ledger=%d adjustments=%d security=%d", repo.adjustmentBalances[42], len(repo.adjustmentLedgerRecords), len(repo.adjustmentsByKey), repo.securityEventCalls)
	}
}

func TestPaymentService_AdminFinancialOperations_RequireCurrentDatabaseAdmin(t *testing.T) {
	tests := []struct {
		name string
		call func(*PaymentService) error
	}{
		{
			name: "balance adjustment",
			call: func(service *PaymentService) error {
				_, err := service.AdjustAdminBalance(context.Background(), 900, "ADMIN", 42, validAdminBalanceAdjustmentInput(100, "authorization-denied-adjustment"), "", "", time.Now())
				return err
			},
		},
		{
			name: "wallet refund",
			call: func(service *PaymentService) error {
				_, err := service.RefundWalletPayment(context.Background(), 900, "ADMIN", 100, "SERVICE_UNAVAILABLE", time.Now())
				return err
			},
		},
		{
			name: "deposit release",
			call: func(service *PaymentService) error {
				_, err := service.ReleaseDeposit(context.Background(), 900, "ADMIN", 100, time.Now())
				return err
			},
		},
		{
			name: "deposit forfeit",
			call: func(service *PaymentService) error {
				_, err := service.ForfeitDeposit(context.Background(), 900, "ADMIN", 100, "DAMAGE_CONFIRMED", "SECURITY_EVENT:1", time.Now())
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMockRepository(nil)
			repo.currentAdminErr = ErrAdminRequired
			repo.adjustmentBalances[42] = 1000

			err := tc.call(NewPaymentService(repo))
			if !errors.Is(err, ErrAdminRequired) {
				t.Fatalf("expected current-admin rejection, got %v", err)
			}
			if repo.currentAdminCalls != 1 {
				t.Fatalf("expected one current-admin check, got %d", repo.currentAdminCalls)
			}
			if repo.balanceCreditCalls != 0 || repo.refundCreateCalls != 0 || repo.depositReleaseCalls != 0 || repo.depositForfeitCalls != 0 || len(repo.adjustmentLedgerRecords) != 0 || repo.auditCalls != 0 || repo.securityEventCalls != 0 {
				t.Fatalf("authorization failure reached financial mutation: %+v", repo)
			}
		})
	}
}

func TestPaymentService_RefundWalletPayment_RejectsSystemRoleBypass(t *testing.T) {
	repo := newMockRepository(nil)
	_, err := NewPaymentService(repo).RefundWalletPayment(context.Background(), 0, "SYSTEM", 100, "SERVICE_UNAVAILABLE", time.Now())
	if !errors.Is(err, ErrAdminRequired) {
		t.Fatalf("expected SYSTEM role rejection, got %v", err)
	}
	if repo.currentAdminCalls != 0 || repo.refundCreateCalls != 0 || repo.balanceCreditCalls != 0 || len(repo.balanceRefundEntries) != 0 {
		t.Fatal("SYSTEM role bypass reached refund authorization or mutation")
	}
}
