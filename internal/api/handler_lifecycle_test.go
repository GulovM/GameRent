package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rent_game_accs/internal/payment"
	shared_middleware "rent_game_accs/internal/shared/middleware"
	shared_response "rent_game_accs/internal/shared/response"
)

func TestExtendRental_ReturnsNotSupportedWithoutMutationDependencies(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/rentals/42/extend", strings.NewReader(`{"duration_hours":1}`))
	recorder := httptest.NewRecorder()

	h.ExtendRental(recorder, req)

	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusNotImplemented, recorder.Body.String())
	}
	var response shared_response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error == nil || response.Error.Code != "EXTENSION_NOT_SUPPORTED" {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
}

func TestAdminUpdateAccount_RejectsGenericStatusFieldBeforeDatabaseMutation(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPatch, "/admin/accounts/42", strings.NewReader(`{"status":2}`))
	req.SetPathValue("accountId", "42")
	ctx := context.WithValue(req.Context(), shared_middleware.UserRoleKey, "ADMIN")
	req = req.WithContext(ctx)
	recorder := httptest.NewRecorder()

	h.AdminUpdateAccount(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
}

func TestPublicDepositStatusMapsUnknownCodeToUnknown(t *testing.T) {
	if got := publicDepositStatus(700, 99); got != "UNKNOWN" {
		t.Fatalf("publicDepositStatus(700, 99)=%q want UNKNOWN", got)
	}
	if got := publicDepositStatus(0, 99); got != "NONE" {
		t.Fatalf("zero deposit status=%q want NONE", got)
	}
	if got := publicDepositStatus(700, 0); got != "NONE" {
		t.Fatalf("missing hold status=%q want NONE", got)
	}
}

func TestAdminForfeitDepositRequest_RequiresAllowlistedReasonAndEvidenceReference(t *testing.T) {
	tests := []struct {
		name    string
		request adminForfeitDepositRequest
		wantErr bool
	}{
		{
			name: "valid",
			request: adminForfeitDepositRequest{
				ReasonCode:        "DAMAGE_CONFIRMED",
				EvidenceReference: "SECURITY_EVENT:42",
			},
		},
		{
			name: "trims canonical values",
			request: adminForfeitDepositRequest{
				ReasonCode:        " DAMAGE_CONFIRMED ",
				EvidenceReference: " SECURITY_EVENT:42 ",
			},
		},
		{
			name: "arbitrary reason rejected",
			request: adminForfeitDepositRequest{
				ReasonCode:        "damage_confirmed",
				EvidenceReference: "SECURITY_EVENT:42",
			},
			wantErr: true,
		},
		{
			name: "missing evidence rejected",
			request: adminForfeitDepositRequest{
				ReasonCode: "DAMAGE_CONFIRMED",
			},
			wantErr: true,
		},
		{
			name: "free form evidence rejected",
			request: adminForfeitDepositRequest{
				ReasonCode:        "DAMAGE_CONFIRMED",
				EvidenceReference: "user pasted evidence here",
			},
			wantErr: true,
		},
		{
			name: "non-positive security event rejected",
			request: adminForfeitDepositRequest{
				ReasonCode:        "DAMAGE_CONFIRMED",
				EvidenceReference: "SECURITY_EVENT:0",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestDepositSettlementResponse_PreservesStatusAndAddsLifecycleFields(t *testing.T) {
	settledAt := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	completedAt := settledAt.Add(time.Second)
	response := depositSettlementResponse(&payment.DepositSettlementResult{
		Changed:       true,
		Idempotent:    false,
		DepositStatus: "RELEASED",
		SettledAt:     &settledAt,
		RentalStatus:  4,
		CompletedAt:   &completedAt,
	})

	if response["status"] != "RELEASED" || response["deposit_status"] != "RELEASED" {
		t.Fatalf("legacy/additive deposit status mismatch: %+v", response)
	}
	if response["changed"] != true || response["idempotent"] != false {
		t.Fatalf("settlement flags mismatch: %+v", response)
	}
	if response["settled_at"] != &settledAt || response["completed_at"] != &completedAt || response["rental_status"] != int16(4) {
		t.Fatalf("settlement lifecycle fields mismatch: %+v", response)
	}
}
