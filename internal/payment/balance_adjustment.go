package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

const adminBalanceAdjustmentProvider = "admin"
const adminBalanceAdjustmentKeyPrefix = "admin:balance-adjustment:"

var (
	ErrBalanceAdjustmentNotFound            = errors.New("balance adjustment not found")
	ErrBalanceAdjustmentAmountRequired      = errors.New("balance adjustment amount must be non-zero")
	ErrBalanceAdjustmentWouldBeNegative     = errors.New("balance adjustment would make balance negative")
	ErrBalanceAdjustmentOverflow            = errors.New("balance adjustment would overflow")
	ErrBalanceAdjustmentCurrencyUnsupported = errors.New("balance adjustment currency must be USD")
	ErrBalanceAdjustmentReasonRequired      = errors.New("balance adjustment reason_code is invalid")
	ErrBalanceAdjustmentCommentTooLong      = errors.New("balance adjustment comment must be at most 500 characters")
	ErrBalanceAdjustmentIdempotencyRequired = errors.New("balance adjustment idempotency_key is invalid")
	ErrBalanceAdjustmentIdempotencyConflict = errors.New("balance adjustment idempotency key was already used for a different request")
)

type AdminBalanceAdjustmentInput struct {
	Amount         int64
	Currency       string
	ReasonCode     string
	Comment        string
	IdempotencyKey string
}

type AdminBalanceAdjustmentRecord struct {
	ActorUserID             int64
	TargetUserID            int64
	Amount                  int64
	Magnitude               int64
	Currency                string
	ReasonCode              string
	Comment                 string
	ClientIdempotencyKey    string
	CanonicalIdempotencyKey string
	PreviousBalance         int64
	NewBalance              int64
	Metadata                string
}

type AdminBalanceAdjustmentResult struct {
	AdjustmentID            int64     `json:"adjustment_id"`
	LedgerEntryID           int64     `json:"ledger_entry_id"`
	ActorUserID             int64     `json:"-"`
	UserID                  int64     `json:"user_id"`
	PreviousBalance         int64     `json:"previous_balance"`
	NewBalance              int64     `json:"new_balance"`
	Amount                  int64     `json:"amount"`
	Currency                string    `json:"currency"`
	ReasonCode              string    `json:"-"`
	Comment                 string    `json:"-"`
	IdempotencyKey          string    `json:"idempotency_key"`
	CanonicalIdempotencyKey string    `json:"-"`
	IdempotentReplay        bool      `json:"idempotent_replay"`
	CreatedAt               time.Time `json:"created_at"`
	entryType               int16
}

type adminBalanceAdjustmentMetadata struct {
	Event                string `json:"event"`
	ActorUserID          int64  `json:"actor_user_id"`
	TargetUserID         int64  `json:"target_user_id"`
	SignedAmount         int64  `json:"signed_amount"`
	PreviousBalance      int64  `json:"previous_balance"`
	NewBalance           int64  `json:"new_balance"`
	ReasonCode           string `json:"reason_code"`
	Comment              string `json:"comment,omitempty"`
	ClientIdempotencyKey string `json:"client_idempotency_key"`
	RecordedAt           string `json:"recorded_at"`
}

func (s *PaymentService) AdjustAdminBalance(
	ctx context.Context,
	actorUserID int64,
	actorRole string,
	targetUserID int64,
	input AdminBalanceAdjustmentInput,
	clientIP string,
	userAgent string,
	now time.Time,
) (*AdminBalanceAdjustmentResult, error) {
	if actorRole != "ADMIN" {
		return nil, ErrAdminRequired
	}

	record, err := normalizeAdminBalanceAdjustment(actorUserID, targetUserID, input)
	if err != nil {
		return nil, err
	}
	now = now.UTC()
	var result *AdminBalanceAdjustmentResult

	err = s.repo.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.RequireCurrentAdmin(txCtx, actorUserID); err != nil {
			if errors.Is(err, ErrAdminRequired) {
				return err
			}
			return fmt.Errorf("authorize current admin for balance adjustment: %w", err)
		}
		if err := s.repo.LockBalanceAdjustmentKey(txCtx, record.CanonicalIdempotencyKey); err != nil {
			return fmt.Errorf("lock balance adjustment idempotency key: %w", err)
		}
		balance, err := s.repo.LockAdminAndUserBalanceForAdjustment(txCtx, actorUserID, targetUserID)
		if err != nil {
			return fmt.Errorf("lock admin actor and target user balance: %w", err)
		}

		existing, err := s.repo.GetAdminBalanceAdjustment(txCtx, record.CanonicalIdempotencyKey)
		if err == nil {
			if !adminBalanceAdjustmentMatches(existing, record) {
				return ErrBalanceAdjustmentIdempotencyConflict
			}
			existing.IdempotentReplay = true
			result = existing
			return nil
		}
		if !errors.Is(err, ErrBalanceAdjustmentNotFound) {
			return fmt.Errorf("load balance adjustment replay: %w", err)
		}

		record.PreviousBalance = balance.AvailableBalance
		record.NewBalance, err = checkedBalanceAdjustment(record.PreviousBalance, record.Amount)
		if err != nil {
			return err
		}

		metadata := adminBalanceAdjustmentMetadata{
			Event:                "admin_balance_adjustment",
			ActorUserID:          actorUserID,
			TargetUserID:         targetUserID,
			SignedAmount:         record.Amount,
			PreviousBalance:      record.PreviousBalance,
			NewBalance:           record.NewBalance,
			ReasonCode:           record.ReasonCode,
			Comment:              record.Comment,
			ClientIdempotencyKey: record.ClientIdempotencyKey,
			RecordedAt:           now.Format(time.RFC3339Nano),
		}
		metadataBytes, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("marshal balance adjustment metadata: %w", err)
		}
		record.Metadata = string(metadataBytes)

		ledgerEntryID, createdAt, err := s.repo.InsertAdminBalanceAdjustmentLedger(txCtx, record)
		if err != nil {
			return fmt.Errorf("insert balance adjustment ledger entry: %w", err)
		}
		if err := s.repo.SetUserBalance(txCtx, targetUserID, record.NewBalance, now); err != nil {
			return fmt.Errorf("set adjusted user balance: %w", err)
		}

		oldValues, err := json.Marshal(map[string]any{
			"balance":  record.PreviousBalance,
			"currency": record.Currency,
		})
		if err != nil {
			return fmt.Errorf("marshal balance adjustment audit old values: %w", err)
		}
		newValues, err := json.Marshal(map[string]any{
			"balance":         record.NewBalance,
			"currency":        record.Currency,
			"amount":          record.Amount,
			"reason_code":     record.ReasonCode,
			"comment":         record.Comment,
			"idempotency_key": record.ClientIdempotencyKey,
			"ledger_entry_id": ledgerEntryID,
		})
		if err != nil {
			return fmt.Errorf("marshal balance adjustment audit new values: %w", err)
		}
		if err := s.repo.InsertAuditLog(txCtx, actorUserID, "user_balance", targetUserID, "admin_balance_adjustment", oldValues, newValues); err != nil {
			return fmt.Errorf("insert balance adjustment audit log: %w", err)
		}
		if err := s.repo.LogAdminBalanceAdjustmentSecurityEvent(txCtx, targetUserID, clientIP, sanitizeBalanceAdjustmentUserAgent(userAgent), metadataBytes); err != nil {
			return fmt.Errorf("insert balance adjustment security event: %w", err)
		}

		result = &AdminBalanceAdjustmentResult{
			AdjustmentID:            ledgerEntryID,
			LedgerEntryID:           ledgerEntryID,
			ActorUserID:             actorUserID,
			UserID:                  targetUserID,
			PreviousBalance:         record.PreviousBalance,
			NewBalance:              record.NewBalance,
			Amount:                  record.Amount,
			Currency:                record.Currency,
			ReasonCode:              record.ReasonCode,
			Comment:                 record.Comment,
			IdempotencyKey:          record.ClientIdempotencyKey,
			CanonicalIdempotencyKey: record.CanonicalIdempotencyKey,
			CreatedAt:               createdAt,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func normalizeAdminBalanceAdjustment(actorUserID, targetUserID int64, input AdminBalanceAdjustmentInput) (AdminBalanceAdjustmentRecord, error) {
	if actorUserID <= 0 || targetUserID <= 0 {
		return AdminBalanceAdjustmentRecord{}, ErrFinancialUserNotFound
	}
	if input.Amount == 0 {
		return AdminBalanceAdjustmentRecord{}, ErrBalanceAdjustmentAmountRequired
	}
	if input.Amount == math.MinInt64 {
		return AdminBalanceAdjustmentRecord{}, ErrBalanceAdjustmentOverflow
	}
	currency := strings.ToUpper(strings.TrimSpace(input.Currency))
	if currency != "USD" {
		return AdminBalanceAdjustmentRecord{}, ErrBalanceAdjustmentCurrencyUnsupported
	}
	reasonCode := strings.TrimSpace(input.ReasonCode)
	if !isSafeReasonCode(reasonCode) {
		return AdminBalanceAdjustmentRecord{}, ErrBalanceAdjustmentReasonRequired
	}
	comment := strings.TrimSpace(input.Comment)
	if len([]rune(comment)) > 500 {
		return AdminBalanceAdjustmentRecord{}, ErrBalanceAdjustmentCommentTooLong
	}
	clientKey := strings.TrimSpace(input.IdempotencyKey)
	if !isSafeBalanceAdjustmentIdempotencyKey(clientKey) {
		return AdminBalanceAdjustmentRecord{}, ErrBalanceAdjustmentIdempotencyRequired
	}
	magnitude := input.Amount
	if magnitude < 0 {
		magnitude = -magnitude
	}
	return AdminBalanceAdjustmentRecord{
		ActorUserID:             actorUserID,
		TargetUserID:            targetUserID,
		Amount:                  input.Amount,
		Magnitude:               magnitude,
		Currency:                currency,
		ReasonCode:              reasonCode,
		Comment:                 comment,
		ClientIdempotencyKey:    clientKey,
		CanonicalIdempotencyKey: adminBalanceAdjustmentKeyPrefix + clientKey,
	}, nil
}

func checkedBalanceAdjustment(previous, amount int64) (int64, error) {
	if amount > 0 && previous > math.MaxInt64-amount {
		return 0, ErrBalanceAdjustmentOverflow
	}
	if amount < 0 && previous < -amount {
		return 0, ErrBalanceAdjustmentWouldBeNegative
	}
	return previous + amount, nil
}

func isSafeBalanceAdjustmentIdempotencyKey(key string) bool {
	if len(key) < 8 || len(key) > 128 {
		return false
	}
	for _, ch := range key {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' {
			continue
		}
		if ch == '-' || ch == '_' || ch == '.' || ch == ':' {
			continue
		}
		return false
	}
	return true
}

func sanitizeBalanceAdjustmentUserAgent(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > 512 {
		runes = runes[:512]
	}
	return string(runes)
}

func adminBalanceAdjustmentMatches(existing *AdminBalanceAdjustmentResult, requested AdminBalanceAdjustmentRecord) bool {
	if existing == nil {
		return false
	}
	expectedType := ledgerEntryAdminBalanceCredit
	if requested.Amount < 0 {
		expectedType = ledgerEntryAdminBalanceDebit
	}
	return existing.entryType == expectedType &&
		existing.ActorUserID == requested.ActorUserID &&
		existing.UserID == requested.TargetUserID &&
		existing.Amount == requested.Amount &&
		existing.Currency == requested.Currency &&
		existing.ReasonCode == requested.ReasonCode &&
		existing.Comment == requested.Comment &&
		existing.IdempotencyKey == requested.ClientIdempotencyKey
}
