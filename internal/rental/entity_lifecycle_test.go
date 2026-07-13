package rental

import (
	"errors"
	"testing"
	"time"
)

func TestRentalCancel_AllowsOnlyWaitingPayment(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)

	waiting := &Rental{Status: StatusWaitingPayment}
	if err := waiting.Cancel("user requested", now); err != nil {
		t.Fatalf("waiting-payment cancel failed: %v", err)
	}
	if waiting.Status != StatusCancelled {
		t.Fatalf("status=%v want=%v", waiting.Status, StatusCancelled)
	}

	active := &Rental{Status: StatusActive}
	if err := active.Cancel("too late", now); !errors.Is(err, ErrCannotCancel) {
		t.Fatalf("active cancel err=%v want=%v", err, ErrCannotCancel)
	}
	if active.Status != StatusActive {
		t.Fatalf("active rental mutated to %v", active.Status)
	}
}

func TestRentalPeriod_IsExpiredAtBoundary(t *testing.T) {
	endAt := time.Date(2026, 7, 13, 13, 0, 0, 0, time.UTC)
	period := RentalPeriod{StartAt: endAt.Add(-time.Hour), EndAt: endAt}

	if period.IsExpired(endAt.Add(-time.Nanosecond)) {
		t.Fatal("period expired before end_at")
	}
	if !period.IsExpired(endAt) {
		t.Fatal("period must be expired at end_at")
	}
}

func TestRentalExpireAndComplete_KeepUsageAndClosureTimestampsSeparate(t *testing.T) {
	endAt := time.Date(2026, 7, 13, 13, 0, 0, 0, time.UTC)
	rental := &Rental{
		Status: StatusActive,
		Period: RentalPeriod{StartAt: endAt.Add(-time.Hour), EndAt: endAt},
	}

	if err := rental.Expire(endAt.Add(-time.Nanosecond)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("early expiry err=%v want=%v", err, ErrInvalidTransition)
	}
	if err := rental.Expire(endAt); err != nil {
		t.Fatalf("expiry at boundary failed: %v", err)
	}
	if rental.ActualFinishedAt == nil || !rental.ActualFinishedAt.Equal(endAt) {
		t.Fatalf("actual_finished_at=%v want=%v", rental.ActualFinishedAt, endAt)
	}

	completedAt := endAt.Add(15 * time.Minute)
	if err := rental.Complete(completedAt); err != nil {
		t.Fatalf("completion failed: %v", err)
	}
	if rental.CompletedAt == nil || !rental.CompletedAt.Equal(completedAt) {
		t.Fatalf("completed_at=%v want=%v", rental.CompletedAt, completedAt)
	}
	if rental.ActualFinishedAt == nil || !rental.ActualFinishedAt.Equal(endAt) {
		t.Fatalf("completion overwrote actual_finished_at: %v", rental.ActualFinishedAt)
	}
}
