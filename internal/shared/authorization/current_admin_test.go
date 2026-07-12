package authorization

import (
	"context"
	"errors"
	"testing"
)

func TestRequireCurrentAdminForMutation_RequiresExplicitTransaction(t *testing.T) {
	err := RequireCurrentAdminForMutation(context.Background(), nil, 1)
	if !errors.Is(err, ErrTransactionRequired) {
		t.Fatalf("expected transaction requirement, got %v", err)
	}
}
