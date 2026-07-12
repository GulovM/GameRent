package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	shared_authorization "rent_game_accs/internal/shared/authorization"
	"rent_game_accs/internal/shared/database"
)

func (r *PostgresRepository) RequireCurrentAdmin(ctx context.Context, actorUserID int64) error {
	db := database.GetTxOrPool(ctx, r.pool)
	err := shared_authorization.RequireCurrentAdminForMutation(ctx, db, actorUserID)
	if errors.Is(err, shared_authorization.ErrCurrentAdminRequired) {
		return ErrAdminRequired
	}
	return err
}

func (r *PostgresRepository) LockBalanceAdjustmentKey(ctx context.Context, canonicalKey string) error {
	db := database.GetTxOrPool(ctx, r.pool)
	_, err := db.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, canonicalKey)
	return err
}

func (r *PostgresRepository) GetAdminBalanceAdjustment(ctx context.Context, canonicalKey string) (*AdminBalanceAdjustmentResult, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	var (
		result      AdminBalanceAdjustmentResult
		metadataRaw []byte
		magnitude   int64
	)
	err := db.QueryRow(ctx, `
		SELECT id, entry_type, user_id, amount, currency, metadata, created_at
		FROM financial_ledger_entries
		WHERE idempotency_key = $1`, canonicalKey,
	).Scan(
		&result.LedgerEntryID,
		&result.entryType,
		&result.UserID,
		&magnitude,
		&result.Currency,
		&metadataRaw,
		&result.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBalanceAdjustmentNotFound
	}
	if err != nil {
		return nil, err
	}

	var metadata adminBalanceAdjustmentMetadata
	if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
		return nil, fmt.Errorf("decode stored balance adjustment metadata: %w", err)
	}
	result.AdjustmentID = result.LedgerEntryID
	result.ActorUserID = metadata.ActorUserID
	result.PreviousBalance = metadata.PreviousBalance
	result.NewBalance = metadata.NewBalance
	result.ReasonCode = metadata.ReasonCode
	result.Comment = metadata.Comment
	result.IdempotencyKey = metadata.ClientIdempotencyKey
	result.CanonicalIdempotencyKey = canonicalKey
	switch result.entryType {
	case ledgerEntryAdminBalanceCredit:
		result.Amount = magnitude
	case ledgerEntryAdminBalanceDebit:
		result.Amount = -magnitude
	default:
		result.Amount = magnitude
	}
	return &result, nil
}

func (r *PostgresRepository) LockAdminAndUserBalanceForAdjustment(ctx context.Context, actorUserID, targetUserID int64) (*UserBalance, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	result := &UserBalance{Currency: "USD"}
	rows, err := db.Query(ctx, `
		SELECT id, role, is_blocked, balance
		FROM users
		WHERE (id = $1 OR id = $2) AND deleted_at IS NULL
		ORDER BY id
		FOR UPDATE`, actorUserID, targetUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	actorAllowed := false
	targetFound := false
	for rows.Next() {
		var (
			userID    int64
			role      string
			isBlocked bool
			balance   int64
		)
		if err := rows.Scan(&userID, &role, &isBlocked, &balance); err != nil {
			return nil, err
		}
		if userID == actorUserID {
			actorAllowed = role == "ADMIN" && !isBlocked
		}
		if userID == targetUserID {
			targetFound = true
			result.UserID = userID
			result.AvailableBalance = balance
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !actorAllowed {
		return nil, ErrAdminRequired
	}
	if !targetFound {
		return nil, ErrFinancialUserNotFound
	}
	return result, nil
}

func (r *PostgresRepository) InsertAdminBalanceAdjustmentLedger(ctx context.Context, record AdminBalanceAdjustmentRecord) (int64, time.Time, error) {
	db := database.GetTxOrPool(ctx, r.pool)
	entryType := ledgerEntryAdminBalanceCredit
	if record.Amount < 0 {
		entryType = ledgerEntryAdminBalanceDebit
	}
	var ledgerEntryID int64
	var createdAt time.Time
	err := db.QueryRow(ctx, `
		INSERT INTO financial_ledger_entries (
			entry_type, user_id, amount, currency, provider,
			idempotency_key, correlation_id, metadata, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $6, $7::jsonb, NOW())
		RETURNING id, created_at`,
		entryType,
		record.TargetUserID,
		record.Magnitude,
		record.Currency,
		adminBalanceAdjustmentProvider,
		record.CanonicalIdempotencyKey,
		record.Metadata,
	).Scan(&ledgerEntryID, &createdAt)
	if isBalanceAdjustmentUniqueViolation(err) {
		return 0, time.Time{}, ErrBalanceAdjustmentIdempotencyConflict
	}
	return ledgerEntryID, createdAt, err
}

func (r *PostgresRepository) SetUserBalance(ctx context.Context, userID, balance int64, now time.Time) error {
	db := database.GetTxOrPool(ctx, r.pool)
	tag, err := db.Exec(ctx, `
		UPDATE users
		SET balance = $2, updated_at = $3
		WHERE id = $1`, userID, balance, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFinancialUserNotFound
	}
	return nil
}

func (r *PostgresRepository) LogAdminBalanceAdjustmentSecurityEvent(ctx context.Context, targetUserID int64, clientIP, userAgent string, metadata []byte) error {
	db := database.GetTxOrPool(ctx, r.pool)
	var ipParam any
	if parsed := net.ParseIP(clientIP); parsed != nil {
		ipParam = parsed.String()
	}
	_, err := db.Exec(ctx, `
		INSERT INTO security_events (
			user_id, event_type, ip_address, user_agent, success, metadata, created_at
		) VALUES ($1, $2, $3, $4, true, $5::jsonb, NOW())`,
		targetUserID,
		securityEventTypeBalanceAdjustment,
		ipParam,
		userAgent,
		metadata,
	)
	return err
}

func isBalanceAdjustmentUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
