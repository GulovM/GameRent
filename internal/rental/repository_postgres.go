package rental

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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
	if err == pgx.ErrNoRows {
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
