package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"rent_game_accs/internal/payment"
	"rent_game_accs/internal/rental"
	shared_middleware "rent_game_accs/internal/shared/middleware"
	shared_response "rent_game_accs/internal/shared/response"
	"strconv"
	"strings"
	"time"
)

func admin(w http.ResponseWriter, r *http.Request) bool {
	if !shared_middleware.IsAdmin(r.Context()) {
		shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", "Admin access is required")
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid ID")
		return 0, false
	}
	return id, true
}

func rentalDTO(rent *rental.Rental) map[string]any {
	return rentalMap(
		rent.ID,
		rent.UserID,
		rent.AccountID,
		int16(rent.Status),
		rent.Period.StartAt,
		rent.Period.EndAt,
		rent.CreatedAt.Add(15*time.Minute),
		rent.RentalPrice.Amount,
		rent.DepositAmount.Amount,
		publicDepositStatus(rent.DepositAmount.Amount, 0),
		nil,
	)
}

func adminRentalSummaryDTO(s payment.AdminRentalSummary) map[string]any {
	return map[string]any{
		"total_count":                  s.TotalCount,
		"eligible_wallet_refund_count": s.EligibleWalletRefundCount,
		"rental_status_counts":         s.RentalStatusCounts,
		"payment_status_counts":        s.PaymentStatusCounts,
		"refund_status_counts":         s.RefundStatusCounts,
	}
}

func adminRentalEntryDTO(item payment.AdminRentalEntry) map[string]any {
	dto := rentalMap(
		item.ID,
		item.UserID,
		item.AccountID,
		item.Status,
		item.StartedAt,
		item.ExpiresAt,
		item.PaymentExpiresAt,
		item.RentalPrice,
		item.SecurityDeposit,
		item.DepositStatus,
		&publicRentalRefundSummary{
			HasRefund:   item.HasRefund,
			Status:      item.RefundStatus,
			TotalAmount: item.RefundTotal,
			ProcessedAt: item.ProcessedAt,
		},
	)
	if item.PaymentID > 0 {
		dto["payment_id"] = item.PaymentID
	}
	dto["payment_status"] = item.PaymentStatus
	dto["payment_provider"] = item.PaymentProvider
	return dto
}

func adminRentalDetailDTO(detail *payment.AdminRentalDetail) map[string]any {
	result := map[string]any{
		"rental": map[string]any{
			"id":                 detail.Rental.ID,
			"user_id":            detail.Rental.UserID,
			"account_id":         detail.Rental.AccountID,
			"status":             detail.Rental.Status,
			"start_at":           detail.Rental.StartAt,
			"end_at":             detail.Rental.EndAt,
			"rental_price":       money(detail.Rental.RentalPrice),
			"deposit_amount":     money(detail.Rental.DepositAmount),
			"payment_expires_at": detail.Rental.PaymentExpiresAt,
			"created_at":         detail.Rental.CreatedAt,
			"updated_at":         detail.Rental.UpdatedAt,
		},
		"payment": nil,
		"deposit": nil,
		"refund_summary": map[string]any{
			"count":                    detail.RefundSummary.Count,
			"latest_refund_status":     detail.RefundSummary.LatestStatus,
			"total_refunded_principal": money(detail.RefundSummary.TotalRefundedPrimary),
			"total_refunded_deposit":   money(detail.RefundSummary.TotalRefundedDeposit),
			"latest_processed_at":      detail.RefundSummary.LatestProcessedAt,
		},
		"ledger_summary": map[string]any{
			"counts_by_display_type": detail.LedgerSummary.CountsByDisplayType,
			"totals_by_display_type": adminLedgerTotalsDTO(detail.LedgerSummary.TotalsByDisplayType),
			"latest_entries":         adminLedgerEntriesDTO(detail.LedgerSummary.LatestEntries),
		},
		"support_flags": map[string]any{
			"eligible_wallet_refund":        detail.SupportFlags.EligibleWalletRefund,
			"refund_ineligible_reason":      detail.SupportFlags.RefundIneligibleReason,
			"has_active_credentials_access": detail.SupportFlags.HasActiveCredentials,
			"payment_window_expired":        detail.SupportFlags.PaymentWindowExpired,
		},
	}
	if detail.Payment != nil {
		result["payment"] = map[string]any{
			"id":         detail.Payment.ID,
			"status":     detail.Payment.Status,
			"provider":   detail.Payment.Provider,
			"amount":     detail.Payment.Amount,
			"currency":   detail.Payment.Currency,
			"created_at": detail.Payment.CreatedAt,
		}
	}
	if detail.Deposit != nil {
		result["deposit"] = map[string]any{
			"amount":       detail.Deposit.Amount,
			"currency":     detail.Deposit.Currency,
			"status":       detail.Deposit.Status,
			"held_at":      detail.Deposit.HeldAt,
			"released_at":  detail.Deposit.ReleasedAt,
			"forfeited_at": detail.Deposit.ForfeitedAt,
			"refunded_at":  detail.Deposit.RefundedAt,
		}
	}
	return result
}

func adminLedgerTotalsDTO(totals map[string]int64) map[string]any {
	result := make(map[string]any, len(totals))
	for key, value := range totals {
		result[key] = money(value)
	}
	return result
}

func adminLedgerEntriesDTO(entries []payment.AdminRentalSafeLedgerEntry) []map[string]any {
	items := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		items = append(items, map[string]any{
			"id":           entry.ID,
			"display_type": entry.DisplayType,
			"amount":       entry.Amount,
			"currency":     entry.Currency,
			"created_at":   entry.CreatedAt,
		})
	}
	return items
}

type publicRentalRefundSummary struct {
	HasRefund   bool
	Status      string
	TotalAmount int64
	ProcessedAt *time.Time
}

func rentalMap(id, userID, accountID int64, status int16, start, end, paymentExpiresAt time.Time, price, deposit int64, depositStatus string, refundSummary *publicRentalRefundSummary) map[string]any {
	dto := map[string]any{
		"id":                 id,
		"user_id":            userID,
		"account_id":         accountID,
		"status":             status,
		"started_at":         start,
		"expires_at":         end,
		"payment_expires_at": paymentExpiresAt,
		"rental_price":       money(price),
		"security_deposit":   money(deposit),
		"deposit_status":     depositStatus,
		"total_price":        money(price + deposit),
	}
	if refundSummary == nil {
		dto["has_refund"] = false
		dto["refund_status"] = "NONE"
		dto["refund_total_amount"] = money(0)
		return dto
	}
	dto["has_refund"] = refundSummary.HasRefund
	dto["refund_status"] = refundSummary.Status
	dto["refund_total_amount"] = money(refundSummary.TotalAmount)
	if refundSummary.ProcessedAt != nil {
		dto["processed_at"] = *refundSummary.ProcessedAt
	}
	return dto
}

func publicRefundSummary(statusCode int16, amount int64, processedAt *time.Time) *publicRentalRefundSummary {
	if statusCode == 0 {
		return nil
	}
	summary := &publicRentalRefundSummary{
		HasRefund:   true,
		Status:      publicRefundStatus(statusCode),
		TotalAmount: amount,
	}
	if processedAt != nil {
		processed := *processedAt
		summary.ProcessedAt = &processed
	}
	return summary
}

func publicRefundStatus(statusCode int16) string {
	switch statusCode {
	case 1:
		return "REQUESTED"
	case 2:
		return "COMPLETED"
	case 3:
		return "FAILED"
	default:
		return "UNKNOWN"
	}
}

func publicDepositStatus(depositAmount int64, holdStatus int16) string {
	if depositAmount <= 0 {
		return "NONE"
	}
	if holdStatus == 0 {
		return "NONE"
	}
	switch holdStatus {
	case 1:
		return "HELD"
	case 2:
		return "RELEASED"
	case 3:
		return "FORFEITED"
	case 4:
		return "REFUNDED"
	default:
		return "UNKNOWN"
	}
}

func ledgerPaginationParams(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	page := 1
	pageSize := 20

	if pageRaw := strings.TrimSpace(r.URL.Query().Get("page")); pageRaw != "" {
		parsed, err := strconv.Atoi(pageRaw)
		if err != nil || parsed <= 0 {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "page must be a positive integer")
			return 0, 0, false
		}
		page = parsed
	}

	if pageSizeRaw := strings.TrimSpace(r.URL.Query().Get("page_size")); pageSizeRaw != "" {
		parsed, err := strconv.Atoi(pageSizeRaw)
		if err != nil || parsed <= 0 {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "page_size must be a positive integer")
			return 0, 0, false
		}
		pageSize = parsed
	}

	return page, pageSize, true
}

func adminPaginationParams(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	page, pageSize, ok := ledgerPaginationParams(w, r)
	if !ok {
		return 0, 0, false
	}
	if pageSize > 100 {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "page_size must be <= 100")
		return 0, 0, false
	}
	return page, pageSize, true
}

func adminRentalListFilters(w http.ResponseWriter, r *http.Request) (payment.AdminRentalListFilter, bool) {
	page, pageSize, ok := adminPaginationParams(w, r)
	if !ok {
		return payment.AdminRentalListFilter{}, false
	}

	filters := payment.AdminRentalListFilter{
		RentalStatus:    strings.TrimSpace(r.URL.Query().Get("rental_status")),
		PaymentStatus:   strings.TrimSpace(r.URL.Query().Get("payment_status")),
		PaymentProvider: strings.TrimSpace(r.URL.Query().Get("payment_provider")),
		DepositStatus:   strings.TrimSpace(r.URL.Query().Get("deposit_status")),
		RefundStatus:    strings.TrimSpace(r.URL.Query().Get("refund_status")),
		Page:            page,
		PageSize:        pageSize,
	}

	if raw := strings.TrimSpace(r.URL.Query().Get("eligible_wallet_refund")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "eligible_wallet_refund must be true or false")
			return payment.AdminRentalListFilter{}, false
		}
		filters.EligibleWalletRefund = &value
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("user_id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "user_id must be a positive integer")
			return payment.AdminRentalListFilter{}, false
		}
		filters.UserID = value
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("rental_id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "rental_id must be a positive integer")
			return payment.AdminRentalListFilter{}, false
		}
		filters.RentalID = value
	}

	return filters, true
}

func clientIPFromRequest(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.TrimSpace(strings.Split(ip, ",")[0])
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return strings.TrimSpace(ip)
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return strings.TrimSpace(ip)
}

func money(amount int64) map[string]any {
	return map[string]any{"amount": amount, "currency": "USD"}
}

func PaymentSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
