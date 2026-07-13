package user

import (
	"context"
	"errors"
	"time"

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

type AdminRepository interface {
	Repository
	GetUserForUpdate(ctx context.Context, id int64) (*User, error)
	UpdateAdminState(ctx context.Context, id int64, trustScore *int, isBlocked *bool, role *string) error
	RevokeActiveRefreshTokens(ctx context.Context, userID int64) error
	RecordAdminUserStateChange(ctx context.Context, actorUserID, userID int64, oldRole string, oldBlocked bool, newRole string, newBlocked bool) error
	ListAuditLogs(ctx context.Context, limit int) ([]AuditLog, error)
}

type AuditLog struct {
	ID          int64
	ActorUserID *int64
	EntityType  string
	EntityID    int64
	Action      string
	CreatedAt   time.Time
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

func (r *PostgresRepository) GetUserForUpdate(ctx context.Context, id int64) (*User, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `SELECT id, email, COALESCE(first_name, ''), COALESCE(last_name, ''), role, trust_score, is_blocked, balance, created_at, updated_at, deleted_at FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`
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

func (r *PostgresRepository) UpdateAdminState(ctx context.Context, id int64, trustScore *int, isBlocked *bool, role *string) error {
	db := database.GetTxOrPool(ctx, r.pool)
	tag, err := db.Exec(ctx, `UPDATE users SET trust_score=COALESCE($1,trust_score), is_blocked=COALESCE($2,is_blocked), role=COALESCE($3,role), updated_at=NOW() WHERE id=$4 AND deleted_at IS NULL`, trustScore, isBlocked, role, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (r *PostgresRepository) RevokeActiveRefreshTokens(ctx context.Context, userID int64) error {
	db := database.GetTxOrPool(ctx, r.pool)
	_, err := db.Exec(ctx, `UPDATE refresh_tokens SET revoked_at=NOW() WHERE user_id=$1 AND revoked_at IS NULL`, userID)
	return err
}

func (r *PostgresRepository) RecordAdminUserStateChange(ctx context.Context, actorUserID, userID int64, oldRole string, oldBlocked bool, newRole string, newBlocked bool) error {
	db := database.GetTxOrPool(ctx, r.pool)
	_, err := db.Exec(ctx, `INSERT INTO audit_logs (actor_user_id, entity_type, entity_id, action, old_values, new_values, created_at) VALUES ($1, 'USER', $2, 'USER_SECURITY_STATE_UPDATED', jsonb_build_object('role',$3::text,'is_blocked',$4::boolean), jsonb_build_object('role',$5::text,'is_blocked',$6::boolean), NOW())`, actorUserID, userID, oldRole, oldBlocked, newRole, newBlocked)
	return err
}

func (r *PostgresRepository) ListAuditLogs(ctx context.Context, limit int) ([]AuditLog, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	rows, err := db.Query(ctx, `SELECT id, actor_user_id, entity_type, entity_id, action, created_at FROM audit_logs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AuditLog, 0)
	for rows.Next() {
		var item AuditLog
		if err := rows.Scan(&item.ID, &item.ActorUserID, &item.EntityType, &item.EntityID, &item.Action, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
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
