package payment

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDepositForfeitPolicyValidation(t *testing.T) {
	if !IsAllowedDepositForfeitReasonCode("DAMAGE_CONFIRMED") {
		t.Fatal("expected DAMAGE_CONFIRMED to be allow-listed")
	}
	if IsAllowedDepositForfeitReasonCode("damage_confirmed") {
		t.Fatal("arbitrary reason code must be rejected")
	}
	if !IsValidDepositEvidenceReference("SECURITY_EVENT:42") {
		t.Fatal("expected canonical evidence reference to be valid")
	}
	for _, invalid := range []string{"", "SECURITY_EVENT:0", "SECURITY_EVENT:-1", "AUDIT_LOG:42", "raw evidence"} {
		if IsValidDepositEvidenceReference(invalid) {
			t.Fatalf("expected evidence reference %q to be invalid", invalid)
		}
	}
}

func TestPaymentService_ForfeitDeposit_DeadlineAndExactReplay(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)

	t.Run("at deadline is rejected", func(t *testing.T) {
		repo := newMockRepository(nil)
		deadline := now
		seedSettlementState(repo, &DepositSettlementState{
			HoldID: 1, RentalID: 10, UserID: 20, AccountID: 30, PaymentID: 40,
			HoldStatus: depositHoldStatusHeld, RentalStatus: 3, PaymentStatus: 2,
			Amount: 500, Currency: "USD", ReviewDeadlineAt: &deadline,
		}, &DepositSettlementEligibility{RentalExists: true})

		_, err := NewPaymentService(repo).ForfeitDeposit(context.Background(), 99, "ADMIN", 10, "DAMAGE_CONFIRMED", "SECURITY_EVENT:1", now)
		if !errors.Is(err, ErrDepositReviewDeadlinePassed) {
			t.Fatalf("expected deadline rejection, got %v", err)
		}
	})

	t.Run("same evidence replays but changed evidence conflicts", func(t *testing.T) {
		repo := newMockRepository(nil)
		deadline := now.Add(time.Hour)
		seedSettlementState(repo, &DepositSettlementState{
			HoldID: 2, RentalID: 11, UserID: 21, AccountID: 31, PaymentID: 41,
			HoldStatus: depositHoldStatusHeld, RentalStatus: 3, PaymentStatus: 2,
			Amount: 500, Currency: "USD", ReviewDeadlineAt: &deadline,
		}, &DepositSettlementEligibility{RentalExists: true})
		service := NewPaymentService(repo)

		first, err := service.ForfeitDeposit(context.Background(), 99, "ADMIN", 11, "DAMAGE_CONFIRMED", "SECURITY_EVENT:1", now)
		if err != nil || !first.Changed || first.RentalStatus != 4 {
			t.Fatalf("first forfeit failed: result=%+v err=%v", first, err)
		}
		replay, err := service.ForfeitDeposit(context.Background(), 99, "ADMIN", 11, "DAMAGE_CONFIRMED", "SECURITY_EVENT:1", now.Add(time.Minute))
		if err != nil || !replay.Idempotent || replay.Changed {
			t.Fatalf("exact replay failed: result=%+v err=%v", replay, err)
		}
		_, err = service.ForfeitDeposit(context.Background(), 99, "ADMIN", 11, "DAMAGE_CONFIRMED", "SECURITY_EVENT:2", now.Add(time.Minute))
		if !errors.Is(err, ErrDepositAlreadySettled) {
			t.Fatalf("changed evidence should conflict, got %v", err)
		}
	})
}

func TestPaymentService_ReleaseDeposit_MissingDeadlineFailsClosed(t *testing.T) {
	repo := newMockRepository(nil)
	state := &DepositSettlementState{
		HoldID: 3, RentalID: 12, UserID: 22, AccountID: 32, PaymentID: 42,
		HoldStatus: depositHoldStatusHeld, RentalStatus: 3, PaymentStatus: 2,
		Amount: 500, Currency: "USD",
	}
	seedSettlementState(repo, state, &DepositSettlementEligibility{RentalExists: true})
	state.ReviewDeadlineAt = nil

	_, err := NewPaymentService(repo).ReleaseDeposit(context.Background(), 99, "ADMIN", 12, time.Now())
	if !errors.Is(err, ErrDepositSettlementNotAllowed) {
		t.Fatalf("expected missing deadline to fail closed, got %v", err)
	}
}

func TestPaymentService_LegacyTerminalSettlementReplayIsIdempotent(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	deadline := now.Add(-time.Hour)

	t.Run("released without source", func(t *testing.T) {
		repo := newMockRepository(nil)
		seedSettlementState(repo, &DepositSettlementState{
			HoldID: 4, RentalID: 13, UserID: 23, AccountID: 33, PaymentID: 43,
			HoldStatus: depositHoldStatusReleased, RentalStatus: 3, PaymentStatus: 2,
			Amount: 500, Currency: "USD", ReviewDeadlineAt: &deadline, ReleasedAt: &now,
		}, &DepositSettlementEligibility{RentalExists: true})

		result, err := NewPaymentService(repo).ReleaseDeposit(context.Background(), 99, "ADMIN", 13, now)
		if err != nil || !result.Idempotent || result.Changed || result.RentalStatus != 4 {
			t.Fatalf("legacy release replay failed: result=%+v err=%v", result, err)
		}
	})

	t.Run("forfeited without source", func(t *testing.T) {
		repo := newMockRepository(nil)
		seedSettlementState(repo, &DepositSettlementState{
			HoldID: 5, RentalID: 14, UserID: 24, AccountID: 34, PaymentID: 44,
			HoldStatus: depositHoldStatusForfeited, RentalStatus: 3, PaymentStatus: 2,
			Amount: 500, Currency: "USD", ReviewDeadlineAt: &deadline, ForfeitedAt: &now,
		}, &DepositSettlementEligibility{RentalExists: true})

		result, err := NewPaymentService(repo).ForfeitDeposit(context.Background(), 99, "ADMIN", 14, "DAMAGE_CONFIRMED", "SECURITY_EVENT:999", now)
		if err != nil || !result.Idempotent || result.Changed || result.RentalStatus != 4 {
			t.Fatalf("legacy forfeit replay failed: result=%+v err=%v", result, err)
		}
	})
}

func TestPaymentService_FinalizeExpiredRentals(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)

	t.Run("zero deposit completes", func(t *testing.T) {
		repo := newMockRepository(nil)
		seedSettlementState(repo, &DepositSettlementState{
			RentalID: 100, UserID: 200, AccountID: 300, PaymentID: 400,
			RentalStatus: 3, PaymentStatus: 2, Currency: "USD",
		}, &DepositSettlementEligibility{RentalExists: true})

		result, err := NewPaymentService(repo).FinalizeExpiredRentals(context.Background(), now, 10)
		if err != nil || result.Processed != 1 || result.Completed != 1 || result.AutoReleased != 0 {
			t.Fatalf("unexpected zero-deposit finalizer result=%+v err=%v", result, err)
		}
		if state := repo.settlementStatesByRental[100]; state.RentalStatus != 4 || state.CompletedAt == nil {
			t.Fatalf("rental was not completed: %+v", state)
		}
	})

	t.Run("held deposit auto releases exactly at deadline", func(t *testing.T) {
		repo := newMockRepository(nil)
		deadline := now
		seedSettlementState(repo, &DepositSettlementState{
			HoldID: 101, RentalID: 110, UserID: 210, AccountID: 310, PaymentID: 410,
			HoldStatus: depositHoldStatusHeld, RentalStatus: 3, PaymentStatus: 2,
			Amount: 700, Currency: "USD", UserBalance: 1000, ReviewDeadlineAt: &deadline,
		}, &DepositSettlementEligibility{RentalExists: true})

		result, err := NewPaymentService(repo).FinalizeExpiredRentals(context.Background(), now, 10)
		if err != nil || result.AutoReleased != 1 || result.Completed != 1 {
			t.Fatalf("unexpected auto-release result=%+v err=%v", result, err)
		}
		state := repo.settlementStatesByRental[110]
		if state.HoldStatus != depositHoldStatusReleased || state.RentalStatus != 4 || state.UserBalance != 1700 {
			t.Fatalf("unexpected auto-release state: %+v", state)
		}
		if len(repo.depositReleaseEntries) != 1 || repo.depositReleaseEntries[0].IdempotencyKey != "deposit:auto-release:rental:110" {
			t.Fatalf("unexpected auto-release ledger: %+v", repo.depositReleaseEntries)
		}
	})

	t.Run("before deadline and inconsistent positive deposit are skipped", func(t *testing.T) {
		repo := newMockRepository(nil)
		deadline := now.Add(time.Second)
		seedSettlementState(repo, &DepositSettlementState{
			HoldID: 102, RentalID: 120, UserID: 220, AccountID: 320, PaymentID: 420,
			HoldStatus: depositHoldStatusHeld, RentalStatus: 3, PaymentStatus: 2,
			Amount: 700, Currency: "USD", ReviewDeadlineAt: &deadline,
		}, &DepositSettlementEligibility{RentalExists: true})
		repo.settlementStatesByRental[121] = &DepositSettlementState{
			RentalID: 121, UserID: 221, AccountID: 321, PaymentID: 421,
			RentalStatus: 3, PaymentStatus: 2, RentalDepositAmount: 700, Currency: "USD",
		}

		result, err := NewPaymentService(repo).FinalizeExpiredRentals(context.Background(), now, 10)
		if err != nil || result.Processed != 0 {
			t.Fatalf("ineligible rentals must be skipped: result=%+v err=%v", result, err)
		}
	})
}
