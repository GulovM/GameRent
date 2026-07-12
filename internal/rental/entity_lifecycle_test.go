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
