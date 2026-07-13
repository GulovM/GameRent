package review

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Create(ctx context.Context, input CreateInput) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`INSERT INTO reviews (rental_id,user_id,account_id,rating,comment) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		input.RentalID, input.UserID, input.AccountID, input.Rating, input.Comment,
	).Scan(&id)
	return id, err
}

func (r *PostgresRepository) ListByAccount(ctx context.Context, accountID int64) ([]Review, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id,user_id,rating,comment,created_at FROM reviews WHERE account_id=$1 ORDER BY created_at DESC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	reviews := make([]Review, 0)
	for rows.Next() {
		var item Review
		item.AccountID = accountID
		if err := rows.Scan(&item.ID, &item.UserID, &item.Rating, &item.Comment, &item.CreatedAt); err != nil {
			return nil, err
		}
		reviews = append(reviews, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return reviews, nil
}
