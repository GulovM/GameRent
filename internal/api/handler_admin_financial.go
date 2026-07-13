package api

import (
	"errors"
	"net/http"
	"rent_game_accs/internal/payment"
	pkg_http_request "rent_game_accs/internal/pkg/transport/http/request"
	shared_middleware "rent_game_accs/internal/shared/middleware"
	shared_response "rent_game_accs/internal/shared/response"
	"strings"
	"time"
)

type adminForfeitDepositRequest struct {
	ReasonCode        string `json:"reason_code"`
	EvidenceReference string `json:"evidence_reference"`
}

func (r *adminForfeitDepositRequest) Validate() error {
	r.ReasonCode = strings.TrimSpace(r.ReasonCode)
	if !payment.IsAllowedDepositForfeitReasonCode(r.ReasonCode) {
		return errText("reason_code must be one of the supported deposit forfeit reason codes")
	}
	r.EvidenceReference = strings.TrimSpace(r.EvidenceReference)
	if !payment.IsValidDepositEvidenceReference(r.EvidenceReference) {
		return errText("evidence_reference must be SECURITY_EVENT:<positive integer>")
	}
	return nil
}

type adminWalletRefundRequest struct {
	ReasonCode string `json:"reason_code"`
}

func (r *adminWalletRefundRequest) Validate() error {
	r.ReasonCode = strings.TrimSpace(r.ReasonCode)
	if !payment.IsAllowedWalletRefundReasonCode(r.ReasonCode) {
		return errText("reason_code must be one of the supported wallet refund reason codes")
	}
	return nil
}

func (h *Handler) AdminWalletRefund(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	rentalID, ok := pathID(w, r, "rentalId")
	if !ok {
		return
	}
	var req adminWalletRefundRequest
	if err := pkg_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	result, err := h.paymentService.RefundWalletPayment(r.Context(), shared_middleware.GetUserID(r.Context()), shared_middleware.GetUserRole(r.Context()), rentalID, req.ReasonCode, time.Now().UTC())
	if err != nil {
		if errors.Is(err, payment.ErrAdminRequired) {
			shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", err.Error())
			return
		}
		if errors.Is(err, payment.ErrInvalidReasonCode) {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			return
		}
		if errors.Is(err, payment.ErrWalletRefundNotFound) {
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			return
		}
		if errors.Is(err, payment.ErrWalletRefundNotAllowed) || errors.Is(err, payment.ErrDepositAlreadySettled) {
			shared_response.Error(w, http.StatusConflict, "WALLET_REFUND_FAILED", err.Error())
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal error")
		return
	}

	shared_response.JSON(w, http.StatusOK, map[string]any{
		"changed":          result.Changed,
		"idempotent":       result.Idempotent,
		"status":           result.Status,
		"principal_amount": money(result.PrincipalAmount),
		"deposit_amount":   money(result.DepositAmount),
		"total_amount":     money(result.TotalAmount),
		"deposit_status":   result.DepositStatus,
	})
}

func (h *Handler) AdminReleaseDeposit(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	rentalID, ok := pathID(w, r, "rentalId")
	if !ok {
		return
	}
	result, err := h.paymentService.ReleaseDeposit(r.Context(), shared_middleware.GetUserID(r.Context()), shared_middleware.GetUserRole(r.Context()), rentalID, time.Now().UTC())
	if err != nil {
		if errors.Is(err, payment.ErrAdminRequired) {
			shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", err.Error())
			return
		}
		if errors.Is(err, payment.ErrDepositHoldNotFound) {
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			return
		}
		if errors.Is(err, payment.ErrDepositSettlementNotAllowed) || errors.Is(err, payment.ErrDepositAlreadySettled) {
			shared_response.Error(w, http.StatusConflict, "DEPOSIT_SETTLEMENT_FAILED", err.Error())
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal error")
		return
	}
	shared_response.JSON(w, http.StatusOK, depositSettlementResponse(result))
}

func (h *Handler) AdminForfeitDeposit(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	rentalID, ok := pathID(w, r, "rentalId")
	if !ok {
		return
	}
	var req adminForfeitDepositRequest
	if err := pkg_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	result, err := h.paymentService.ForfeitDeposit(r.Context(), shared_middleware.GetUserID(r.Context()), shared_middleware.GetUserRole(r.Context()), rentalID, req.ReasonCode, req.EvidenceReference, time.Now().UTC())
	if err != nil {
		if errors.Is(err, payment.ErrAdminRequired) {
			shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", err.Error())
			return
		}
		if errors.Is(err, payment.ErrInvalidReasonCode) || errors.Is(err, payment.ErrInvalidEvidenceReference) {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			return
		}
		if errors.Is(err, payment.ErrDepositHoldNotFound) {
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			return
		}
		if errors.Is(err, payment.ErrDepositSettlementNotAllowed) ||
			errors.Is(err, payment.ErrDepositAlreadySettled) ||
			errors.Is(err, payment.ErrDepositReviewDeadlinePassed) {
			shared_response.Error(w, http.StatusConflict, "DEPOSIT_SETTLEMENT_FAILED", err.Error())
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal error")
		return
	}
	shared_response.JSON(w, http.StatusOK, depositSettlementResponse(result))
}

func depositSettlementResponse(result *payment.DepositSettlementResult) map[string]any {
	return map[string]any{
		"changed":        result.Changed,
		"idempotent":     result.Idempotent,
		"status":         result.DepositStatus,
		"deposit_status": result.DepositStatus,
		"settled_at":     result.SettledAt,
		"rental_status":  result.RentalStatus,
		"completed_at":   result.CompletedAt,
	}
}
