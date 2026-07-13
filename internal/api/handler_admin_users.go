package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"rent_game_accs/internal/payment"
	shared_middleware "rent_game_accs/internal/shared/middleware"
	shared_response "rent_game_accs/internal/shared/response"
	"rent_game_accs/internal/user"
	"strconv"
	"time"
)

func (h *Handler) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	users, err := h.adminUserService.ListUsers(r.Context())
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load users")
		return
	}
	items := make([]map[string]any, 0, len(users))
	for _, item := range users {
		items = append(items, map[string]any{"id": item.ID, "email": item.Email, "first_name": item.FirstName, "last_name": item.LastName, "role": item.Role, "trust_score": item.TrustScore, "is_blocked": item.IsBlocked, "balance": item.Balance})
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"users": items})
}

func (h *Handler) AdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	id, ok := pathID(w, r, "userId")
	if !ok {
		return
	}
	var req struct {
		TrustScore *int    `json:"trust_score"`
		IsBlocked  *bool   `json:"is_blocked"`
		Role       *string `json:"role"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid admin user update payload")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid admin user update payload")
		return
	}
	if req.Role != nil && *req.Role != "ADMIN" && *req.Role != "RENT" {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "role must be ADMIN or RENT")
		return
	}
	if req.TrustScore != nil && (*req.TrustScore < 0 || *req.TrustScore > 1000) {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "trust_score must be between 0 and 1000")
		return
	}

	err := h.adminUserService.UpdateUser(r.Context(), shared_middleware.GetUserID(r.Context()), id, user.AdminUpdateInput{TrustScore: req.TrustScore, IsBlocked: req.IsBlocked, Role: req.Role})
	if err != nil {
		switch {
		case errors.Is(err, user.ErrAdminAuthorization):
			shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", "Current administrator authorization is required")
		case errors.Is(err, user.ErrAdminSelfUpdateForbidden):
			shared_response.Error(w, http.StatusConflict, "ADMIN_USER_UPDATE_FORBIDDEN", "Administrators cannot change their own administrative user state")
		case errors.Is(err, user.ErrUserNotFound):
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "User not found")
		default:
			shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update user")
		}
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]string{"message": "User updated"})
}

type adminBalanceAdjustmentRequest struct {
	Amount         int64  `json:"amount"`
	Currency       string `json:"currency"`
	ReasonCode     string `json:"reason_code"`
	Comment        string `json:"comment"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (h *Handler) AdminAdjustUserBalance(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}

	targetUserID, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil || targetUserID <= 0 {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "userId must be a positive integer")
		return
	}

	var req adminBalanceAdjustmentRequest
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid balance adjustment payload")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid balance adjustment payload")
		return
	}

	result, err := h.paymentService.AdjustAdminBalance(
		r.Context(),
		shared_middleware.GetUserID(r.Context()),
		shared_middleware.GetUserRole(r.Context()),
		targetUserID,
		payment.AdminBalanceAdjustmentInput{
			Amount:         req.Amount,
			Currency:       req.Currency,
			ReasonCode:     req.ReasonCode,
			Comment:        req.Comment,
			IdempotencyKey: req.IdempotencyKey,
		},
		clientIPFromRequest(r),
		r.Header.Get("User-Agent"),
		time.Now().UTC(),
	)
	if err != nil {
		switch {
		case errors.Is(err, payment.ErrAdminRequired):
			shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", err.Error())
		case errors.Is(err, payment.ErrFinancialUserNotFound):
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "User not found")
		case errors.Is(err, payment.ErrBalanceAdjustmentAmountRequired),
			errors.Is(err, payment.ErrBalanceAdjustmentCurrencyUnsupported),
			errors.Is(err, payment.ErrBalanceAdjustmentReasonRequired),
			errors.Is(err, payment.ErrBalanceAdjustmentCommentTooLong),
			errors.Is(err, payment.ErrBalanceAdjustmentIdempotencyRequired),
			errors.Is(err, payment.ErrBalanceAdjustmentOverflow):
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		case errors.Is(err, payment.ErrBalanceAdjustmentWouldBeNegative),
			errors.Is(err, payment.ErrBalanceAdjustmentIdempotencyConflict):
			shared_response.Error(w, http.StatusConflict, "BALANCE_ADJUSTMENT_FAILED", err.Error())
		default:
			shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to adjust user balance")
		}
		return
	}

	status := http.StatusCreated
	if result.IdempotentReplay {
		status = http.StatusOK
	}
	shared_response.JSON(w, status, map[string]any{
		"adjustment_id":     result.AdjustmentID,
		"user_id":           result.UserID,
		"previous_balance":  result.PreviousBalance,
		"new_balance":       result.NewBalance,
		"amount":            result.Amount,
		"currency":          result.Currency,
		"ledger_entry_id":   result.LedgerEntryID,
		"idempotency_key":   result.IdempotencyKey,
		"idempotent_replay": result.IdempotentReplay,
		"created_at":        result.CreatedAt,
	})
}

func (h *Handler) AdminAuditLogs(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	logs, err := h.adminUserService.ListAuditLogs(r.Context())
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load audit logs")
		return
	}
	items := make([]map[string]any, 0, len(logs))
	for _, item := range logs {
		items = append(items, map[string]any{"id": item.ID, "actor_user_id": item.ActorUserID, "entity_type": item.EntityType, "entity_id": item.EntityID, "action": item.Action, "created_at": item.CreatedAt})
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"audit_logs": items})
}
