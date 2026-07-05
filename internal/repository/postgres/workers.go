package repo_postgres

import (
	"context"
	"errors"
	"time"

	pkg_postgres_pool "rent_game_accs/internal/pkg/repository/postgres/pool"

	"github.com/jackc/pgx/v5"
)

type ExpiredRental struct {
	ID        int64
	AccountID int64
}

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

func (r *Repository) ExpireRental(ctx context.Context, rentalID, accountID int64) error {
	var tx pgx.Tx
	var err error

	if connPool, ok := r.pool.(*pkg_postgres_pool.ConnectionPool); ok {
		tx, err = connPool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
	}

	queryRental := `UPDATE rentals SET status = 3 WHERE id = $1 AND status = 2`
	if tx != nil {
		_, err = tx.Exec(ctx, queryRental, rentalID)
	} else {
		_, err = r.pool.Exec(ctx, queryRental, rentalID)
	}
	if err != nil {
		return err
	}

	queryAccount := `UPDATE accounts SET status = 2 WHERE id = $1`
	if tx != nil {
		_, err = tx.Exec(ctx, queryAccount, accountID)
	} else {
		_, err = r.pool.Exec(ctx, queryAccount, accountID)
	}
	if err != nil {
		return err
	}

	if tx != nil {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}

	return nil
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

func (r *Repository) BanAccount(ctx context.Context, accountID int64) error {
	query := `UPDATE accounts SET status = 6, deleted_at = NOW(), updated_at = NOW() WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, accountID)
	return err
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

	var primaryGameID *int64

	for _, g := range games {
		var gameID int64

		findQuery := `SELECT id FROM games WHERE steam_app_id = CAST($1 AS INTEGER)`
		var findErr error
		if tx != nil {
			findErr = tx.QueryRow(ctx, findQuery, g.StoreGameID).Scan(&gameID)
		} else {
			findErr = r.pool.QueryRow(ctx, findQuery, g.StoreGameID).Scan(&gameID)
		}

		if findErr == pgx.ErrNoRows {

			insertQuery := `INSERT INTO games (name, steam_app_id) VALUES ($1, CAST($2 AS INTEGER)) RETURNING id`
			if tx != nil {
				findErr = tx.QueryRow(ctx, insertQuery, g.Name, g.StoreGameID).Scan(&gameID)
			} else {
				findErr = r.pool.QueryRow(ctx, insertQuery, g.Name, g.StoreGameID).Scan(&gameID)
			}
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
		var checkErr error
		if tx != nil {
			checkErr = tx.QueryRow(ctx, checkQuery, accountID, gameID).Scan(&playtime)
		} else {
			checkErr = r.pool.QueryRow(ctx, checkQuery, accountID, gameID).Scan(&playtime)
		}

		if errors.Is(checkErr, pgx.ErrNoRows) {
			insertRel := `INSERT INTO account_games (account_id, game_id, playtime_minutes) VALUES ($1, $2, $3)`
			if tx != nil {
				_, err = tx.Exec(ctx, insertRel, accountID, gameID, g.PlaytimeMinutes)
			} else {
				_, err = r.pool.Exec(ctx, insertRel, accountID, gameID, g.PlaytimeMinutes)
			}
			if err != nil {
				return err
			}
		} else if checkErr == nil {
			updateRel := `UPDATE account_games SET playtime_minutes = $1 WHERE account_id = $2 AND game_id = $3`
			if tx != nil {
				_, err = tx.Exec(ctx, updateRel, g.PlaytimeMinutes, accountID, gameID)
			} else {
				_, err = r.pool.Exec(ctx, updateRel, g.PlaytimeMinutes, accountID, gameID)
			}
			if err != nil {
				return err
			}
		} else {
			return checkErr
		}
	}

	if primaryGameID != nil {
		updateAccount := `UPDATE accounts SET library_synced_at = NOW(), updated_at = NOW() WHERE id = $1`
		if tx != nil {
			_, err = tx.Exec(ctx, updateAccount, accountID)
		} else {
			_, err = r.pool.Exec(ctx, updateAccount, accountID)
		}
		if err != nil {
			return err
		}
	}

	if tx != nil {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}

	return nil
}
