package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"rent_game_accs/internal/payment"
	pkg_http_request "rent_game_accs/internal/pkg/transport/http/request"
	"rent_game_accs/internal/rental"
	shared_middleware "rent_game_accs/internal/shared/middleware"
	shared_response "rent_game_accs/internal/shared/response"
	"time"
)

type createRentalRequest struct {
	AccountID     int64 `json:"account_id"`
	DurationHours int   `json:"duration_hours"`
}

func (r *createRentalRequest) Validate() error {
	if r.AccountID <= 0 {
		return errText("account_id is required")
	}
	if r.DurationHours < 1 || r.DurationHours > 720 {
		return errText("duration_hours must be between 1 and 720")
	}
	return nil
}

type errText string

func (e errText) Error() string { return string(e) }

func (h *Handler) CreateRental(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	var req createRentalRequest
	if err := pkg_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	rent, err := h.rentalService.RentAccount(r.Context(), userID, req.AccountID, time.Duration(req.DurationHours)*time.Hour, time.Now().UTC())
	if err != nil {
		shared_response.Error(w, http.StatusConflict, "RENTAL_FAILED", err.Error())
		return
	}
	shared_response.JSON(w, http.StatusCreated, rentalDTO(rent))
}

func (h *Handler) ListMyRentals(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	rentals, err := h.rentalService.ListCustomerRentals(r.Context(), userID)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	items := make([]map[string]any, 0, len(rentals))
	for _, item := range rentals {
		items = append(items, rentalMap(item.ID, item.UserID, item.AccountID, item.Status, item.StartAt, item.EndAt, item.PaymentExpiresAt, item.RentalPrice, item.DepositAmount, publicDepositStatus(item.DepositAmount, item.DepositHoldStatus), publicRefundSummary(item.RefundStatus, item.RefundTotalAmount, item.RefundProcessedAt)))
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"rentals": items})
}

func (h *Handler) GetRental(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	id, ok := pathID(w, r, "rentalId")
	if !ok {
		return
	}
	rent, err := h.rentalService.GetCustomerRental(r.Context(), userID, id)
	if err != nil {
		if errors.Is(err, rental.ErrRentalAccessDenied) {
			shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", "You can access only your rentals")
			return
		}
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Rental not found")
		return
	}
	shared_response.JSON(w, http.StatusOK, rentalMap(rent.ID, rent.UserID, rent.AccountID, rent.Status, rent.StartAt, rent.EndAt, rent.PaymentExpiresAt, rent.RentalPrice, rent.DepositAmount, publicDepositStatus(rent.DepositAmount, rent.DepositHoldStatus), publicRefundSummary(rent.RefundStatus, rent.RefundTotalAmount, rent.RefundProcessedAt)))
}

func (h *Handler) GetMyBalance(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	balance, err := h.paymentService.GetUserBalance(r.Context(), userID)
	if err != nil {
		if errors.Is(err, payment.ErrFinancialUserNotFound) {
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Balance not found")
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load balance")
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{
		"available_balance": balance.AvailableBalance,
		"currency":          balance.Currency,
	})
}

func (h *Handler) ListMyLedger(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	page, pageSize, ok := ledgerPaginationParams(w, r)
	if !ok {
		return
	}

	ledgerPage, err := h.paymentService.ListUserLedger(r.Context(), userID, page, pageSize)
	if err != nil {
		if errors.Is(err, payment.ErrInvalidLedgerPagination) {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "page and page_size must be positive; page_size must be <= 100")
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load ledger")
		return
	}

	totalPages := int(ledgerPage.TotalItems) / ledgerPage.PageSize
	if int(ledgerPage.TotalItems)%ledgerPage.PageSize != 0 {
		totalPages++
	}
	if ledgerPage.TotalItems == 0 {
		totalPages = 0
	}

	items := make([]map[string]any, 0, len(ledgerPage.Entries))
	for _, entry := range ledgerPage.Entries {
		dto := map[string]any{
			"id":           entry.ID,
			"entry_type":   entry.EntryType,
			"amount":       entry.Amount,
			"currency":     entry.Currency,
			"created_at":   entry.CreatedAt,
			"display_type": entry.DisplayType,
		}
		if entry.RentalID != nil {
			dto["rental_id"] = *entry.RentalID
		}
		if entry.PaymentID != nil {
			dto["payment_id"] = *entry.PaymentID
		}
		items = append(items, dto)
	}

	shared_response.JSON(w, http.StatusOK, map[string]any{
		"entries": items,
		"pagination": map[string]any{
			"page":        ledgerPage.Page,
			"page_size":   ledgerPage.PageSize,
			"total_items": ledgerPage.TotalItems,
			"total_pages": totalPages,
		},
	})
}

func (h *Handler) ListMyRefunds(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	page, pageSize, ok := ledgerPaginationParams(w, r)
	if !ok {
		return
	}

	refundPage, err := h.paymentService.ListUserRefunds(r.Context(), userID, page, pageSize)
	if err != nil {
		if errors.Is(err, payment.ErrInvalidRefundPagination) {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "page and page_size must be positive; page_size must be <= 100")
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load refunds")
		return
	}

	totalPages := int(refundPage.TotalItems) / refundPage.PageSize
	if int(refundPage.TotalItems)%refundPage.PageSize != 0 {
		totalPages++
	}
	if refundPage.TotalItems == 0 {
		totalPages = 0
	}

	items := make([]map[string]any, 0, len(refundPage.Entries))
	for _, entry := range refundPage.Entries {
		dto := map[string]any{
			"id":               entry.ID,
			"rental_id":        entry.RentalID,
			"payment_id":       entry.PaymentID,
			"status":           entry.Status,
			"principal_amount": entry.PrincipalAmount,
			"deposit_amount":   entry.DepositAmount,
			"total_amount":     entry.TotalAmount,
			"currency":         entry.Currency,
			"created_at":       entry.CreatedAt,
			"processed_at":     entry.ProcessedAt,
		}
		if entry.ReasonCode != nil {
			dto["reason_code"] = *entry.ReasonCode
		}
		items = append(items, dto)
	}

	shared_response.JSON(w, http.StatusOK, map[string]any{
		"refunds": items,
		"pagination": map[string]any{
			"page":        refundPage.Page,
			"page_size":   refundPage.PageSize,
			"total_items": refundPage.TotalItems,
			"total_pages": totalPages,
		},
	})
}

type rentalCredentialsResponse struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

func (h *Handler) GetMyRentalCredentials(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	rentalID, ok := pathID(w, r, "rentalId")
	if !ok {
		return
	}

	creds, err := h.rentalService.GetRentalCredentials(r.Context(), userID, rentalID, rental.CredentialRequestContext{
		IPAddress: clientIPFromRequest(r),
		UserAgent: r.UserAgent(),
	}, time.Now().UTC())
	if err != nil {
		log.Printf("GetRentalCredentials error for rentalID=%d, userID=%d: %v", rentalID, userID, err)
		if errors.Is(err, rental.ErrCredentialsNotAvailable) {
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Rental not found")
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load rental credentials")
		return
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	shared_response.JSON(w, http.StatusOK, rentalCredentialsResponse{
		Login:    creds.Login,
		Password: creds.Password,
	})
}

func (h *Handler) PayRentalWithBalance(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	rentalID, ok := pathID(w, r, "rentalId")
	if !ok {
		return
	}

	result, err := h.paymentService.PayRentalWithBalance(r.Context(), userID, rentalID, clientIPFromRequest(r), r.Header.Get("User-Agent"), time.Now().UTC())
	if err != nil {
		if errors.Is(err, payment.ErrWalletPaymentNotFound) {
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Rental not found")
			return
		}
		if errors.Is(err, payment.ErrWalletInsufficientBalance) {
			shared_response.Error(w, http.StatusConflict, "PAYMENT_FAILED", "Insufficient balance")
			return
		}
		if errors.Is(err, payment.ErrWalletPaymentExpired) || errors.Is(err, payment.ErrWalletPaymentNotAllowed) {
			shared_response.Error(w, http.StatusConflict, "PAYMENT_FAILED", "Rental cannot be paid with balance")
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to pay rental with balance")
		return
	}

	shared_response.JSON(w, http.StatusOK, map[string]any{
		"changed":          result.Changed,
		"idempotent":       result.Idempotent,
		"payment_id":       result.PaymentID,
		"rental_id":        result.RentalID,
		"account_id":       result.AccountID,
		"payment_status":   result.PaymentStatus,
		"rental_status":    result.RentalStatus,
		"account_status":   result.AccountStatus,
		"payment_provider": result.PaymentProvider,
	})
}

func (h *Handler) CancelRental(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	id, ok := pathID(w, r, "rentalId")
	if !ok {
		return
	}

	_, err := h.rentalService.CancelRental(r.Context(), userID, id, "cancelled by user", time.Now().UTC())
	if err != nil {
		if errors.Is(err, rental.ErrCannotCancel) || errors.Is(err, rental.ErrRentalNotFound) {
			shared_response.Error(w, http.StatusConflict, "CANCEL_FAILED", "Rental cannot be cancelled")
			return
		}
		shared_response.Error(w, http.StatusConflict, "CANCEL_FAILED", "Rental cannot be cancelled")
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]string{"message": "Rental cancelled"})
}

func (h *Handler) CalculateRental(w http.ResponseWriter, r *http.Request) {
	var req createRentalRequest
	if err := pkg_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	quote, total, err := h.rentalService.QuoteRental(r.Context(), req.AccountID, int64(req.DurationHours))
	if err != nil {
		if errors.Is(err, rental.ErrInvalidRentalPricing) || errors.Is(err, rental.ErrRentalPriceOverflow) {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Configured rental price exceeds the supported range")
			return
		}
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Account not found")
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"price_per_hour": money(quote.HourlyPrice), "security_deposit": money(quote.DepositAmount), "duration_hours": req.DurationHours, "total_price": money(total)})
}

func (h *Handler) ExtendRental(w http.ResponseWriter, r *http.Request) {
	shared_response.Error(w, http.StatusNotImplemented, "EXTENSION_NOT_SUPPORTED", "Paid rental extension is not supported")
}

func (h *Handler) CreatePayment(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	var req struct {
		RentalID int64 `json:"rental_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RentalID <= 0 {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "rental_id is required")
		return
	}
	paymentResult, err := h.paymentService.CreateCustomerPayment(r.Context(), userID, req.RentalID)
	if errors.Is(err, payment.ErrCustomerRentalNotFound) {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Rental not found")
		return
	}
	if err != nil {
		shared_response.Error(w, http.StatusConflict, "PAYMENT_FAILED", "Payment record is unavailable for this rental")
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"id": paymentResult.ID, "rental_id": req.RentalID, "amount": paymentResult.Amount, "currency": paymentResult.Currency, "status": paymentResult.Status})
}

func (h *Handler) ListMyPayments(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	payments, err := h.paymentService.ListCustomerPayments(r.Context(), userID)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	items := make([]map[string]any, 0, len(payments))
	for _, item := range payments {
		items = append(items, map[string]any{"id": item.ID, "rental_id": item.RentalID, "amount": item.Amount, "currency": item.Currency, "status": item.Status, "created_at": item.CreatedAt})
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"payments": items})
}

func (h *Handler) GetPayment(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	id, ok := pathID(w, r, "paymentId")
	if !ok {
		return
	}
	paymentResult, err := h.paymentService.GetCustomerPayment(r.Context(), userID, id)
	if err != nil {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Payment not found")
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"id": id, "rental_id": paymentResult.RentalID, "amount": paymentResult.Amount, "currency": paymentResult.Currency, "status": paymentResult.Status})
}
