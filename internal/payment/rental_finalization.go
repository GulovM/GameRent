package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var depositForfeitReasonCodes = map[string]struct{}{
	"ACCOUNT_SECURITY_VIOLATION": {},
	"ADMIN_CORRECTION":           {},
	"CREDENTIAL_MISUSE":          {},
	"DAMAGE_CONFIRMED":           {},
	"USER_CAUSED_GAME_BAN":       {},
}

func IsAllowedDepositForfeitReasonCode(reasonCode string) bool {
	_, ok := depositForfeitReasonCodes[strings.TrimSpace(reasonCode)]
	return ok
}

func IsValidDepositEvidenceReference(reference string) bool {
	_, ok := parseDepositEvidenceReference(strings.TrimSpace(reference))
	return ok
}

func parseDepositEvidenceReference(reference string) (int64, bool) {
	const prefix = "SECURITY_EVENT:"
	if !strings.HasPrefix(reference, prefix) || len(reference) > 255 {
		return 0, false
	}
	rawID := strings.TrimPrefix(reference, prefix)
	if rawID == "" || strings.TrimSpace(rawID) != rawID {
		return 0, false
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	return id, err == nil && id > 0
}

func isConsistentPositiveDepositState(state *DepositSettlementState) bool {
	return state != nil &&
		state.RentalDepositAmount > 0 &&
		state.HasDepositHold &&
		state.Amount == state.RentalDepositAmount &&
		state.HoldUserID == state.UserID &&
		state.HoldPaymentID == state.PaymentID &&
		state.HoldCurrency == state.Currency
}

func setDepositSettlementAliases(result *DepositSettlementResult, status string) {
	if result.DepositStatus == "" {
		result.DepositStatus = status
	}
	result.Status = result.DepositStatus
}

func setDepositSettlementResult(result *DepositSettlementResult, state *DepositSettlementState, status string) {
	setDepositSettlementAliases(result, status)
	result.RentalStatus = state.RentalStatus
	result.CompletedAt = state.CompletedAt
	switch status {
	case "RELEASED":
		result.SettledAt = state.ReleasedAt
	case "FORFEITED":
		result.SettledAt = state.ForfeitedAt
	case "REFUNDED":
		result.SettledAt = state.RefundedAt
	}
}

func int16Pointer(value int16) *int16        { return &value }
func timePointer(value time.Time) *time.Time { return &value }

func (s *PaymentService) completeRentalAfterSettlement(
	ctx context.Context,
	state *DepositSettlementState,
	actorUserID *int64,
	source string,
	now time.Time,
	result *DepositSettlementResult,
) error {
	if state.RentalStatus == 4 {
		result.RentalStatus = 4
		result.CompletedAt = state.CompletedAt
		return nil
	}
	if state.RentalStatus != 3 || state.PaymentStatus != 2 {
		return ErrDepositSettlementNotAllowed
	}

	changed, err := s.repo.MarkRentalCompleted(ctx, state.RentalID, now.UTC())
	if err != nil {
		return fmt.Errorf("complete rental: %w", err)
	}
	if !changed {
		return ErrDepositSettlementNotAllowed
	}

	idempotencyKey := fmt.Sprintf("rental:complete:%d", state.RentalID)
	eventMetadata, err := json.Marshal(map[string]any{
		"event":           "rental_completed",
		"idempotency_key": idempotencyKey,
		"rental_id":       strconv.FormatInt(state.RentalID, 10),
		"account_id":      strconv.FormatInt(state.AccountID, 10),
		"source":          source,
	})
	if err != nil {
		return fmt.Errorf("marshal rental completion security metadata: %w", err)
	}
	userAgent := "api"
	if actorUserID == nil {
		userAgent = "system"
	}
	if err := s.repo.LogDepositSecurityEvent(ctx, securityEventTypeRentalCompleted, state.UserID, state.AccountID, state.RentalID, userAgent, eventMetadata); err != nil {
		return fmt.Errorf("log rental completion security event: %w", err)
	}

	oldValues, _ := json.Marshal(map[string]any{"status": "EXPIRED"})
	newValues, _ := json.Marshal(map[string]any{
		"status":            "COMPLETED",
		"completed_at":      now.UTC().Format(time.RFC3339),
		"completion_key":    idempotencyKey,
		"completion_source": source,
	})
	if actorUserID == nil {
		err = s.repo.InsertSystemAuditLog(ctx, "rental", state.RentalID, "rental_complete", oldValues, newValues)
	} else {
		err = s.repo.InsertAuditLog(ctx, *actorUserID, "rental", state.RentalID, "rental_complete", oldValues, newValues)
	}
	if err != nil {
		return fmt.Errorf("insert rental completion audit log: %w", err)
	}

	state.RentalStatus = 4
	state.CompletedAt = timePointer(now.UTC())
	result.RentalStatus = 4
	result.CompletedAt = state.CompletedAt
	return nil
}

func (s *PaymentService) FinalizeExpiredRentals(ctx context.Context, now time.Time, limit int) (*RentalFinalizationResult, error) {
	if limit <= 0 || limit > 100 {
		return nil, ErrInvalidFinalizationLimit
	}
	now = now.UTC()
	result := &RentalFinalizationResult{}

	for result.Processed < limit {
		found := false
		autoReleased := false
		err := s.repo.WithinTransaction(ctx, func(txCtx context.Context) error {
			state, err := s.repo.LockNextRentalFinalizationState(txCtx, now)
			if errors.Is(err, ErrDepositHoldNotFound) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("lock next rental finalization state: %w", err)
			}
			found = true

			if state.RentalStatus != 3 || state.PaymentStatus != 2 {
				return ErrDepositSettlementNotAllowed
			}

			completionSource := "NO_DEPOSIT"
			switch {
			case state.RentalDepositAmount == 0 && !state.HasDepositHold:

			case isConsistentPositiveDepositState(state) && state.HoldStatus == depositHoldStatusHeld:
				if state.ReviewDeadlineAt == nil || now.Before(state.ReviewDeadlineAt.UTC()) {
					return ErrDepositSettlementNotAllowed
				}
				if err := s.autoReleaseDeposit(txCtx, state, now); err != nil {
					return err
				}
				autoReleased = true
				completionSource = "AUTO_RELEASE"
			case isConsistentPositiveDepositState(state) &&
				(state.HoldStatus == depositHoldStatusReleased || state.HoldStatus == depositHoldStatusForfeited || state.HoldStatus == depositHoldStatusRefunded):
				completionSource = "TERMINAL_DEPOSIT_CATCH_UP"
			default:
				return ErrDepositSettlementNotAllowed
			}

			settlementResult := &DepositSettlementResult{}
			return s.completeRentalAfterSettlement(txCtx, state, nil, completionSource, now, settlementResult)
		})
		if err != nil {
			return nil, err
		}
		if !found {
			break
		}
		result.Processed++
		result.Completed++
		if autoReleased {
			result.AutoReleased++
		}
	}

	return result, nil
}

func (s *PaymentService) autoReleaseDeposit(ctx context.Context, state *DepositSettlementState, now time.Time) error {
	if err := s.repo.MarkDepositReleased(ctx, state.HoldID, depositSettlementSourceAutoRelease, nil, now); err != nil {
		return fmt.Errorf("mark deposit auto-released: %w", err)
	}
	if err := s.repo.CreditUserBalance(ctx, state.UserID, state.Amount, now); err != nil {
		return fmt.Errorf("credit auto-released deposit: %w", err)
	}

	idempotencyKey := fmt.Sprintf("deposit:auto-release:rental:%d", state.RentalID)
	metadata, err := marshalFinancialMetadata(map[string]any{
		"event":       "deposit_auto_released_to_balance",
		"payment_id":  strconv.FormatInt(state.PaymentID, 10),
		"rental_id":   strconv.FormatInt(state.RentalID, 10),
		"account_id":  strconv.FormatInt(state.AccountID, 10),
		"source":      "SYSTEM",
		"recorded_at": now.Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal auto-release ledger metadata: %w", err)
	}
	if err := s.repo.RecordDepositReleasedToBalance(ctx, FinancialLedgerEntry{
		UserID:         state.UserID,
		RentalID:       state.RentalID,
		PaymentID:      state.PaymentID,
		AccountID:      state.AccountID,
		Amount:         state.Amount,
		Currency:       state.Currency,
		Provider:       "internal",
		IdempotencyKey: idempotencyKey,
		CorrelationID:  idempotencyKey,
		Metadata:       metadata,
	}); err != nil {
		return fmt.Errorf("insert auto-release ledger entry: %w", err)
	}

	eventMetadata, _ := json.Marshal(map[string]any{
		"event":      "deposit_auto_released",
		"payment_id": strconv.FormatInt(state.PaymentID, 10),
		"rental_id":  strconv.FormatInt(state.RentalID, 10),
		"account_id": strconv.FormatInt(state.AccountID, 10),
	})
	if err := s.repo.LogDepositSecurityEvent(ctx, securityEventTypeDepositReleased, state.UserID, state.AccountID, state.RentalID, "system", eventMetadata); err != nil {
		return fmt.Errorf("log auto-release security event: %w", err)
	}
	oldValues, _ := json.Marshal(map[string]any{"status": "HELD"})
	newValues, _ := json.Marshal(map[string]any{"status": "RELEASED", "source": "SYSTEM_AUTO_RELEASE"})
	if err := s.repo.InsertSystemAuditLog(ctx, "deposit_hold", state.HoldID, "deposit_auto_release", oldValues, newValues); err != nil {
		return fmt.Errorf("insert auto-release audit log: %w", err)
	}

	state.HoldStatus = depositHoldStatusReleased
	state.SettlementSource = int16Pointer(depositSettlementSourceAutoRelease)
	state.SettledByUserID = nil
	state.ReleasedAt = timePointer(now)
	return nil
}
