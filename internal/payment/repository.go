package payment

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"rent_game_accs/internal/shared/database"
)

type Repository interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
	UpdatePaymentSuccess(ctx context.Context, paymentID int64, extTxID string) (rentalID, userID int64, amount int64, currency string, err error)
	ActivateRental(ctx context.Context, rentalID int64) (accountID int64, err error)
	MarkAccountRented(ctx context.Context, accountID int64) (login string, encPassword []byte, steamID64 string, err error)
	LogSecurityEvent(ctx context.Context, userID, accountID, rentalID int64, clientIP, userAgent string, metadata []byte) error
}

type PostgresRepository struct {
	pool      *pgxpool.Pool
	txManager database.TxManager
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{
		pool:      pool,
		txManager: database.NewTxManager(pool),
	}
}

func (r *PostgresRepository) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return r.txManager.WithinTransaction(ctx, fn)
}

func (r *PostgresRepository) UpdatePaymentSuccess(ctx context.Context, paymentID int64, extTxID string) (int64, int64, int64, string, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	var rentalID, userID int64
	var amount int64
	var currency string
	query := `
		UPDATE payments 
		SET status = 2, external_transaction_id = $1, processed_at = NOW() 
		WHERE (id = $2 OR rental_id = $2) AND status < 2
		RETURNING rental_id, user_id, amount, currency`
	err := db.QueryRow(ctx, query, extTxID, paymentID).Scan(&rentalID, &userID, &amount, &currency)
	return rentalID, userID, amount, currency, err
}

func (r *PostgresRepository) ActivateRental(ctx context.Context, rentalID int64) (int64, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	var accountID int64
	query := `
		UPDATE rentals 
		SET status = 2 
		WHERE id = $1 
		RETURNING account_id`
	err := db.QueryRow(ctx, query, rentalID).Scan(&accountID)
	return accountID, err
}

func (r *PostgresRepository) MarkAccountRented(ctx context.Context, accountID int64) (string, []byte, string, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	var login string
	var encPassword []byte
	var steamID64 string
	query := `
		UPDATE accounts 
		SET status = 4, updated_at = NOW() 
		WHERE id = $1 
		RETURNING login, encrypted_password, steam_id64`
	err := db.QueryRow(ctx, query, accountID).Scan(&login, &encPassword, &steamID64)
	return login, encPassword, steamID64, err
}

func (r *PostgresRepository) LogSecurityEvent(ctx context.Context, userID, accountID, rentalID int64, clientIP, userAgent string, metadata []byte) error {
	db := database.GetTxOrPool(ctx, r.pool)
	query := `
		INSERT INTO security_events (
			user_id, account_id, rental_id, event_type, ip_address, user_agent, success, metadata, created_at
		) VALUES ($1, $2, $3, 2, $4, $5, true, $6, NOW())`

	var ipParam any = clientIP
	if clientIP == "" || clientIP == "::1" || clientIP == "127.0.0.1" {
		ipParam = "127.0.0.1"
	}

	_, err := db.Exec(ctx, query, userID, accountID, rentalID, ipParam, userAgent, metadata)
	return err
}
