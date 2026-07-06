package rental

import (
	"context"
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

	query := `SELECT id, user_id, account_id, rental_price, deposit_amount, start_at, end_at, status FROM rentals WHERE id = $1`

	var rental Rental
	var rentalPriceVal, depositAmountVal int64
	var statusVal int16
	var startAt, endAt time.Time

	err := db.QueryRow(ctx, query, id).Scan(
		&rental.ID, &rental.UserID, &rental.AccountID, &rentalPriceVal, &depositAmountVal, &startAt, &endAt, &statusVal,
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

	return &rental, nil
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
			a.status,
			a.login,
			a.encrypted_password,
			a.steam_id64
		FROM rentals r
		JOIN accounts a ON a.id = r.account_id
		WHERE r.id = $1
			AND r.user_id = $2
			AND r.status = $3
			AND r.end_at > $4
			AND r.payment_expires_at > $4
			AND a.status = $5
			AND EXISTS (
				SELECT 1
				FROM payments p
				WHERE p.rental_id = r.id
					AND p.user_id = r.user_id
					AND p.status = $6
			)
		FOR UPDATE OF r, a
	`

	var rec RentalCredentialsRecord
	err := db.QueryRow(ctx, query, rentalID, userID, StatusActive, now, int16(account.StatusRented), int16(2)).Scan(
		&rec.RentalID,
		&rec.UserID,
		&rec.AccountID,
		&rec.RentalStatus,
		&rec.PaymentExpiresAt,
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
