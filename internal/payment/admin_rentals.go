package payment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"rent_game_accs/internal/shared/database"

	"github.com/jackc/pgx/v5"
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
		"UNKNOWN":   {},
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

type AdminRentalDetail struct {
	Rental        AdminRentalDetailRental
	Payment       *AdminRentalDetailPayment
	Deposit       *AdminRentalDetailDeposit
	RefundSummary AdminRentalDetailRefundSummary
	LedgerSummary AdminRentalDetailLedgerSummary
	SupportFlags  AdminRentalSupportFlags
}

type AdminRentalDetailRental struct {
	ID               int64
	UserID           int64
	AccountID        int64
	Status           int16
	StartAt          time.Time
	EndAt            time.Time
	RentalPrice      int64
	DepositAmount    int64
	PaymentExpiresAt time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type AdminRentalDetailPayment struct {
	ID        int64
	Status    int16
	Provider  string
	Amount    int64
	Currency  string
	CreatedAt time.Time
}

type AdminRentalDetailDeposit struct {
	Amount      int64
	Currency    string
	Status      string
	HeldAt      *time.Time
	ReleasedAt  *time.Time
	ForfeitedAt *time.Time
	RefundedAt  *time.Time
}

type AdminRentalDetailRefundSummary struct {
	Count                int64
	LatestStatus         string
	TotalRefundedPrimary int64
	TotalRefundedDeposit int64
	LatestProcessedAt    *time.Time
}

type AdminRentalDetailLedgerSummary struct {
	CountsByDisplayType map[string]int64
	TotalsByDisplayType map[string]int64
	LatestEntries       []AdminRentalSafeLedgerEntry
}

type AdminRentalSafeLedgerEntry struct {
	ID          int64
	DisplayType string
	Amount      int64
	Currency    string
	CreatedAt   time.Time
}

type AdminRentalSupportFlags struct {
	EligibleWalletRefund   bool
	RefundIneligibleReason string
	HasActiveCredentials   bool
	PaymentWindowExpired   bool
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

func (s *PaymentService) GetAdminRentalDetail(ctx context.Context, rentalID int64) (*AdminRentalDetail, error) {
	if rentalID <= 0 {
		return nil, newInvalidAdminRentalFiltersError("rental_id must be a positive integer")
	}
	return s.repo.GetAdminRentalDetail(ctx, rentalID)
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

func (r *PostgresRepository) GetAdminRentalDetail(ctx context.Context, rentalID int64) (*AdminRentalDetail, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	now := time.Now().UTC()

	detail := &AdminRentalDetail{}

	var (
		paymentID          int64
		paymentStatus      int16
		paymentProvider    string
		paymentAmount      int64
		paymentCurrency    string
		paymentCreatedAt   sql.NullTime
		depositAmount      sql.NullInt64
		depositCurrency    sql.NullString
		depositHoldStatus  int16
		depositHeldAt      sql.NullTime
		depositReleasedAt  sql.NullTime
		depositForfeitedAt sql.NullTime
		depositRefundedAt  sql.NullTime
	)
	query := `
		SELECT
			r.id,
			r.user_id,
			r.account_id,
			r.status,
			r.start_at,
			r.end_at,
			r.rental_price,
			r.deposit_amount,
			r.payment_expires_at,
			r.created_at,
			r.updated_at,
			COALESCE(lp.id, 0),
			COALESCE(lp.status, 0),
			COALESCE(lp.provider, ''),
			COALESCE(lp.amount, 0),
			COALESCE(lp.currency, ''),
			lp.created_at,
			d.amount,
			d.currency,
			COALESCE(d.status, 0),
			d.held_at,
			d.released_at,
			d.forfeited_at,
			d.refunded_at
		FROM rentals r
		LEFT JOIN LATERAL (
			SELECT id, status, provider, amount, currency, created_at
			FROM payments
			WHERE rental_id = r.id
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		) lp ON TRUE
		LEFT JOIN deposit_holds d ON d.rental_id = r.id
		WHERE r.id = $1`
	if err := db.QueryRow(ctx, query, rentalID).Scan(
		&detail.Rental.ID,
		&detail.Rental.UserID,
		&detail.Rental.AccountID,
		&detail.Rental.Status,
		&detail.Rental.StartAt,
		&detail.Rental.EndAt,
		&detail.Rental.RentalPrice,
		&detail.Rental.DepositAmount,
		&detail.Rental.PaymentExpiresAt,
		&detail.Rental.CreatedAt,
		&detail.Rental.UpdatedAt,
		&paymentID,
		&paymentStatus,
		&paymentProvider,
		&paymentAmount,
		&paymentCurrency,
		&paymentCreatedAt,
		&depositAmount,
		&depositCurrency,
		&depositHoldStatus,
		&depositHeldAt,
		&depositReleasedAt,
		&depositForfeitedAt,
		&depositRefundedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAdminRentalNotFound
		}
		return nil, err
	}

	if paymentID > 0 {
		detail.Payment = &AdminRentalDetailPayment{
			ID:       paymentID,
			Status:   paymentStatus,
			Provider: paymentProvider,
			Amount:   paymentAmount,
			Currency: paymentCurrency,
		}
		if paymentCreatedAt.Valid {
			detail.Payment.CreatedAt = paymentCreatedAt.Time
		}
	}

	if depositAmount.Valid && depositAmount.Int64 > 0 {
		detail.Deposit = &AdminRentalDetailDeposit{
			Amount:   depositAmount.Int64,
			Currency: depositCurrency.String,
			Status:   adminPublicDepositStatus(depositAmount.Int64, depositHoldStatus),
		}
		if depositHeldAt.Valid {
			value := depositHeldAt.Time
			detail.Deposit.HeldAt = &value
		}
		if depositReleasedAt.Valid {
			value := depositReleasedAt.Time
			detail.Deposit.ReleasedAt = &value
		}
		if depositForfeitedAt.Valid {
			value := depositForfeitedAt.Time
			detail.Deposit.ForfeitedAt = &value
		}
		if depositRefundedAt.Valid {
			value := depositRefundedAt.Time
			detail.Deposit.RefundedAt = &value
		}
	}

	var latestRefundStatusCode int16
	var latestProcessedAt sql.NullTime
	refundSummaryQuery := `
		SELECT
			COUNT(*),
			COALESCE(SUM(amount_principal) FILTER (WHERE status = $2), 0),
			COALESCE(SUM(amount_deposit) FILTER (WHERE status = $2), 0),
			COALESCE((
				SELECT rf.status
				FROM refunds rf
				WHERE rf.rental_id = $1
				ORDER BY rf.created_at DESC, rf.id DESC
				LIMIT 1
			), 0),
			(
				SELECT rf.processed_at
				FROM refunds rf
				WHERE rf.rental_id = $1
				ORDER BY rf.created_at DESC, rf.id DESC
				LIMIT 1
			)
		FROM refunds
		WHERE rental_id = $1`
	if err := db.QueryRow(ctx, refundSummaryQuery, rentalID, refundStatusCompleted).Scan(
		&detail.RefundSummary.Count,
		&detail.RefundSummary.TotalRefundedPrimary,
		&detail.RefundSummary.TotalRefundedDeposit,
		&latestRefundStatusCode,
		&latestProcessedAt,
	); err != nil {
		return nil, err
	}
	if latestRefundStatusCode == 0 {
		detail.RefundSummary.LatestStatus = "NONE"
	} else {
		detail.RefundSummary.LatestStatus = publicRefundStatus(latestRefundStatusCode)
	}
	if latestProcessedAt.Valid {
		value := latestProcessedAt.Time
		detail.RefundSummary.LatestProcessedAt = &value
	}

	detail.LedgerSummary = AdminRentalDetailLedgerSummary{
		CountsByDisplayType: map[string]int64{},
		TotalsByDisplayType: map[string]int64{},
		LatestEntries:       []AdminRentalSafeLedgerEntry{},
	}
	ledgerAggregateRows, err := db.Query(ctx, `
		SELECT entry_type, COUNT(*), COALESCE(SUM(amount), 0)
		FROM financial_ledger_entries
		WHERE rental_id = $1
		GROUP BY entry_type`, rentalID)
	if err != nil {
		return nil, err
	}
	defer ledgerAggregateRows.Close()
	for ledgerAggregateRows.Next() {
		var entryType int16
		var count int64
		var total int64
		if err := ledgerAggregateRows.Scan(&entryType, &count, &total); err != nil {
			return nil, err
		}
		displayType := publicLedgerDisplayType(entryType)
		detail.LedgerSummary.CountsByDisplayType[displayType] = count
		detail.LedgerSummary.TotalsByDisplayType[displayType] = total
	}
	if err := ledgerAggregateRows.Err(); err != nil {
		return nil, err
	}

	ledgerRows, err := db.Query(ctx, `
		SELECT id, entry_type, amount, currency, created_at
		FROM financial_ledger_entries
		WHERE rental_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 5`, rentalID)
	if err != nil {
		return nil, err
	}
	defer ledgerRows.Close()
	for ledgerRows.Next() {
		var (
			entry     AdminRentalSafeLedgerEntry
			entryType int16
		)
		if err := ledgerRows.Scan(&entry.ID, &entryType, &entry.Amount, &entry.Currency, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entry.DisplayType = publicLedgerDisplayType(entryType)
		detail.LedgerSummary.LatestEntries = append(detail.LedgerSummary.LatestEntries, entry)
	}
	if err := ledgerRows.Err(); err != nil {
		return nil, err
	}

	detail.SupportFlags.PaymentWindowExpired = !detail.Rental.PaymentExpiresAt.After(now)
	detail.SupportFlags.HasActiveCredentials = detail.Rental.Status == 2 && detail.Rental.EndAt.After(now) && detail.Payment != nil && detail.Payment.Status == 2
	detail.SupportFlags.EligibleWalletRefund, detail.SupportFlags.RefundIneligibleReason = adminWalletRefundEligibility(
		detail.Rental.Status,
		func() int16 {
			if detail.Payment == nil {
				return 0
			}
			return detail.Payment.Status
		}(),
		func() string {
			if detail.Payment == nil {
				return ""
			}
			return detail.Payment.Provider
		}(),
		detail.RefundSummary.LatestStatus,
	)

	return detail, nil
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
		case "UNKNOWN":
			conditions = append(conditions, "(r.deposit_amount > 0 AND d.status IS NOT NULL AND d.status NOT IN (1, 2, 3, 4))")
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
		return "UNKNOWN"
	}
}

func adminWalletRefundEligibility(rentalStatus int16, paymentStatus int16, paymentProvider string, latestRefundStatus string) (bool, string) {
	if latestRefundStatus == "COMPLETED" {
		return false, "REFUND_ALREADY_COMPLETED"
	}
	if paymentProvider != walletPaymentProvider {
		return false, "PAYMENT_PROVIDER_NOT_BALANCE"
	}
	if paymentStatus != 2 {
		return false, "PAYMENT_NOT_SUCCESS"
	}
	if rentalStatus != 3 && rentalStatus != 4 {
		return false, "RENTAL_NOT_EXPIRED_OR_COMPLETED"
	}
	return true, ""
}
