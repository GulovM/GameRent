package payment

import (
	"strings"
	"testing"
)

func TestAdminPublicDepositStatusMapsUnknownCodeToUnknown(t *testing.T) {
	if got := adminPublicDepositStatus(700, 99); got != "UNKNOWN" {
		t.Fatalf("adminPublicDepositStatus(700, 99)=%q want UNKNOWN", got)
	}
	if got := adminPublicDepositStatus(0, 99); got != "NONE" {
		t.Fatalf("zero deposit status=%q want NONE", got)
	}
	if got := adminPublicDepositStatus(700, 0); got != "NONE" {
		t.Fatalf("missing hold status=%q want NONE", got)
	}
}

func TestAdminDepositUnknownFilterDoesNotMatchKnownOrMissingHolds(t *testing.T) {
	query, _ := adminRentalsBaseQuery(AdminRentalListFilter{DepositStatus: "UNKNOWN"})
	if want := "d.status NOT IN (1, 2, 3, 4)"; !strings.Contains(query, want) {
		t.Fatalf("UNKNOWN filter query does not contain %q: %s", want, query)
	}
}
