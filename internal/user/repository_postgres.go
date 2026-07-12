package user

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"rent_game_accs/internal/shared/database"
)

var (
	ErrUserNotFound = errors.New("user not found")
)

type Repository interface {
	CreateUser(ctx context.Context, u *User) error
	GetUser(ctx context.Context, id int64) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	UpdateUser(ctx context.Context, u *User) error
	ListUsers(ctx context.Context, limit, offset int) ([]*User, error)
}

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) CreateUser(ctx context.Context, u *User) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `INSERT INTO users (email, password_hash, first_name, last_name, role, trust_score, is_blocked, created_at, updated_at, deleted_at)
		VALUES ($1, '', $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`
	err := db.QueryRow(ctx, query, u.Email, u.FirstName, u.LastName, string(u.Role), u.TrustScore, u.IsBlocked, u.CreatedAt, u.UpdatedAt, u.DeletedAt).Scan(&u.ID)
	if err == nil {
		u.Balance = 0
	}
	return err
}

func (r *PostgresRepository) GetUser(ctx context.Context, id int64) (*User, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `SELECT id, email, COALESCE(first_name, ''), COALESCE(last_name, ''), role, trust_score, is_blocked, balance, created_at, updated_at, deleted_at FROM users WHERE id = $1 AND deleted_at IS NULL`
	var u User
	err := db.QueryRow(ctx, query, id).Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.Role, &u.TrustScore, &u.IsBlocked, &u.Balance, &u.CreatedAt, &u.UpdatedAt, &u.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *PostgresRepository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `SELECT id, email, COALESCE(first_name, ''), COALESCE(last_name, ''), role, trust_score, is_blocked, balance, created_at, updated_at, deleted_at FROM users WHERE email = $1 AND deleted_at IS NULL`
	var u User
	err := db.QueryRow(ctx, query, email).Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.Role, &u.TrustScore, &u.IsBlocked, &u.Balance, &u.CreatedAt, &u.UpdatedAt, &u.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *PostgresRepository) UpdateUser(ctx context.Context, u *User) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `UPDATE users SET first_name = $1, last_name = $2, updated_at = $3 WHERE id = $4 AND deleted_at IS NULL`
	_, err := db.Exec(ctx, query, u.FirstName, u.LastName, u.UpdatedAt, u.ID)
	return err
}

func (r *PostgresRepository) ListUsers(ctx context.Context, limit, offset int) ([]*User, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `SELECT id, email, COALESCE(first_name, ''), COALESCE(last_name, ''), role, trust_score, is_blocked, balance, created_at, updated_at, deleted_at 
		FROM users WHERE deleted_at IS NULL ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := db.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		err := rows.Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.Role, &u.TrustScore, &u.IsBlocked, &u.Balance, &u.CreatedAt, &u.UpdatedAt, &u.DeletedAt)
		if err != nil {
			return nil, err
		}
		users = append(users, &u)
	}
	return users, rows.Err()
}
