package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
