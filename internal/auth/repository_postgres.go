package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"rent_game_accs/internal/shared/database"
)

var (
	ErrUserNotFound         = errors.New("user not found")
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
)

type Repository interface {
	CreateUser(ctx context.Context, u *User) error
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	GetUserByID(ctx context.Context, id int64) (*User, error)
	UpdateUser(ctx context.Context, u *User) error

	CreateRefreshToken(ctx context.Context, token *RefreshToken) error
	GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error)
	GetRefreshTokenForUpdate(ctx context.Context, tokenHash string) (*RefreshToken, error)
	UpdateRefreshToken(ctx context.Context, token *RefreshToken) error
	DeleteExpiredRefreshTokens(ctx context.Context) error
}

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) CreateUser(ctx context.Context, u *User) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `INSERT INTO users (email, password_hash, first_name, last_name, role, email_verified, is_blocked, created_at, deleted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`
	err := db.QueryRow(ctx, query, u.Email, u.PasswordHash, u.FirstName, u.LastName, string(u.Role), u.EmailVerified, u.IsBlocked, u.CreatedAt, u.DeletedAt).Scan(&u.ID)
	return err
}

func (r *PostgresRepository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `SELECT id, email, password_hash, COALESCE(first_name, ''), COALESCE(last_name, ''), role, email_verified, is_blocked, created_at, updated_at, deleted_at FROM users WHERE email = $1 AND deleted_at IS NULL`
	var u User
	err := db.QueryRow(ctx, query, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FirstName, &u.LastName, &u.Role, &u.EmailVerified, &u.IsBlocked, &u.CreatedAt, &u.UpdatedAt, &u.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *PostgresRepository) GetUserByID(ctx context.Context, id int64) (*User, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `SELECT id, email, password_hash, COALESCE(first_name, ''), COALESCE(last_name, ''), role, email_verified, is_blocked, created_at, updated_at, deleted_at FROM users WHERE id = $1 AND deleted_at IS NULL`
	var u User
	err := db.QueryRow(ctx, query, id).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FirstName, &u.LastName, &u.Role, &u.EmailVerified, &u.IsBlocked, &u.CreatedAt, &u.UpdatedAt, &u.DeletedAt)
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
	query := `UPDATE users SET email = $1, password_hash = $2, first_name = $3, last_name = $4, role = $5, email_verified = $6, is_blocked = $7, updated_at = $8, deleted_at = $9 WHERE id = $10`
	_, err := db.Exec(ctx, query, u.Email, u.PasswordHash, u.FirstName, u.LastName, string(u.Role), u.EmailVerified, u.IsBlocked, u.UpdatedAt, u.DeletedAt, u.ID)
	return err
}

func (r *PostgresRepository) CreateRefreshToken(ctx context.Context, token *RefreshToken) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `INSERT INTO refresh_tokens (user_id, token_hash, expires_at, revoked_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`
	err := db.QueryRow(ctx, query, token.UserID, token.TokenHash, token.ExpiresAt, token.RevokedAt, token.CreatedAt).Scan(&token.ID)
	return err
}

func (r *PostgresRepository) GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	return r.getRefreshToken(ctx, tokenHash, false)
}

func (r *PostgresRepository) GetRefreshTokenForUpdate(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	return r.getRefreshToken(ctx, tokenHash, true)
}

func (r *PostgresRepository) getRefreshToken(ctx context.Context, tokenHash string, forUpdate bool) (*RefreshToken, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `SELECT id, user_id, token_hash, expires_at, revoked_at, created_at FROM refresh_tokens WHERE token_hash = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var token RefreshToken
	err := db.QueryRow(ctx, query, tokenHash).Scan(&token.ID, &token.UserID, &token.TokenHash, &token.ExpiresAt, &token.RevokedAt, &token.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRefreshTokenNotFound
	}
	if err != nil {
		return nil, err
	}
	return &token, nil
}

func (r *PostgresRepository) UpdateRefreshToken(ctx context.Context, token *RefreshToken) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `UPDATE refresh_tokens SET revoked_at = $1 WHERE id = $2 AND revoked_at IS NULL`
	tag, err := db.Exec(ctx, query, token.RevokedAt, token.ID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrTokenAlreadyRevoked
	}
	return err
}

func (r *PostgresRepository) DeleteExpiredRefreshTokens(ctx context.Context) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `DELETE FROM refresh_tokens WHERE expires_at < $1`
	_, err := db.Exec(ctx, query, time.Now())
	return err
}
