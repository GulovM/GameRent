package payment

import (
	"context"
	"errors"
	"time"

	"rent_game_accs/internal/shared/database"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const webhookPaymentProvider = "internal"
const walletPaymentProvider = "balance"

const (
	ledgerEntryProviderPaymentReceived int16 = 1
	ledgerEntryDepositHeld             int16 = 2
	ledgerEntryDepositReleasedBalance  int16 = 3
	ledgerEntryDepositForfeited        int16 = 4
	ledgerEntryBalanceDebit            int16 = 5
	depositHoldStatusHeld              int16 = 1
	depositHoldStatusReleased          int16 = 2
	depositHoldStatusForfeited         int16 = 3
	securityEventTypeDepositReleased   int16 = 11
	securityEventTypeDepositForfeited  int16 = 12
	securityEventTypeWalletPayment     int16 = 13
)

type Repository interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
	CreatePendingPayment(ctx context.Context, rentalID, userID int64, amount int64, currency string) (paymentID int64, err error)
	LockWalletPaymentState(ctx context.Context, rentalID, userID int64) (*WalletPaymentState, error)
	DebitUserBalance(ctx context.Context, userID, amount int64, now time.Time) error
	MarkPaymentSuccessfulForWallet(ctx context.Context, paymentID int64, now time.Time) error
	RecordBalanceDebit(ctx context.Context, entry FinancialLedgerEntry) error
	GetUserBalance(ctx context.Context, userID int64) (*UserBalance, error)
	ListUserLedgerEntries(ctx context.Context, userID int64, limit, offset int) ([]PublicLedgerEntry, error)
	CountUserLedgerEntries(ctx context.Context, userID int64) (int64, error)
	LockPaymentForWebhookByID(ctx context.Context, paymentID int64) (*WebhookPaymentState, error)
	LockPaymentForWebhookByExternalTransaction(ctx context.Context, provider, externalTransactionID string) (*WebhookPaymentState, error)
	MarkPaymentSuccessful(ctx context.Context, paymentID int64, externalTransactionID string) error
	ActivateRentalForWebhook(ctx context.Context, rentalID int64, now time.Time) error
	MarkAccountRentedForWebhook(ctx context.Context, accountID int64, now time.Time) error
	RecordProviderPaymentReceived(ctx context.Context, entry FinancialLedgerEntry) error
	CreateDepositHold(ctx context.Context, hold DepositHold) error
	RecordDepositHeld(ctx context.Context, entry FinancialLedgerEntry) error
	LockDepositSettlementState(ctx context.Context, rentalID int64) (*DepositSettlementState, error)
	LoadDepositSettlementEligibility(ctx context.Context, rentalID int64) (*DepositSettlementEligibility, error)
	MarkDepositReleased(ctx context.Context, holdID int64, now time.Time) error
	MarkDepositForfeited(ctx context.Context, holdID int64, now time.Time) error
	CreditUserBalance(ctx context.Context, userID, amount int64, now time.Time) error
	RecordDepositReleasedToBalance(ctx context.Context, entry FinancialLedgerEntry) error
	RecordDepositForfeited(ctx context.Context, entry FinancialLedgerEntry) error
	LogDepositSecurityEvent(ctx context.Context, eventType int16, userID, accountID, rentalID int64, userAgent string, metadata []byte) error
	LogWalletSecurityEvent(ctx context.Context, userID, accountID, rentalID int64, clientIP, userAgent string, metadata []byte) error
	InsertAuditLog(ctx context.Context, actorUserID int64, entityType string, entityID int64, action string, oldValues, newValues []byte) error
	UpdatePaymentSuccess(ctx context.Context, paymentID int64, extTxID string) (rentalID, userID int64, amount int64, currency string, err error)
	ActivateRental(ctx context.Context, rentalID int64) (accountID int64, err error)
	MarkAccountRented(ctx context.Context, accountID int64) (login string, encPassword []byte, steamID64 string, err error)
	LogSecurityEvent(ctx context.Context, userID, accountID, rentalID int64, clientIP, userAgent string, metadata []byte) error
}

type WebhookPaymentState struct {
	PaymentID             int64
	RentalID              int64
	UserID                int64
	AccountID             int64
	Provider              string
	ExternalTransactionID string
	Status                int16
	Amount                int64
	Currency              string
	RentalPrice           int64
	DepositAmount         int64
	PaymentExpiresAt      time.Time
	RentalStatus          int16
	AccountStatus         int16
}

type FinancialLedgerEntry struct {
	UserID                int64
	RentalID              int64
	PaymentID             int64
	AccountID             int64
	Amount                int64
	Currency              string
	Provider              string
	ExternalTransactionID string
	IdempotencyKey        string
	CorrelationID         string
	Metadata              string
}

type WalletPaymentState struct {
	PaymentID             int64
	RentalID              int64
	UserID                int64
	AccountID             int64
	Provider              string
	ExternalTransactionID string
	PaymentStatus         int16
	RentalStatus          int16
	AccountStatus         int16
	RentalPrice           int64
	DepositAmount         int64
	PaymentExpiresAt      time.Time
	Currency              string
	UserBalance           int64
}

type UserBalance struct {
	UserID           int64
	AvailableBalance int64
	Currency         string
}

type PublicLedgerEntry struct {
	ID          int64
	EntryType   int16
	Amount      int64
	Currency    string
	RentalID    *int64
	PaymentID   *int64
	CreatedAt   time.Time
	DisplayType string
}

type DepositHold struct {
	UserID         int64
	RentalID       int64
	PaymentID      int64
	Amount         int64
	Currency       string
	HeldAt         time.Time
	IdempotencyKey string
}

type DepositSettlementState struct {
	HoldID        int64
	RentalID      int64
	UserID        int64
	AccountID     int64
	PaymentID     int64
	HoldStatus    int16
	RentalStatus  int16
	PaymentStatus int16
	Amount        int64
	Currency      string
	UserBalance   int64
}

type DepositSettlementEligibility struct {
	RentalExists   bool
	RentalStatus   int16
	PaymentStatus  int16
	DepositAmount  int64
	HasDepositHold bool
}

type PostgresRepository struct {
	pool      *pgxpool.Pool
	txManager database.TxManager
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{
		pool:      pool,
		txManager: database.NewTxManager(pool),
	}
}

func (r *PostgresRepository) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return r.txManager.WithinTransaction(ctx, fn)
}

func (r *PostgresRepository) CreatePendingPayment(ctx context.Context, rentalID, userID int64, amount int64, currency string) (int64, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	var id int64
	query := `
		INSERT INTO payments (
			rental_id, user_id, payment_type, provider, status, amount, currency
		) VALUES ($1, $2, 1, 'internal', 1, $3, $4)
		RETURNING id`
	err := db.QueryRow(ctx, query, rentalID, userID, amount, currency).Scan(&id)
	return id, err
}

func (r *PostgresRepository) LockWalletPaymentState(ctx context.Context, rentalID, userID int64) (*WalletPaymentState, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		SELECT
			p.id,
			r.id,
			r.user_id,
			r.account_id,
			p.provider,
			COALESCE(p.external_transaction_id, ''),
			p.status,
			r.status,
			a.status,
			r.rental_price,
			r.deposit_amount,
			r.payment_expires_at,
			p.currency,
			u.balance
		FROM rentals r
		JOIN accounts a ON a.id = r.account_id
		JOIN users u ON u.id = r.user_id
		JOIN payments p ON p.rental_id = r.id AND p.user_id = r.user_id
		WHERE r.id = $1 AND r.user_id = $2
		FOR UPDATE OF p, r, a, u`

	var state WalletPaymentState
	err := db.QueryRow(ctx, query, rentalID, userID).Scan(
		&state.PaymentID,
		&state.RentalID,
		&state.UserID,
		&state.AccountID,
		&state.Provider,
		&state.ExternalTransactionID,
		&state.PaymentStatus,
		&state.RentalStatus,
		&state.AccountStatus,
		&state.RentalPrice,
		&state.DepositAmount,
		&state.PaymentExpiresAt,
		&state.Currency,
		&state.UserBalance,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWalletPaymentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func (r *PostgresRepository) GetUserBalance(ctx context.Context, userID int64) (*UserBalance, error) {
	db := database.GetTxOrPool(ctx, r.pool)

	var result UserBalance
	result.Currency = "USD"
	err := db.QueryRow(ctx, `SELECT id, balance FROM users WHERE id = $1 AND deleted_at IS NULL`, userID).Scan(&result.UserID, &result.AvailableBalance)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFinancialUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *PostgresRepository) ListUserLedgerEntries(ctx context.Context, userID int64, limit, offset int) ([]PublicLedgerEntry, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	rows, err := db.Query(ctx, `
		SELECT id, entry_type, amount, currency, rental_id, payment_id, created_at
		FROM financial_ledger_entries
		WHERE user_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]PublicLedgerEntry, 0, limit)
	for rows.Next() {
		var entry PublicLedgerEntry
		if err := rows.Scan(
			&entry.ID,
			&entry.EntryType,
			&entry.Amount,
			&entry.Currency,
			&entry.RentalID,
			&entry.PaymentID,
			&entry.CreatedAt,
		); err != nil {
			return nil, err
		}
		entry.DisplayType = publicLedgerDisplayType(entry.EntryType)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func (r *PostgresRepository) CountUserLedgerEntries(ctx context.Context, userID int64) (int64, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	var total int64
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM financial_ledger_entries WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func publicLedgerDisplayType(entryType int16) string {
	switch entryType {
	case ledgerEntryProviderPaymentReceived:
		return "PROVIDER_PAYMENT_RECEIVED"
	case ledgerEntryDepositHeld:
		return "DEPOSIT_HELD"
	case ledgerEntryDepositReleasedBalance:
		return "DEPOSIT_RELEASED_TO_BALANCE"
	case ledgerEntryDepositForfeited:
		return "DEPOSIT_FORFEITED"
	case ledgerEntryBalanceDebit:
		return "BALANCE_DEBIT"
	default:
		return "UNKNOWN"
	}
}

func (r *PostgresRepository) LockPaymentForWebhookByID(ctx context.Context, paymentID int64) (*WebhookPaymentState, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		SELECT
			p.id,
			p.rental_id,
			p.user_id,
			r.account_id,
			p.provider,
			COALESCE(p.external_transaction_id, ''),
			p.status,
			p.amount,
			p.currency,
			r.rental_price,
			r.deposit_amount,
			r.payment_expires_at,
			r.status,
			a.status
		FROM payments p
		JOIN rentals r ON r.id = p.rental_id
		JOIN accounts a ON a.id = r.account_id
		WHERE p.id = $1
		FOR UPDATE OF p, r, a`

	var state WebhookPaymentState
	err := db.QueryRow(ctx, query, paymentID).Scan(
		&state.PaymentID,
		&state.RentalID,
		&state.UserID,
		&state.AccountID,
		&state.Provider,
		&state.ExternalTransactionID,
		&state.Status,
		&state.Amount,
		&state.Currency,
		&state.RentalPrice,
		&state.DepositAmount,
		&state.PaymentExpiresAt,
		&state.RentalStatus,
		&state.AccountStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPaymentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func (r *PostgresRepository) LockPaymentForWebhookByExternalTransaction(ctx context.Context, provider, externalTransactionID string) (*WebhookPaymentState, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		SELECT
			p.id,
			p.rental_id,
			p.user_id,
			r.account_id,
			p.provider,
			COALESCE(p.external_transaction_id, ''),
			p.status,
			p.amount,
			p.currency,
			r.rental_price,
			r.deposit_amount,
			r.payment_expires_at,
			r.status,
			a.status
		FROM payments p
		JOIN rentals r ON r.id = p.rental_id
		JOIN accounts a ON a.id = r.account_id
		WHERE p.provider = $1 AND p.external_transaction_id = $2
		FOR UPDATE OF p, r, a`

	var state WebhookPaymentState
	err := db.QueryRow(ctx, query, provider, externalTransactionID).Scan(
		&state.PaymentID,
		&state.RentalID,
		&state.UserID,
		&state.AccountID,
		&state.Provider,
		&state.ExternalTransactionID,
		&state.Status,
		&state.Amount,
		&state.Currency,
		&state.RentalPrice,
		&state.DepositAmount,
		&state.PaymentExpiresAt,
		&state.RentalStatus,
		&state.AccountStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPaymentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func (r *PostgresRepository) MarkPaymentSuccessful(ctx context.Context, paymentID int64, externalTransactionID string) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		UPDATE payments
		SET status = 2,
			external_transaction_id = $1,
			processed_at = NOW()
		WHERE id = $2 AND status = 1`
	tag, err := db.Exec(ctx, query, externalTransactionID, paymentID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPaymentAlreadyProcessed
	}
	return nil
}

func (r *PostgresRepository) MarkPaymentSuccessfulForWallet(ctx context.Context, paymentID int64, now time.Time) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		UPDATE payments
		SET status = 2,
			provider = $2,
			external_transaction_id = NULL,
			processed_at = $3
		WHERE id = $1 AND status = 1`
	tag, err := db.Exec(ctx, query, paymentID, walletPaymentProvider, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPaymentAlreadyProcessed
	}
	return nil
}

func (r *PostgresRepository) ActivateRentalForWebhook(ctx context.Context, rentalID int64, now time.Time) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		UPDATE rentals
		SET status = 2, updated_at = $2
		WHERE id = $1
			AND status = 1
			AND payment_expires_at > NOW()`
	tag, err := db.Exec(ctx, query, rentalID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrRentalNotEligible
	}
	return nil
}

func (r *PostgresRepository) MarkAccountRentedForWebhook(ctx context.Context, accountID int64, now time.Time) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		UPDATE accounts
		SET status = 4, updated_at = $2
		WHERE id = $1 AND status = 3`
	tag, err := db.Exec(ctx, query, accountID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAccountNotReserved
	}
	return nil
}

func (r *PostgresRepository) RecordProviderPaymentReceived(ctx context.Context, entry FinancialLedgerEntry) error {
	return r.insertLedgerEntry(ctx, ledgerEntryProviderPaymentReceived, entry)
}

func (r *PostgresRepository) CreateDepositHold(ctx context.Context, hold DepositHold) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		INSERT INTO deposit_holds (
			rental_id, user_id, payment_id, amount, currency, status, held_at, idempotency_key, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $7, $7)
		ON CONFLICT (idempotency_key) DO NOTHING`
	_, err := db.Exec(
		ctx,
		query,
		hold.RentalID,
		hold.UserID,
		hold.PaymentID,
		hold.Amount,
		hold.Currency,
		depositHoldStatusHeld,
		hold.HeldAt,
		hold.IdempotencyKey,
	)
	return err
}

func (r *PostgresRepository) RecordDepositHeld(ctx context.Context, entry FinancialLedgerEntry) error {
	return r.insertLedgerEntry(ctx, ledgerEntryDepositHeld, entry)
}

func (r *PostgresRepository) LockDepositSettlementState(ctx context.Context, rentalID int64) (*DepositSettlementState, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		SELECT
			d.id,
			r.id,
			r.user_id,
			r.account_id,
			p.id,
			d.status,
			r.status,
			p.status,
			d.amount,
			d.currency,
			u.balance
		FROM deposit_holds d
		JOIN rentals r ON r.id = d.rental_id
		JOIN payments p ON p.id = d.payment_id
		JOIN accounts a ON a.id = r.account_id
		JOIN users u ON u.id = r.user_id
		WHERE d.rental_id = $1
		FOR UPDATE OF d, r, p, a, u`

	var state DepositSettlementState
	err := db.QueryRow(ctx, query, rentalID).Scan(
		&state.HoldID,
		&state.RentalID,
		&state.UserID,
		&state.AccountID,
		&state.PaymentID,
		&state.HoldStatus,
		&state.RentalStatus,
		&state.PaymentStatus,
		&state.Amount,
		&state.Currency,
		&state.UserBalance,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDepositHoldNotFound
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func (r *PostgresRepository) LoadDepositSettlementEligibility(ctx context.Context, rentalID int64) (*DepositSettlementEligibility, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		SELECT
			r.id,
			r.status,
			r.deposit_amount,
			COALESCE(p.status, -1),
			EXISTS(SELECT 1 FROM deposit_holds d WHERE d.rental_id = r.id)
		FROM rentals r
		LEFT JOIN payments p ON p.rental_id = r.id AND p.user_id = r.user_id
		WHERE r.id = $1`

	var eligibility DepositSettlementEligibility
	var rentalIDValue int64
	err := db.QueryRow(ctx, query, rentalID).Scan(
		&rentalIDValue,
		&eligibility.RentalStatus,
		&eligibility.DepositAmount,
		&eligibility.PaymentStatus,
		&eligibility.HasDepositHold,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return &eligibility, nil
	}
	if err != nil {
		return nil, err
	}
	eligibility.RentalExists = true
	return &eligibility, nil
}

func (r *PostgresRepository) MarkDepositReleased(ctx context.Context, holdID int64, now time.Time) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		UPDATE deposit_holds
		SET status = $2,
			released_at = $3,
			updated_at = $3
		WHERE id = $1 AND status = $4`
	tag, err := db.Exec(ctx, query, holdID, depositHoldStatusReleased, now, depositHoldStatusHeld)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrDepositSettlementNotAllowed
	}
	return nil
}

func (r *PostgresRepository) MarkDepositForfeited(ctx context.Context, holdID int64, now time.Time) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		UPDATE deposit_holds
		SET status = $2,
			forfeited_at = $3,
			updated_at = $3
		WHERE id = $1 AND status = $4`
	tag, err := db.Exec(ctx, query, holdID, depositHoldStatusForfeited, now, depositHoldStatusHeld)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrDepositSettlementNotAllowed
	}
	return nil
}

func (r *PostgresRepository) CreditUserBalance(ctx context.Context, userID, amount int64, now time.Time) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		UPDATE users
		SET balance = balance + $2,
			updated_at = $3
		WHERE id = $1`
	tag, err := db.Exec(ctx, query, userID, amount, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPaymentNotFound
	}
	return nil
}

func (r *PostgresRepository) DebitUserBalance(ctx context.Context, userID, amount int64, now time.Time) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		UPDATE users
		SET balance = balance - $2,
			updated_at = $3
		WHERE id = $1
			AND balance >= $2`
	tag, err := db.Exec(ctx, query, userID, amount, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrWalletInsufficientBalance
	}
	return nil
}

func (r *PostgresRepository) RecordBalanceDebit(ctx context.Context, entry FinancialLedgerEntry) error {
	return r.insertLedgerEntry(ctx, ledgerEntryBalanceDebit, entry)
}

func (r *PostgresRepository) RecordDepositReleasedToBalance(ctx context.Context, entry FinancialLedgerEntry) error {
	return r.insertLedgerEntry(ctx, ledgerEntryDepositReleasedBalance, entry)
}

func (r *PostgresRepository) RecordDepositForfeited(ctx context.Context, entry FinancialLedgerEntry) error {
	return r.insertLedgerEntry(ctx, ledgerEntryDepositForfeited, entry)
}

func (r *PostgresRepository) insertLedgerEntry(ctx context.Context, entryType int16, entry FinancialLedgerEntry) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		INSERT INTO financial_ledger_entries (
			entry_type,
			user_id,
			rental_id,
			payment_id,
			account_id,
			amount,
			currency,
			provider,
			external_transaction_id,
			idempotency_key,
			correlation_id,
			metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, COALESCE($12::jsonb, '{}'::jsonb))
		ON CONFLICT (idempotency_key) DO NOTHING`
	_, err := db.Exec(
		ctx,
		query,
		entryType,
		entry.UserID,
		entry.RentalID,
		entry.PaymentID,
		entry.AccountID,
		entry.Amount,
		entry.Currency,
		entry.Provider,
		entry.ExternalTransactionID,
		entry.IdempotencyKey,
		entry.CorrelationID,
		entry.Metadata,
	)
	return err
}

func (r *PostgresRepository) LogDepositSecurityEvent(ctx context.Context, eventType int16, userID, accountID, rentalID int64, userAgent string, metadata []byte) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		INSERT INTO security_events (
			user_id, account_id, rental_id, event_type, ip_address, user_agent, success, metadata, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, true, $7, NOW())`
	_, err := db.Exec(ctx, query, userID, accountID, rentalID, eventType, nil, userAgent, metadata)
	return err
}

func (r *PostgresRepository) LogWalletSecurityEvent(ctx context.Context, userID, accountID, rentalID int64, clientIP, userAgent string, metadata []byte) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		INSERT INTO security_events (
			user_id, account_id, rental_id, event_type, ip_address, user_agent, success, metadata, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, true, $7, NOW())`

	var ipParam any = clientIP
	if clientIP == "" || clientIP == "::1" || clientIP == "127.0.0.1" {
		ipParam = "127.0.0.1"
	}

	_, err := db.Exec(ctx, query, userID, accountID, rentalID, securityEventTypeWalletPayment, ipParam, userAgent, metadata)
	return err
}

func (r *PostgresRepository) InsertAuditLog(ctx context.Context, actorUserID int64, entityType string, entityID int64, action string, oldValues, newValues []byte) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		INSERT INTO audit_logs (
			actor_user_id, entity_type, entity_id, action, old_values, new_values, created_at
		) VALUES ($1, $2, $3, $4, COALESCE($5::jsonb, '{}'::jsonb), COALESCE($6::jsonb, '{}'::jsonb), NOW())`
	_, err := db.Exec(ctx, query, actorUserID, entityType, entityID, action, oldValues, newValues)
	return err
}

func (r *PostgresRepository) UpdatePaymentSuccess(ctx context.Context, paymentID int64, extTxID string) (int64, int64, int64, string, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	var rentalID, userID int64
	var amount int64
	var currency string
	query := `
		UPDATE payments 
		SET status = 2, external_transaction_id = $1, processed_at = NOW() 
		WHERE (id = $2 OR rental_id = $2) AND status < 2
		RETURNING rental_id, user_id, amount, currency`
	err := db.QueryRow(ctx, query, extTxID, paymentID).Scan(&rentalID, &userID, &amount, &currency)
	return rentalID, userID, amount, currency, err
}

func (r *PostgresRepository) ActivateRental(ctx context.Context, rentalID int64) (int64, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	var accountID int64
	query := `
		UPDATE rentals 
		SET status = 2 
		WHERE id = $1 
		RETURNING account_id`
	err := db.QueryRow(ctx, query, rentalID).Scan(&accountID)
	return accountID, err
}

func (r *PostgresRepository) MarkAccountRented(ctx context.Context, accountID int64) (string, []byte, string, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	var login string
	var encPassword []byte
	var steamID64 string
	query := `
		UPDATE accounts 
		SET status = 4, updated_at = NOW() 
		WHERE id = $1 
		RETURNING login, encrypted_password, steam_id64`
	err := db.QueryRow(ctx, query, accountID).Scan(&login, &encPassword, &steamID64)
	return login, encPassword, steamID64, err
}

func (r *PostgresRepository) LogSecurityEvent(ctx context.Context, userID, accountID, rentalID int64, clientIP, userAgent string, metadata []byte) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		INSERT INTO security_events (
			user_id, account_id, rental_id, event_type, ip_address, user_agent, success, metadata, created_at
		) VALUES ($1, $2, $3, 2, $4, $5, true, $6, NOW())`

	var ipParam any = clientIP
	if clientIP == "" || clientIP == "::1" || clientIP == "127.0.0.1" {
		ipParam = "127.0.0.1"
	}

	_, err := db.Exec(ctx, query, userID, accountID, rentalID, ipParam, userAgent, metadata)
	return err
}
