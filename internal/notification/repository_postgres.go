package notification

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

func (r *PostgresRepository) ListByUser(ctx context.Context, userID int64) ([]Notification, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id,type,title,body,is_read,created_at FROM notifications WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notifications := make([]Notification, 0)
	for rows.Next() {
		var item Notification
		if err := rows.Scan(&item.ID, &item.Type, &item.Title, &item.Body, &item.Read, &item.CreatedAt); err != nil {
			return nil, err
		}
		notifications = append(notifications, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return notifications, nil
}

func (r *PostgresRepository) MarkRead(ctx context.Context, notificationID, userID int64) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE notifications SET is_read=true WHERE id=$1 AND user_id=$2`, notificationID, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() != 0, nil
}
