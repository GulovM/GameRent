package rental

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rent_game_accs/internal/account"
	"rent_game_accs/internal/shared/database"
)

var (
	ErrRentalNotFound = errors.New("rental not found")
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) CreateRental(ctx context.Context, rental *Rental) error {
	db := database.GetTxOrPool(ctx, r.pool)

	query := `INSERT INTO rentals (id, user_id, account_id, rental_price, deposit_amount, start_at, end_at, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := db.Exec(ctx, query,
		rental.ID, rental.UserID, rental.AccountID, rental.RentalPrice.Amount, rental.DepositAmount.Amount, rental.Period.StartAt, rental.Period.EndAt, int16(rental.Status),
	)
	return err
}

func (r *PostgresRepository) GetRental(ctx context.Context, id int64) (*Rental, error) {
	db := database.GetTxOrPool(ctx, r.pool)

	query := `
		SELECT id, user_id, account_id, rental_price, deposit_amount, start_at, end_at, status,
		       actual_finished_at, deposit_review_deadline_at, completed_at
		FROM rentals
		WHERE id = $1`

	var rental Rental
	var rentalPriceVal, depositAmountVal int64
	var statusVal int16
	var startAt, endAt time.Time
	var actualFinishedAt, reviewDeadlineAt, completedAt sql.NullTime

	err := db.QueryRow(ctx, query, id).Scan(
		&rental.ID, &rental.UserID, &rental.AccountID, &rentalPriceVal, &depositAmountVal, &startAt, &endAt, &statusVal,
		&actualFinishedAt, &reviewDeadlineAt, &completedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRentalNotFound
	}
	if err != nil {
		return nil, err
	}

	rentalPrice, err := NewMoney(rentalPriceVal, "USD")
	if err != nil {
		return nil, err
	}
	rental.RentalPrice = rentalPrice

	depositAmount, err := NewMoney(depositAmountVal, "USD")
	if err != nil {
		return nil, err
	}
	rental.DepositAmount = depositAmount

	period, err := NewRentalPeriod(startAt, endAt)
	if err != nil {
		return nil, err
	}
	rental.Period = period
	rental.Status = RentalStatus(statusVal)
	if actualFinishedAt.Valid {
		rental.ActualFinishedAt = &actualFinishedAt.Time
	}
	if reviewDeadlineAt.Valid {
		rental.DepositReviewDeadlineAt = &reviewDeadlineAt.Time
	}
	if completedAt.Valid {
		rental.CompletedAt = &completedAt.Time
	}

	return &rental, nil
}

func (r *PostgresRepository) ListCustomerRentals(ctx context.Context, userID int64) ([]CustomerRental, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	rows, err := db.Query(ctx, customerRentalsQuery+` WHERE r.user_id = $1 ORDER BY r.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CustomerRental, 0)
	for rows.Next() {
		item, err := scanCustomerRental(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *PostgresRepository) GetCustomerRental(ctx context.Context, rentalID int64) (*CustomerRental, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	row := db.QueryRow(ctx, customerRentalsQuery+` WHERE r.id = $1`, rentalID)
	item, err := scanCustomerRental(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRentalNotFound
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (r *PostgresRepository) GetRentalQuote(ctx context.Context, accountID int64) (*RentalQuote, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	var quote RentalQuote
	err := db.QueryRow(ctx, `SELECT hourly_price, deposit_amount FROM accounts WHERE id=$1 AND deleted_at IS NULL`, accountID).Scan(&quote.HourlyPrice, &quote.DepositAmount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRentalNotFound
	}
	if err != nil {
		return nil, err
	}
	return &quote, nil
}

const customerRentalsQuery = `
	SELECT r.id, r.user_id, r.account_id, r.status, r.start_at, r.end_at, r.payment_expires_at,
		r.rental_price, r.deposit_amount, COALESCE(d.status, 0), COALESCE(rf.status, 0),
		COALESCE(rf.amount_total, 0), rf.processed_at
	FROM rentals r
	LEFT JOIN deposit_holds d ON d.rental_id = r.id
	LEFT JOIN LATERAL (
		SELECT status, amount_total, processed_at FROM refunds
		WHERE rental_id = r.id AND user_id = r.user_id
		ORDER BY created_at DESC, id DESC LIMIT 1
	) rf ON TRUE`

type customerRentalScanner interface{ Scan(...any) error }

func scanCustomerRental(row customerRentalScanner) (*CustomerRental, error) {
	var item CustomerRental
	var processedAt sql.NullTime
	err := row.Scan(&item.ID, &item.UserID, &item.AccountID, &item.Status, &item.StartAt, &item.EndAt,
		&item.PaymentExpiresAt, &item.RentalPrice, &item.DepositAmount, &item.DepositHoldStatus,
		&item.RefundStatus, &item.RefundTotalAmount, &processedAt)
	if err != nil {
		return nil, err
	}
	if processedAt.Valid {
		value := processedAt.Time
		item.RefundProcessedAt = &value
	}
	return &item, nil
}

func (r *PostgresRepository) GetRentalCredentials(ctx context.Context, rentalID, userID int64, now time.Time) (*RentalCredentialsRecord, error) {
	db := database.GetTxOrPool(ctx, r.pool)

	query := `
		SELECT
			r.id,
			r.user_id,
			r.account_id,
			r.status,
			r.payment_expires_at,
			p.id,
			a.status,
			a.login,
			a.encrypted_password,
			a.steam_id64
		FROM rentals r
		JOIN accounts a ON a.id = r.account_id
		JOIN payments p ON p.rental_id = r.id
			AND p.user_id = r.user_id
			AND p.status = $6
		WHERE r.id = $1
			AND r.user_id = $2
			AND r.status = $3
			AND r.start_at <= $4
			AND r.end_at > $4
			AND a.status = $5
			AND NOT EXISTS (
				SELECT 1
				FROM refunds f
				WHERE f.rental_id = r.id
					AND f.user_id = r.user_id
					AND f.status IN (1, 2)
			)
		FOR UPDATE OF r, a, p
	`

	var rec RentalCredentialsRecord
	err := db.QueryRow(ctx, query, rentalID, userID, StatusActive, now, int16(account.StatusRented), int16(2)).Scan(
		&rec.RentalID,
		&rec.UserID,
		&rec.AccountID,
		&rec.RentalStatus,
		&rec.PaymentExpiresAt,
		&rec.PaymentID,
		&rec.AccountStatus,
		&rec.Login,
		&rec.EncryptedPassword,
		&rec.SteamID64,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRentalNotFound
	}
	if err != nil {
		return nil, err
	}

	return &rec, nil
}

func (r *PostgresRepository) RecordCredentialIssued(ctx context.Context, event CredentialIssueEvent) error {
	db := database.GetTxOrPool(ctx, r.pool)
	var ipAddress any
	if event.IPAddress != "" {
		ipAddress = event.IPAddress
	}
	_, err := db.Exec(ctx, `
		INSERT INTO security_events (
			user_id, account_id, rental_id, event_type, ip_address, user_agent, success, metadata, created_at
		) VALUES (
			$1, $2, $3, 7, $4, $5, true,
			jsonb_build_object('event', 'rental_credentials_issued', 'rental_id', $3::bigint, 'user_id', $1::bigint),
			$6
		)`, event.UserID, event.AccountID, event.RentalID, ipAddress, event.UserAgent, event.CreatedAt.UTC())
	return err
}

func (r *PostgresRepository) CancelWaitingPaymentRental(ctx context.Context, rentalID, userID int64, reason string, now time.Time) (bool, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "cancelled by user"
	}

	query := `
		WITH locked AS (
			SELECT
				r.id AS rental_id,
				r.account_id,
				r.user_id,
				p.id AS payment_id
			FROM rentals r
			JOIN payments p ON p.rental_id = r.id AND p.user_id = r.user_id
			JOIN accounts a ON a.id = r.account_id
			WHERE r.id = $1
				AND r.user_id = $2
				AND r.status = $4
				AND p.status = $5
				AND a.status = $6
			FOR UPDATE OF r, p, a
		),
		payment_update AS (
			UPDATE payments p
			SET status = $7,
				processed_at = $3
			FROM locked
			WHERE p.id = locked.payment_id
			RETURNING 1
		),
		rental_update AS (
			UPDATE rentals r
			SET status = $8,
				cancellation_reason = $9,
				actual_finished_at = $3,
				updated_at = $3
			FROM locked
			WHERE r.id = locked.rental_id
			RETURNING 1
		),
		account_update AS (
			UPDATE accounts a
			SET status = $10,
				updated_at = $3
			FROM locked
			WHERE a.id = locked.account_id
				AND NOT EXISTS (
					SELECT 1
					FROM rentals active_rentals
					WHERE active_rentals.account_id = locked.account_id
						AND active_rentals.status = $11
				)
			RETURNING 1
		),
		audit_insert AS (
			INSERT INTO security_events (
				user_id, account_id, rental_id, event_type, ip_address, user_agent, success, metadata, created_at
			)
			SELECT
				locked.user_id,
				locked.account_id,
				locked.rental_id,
				$12,
				NULL,
				'api',
				true,
				jsonb_build_object(
					'event', 'rental_cancelled',
					'payment_id', locked.payment_id,
					'rental_id', locked.rental_id,
					'account_id', locked.account_id
				),
				$3
			FROM locked
			WHERE EXISTS (SELECT 1 FROM payment_update)
				AND EXISTS (SELECT 1 FROM rental_update)
				AND EXISTS (SELECT 1 FROM account_update)
			RETURNING 1
		)
		SELECT
			(SELECT COUNT(*) FROM locked),
			(SELECT COUNT(*) FROM account_update),
			(SELECT COUNT(*) FROM audit_insert)`

	var lockedCount, accountUpdated, auditInserted int
	err := db.QueryRow(
		ctx,
		query,
		rentalID,
		userID,
		now,
		int16(StatusWaitingPayment),
		int16(1),
		int16(account.StatusReserved),
		int16(3),
		int16(StatusCancelled),
		reason,
		int16(account.StatusAvailable),
		int16(StatusActive),
		int16(9),
	).Scan(&lockedCount, &accountUpdated, &auditInserted)
	if err != nil {
		return false, fmt.Errorf("cancel waiting payment rental transition: %w", err)
	}
	if lockedCount > 0 {
		if accountUpdated == 0 || auditInserted == 0 {
			return false, fmt.Errorf("cancel waiting payment rental transition: %w", ErrInvalidTransition)
		}
		return true, nil
	}

	var status RentalStatus
	err = db.QueryRow(ctx, `SELECT status FROM rentals WHERE id = $1 AND user_id = $2`, rentalID, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrRentalNotFound
	}
	if err != nil {
		return false, fmt.Errorf("load rental after cancel no-op: %w", err)
	}
	if status == StatusCancelled {
		return false, nil
	}
	return false, ErrCannotCancel
}
