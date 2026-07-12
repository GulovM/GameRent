package repo_postgres

import (
	"context"
	"errors"
	"time"

	pkg_postgres_pool "rent_game_accs/internal/pkg/repository/postgres/pool"
	shared_authorization "rent_game_accs/internal/shared/authorization"
	"rent_game_accs/internal/shared/database"

	"github.com/jackc/pgx/v5"
)

type ExpiredRental struct {
	ID        int64
	AccountID int64
}

type ExpiredWaitingPaymentReservation struct {
	PaymentID int64
	RentalID  int64
	AccountID int64
	UserID    int64
}

const (
	paymentStatusPending                int16 = 1
	paymentStatusFailed                 int16 = 3
	rentalStatusWaiting                 int16 = 1
	rentalStatusActive                  int16 = 2
	rentalStatusExpired                 int16 = 3
	accountStatusAvail                  int16 = 2
	accountStatusReserved               int16 = 3
	accountStatusRented                 int16 = 4
	securityEventTypeReservationExpired int16 = 8
	securityEventTypeRentalExpired      int16 = 10
)

var ErrAccountLifecycleConflict = errors.New("account has an exclusive lifecycle state or rental")

type AccountGameSyncInfo struct {
	StoreGameID     string
	Name            string
	PlaytimeMinutes int
}

func (r *Repository) GetExpiredRentals(ctx context.Context, now time.Time) ([]ExpiredRental, error) {
	query := `SELECT id, account_id FROM rentals WHERE status = 2 AND end_at < $1`
	rows, err := r.pool.Query(ctx, query, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rentals []ExpiredRental
	for rows.Next() {
		var er ExpiredRental
		if err := rows.Scan(&er.ID, &er.AccountID); err != nil {
			return nil, err
		}
		rentals = append(rentals, er)
	}
	return rentals, rows.Err()
}

func (r *Repository) GetExpiredWaitingPaymentReservations(ctx context.Context, now time.Time) ([]ExpiredWaitingPaymentReservation, error) {
	query := `
		SELECT p.id, r.id, r.account_id, r.user_id
		FROM payments p
		JOIN rentals r ON r.id = p.rental_id
		JOIN accounts a ON a.id = r.account_id
		WHERE p.provider = 'internal'
			AND p.status = $2
			AND r.status = $3
			AND a.status = $4
			AND r.payment_expires_at <= $1
		ORDER BY r.payment_expires_at ASC, p.id ASC`

	rows, err := r.pool.Query(ctx, query, now, paymentStatusPending, rentalStatusWaiting, accountStatusReserved)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reservations []ExpiredWaitingPaymentReservation
	for rows.Next() {
		var item ExpiredWaitingPaymentReservation
		if err := rows.Scan(&item.PaymentID, &item.RentalID, &item.AccountID, &item.UserID); err != nil {
			return nil, err
		}
		reservations = append(reservations, item)
	}

	return reservations, rows.Err()
}

func (r *Repository) ExpireWaitingPaymentReservation(ctx context.Context, paymentID int64, now time.Time) (bool, error) {
	query := `
		WITH locked AS (
			SELECT p.id AS payment_id, p.rental_id, p.user_id, r.account_id
			FROM payments p
			JOIN rentals r ON r.id = p.rental_id
			JOIN accounts a ON a.id = r.account_id
			WHERE p.id = $1
				AND p.provider = 'internal'
				AND p.status = $3
				AND r.status = $4
				AND a.status = $5
				AND r.payment_expires_at <= $2
			FOR UPDATE OF p, r, a
		),
		payment_update AS (
			UPDATE payments p
			SET status = $6,
				processed_at = $2
			FROM locked
			WHERE p.id = locked.payment_id
			RETURNING 1
		),
		rental_update AS (
			UPDATE rentals r
			SET status = $7,
				updated_at = $2
			FROM locked
			WHERE r.id = locked.rental_id
			RETURNING 1
		),
		account_update AS (
			UPDATE accounts a
			SET status = $8,
				updated_at = $2
			FROM locked
			WHERE a.id = locked.account_id
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
				$9,
				NULL,
				'scheduler',
				true,
				jsonb_build_object(
					'event', 'waiting_payment_expired',
					'payment_id', locked.payment_id,
					'rental_id', locked.rental_id,
					'account_id', locked.account_id
				),
				$2
			FROM locked
			RETURNING 1
		)
		SELECT locked.payment_id
		FROM locked`

	var selectedPaymentID int64
	err := r.pool.QueryRow(
		ctx,
		query,
		paymentID,
		now,
		paymentStatusPending,
		rentalStatusWaiting,
		accountStatusReserved,
		paymentStatusFailed,
		rentalStatusExpired,
		accountStatusAvail,
		securityEventTypeReservationExpired,
	).Scan(&selectedPaymentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return selectedPaymentID != 0, nil
}

func (r *Repository) ExpireRental(ctx context.Context, rentalID, accountID int64, now time.Time) (bool, error) {
	connPool, ok := r.pool.(*pkg_postgres_pool.ConnectionPool)
	if !ok {
		return false, shared_authorization.ErrTransactionRequired
	}
	tx, err := connPool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	query := `
		WITH locked AS (
			SELECT r.id AS rental_id, r.account_id, r.user_id
			FROM rentals r
			JOIN accounts a ON a.id = r.account_id
			WHERE r.id = $1
				AND r.account_id = $2
				AND r.status = $4
				AND r.end_at < $3
				AND a.status = $5
			FOR UPDATE OF r, a SKIP LOCKED
		),
		rental_update AS (
			UPDATE rentals r
			SET status = $6,
				actual_finished_at = $3,
				updated_at = $3
			FROM locked
			WHERE r.id = locked.rental_id
			RETURNING 1
		),
		account_update AS (
			UPDATE accounts a
			SET status = $7,
				updated_at = $3
			FROM locked
			WHERE a.id = locked.account_id
				AND NOT EXISTS (
					SELECT 1
					FROM rentals active_rentals
					WHERE active_rentals.account_id = locked.account_id
						AND active_rentals.id <> locked.rental_id
						AND active_rentals.status = $4
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
				$8,
				NULL,
				'scheduler',
				true,
				jsonb_build_object(
					'event', 'rental_expired',
					'rental_id', locked.rental_id,
					'account_id', locked.account_id
				),
				$3
			FROM locked
			WHERE EXISTS (SELECT 1 FROM rental_update)
				AND EXISTS (SELECT 1 FROM account_update)
			RETURNING 1
		)
		SELECT
			(SELECT COUNT(*) FROM locked),
			(SELECT COUNT(*) FROM account_update),
			(SELECT COUNT(*) FROM audit_insert)`

	var lockedCount, accountUpdated, auditInserted int
	err = tx.QueryRow(
		ctx,
		query,
		rentalID,
		accountID,
		now,
		rentalStatusActive,
		accountStatusRented,
		rentalStatusExpired,
		accountStatusAvail,
		securityEventTypeRentalExpired,
	).Scan(&lockedCount, &accountUpdated, &auditInserted)
	if err != nil {
		return false, err
	}
	if lockedCount == 0 {
		return false, nil
	}
	if accountUpdated == 0 || auditInserted == 0 {
		return false, errors.New("expire rental transition did not update all required records")
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) GetAccountsForSync(ctx context.Context) ([]int64, error) {
	query := `SELECT id FROM accounts`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *Repository) GetAccountSyncDetails(ctx context.Context, accountID int64) (string, string, error) {
	query := `SELECT login, COALESCE(steam_id64, '') FROM accounts WHERE id = $1`
	var login, steamID64 string
	err := r.pool.QueryRow(ctx, query, accountID).Scan(&login, &steamID64)
	if err != nil {
		return "", "", err
	}
	return login, steamID64, nil
}

func (r *Repository) DisableAccountIfIdle(ctx context.Context, accountID int64) error {
	connPool, ok := r.pool.(*pkg_postgres_pool.ConnectionPool)
	if !ok {
		return shared_authorization.ErrTransactionRequired
	}
	tx, err := connPool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := disableAccountIfIdle(ctx, tx, accountID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) DisableAccountIfIdleAsCurrentAdmin(ctx context.Context, actorUserID, accountID int64) error {
	connPool, ok := r.pool.(*pkg_postgres_pool.ConnectionPool)
	if !ok {
		return shared_authorization.ErrTransactionRequired
	}
	tx, err := connPool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := shared_authorization.RequireCurrentAdminForMutation(ctx, tx, actorUserID); err != nil {
		return err
	}
	if err := disableAccountIfIdle(ctx, tx, accountID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func disableAccountIfIdle(ctx context.Context, db database.DB, accountID int64) error {
	query := `
		WITH locked AS (
			SELECT id
			FROM accounts
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		), updated AS (
			UPDATE accounts a
			SET status = 6, deleted_at = NOW(), updated_at = NOW()
			FROM locked
			WHERE a.id = locked.id
				AND a.status NOT IN ($4, $5)
				AND NOT EXISTS (
					SELECT 1
					FROM rentals r
					WHERE r.account_id = locked.id
						AND r.status IN ($2, $3)
				)
			RETURNING a.id
		)
		SELECT (SELECT COUNT(*) FROM locked), (SELECT COUNT(*) FROM updated)`

	var lockedCount, updatedCount int
	if err := db.QueryRow(ctx, query, accountID, rentalStatusWaiting, rentalStatusActive, accountStatusReserved, accountStatusRented).Scan(&lockedCount, &updatedCount); err != nil {
		return err
	}
	if lockedCount == 0 {
		return pgx.ErrNoRows
	}
	if updatedCount == 0 {
		return ErrAccountLifecycleConflict
	}
	return nil
}

func (r *Repository) SyncAccountGames(ctx context.Context, accountID int64, games []AccountGameSyncInfo) error {
	var tx pgx.Tx
	var err error

	if connPool, ok := r.pool.(*pkg_postgres_pool.ConnectionPool); ok {
		tx, err = connPool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
	}

	db := database.DB(r.pool)
	if tx != nil {
		db = tx
	}
	if err := syncAccountGames(ctx, db, accountID, games); err != nil {
		return err
	}

	if tx != nil {
		return tx.Commit(ctx)
	}
	return nil
}

func (r *Repository) SyncAccountGamesAsCurrentAdmin(ctx context.Context, actorUserID, accountID int64, games []AccountGameSyncInfo) error {
	connPool, ok := r.pool.(*pkg_postgres_pool.ConnectionPool)
	if !ok {
		return shared_authorization.ErrTransactionRequired
	}
	tx, err := connPool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := shared_authorization.RequireCurrentAdminForMutation(ctx, tx, actorUserID); err != nil {
		return err
	}
	if err := syncAccountGames(ctx, tx, accountID, games); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func syncAccountGames(ctx context.Context, db database.DB, accountID int64, games []AccountGameSyncInfo) error {
	var primaryGameID *int64

	for _, g := range games {
		var gameID int64

		findQuery := `SELECT id FROM games WHERE steam_app_id = CAST($1 AS INTEGER)`
		findErr := db.QueryRow(ctx, findQuery, g.StoreGameID).Scan(&gameID)

		if errors.Is(findErr, pgx.ErrNoRows) {

			insertQuery := `INSERT INTO games (name, steam_app_id) VALUES ($1, CAST($2 AS INTEGER)) RETURNING id`
			findErr = db.QueryRow(ctx, insertQuery, g.Name, g.StoreGameID).Scan(&gameID)
			if findErr != nil {
				return findErr
			}
		} else if findErr != nil {
			return findErr
		}

		if primaryGameID == nil {
			primaryGameID = &gameID
		}

		var playtime int
		checkQuery := `SELECT playtime_minutes FROM account_games WHERE account_id = $1 AND game_id = $2`
		checkErr := db.QueryRow(ctx, checkQuery, accountID, gameID).Scan(&playtime)

		if errors.Is(checkErr, pgx.ErrNoRows) {
			insertRel := `INSERT INTO account_games (account_id, game_id, playtime_minutes) VALUES ($1, $2, $3)`
			_, err := db.Exec(ctx, insertRel, accountID, gameID, g.PlaytimeMinutes)
			if err != nil {
				return err
			}
		} else if checkErr == nil {
			updateRel := `UPDATE account_games SET playtime_minutes = $1 WHERE account_id = $2 AND game_id = $3`
			_, err := db.Exec(ctx, updateRel, g.PlaytimeMinutes, accountID, gameID)
			if err != nil {
				return err
			}
		} else {
			return checkErr
		}
	}

	if primaryGameID != nil {
		updateAccount := `UPDATE accounts SET library_synced_at = NOW(), updated_at = NOW() WHERE id = $1`
		_, err := db.Exec(ctx, updateAccount, accountID)
		if err != nil {
			return err
		}
	}

	return nil
}
