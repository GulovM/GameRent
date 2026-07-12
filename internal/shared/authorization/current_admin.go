package authorization

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5"
	"rent_game_accs/internal/shared/database"
)

const adminMutationAdvisoryLock int64 = 510109837220260711

var (
	ErrCurrentUserForbidden = errors.New("current user is not allowed")
	ErrCurrentAdminRequired = errors.New("current admin authorization is required")
	ErrTransactionRequired  = errors.New("current admin mutation authorization requires a transaction")
)

type CurrentUser struct {
	ID        int64
	Role      string
	IsBlocked bool
	DeletedAt sql.NullTime
}

func LoadCurrentUser(ctx context.Context, db database.DB, userID int64, forUpdate bool) (*CurrentUser, error) {
	if userID <= 0 {
		return nil, ErrCurrentUserForbidden
	}

	query := `SELECT id, role, is_blocked, deleted_at FROM users WHERE id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}

	var user CurrentUser
	err := db.QueryRow(ctx, query, userID).Scan(&user.ID, &user.Role, &user.IsBlocked, &user.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCurrentUserForbidden
	}
	if err != nil {
		return nil, err
	}
	if user.IsBlocked || user.DeletedAt.Valid {
		return nil, ErrCurrentUserForbidden
	}
	return &user, nil
}

func RequireCurrentAdmin(ctx context.Context, db database.DB, userID int64, forUpdate bool) error {
	user, err := LoadCurrentUser(ctx, db, userID, forUpdate)
	if err != nil {
		if errors.Is(err, ErrCurrentUserForbidden) {
			return ErrCurrentAdminRequired
		}
		return err
	}
	if user.Role != "ADMIN" {
		return ErrCurrentAdminRequired
	}
	return nil
}

// RequireCurrentAdminForMutation must be called inside the same PostgreSQL
// transaction as the privileged mutation. The transaction-scoped advisory
// lock serializes admin financial and privilege mutations before actor rows are
// locked, preventing cross-admin lock-order races.
func RequireCurrentAdminForMutation(ctx context.Context, db database.DB, userID int64) error {
	if _, ok := db.(pgx.Tx); !ok {
		return ErrTransactionRequired
	}
	if _, err := db.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, adminMutationAdvisoryLock); err != nil {
		return err
	}
	return RequireCurrentAdmin(ctx, db, userID, true)
}
