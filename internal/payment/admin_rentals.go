package payment

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"rent_game_accs/internal/shared/database"
)

var (
	adminRentalStatusFilterValues = map[string]int16{
		"WAITING_PAYMENT": 1,
		"ACTIVE":          2,
		"EXPIRED":         3,
		"COMPLETED":       4,
		"CANCELLED":       5,
	}
	adminPaymentStatusFilterValues = map[string]int16{
		"PENDING": 1,
		"SUCCESS": 2,
		"FAILED":  3,
	}
	adminPaymentProviderFilterValues = map[string]struct{}{
		walletPaymentProvider:  {},
		webhookPaymentProvider: {},
	}
	adminDepositStatusFilterValues = map[string]struct{}{
		"NONE":      {},
		"HELD":      {},
		"RELEASED":  {},
		"FORFEITED": {},
		"REFUNDED":  {},
	}
	adminRefundStatusFilterValues = map[string]int16{
		"NONE":      0,
		"REQUESTED": refundStatusRequested,
		"COMPLETED": refundStatusCompleted,
		"FAILED":    refundStatusFailed,
	}
)

type AdminRentalListFilter struct {
	RentalStatus         string
	PaymentStatus        string
	PaymentProvider      string
	DepositStatus        string
	RefundStatus         string
	EligibleWalletRefund *bool
	UserID               int64
	RentalID             int64
	Page                 int
	PageSize             int
}

type AdminRentalEntry struct {
	ID               int64
	UserID           int64
	AccountID        int64
	Status           int16
	StartedAt        time.Time
	ExpiresAt        time.Time
	PaymentExpiresAt time.Time
	RentalPrice      int64
	SecurityDeposit  int64
	DepositStatus    string
	PaymentID        int64
	PaymentStatus    int16
	PaymentProvider  string
	HasRefund        bool
	RefundStatus     string
	RefundTotal      int64
	ProcessedAt      *time.Time
}

type AdminRentalSummary struct {
	TotalCount                int64
	EligibleWalletRefundCount int64
	RentalStatusCounts        map[string]int64
	PaymentStatusCounts       map[string]int64
	RefundStatusCounts        map[string]int64
}

type AdminRentalPage struct {
	Rentals    []AdminRentalEntry
	Summary    AdminRentalSummary
	Page       int
	PageSize   int
	TotalItems int64
}

func newInvalidAdminRentalFiltersError(message string) error {
	return &adminRentalFilterError{message: message}
}

type adminRentalFilterError struct {
	message string
}

func (e *adminRentalFilterError) Error() string {
	return e.message
}

func (e *adminRentalFilterError) Unwrap() error {
	return ErrInvalidAdminRentalFilters
}

func (s *PaymentService) ListAdminRentals(ctx context.Context, filters AdminRentalListFilter) (*AdminRentalPage, error) {
	if err := validateAdminRentalFilters(filters); err != nil {
		return nil, err
	}

	items, err := s.repo.ListAdminRentalEntries(ctx, filters)
	if err != nil {
		return nil, err
	}
	summary, err := s.repo.SummarizeAdminRentals(ctx, filters)
	if err != nil {
		return nil, err
	}

	return &AdminRentalPage{
		Rentals:    items,
		Summary:    summary,
		Page:       filters.Page,
		PageSize:   filters.PageSize,
		TotalItems: summary.TotalCount,
	}, nil
}

func validateAdminRentalFilters(filters AdminRentalListFilter) error {
	if filters.Page <= 0 {
		return newInvalidAdminRentalFiltersError("page must be a positive integer")
	}
	if filters.PageSize <= 0 {
		return newInvalidAdminRentalFiltersError("page_size must be a positive integer")
	}
	if filters.PageSize > 100 {
		return newInvalidAdminRentalFiltersError("page_size must be <= 100")
	}
	if filters.UserID < 0 {
		return newInvalidAdminRentalFiltersError("user_id must be a positive integer")
	}
	if filters.RentalID < 0 {
		return newInvalidAdminRentalFiltersError("rental_id must be a positive integer")
	}
	if filters.UserID == 0 && filters.RentalStatus == "" && filters.PaymentStatus == "" && filters.PaymentProvider == "" && filters.DepositStatus == "" && filters.RefundStatus == "" && filters.EligibleWalletRefund == nil && filters.RentalID == 0 {
		return nil
	}
	if filters.RentalStatus != "" {
		if _, ok := adminRentalStatusFilterValues[filters.RentalStatus]; !ok {
			return newInvalidAdminRentalFiltersError("rental_status is invalid")
		}
	}
	if filters.PaymentStatus != "" {
		if _, ok := adminPaymentStatusFilterValues[filters.PaymentStatus]; !ok {
			return newInvalidAdminRentalFiltersError("payment_status is invalid")
		}
	}
	if filters.PaymentProvider != "" {
		if _, ok := adminPaymentProviderFilterValues[filters.PaymentProvider]; !ok {
			return newInvalidAdminRentalFiltersError("payment_provider is invalid")
		}
	}
	if filters.DepositStatus != "" {
		if _, ok := adminDepositStatusFilterValues[filters.DepositStatus]; !ok {
			return newInvalidAdminRentalFiltersError("deposit_status is invalid")
		}
	}
	if filters.RefundStatus != "" {
		if _, ok := adminRefundStatusFilterValues[filters.RefundStatus]; !ok {
			return newInvalidAdminRentalFiltersError("refund_status is invalid")
		}
	}
	return nil
}

func (r *PostgresRepository) ListAdminRentalEntries(ctx context.Context, filters AdminRentalListFilter) ([]AdminRentalEntry, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	offset := (filters.Page - 1) * filters.PageSize
	baseQuery, args := adminRentalsBaseQuery(filters)
	query := `
		SELECT
			filtered.id,
			filtered.user_id,
			filtered.account_id,
			filtered.rental_status,
			filtered.start_at,
			filtered.end_at,
			filtered.payment_expires_at,
			filtered.rental_price,
			filtered.deposit_amount,
			filtered.deposit_hold_status,
			filtered.payment_id,
			filtered.payment_status,
			filtered.payment_provider,
			filtered.refund_status_code,
			filtered.refund_total_amount,
			filtered.refund_processed_at
		FROM (` + baseQuery + `) AS filtered
		ORDER BY filtered.created_at DESC, filtered.id DESC
		LIMIT $` + fmt.Sprint(len(args)+1) + ` OFFSET $` + fmt.Sprint(len(args)+2)
	args = append(args, filters.PageSize, offset)
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]AdminRentalEntry, 0, filters.PageSize)
	for rows.Next() {
		var (
			entry             AdminRentalEntry
			depositHoldStatus int16
			refundStatusCode  int16
			refundProcessedAt sql.NullTime
		)
		if err := rows.Scan(
			&entry.ID,
			&entry.UserID,
			&entry.AccountID,
			&entry.Status,
			&entry.StartedAt,
			&entry.ExpiresAt,
			&entry.PaymentExpiresAt,
			&entry.RentalPrice,
			&entry.SecurityDeposit,
			&depositHoldStatus,
			&entry.PaymentID,
			&entry.PaymentStatus,
			&entry.PaymentProvider,
			&refundStatusCode,
			&entry.RefundTotal,
			&refundProcessedAt,
		); err != nil {
			return nil, err
		}
		entry.DepositStatus = adminPublicDepositStatus(entry.SecurityDeposit, depositHoldStatus)
		entry.HasRefund = refundStatusCode != 0
		if refundStatusCode == 0 {
			entry.RefundStatus = "NONE"
		} else {
			entry.RefundStatus = publicRefundStatus(refundStatusCode)
		}
		if refundProcessedAt.Valid {
			value := refundProcessedAt.Time
			entry.ProcessedAt = &value
		}
		items = append(items, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *PostgresRepository) SummarizeAdminRentals(ctx context.Context, filters AdminRentalListFilter) (AdminRentalSummary, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	baseQuery, args := adminRentalsBaseQuery(filters)
	query := `
		SELECT
			COUNT(*) AS total_count,
			COUNT(*) FILTER (WHERE filtered.eligible_wallet_refund) AS eligible_wallet_refund_count,
			COUNT(*) FILTER (WHERE filtered.rental_status = 1) AS rental_waiting_payment_count,
			COUNT(*) FILTER (WHERE filtered.rental_status = 2) AS rental_active_count,
			COUNT(*) FILTER (WHERE filtered.rental_status = 3) AS rental_expired_count,
			COUNT(*) FILTER (WHERE filtered.rental_status = 4) AS rental_completed_count,
			COUNT(*) FILTER (WHERE filtered.rental_status = 5) AS rental_cancelled_count,
			COUNT(*) FILTER (WHERE filtered.payment_status = 1) AS payment_pending_count,
			COUNT(*) FILTER (WHERE filtered.payment_status = 2) AS payment_success_count,
			COUNT(*) FILTER (WHERE filtered.payment_status = 3) AS payment_failed_count,
			COUNT(*) FILTER (WHERE filtered.payment_status = 4) AS payment_cancelled_count,
			COUNT(*) FILTER (WHERE filtered.refund_status_code = 0) AS refund_none_count,
			COUNT(*) FILTER (WHERE filtered.refund_status_code = 1) AS refund_requested_count,
			COUNT(*) FILTER (WHERE filtered.refund_status_code = 2) AS refund_completed_count,
			COUNT(*) FILTER (WHERE filtered.refund_status_code = 3) AS refund_failed_count
		FROM (` + baseQuery + `) AS filtered`

	var (
		summary               AdminRentalSummary
		rentalWaitingCount    int64
		rentalActiveCount     int64
		rentalExpiredCount    int64
		rentalCompletedCount  int64
		rentalCancelledCount  int64
		paymentPendingCount   int64
		paymentSuccessCount   int64
		paymentFailedCount    int64
		paymentCancelledCount int64
		refundNoneCount       int64
		refundRequestedCount  int64
		refundCompletedCount  int64
		refundFailedCount     int64
	)
	if err := db.QueryRow(ctx, query, args...).Scan(
		&summary.TotalCount,
		&summary.EligibleWalletRefundCount,
		&rentalWaitingCount,
		&rentalActiveCount,
		&rentalExpiredCount,
		&rentalCompletedCount,
		&rentalCancelledCount,
		&paymentPendingCount,
		&paymentSuccessCount,
		&paymentFailedCount,
		&paymentCancelledCount,
		&refundNoneCount,
		&refundRequestedCount,
		&refundCompletedCount,
		&refundFailedCount,
	); err != nil {
		return AdminRentalSummary{}, err
	}

	summary.RentalStatusCounts = map[string]int64{
		"WAITING_PAYMENT": rentalWaitingCount,
		"ACTIVE":          rentalActiveCount,
		"EXPIRED":         rentalExpiredCount,
		"COMPLETED":       rentalCompletedCount,
		"CANCELLED":       rentalCancelledCount,
	}
	summary.PaymentStatusCounts = map[string]int64{
		"PENDING":   paymentPendingCount,
		"SUCCESS":   paymentSuccessCount,
		"FAILED":    paymentFailedCount,
		"CANCELLED": paymentCancelledCount,
	}
	summary.RefundStatusCounts = map[string]int64{
		"NONE":      refundNoneCount,
		"REQUESTED": refundRequestedCount,
		"COMPLETED": refundCompletedCount,
		"FAILED":    refundFailedCount,
	}

	return summary, nil
}

func adminRentalsBaseQuery(filters AdminRentalListFilter) (string, []any) {
	base := `
		SELECT
			r.id,
			r.user_id,
			r.account_id,
			r.status AS rental_status,
			r.start_at,
			r.end_at,
			r.payment_expires_at,
			r.rental_price,
			r.deposit_amount,
			COALESCE(d.status, 0) AS deposit_hold_status,
			COALESCE(lp.id, 0) AS payment_id,
			COALESCE(lp.status, 0) AS payment_status,
			COALESCE(lp.provider, '') AS payment_provider,
			COALESCE(rf.status, 0) AS refund_status_code,
			COALESCE(rf.amount_total, 0) AS refund_total_amount,
			rf.processed_at AS refund_processed_at,
			r.created_at,
			(COALESCE(lp.provider, '') = 'balance'
				AND COALESCE(lp.status, 0) = 2
				AND r.status IN (3, 4)
				AND COALESCE(rf.status, 0) <> 2) AS eligible_wallet_refund
		FROM rentals r
		LEFT JOIN deposit_holds d ON d.rental_id = r.id
		LEFT JOIN LATERAL (
			SELECT id, status, provider
			FROM payments
			WHERE rental_id = r.id
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		) lp ON TRUE
		LEFT JOIN LATERAL (
			SELECT status, amount_total, processed_at
			FROM refunds
			WHERE rental_id = r.id AND user_id = r.user_id
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		) rf ON TRUE`

	conditions := make([]string, 0, 8)
	args := make([]any, 0, 8)
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	if filters.RentalStatus != "" {
		conditions = append(conditions, "r.status = "+addArg(adminRentalStatusFilterValues[filters.RentalStatus]))
	}
	if filters.PaymentStatus != "" {
		conditions = append(conditions, "COALESCE(lp.status, 0) = "+addArg(adminPaymentStatusFilterValues[filters.PaymentStatus]))
	}
	if filters.PaymentProvider != "" {
		conditions = append(conditions, "COALESCE(lp.provider, '') = "+addArg(filters.PaymentProvider))
	}
	if filters.DepositStatus != "" {
		switch filters.DepositStatus {
		case "NONE":
			conditions = append(conditions, "(r.deposit_amount <= 0 OR COALESCE(d.status, 0) = 0)")
		case "HELD":
			conditions = append(conditions, fmt.Sprintf("(r.deposit_amount > 0 AND COALESCE(d.status, 0) = %d)", depositHoldStatusHeld))
		case "RELEASED":
			conditions = append(conditions, fmt.Sprintf("(r.deposit_amount > 0 AND COALESCE(d.status, 0) = %d)", depositHoldStatusReleased))
		case "FORFEITED":
			conditions = append(conditions, fmt.Sprintf("(r.deposit_amount > 0 AND COALESCE(d.status, 0) = %d)", depositHoldStatusForfeited))
		case "REFUNDED":
			conditions = append(conditions, fmt.Sprintf("(r.deposit_amount > 0 AND COALESCE(d.status, 0) = %d)", depositHoldStatusRefunded))
		}
	}
	if filters.RefundStatus != "" {
		conditions = append(conditions, "COALESCE(rf.status, 0) = "+addArg(adminRefundStatusFilterValues[filters.RefundStatus]))
	}
	if filters.EligibleWalletRefund != nil {
		expr := "(COALESCE(lp.provider, '') = 'balance' AND COALESCE(lp.status, 0) = 2 AND r.status IN (3, 4) AND COALESCE(rf.status, 0) <> 2)"
		if *filters.EligibleWalletRefund {
			conditions = append(conditions, expr)
		} else {
			conditions = append(conditions, "NOT "+expr)
		}
	}
	if filters.UserID > 0 {
		conditions = append(conditions, "r.user_id = "+addArg(filters.UserID))
	}
	if filters.RentalID > 0 {
		conditions = append(conditions, "r.id = "+addArg(filters.RentalID))
	}
	if len(conditions) == 0 {
		return base, args
	}

	return base + "\nWHERE " + strings.Join(conditions, "\n  AND "), args
}

func adminPublicDepositStatus(depositAmount int64, holdStatus int16) string {
	if depositAmount <= 0 || holdStatus == 0 {
		return "NONE"
	}
	switch holdStatus {
	case depositHoldStatusHeld:
		return "HELD"
	case depositHoldStatusReleased:
		return "RELEASED"
	case depositHoldStatusForfeited:
		return "FORFEITED"
	case depositHoldStatusRefunded:
		return "REFUNDED"
	default:
		return "NONE"
	}
}
