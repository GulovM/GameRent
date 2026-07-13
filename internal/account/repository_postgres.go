package account

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"rent_game_accs/internal/shared/database"
)

var (
	ErrAccountNotFound = errors.New("account not found")
)

type Repository interface {
	CreateAccount(ctx context.Context, a *Account) error
	GetAccount(ctx context.Context, id int64) (*Account, error)
	GetAccountForUpdate(ctx context.Context, id int64) (*Account, error)
	ReserveAccount(ctx context.Context, id int64, now time.Time) error
	ListAccounts(ctx context.Context, limit, offset int) ([]*Account, error)
	SearchAccounts(ctx context.Context, limit, offset int, search string, gameID int64, minPrice, maxPrice int64, status string) ([]*Account, error)
	SyncAccountGames(ctx context.Context, accountID int64, games []AccountGame) error

	Encrypt(plaintext string) ([]byte, error)
	Decrypt(ciphertext []byte) (string, error)
}

type AdminRepository interface {
	Repository
	CreateAdminAccount(ctx context.Context, steamID64, login string, encryptedPassword []byte, hourlyPrice, depositAmount int64) (int64, error)
	UpdateAdminPricing(ctx context.Context, id int64, hourlyPrice, depositAmount *int64) error
}

func (r *PostgresRepository) CreateAdminAccount(ctx context.Context, steamID64, login string, encryptedPassword []byte, hourlyPrice, depositAmount int64) (int64, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	var id int64
	err := db.QueryRow(ctx, `INSERT INTO accounts (steam_id64,login,encrypted_password,hourly_price,deposit_amount,status,steam_guard_enabled,inventory_verified,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,2,true,true,NOW(),NOW()) RETURNING id`, steamID64, login, encryptedPassword, hourlyPrice, depositAmount).Scan(&id)
	return id, err
}

func (r *PostgresRepository) UpdateAdminPricing(ctx context.Context, id int64, hourlyPrice, depositAmount *int64) error {
	db := database.GetTxOrPool(ctx, r.pool)
	tag, err := db.Exec(ctx, `UPDATE accounts SET hourly_price=COALESCE($1,hourly_price), deposit_amount=COALESCE($2,deposit_amount), updated_at=NOW() WHERE id=$3 AND deleted_at IS NULL`, hourlyPrice, depositAmount, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAccountNotFound
	}
	return nil
}

type PostgresRepository struct {
	pool          *pgxpool.Pool
	encryptionKey []byte
}

func NewPostgresRepository(pool *pgxpool.Pool, encryptionKey string) *PostgresRepository {
	return &PostgresRepository{
		pool:          pool,
		encryptionKey: []byte(encryptionKey),
	}
}

func (r *PostgresRepository) Encrypt(plaintext string) ([]byte, error) {
	if len(r.encryptionKey) == 0 {
		return nil, errors.New("encryption key not configured")
	}
	block, err := aes.NewCipher(r.encryptionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return ciphertext, nil
}

func (r *PostgresRepository) Decrypt(ciphertext []byte) (string, error) {
	if len(r.encryptionKey) == 0 {
		return "", errors.New("encryption key not configured")
	}
	block, err := aes.NewCipher(r.encryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	nonce, encryptedMsg := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encryptedMsg, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func (r *PostgresRepository) CreateAccount(ctx context.Context, a *Account) error {
	db := database.GetTxOrPool(ctx, r.pool)

	statusVal := mapStatusToDB(a.Status)

	query := `INSERT INTO accounts (
		id, login, encrypted_password, status, 
		steam_guard_enabled, inventory_verified, last_security_check, 
		hourly_price, deposit_amount, profile_url, avatar_url, 
		library_synced_at, created_at, updated_at, deleted_at, steam_id64
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`

	_, err := db.Exec(ctx, query,
		a.ID, a.Credentials.Login, a.Credentials.EncryptedPassword, statusVal,
		a.SteamGuardEnabled, a.InventoryVerified, a.LastSecurityCheck,
		a.HourlyPrice.Amount, a.DepositAmount.Amount, a.ProfileURL, a.AvatarURL,
		a.LibrarySyncedAt, a.CreatedAt, a.UpdatedAt, a.DeletedAt, a.Credentials.SteamID64,
	)
	if err != nil {
		return err
	}

	return r.SyncAccountGames(ctx, a.ID, a.Games)
}

func (r *PostgresRepository) GetAccount(ctx context.Context, id int64) (*Account, error) {
	db := database.GetTxOrPool(ctx, r.pool)

	query := `SELECT 
		id, login, encrypted_password, status, steam_guard_enabled, 
		inventory_verified, COALESCE(last_security_check, '0001-01-01'::timestamp), hourly_price, deposit_amount, 
		COALESCE(profile_url, ''), COALESCE(avatar_url, ''), COALESCE(library_synced_at, '0001-01-01'::timestamp), created_at, updated_at, deleted_at, steam_id64
		FROM accounts WHERE id = $1 AND deleted_at IS NULL`

	var a Account
	var login, steamID64 string
	var encPassword []byte
	var statusVal int16
	var hourlyPriceVal, depositAmountVal int64

	err := db.QueryRow(ctx, query, id).Scan(
		&a.ID, &login, &encPassword, &statusVal, &a.SteamGuardEnabled,
		&a.InventoryVerified, &a.LastSecurityCheck, &hourlyPriceVal, &depositAmountVal,
		&a.ProfileURL, &a.AvatarURL, &a.LibrarySyncedAt, &a.CreatedAt, &a.UpdatedAt, &a.DeletedAt, &steamID64,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAccountNotFound
	}
	if err != nil {
		return nil, err
	}

	creds, err := NewSteamCredentials(login, encPassword, steamID64)
	if err != nil {
		return nil, err
	}
	a.Credentials = creds

	hourlyPrice, err := NewMoney(hourlyPriceVal, "USD")
	if err != nil {
		return nil, err
	}
	a.HourlyPrice = hourlyPrice

	depositAmount, err := NewMoney(depositAmountVal, "USD")
	if err != nil {
		return nil, err
	}
	a.DepositAmount = depositAmount

	a.Status = mapStatusFromDB(statusVal)

	games, err := r.getAccountGames(ctx, db, a.ID)
	if err != nil {
		return nil, err
	}
	a.Games = games

	return &a, nil
}

func (r *PostgresRepository) GetAccountForUpdate(ctx context.Context, id int64) (*Account, error) {
	db := database.GetTxOrPool(ctx, r.pool)

	query := `SELECT 
		id, login, encrypted_password, status, steam_guard_enabled, 
		inventory_verified, COALESCE(last_security_check, '0001-01-01'::timestamp), hourly_price, deposit_amount, 
		COALESCE(profile_url, ''), COALESCE(avatar_url, ''), COALESCE(library_synced_at, '0001-01-01'::timestamp), created_at, updated_at, deleted_at, steam_id64
		FROM accounts WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`

	var a Account
	var login, steamID64 string
	var encPassword []byte
	var statusVal int16
	var hourlyPriceVal, depositAmountVal int64

	err := db.QueryRow(ctx, query, id).Scan(
		&a.ID, &login, &encPassword, &statusVal, &a.SteamGuardEnabled,
		&a.InventoryVerified, &a.LastSecurityCheck, &hourlyPriceVal, &depositAmountVal,
		&a.ProfileURL, &a.AvatarURL, &a.LibrarySyncedAt, &a.CreatedAt, &a.UpdatedAt, &a.DeletedAt, &steamID64,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAccountNotFound
	}
	if err != nil {
		return nil, err
	}

	creds, err := NewSteamCredentials(login, encPassword, steamID64)
	if err != nil {
		return nil, err
	}
	a.Credentials = creds

	hourlyPrice, err := NewMoney(hourlyPriceVal, "USD")
	if err != nil {
		return nil, err
	}
	a.HourlyPrice = hourlyPrice

	depositAmount, err := NewMoney(depositAmountVal, "USD")
	if err != nil {
		return nil, err
	}
	a.DepositAmount = depositAmount

	a.Status = mapStatusFromDB(statusVal)

	games, err := r.getAccountGames(ctx, db, a.ID)
	if err != nil {
		return nil, err
	}
	a.Games = games

	return &a, nil
}

func (r *PostgresRepository) getAccountGames(ctx context.Context, db database.DB, accountID int64) ([]AccountGame, error) {
	query := `SELECT 
		g.id, COALESCE(g.steam_app_id, 0), g.name, COALESCE(g.short_description, ''), COALESCE(g.header_image, ''), 
		g.release_date, COALESCE(g.developers, '[]'::jsonb), COALESCE(g.publishers, '[]'::jsonb), COALESCE(g.genres, '[]'::jsonb), g.created_at, g.updated_at, 
		ag.playtime_minutes
		FROM account_games ag
		JOIN games g ON ag.game_id = g.id
		WHERE ag.account_id = $1`

	rows, err := db.Query(ctx, query, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []AccountGame
	for rows.Next() {
		var g Game
		var devBytes, pubBytes, genBytes []byte
		var playtime int

		err := rows.Scan(
			&g.ID, &g.SteamAppID, &g.Name, &g.ShortDescription, &g.HeaderImage,
			&g.ReleaseDate, &devBytes, &pubBytes, &genBytes, &g.CreatedAt, &g.UpdatedAt,
			&playtime,
		)
		if err != nil {
			return nil, err
		}

		if len(devBytes) > 0 {
			_ = json.Unmarshal(devBytes, &g.Developers)
		}
		if len(pubBytes) > 0 {
			_ = json.Unmarshal(pubBytes, &g.Publishers)
		}
		if len(genBytes) > 0 {
			_ = json.Unmarshal(genBytes, &g.Genres)
		}

		games = append(games, AccountGame{
			Game:            g,
			PlaytimeMinutes: playtime,
		})
	}

	return games, rows.Err()
}

func (r *PostgresRepository) SyncAccountGames(ctx context.Context, accountID int64, games []AccountGame) error {
	db := database.GetTxOrPool(ctx, r.pool)

	deleteRel := `DELETE FROM account_games WHERE account_id = $1`
	_, err := db.Exec(ctx, deleteRel, accountID)
	if err != nil {
		return err
	}

	insertRel := `INSERT INTO account_games (account_id, game_id, playtime_minutes) VALUES ($1, $2, $3)`
	for _, ag := range games {
		var existID int64
		err := db.QueryRow(ctx, `SELECT id FROM games WHERE id = $1`, ag.Game.ID).Scan(&existID)
		if errors.Is(err, pgx.ErrNoRows) {
			devBytes, _ := json.Marshal(ag.Game.Developers)
			pubBytes, _ := json.Marshal(ag.Game.Publishers)
			genBytes, _ := json.Marshal(ag.Game.Genres)

			insertGame := `INSERT INTO games (
				id, name, steam_app_id, 
				short_description, header_image, release_date, 
				developers, publishers, genres, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

			_, err = db.Exec(ctx, insertGame,
				ag.Game.ID, ag.Game.Name, ag.Game.SteamAppID,
				ag.Game.ShortDescription, ag.Game.HeaderImage, ag.Game.ReleaseDate,
				devBytes, pubBytes, genBytes, ag.Game.CreatedAt, ag.Game.UpdatedAt,
			)
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		_, err = db.Exec(ctx, insertRel, accountID, ag.Game.ID, ag.PlaytimeMinutes)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *PostgresRepository) ReserveAccount(ctx context.Context, id int64, now time.Time) error {
	db := database.GetTxOrPool(ctx, r.pool)
	tag, err := db.Exec(ctx, `
		UPDATE accounts
		SET status = $2, updated_at = $3
		WHERE id = $1 AND status = $4 AND deleted_at IS NULL`,
		id, int16(StatusReserved), now, int16(StatusAvailable),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidState
	}
	return nil
}

func (r *PostgresRepository) ListAccounts(ctx context.Context, limit, offset int) ([]*Account, error) {
	return r.SearchAccounts(ctx, limit, offset, "", 0, 0, 0, "")
}

func (r *PostgresRepository) SearchAccounts(ctx context.Context, limit, offset int, search string, gameID int64, minPrice, maxPrice int64, status string) ([]*Account, error) {
	db := database.GetTxOrPool(ctx, r.pool)

	query := `SELECT 
		id, login, encrypted_password, status, steam_guard_enabled, 
		inventory_verified, COALESCE(last_security_check, '0001-01-01'::timestamp), hourly_price, deposit_amount, 
		COALESCE(profile_url, ''), COALESCE(avatar_url, ''), COALESCE(library_synced_at, '0001-01-01'::timestamp), created_at, updated_at, deleted_at, steam_id64
		FROM accounts a
		WHERE deleted_at IS NULL
		AND ($1 = '' OR a.login ILIKE '%' || $1 || '%' OR a.steam_id64 ILIKE '%' || $1 || '%' OR EXISTS (
			SELECT 1 FROM account_games ag JOIN games g ON g.id = ag.game_id
			WHERE ag.account_id = a.id AND g.name ILIKE '%' || $1 || '%'
		))
		AND ($2::BIGINT = 0 OR EXISTS (
			SELECT 1 FROM account_games ag WHERE ag.account_id = a.id AND ag.game_id = $2
		))
		AND ($3::BIGINT = 0 OR a.hourly_price >= $3)
		AND ($4::BIGINT = 0 OR a.hourly_price <= $4)
		AND ($5::SMALLINT = -1 OR a.status = $5)
		ORDER BY created_at DESC LIMIT $6 OFFSET $7`

	statusValue := int16(-1)
	switch status {
	case "Created":
		statusValue = int16(StatusCreated)
	case "Verifying":
		statusValue = int16(StatusVerifying)
	case "Available":
		statusValue = int16(StatusAvailable)
	case "Reserved":
		statusValue = int16(StatusReserved)
	case "Rented":
		statusValue = int16(StatusRented)
	case "Maintenance":
		statusValue = int16(StatusMaintenance)
	case "Disabled":
		statusValue = int16(StatusDisabled)
	}

	rows, err := db.Query(ctx, query, search, gameID, minPrice, maxPrice, statusValue, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []*Account
	for rows.Next() {
		var a Account
		var login, steamID64 string
		var encPassword []byte
		var statusVal int16
		var hourlyPriceVal, depositAmountVal int64

		err := rows.Scan(
			&a.ID, &login, &encPassword, &statusVal, &a.SteamGuardEnabled,
			&a.InventoryVerified, &a.LastSecurityCheck, &hourlyPriceVal, &depositAmountVal,
			&a.ProfileURL, &a.AvatarURL, &a.LibrarySyncedAt, &a.CreatedAt, &a.UpdatedAt, &a.DeletedAt, &steamID64,
		)
		if err != nil {
			return nil, err
		}

		creds, err := NewSteamCredentials(login, encPassword, steamID64)
		if err != nil {
			return nil, err
		}
		a.Credentials = creds

		hourlyPrice, err := NewMoney(hourlyPriceVal, "USD")
		if err != nil {
			return nil, err
		}
		a.HourlyPrice = hourlyPrice

		depositAmount, err := NewMoney(depositAmountVal, "USD")
		if err != nil {
			return nil, err
		}
		a.DepositAmount = depositAmount

		a.Status = mapStatusFromDB(statusVal)

		games, err := r.getAccountGames(ctx, db, a.ID)
		if err != nil {
			return nil, err
		}
		a.Games = games

		accounts = append(accounts, &a)
	}

	return accounts, rows.Err()
}

func mapStatusToDB(status AccountStatus) int16 {
	return int16(status)
}

func mapStatusFromDB(statusVal int16) AccountStatus {
	return AccountStatus(statusVal)
}
