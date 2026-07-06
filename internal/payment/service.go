package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	ErrPaymentNotFound             = errors.New("payment not found")
	ErrWebhookMissingIdentifier    = errors.New("missing payment_id or external_transaction_id in request")
	ErrWebhookMissingExternalTxID  = errors.New("external_transaction_id is required for successful payment webhook")
	ErrWebhookNotSuccessful        = errors.New("webhook status is not successful")
	ErrWebhookInvalidTransition    = errors.New("payment, rental or account is not in an activatable state")
	ErrWebhookExternalTxMismatch   = errors.New("external_transaction_id does not match the stored payment")
	ErrPaymentAlreadyProcessed     = errors.New("payment already processed")
	ErrRentalNotEligible           = errors.New("rental is not eligible for activation")
	ErrAccountNotReserved          = errors.New("account is not reserved")
	ErrFinancialUserNotFound       = errors.New("financial user not found")
	ErrWalletPaymentNotFound       = errors.New("wallet payment rental not found")
	ErrWalletPaymentNotAllowed     = errors.New("wallet payment is not allowed")
	ErrWalletPaymentExpired        = errors.New("wallet payment has expired")
	ErrWalletInsufficientBalance   = errors.New("insufficient wallet balance")
	ErrDepositHoldNotFound         = errors.New("deposit hold not found")
	ErrDepositSettlementNotAllowed = errors.New("deposit settlement is not allowed")
	ErrDepositAlreadySettled       = errors.New("deposit is already settled")
	ErrAdminRequired               = errors.New("admin role is required")
	ErrInvalidReasonCode           = errors.New("invalid reason_code")
	ErrInvalidLedgerPagination     = errors.New("invalid ledger pagination")
)

type Service interface {
	ProcessWebhook(ctx context.Context, req WebhookRequest, clientIP, userAgent string) (*WebhookResult, error)
	VerifySignature(payload []byte, signature string) bool
	GetUserBalance(ctx context.Context, userID int64) (*UserBalance, error)
	ListUserLedger(ctx context.Context, userID int64, page, pageSize int) (*UserLedgerPage, error)
	PayRentalWithBalance(ctx context.Context, userID, rentalID int64, clientIP, userAgent string, now time.Time) (*WalletPaymentResult, error)
	ReleaseDeposit(ctx context.Context, actorUserID int64, actorRole string, rentalID int64, now time.Time) (*DepositSettlementResult, error)
	ForfeitDeposit(ctx context.Context, actorUserID int64, actorRole string, rentalID int64, reasonCode string, now time.Time) (*DepositSettlementResult, error)
}

type WebhookResult struct {
	PaymentID  int64
	RentalID   int64
	AccountID  int64
	Processed  bool
	Idempotent bool
}

type DepositSettlementResult struct {
	Changed bool
	Status  string
}

type UserLedgerPage struct {
	Entries    []PublicLedgerEntry
	Page       int
	PageSize   int
	TotalItems int64
}

type WalletPaymentResult struct {
	Changed         bool
	Idempotent      bool
	PaymentID       int64
	RentalID        int64
	AccountID       int64
	PaymentStatus   int16
	RentalStatus    int16
	AccountStatus   int16
	PaymentProvider string
}

type PaymentService struct {
	repo          Repository
	webhookSecret string
}

func NewPaymentService(repo Repository) *PaymentService {
	webhookSecret := os.Getenv("PAYMENT_WEBHOOK_SECRET")
	if webhookSecret == "" {
		webhookSecret = "payment-webhook-secret-placeholder"
	}

	return &PaymentService{
		repo:          repo,
		webhookSecret: webhookSecret,
	}
}

func (s *PaymentService) VerifySignature(payload []byte, signature string) bool {
	if s.webhookSecret == "" || signature == "" {
		return true
	}
	mac := hmac.New(sha256.New, []byte(s.webhookSecret))
	mac.Write(payload)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

func (s *PaymentService) ProcessWebhook(ctx context.Context, req WebhookRequest, clientIP, userAgent string) (*WebhookResult, error) {
	if !strings.EqualFold(strings.TrimSpace(req.Status), "success") {
		return nil, ErrWebhookNotSuccessful
	}
	if strings.TrimSpace(req.PaymentID) == "" && strings.TrimSpace(req.ExternalTransactionID) == "" {
		return nil, ErrWebhookMissingIdentifier
	}

	now := time.Now()
	result := &WebhookResult{}

	err := s.repo.WithinTransaction(ctx, func(txCtx context.Context) error {
		state, err := s.loadWebhookState(txCtx, req)
		if err != nil {
			return err
		}

		result.PaymentID = state.PaymentID
		result.RentalID = state.RentalID
		result.AccountID = state.AccountID

		if state.Status == 2 {
			if state.Provider != walletPaymentProvider && req.ExternalTransactionID != "" && state.ExternalTransactionID != req.ExternalTransactionID {
				return ErrWebhookExternalTxMismatch
			}
			if state.RentalStatus != 2 || state.AccountStatus != 4 {
				return fmt.Errorf("%w: payment=%d rental=%d account=%d", ErrWebhookInvalidTransition, state.Status, state.RentalStatus, state.AccountStatus)
			}
			result.Processed = true
			result.Idempotent = true
			return nil
		}

		if state.Status != 1 {
			return ErrWebhookNotSuccessful
		}
		if req.ExternalTransactionID == "" {
			return ErrWebhookMissingExternalTxID
		}
		if state.RentalStatus != 1 || state.AccountStatus != 3 {
			return fmt.Errorf("%w: payment=%d rental=%d account=%d", ErrWebhookInvalidTransition, state.Status, state.RentalStatus, state.AccountStatus)
		}

		if err := s.repo.MarkPaymentSuccessful(txCtx, state.PaymentID, req.ExternalTransactionID); err != nil {
			if errors.Is(err, ErrPaymentAlreadyProcessed) {
				updated, lockErr := s.loadWebhookStateByExternalTransaction(txCtx, state.Provider, req.ExternalTransactionID)
				if lockErr != nil {
					return lockErr
				}
				if updated.RentalStatus != 2 || updated.AccountStatus != 4 {
					return ErrWebhookInvalidTransition
				}
				result.PaymentID = updated.PaymentID
				result.RentalID = updated.RentalID
				result.AccountID = updated.AccountID
				result.Processed = true
				result.Idempotent = true
				return nil
			}
			return fmt.Errorf("mark payment successful: %w", err)
		}

		if err := s.repo.ActivateRentalForWebhook(txCtx, state.RentalID, now); err != nil {
			if errors.Is(err, ErrRentalNotEligible) {
				return fmt.Errorf("activate rental: %w", err)
			}
			return fmt.Errorf("activate rental: %w", err)
		}
		if err := s.repo.MarkAccountRentedForWebhook(txCtx, state.AccountID, now); err != nil {
			if errors.Is(err, ErrAccountNotReserved) {
				return fmt.Errorf("mark account rented: %w", err)
			}
			return fmt.Errorf("mark account rented: %w", err)
		}

		if err := s.recordFinancialFacts(txCtx, state, req.ExternalTransactionID, now); err != nil {
			return fmt.Errorf("record financial facts: %w", err)
		}

		metadata := map[string]any{
			"payment_id":     strconv.FormatInt(state.PaymentID, 10),
			"rental_id":      strconv.FormatInt(state.RentalID, 10),
			"account_id":     strconv.FormatInt(state.AccountID, 10),
			"amount":         state.Amount,
			"currency":       state.Currency,
			"activated_at":   now.Format(time.RFC3339),
			"external_tx_id": req.ExternalTransactionID,
		}
		metadataBytes, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal webhook metadata: %w", err)
		}

		if err := s.repo.LogSecurityEvent(txCtx, state.UserID, state.AccountID, state.RentalID, clientIP, userAgent, metadataBytes); err != nil {
			return fmt.Errorf("failed to log security event: %w", err)
		}

		result.Processed = true
		return nil
	})

	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *PaymentService) recordFinancialFacts(ctx context.Context, state *WebhookPaymentState, externalTransactionID string, now time.Time) error {
	providerPaymentKey := fmt.Sprintf("payment:webhook:%s:%s", state.Provider, externalTransactionID)
	providerMetadata, err := marshalFinancialMetadata(map[string]any{
		"event":        "provider_payment_received",
		"payment_id":   strconv.FormatInt(state.PaymentID, 10),
		"rental_id":    strconv.FormatInt(state.RentalID, 10),
		"account_id":   strconv.FormatInt(state.AccountID, 10),
		"rental_price": state.RentalPrice,
		"deposit":      state.DepositAmount,
		"recorded_at":  now.Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal provider payment ledger metadata: %w", err)
	}

	totalAmount := state.RentalPrice + state.DepositAmount
	if err := s.repo.RecordProviderPaymentReceived(ctx, FinancialLedgerEntry{
		UserID:                state.UserID,
		RentalID:              state.RentalID,
		PaymentID:             state.PaymentID,
		AccountID:             state.AccountID,
		Amount:                totalAmount,
		Currency:              state.Currency,
		Provider:              state.Provider,
		ExternalTransactionID: externalTransactionID,
		IdempotencyKey:        providerPaymentKey,
		CorrelationID:         providerPaymentKey,
		Metadata:              providerMetadata,
	}); err != nil {
		return fmt.Errorf("insert provider payment ledger entry: %w", err)
	}

	if state.DepositAmount <= 0 {
		return nil
	}

	depositKey := fmt.Sprintf("deposit:hold:rental:%d", state.RentalID)
	if err := s.repo.CreateDepositHold(ctx, DepositHold{
		UserID:         state.UserID,
		RentalID:       state.RentalID,
		PaymentID:      state.PaymentID,
		Amount:         state.DepositAmount,
		Currency:       state.Currency,
		HeldAt:         now,
		IdempotencyKey: depositKey,
	}); err != nil {
		return fmt.Errorf("create deposit hold: %w", err)
	}

	depositMetadata, err := marshalFinancialMetadata(map[string]any{
		"event":       "deposit_held",
		"payment_id":  strconv.FormatInt(state.PaymentID, 10),
		"rental_id":   strconv.FormatInt(state.RentalID, 10),
		"account_id":  strconv.FormatInt(state.AccountID, 10),
		"recorded_at": now.Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal deposit hold ledger metadata: %w", err)
	}

	if err := s.repo.RecordDepositHeld(ctx, FinancialLedgerEntry{
		UserID:                state.UserID,
		RentalID:              state.RentalID,
		PaymentID:             state.PaymentID,
		AccountID:             state.AccountID,
		Amount:                state.DepositAmount,
		Currency:              state.Currency,
		Provider:              state.Provider,
		ExternalTransactionID: externalTransactionID,
		IdempotencyKey:        depositKey,
		CorrelationID:         providerPaymentKey,
		Metadata:              depositMetadata,
	}); err != nil {
		return fmt.Errorf("insert deposit hold ledger entry: %w", err)
	}

	return nil
}

func marshalFinancialMetadata(metadata map[string]any) (string, error) {
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	return string(metadataBytes), nil
}

func (s *PaymentService) GetUserBalance(ctx context.Context, userID int64) (*UserBalance, error) {
	balance, err := s.repo.GetUserBalance(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrFinancialUserNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("get user balance: %w", err)
	}
	return balance, nil
}

func (s *PaymentService) ListUserLedger(ctx context.Context, userID int64, page, pageSize int) (*UserLedgerPage, error) {
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return nil, ErrInvalidLedgerPagination
	}

	offset := (page - 1) * pageSize
	entries, err := s.repo.ListUserLedgerEntries(ctx, userID, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("list user ledger entries: %w", err)
	}
	total, err := s.repo.CountUserLedgerEntries(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("count user ledger entries: %w", err)
	}

	return &UserLedgerPage{
		Entries:    entries,
		Page:       page,
		PageSize:   pageSize,
		TotalItems: total,
	}, nil
}

func (s *PaymentService) PayRentalWithBalance(ctx context.Context, userID, rentalID int64, clientIP, userAgent string, now time.Time) (*WalletPaymentResult, error) {
	result := &WalletPaymentResult{}

	err := s.repo.WithinTransaction(ctx, func(txCtx context.Context) error {
		state, err := s.repo.LockWalletPaymentState(txCtx, rentalID, userID)
		if err != nil {
			if errors.Is(err, ErrWalletPaymentNotFound) {
				return err
			}
			return fmt.Errorf("lock wallet payment state: %w", err)
		}

		result.PaymentID = state.PaymentID
		result.RentalID = state.RentalID
		result.AccountID = state.AccountID
		result.PaymentProvider = state.Provider
		result.PaymentStatus = state.PaymentStatus
		result.RentalStatus = state.RentalStatus
		result.AccountStatus = state.AccountStatus

		if state.PaymentStatus == 2 {
			if state.RentalStatus != 2 || state.AccountStatus != 4 {
				return fmt.Errorf("%w: payment=%d rental=%d account=%d", ErrWalletPaymentNotAllowed, state.PaymentStatus, state.RentalStatus, state.AccountStatus)
			}
			result.Idempotent = true
			return nil
		}

		if state.PaymentStatus != 1 || state.RentalStatus != 1 || state.AccountStatus != 3 {
			return fmt.Errorf("%w: payment=%d rental=%d account=%d", ErrWalletPaymentNotAllowed, state.PaymentStatus, state.RentalStatus, state.AccountStatus)
		}
		if !state.PaymentExpiresAt.After(now.UTC()) {
			return ErrWalletPaymentExpired
		}

		totalAmount := state.RentalPrice + state.DepositAmount
		if state.UserBalance < totalAmount {
			return ErrWalletInsufficientBalance
		}

		if err := s.repo.DebitUserBalance(txCtx, state.UserID, totalAmount, now.UTC()); err != nil {
			if errors.Is(err, ErrWalletInsufficientBalance) {
				return err
			}
			return fmt.Errorf("debit user balance: %w", err)
		}
		if err := s.repo.MarkPaymentSuccessfulForWallet(txCtx, state.PaymentID, now.UTC()); err != nil {
			return fmt.Errorf("mark payment successful for wallet: %w", err)
		}
		if err := s.repo.ActivateRentalForWebhook(txCtx, state.RentalID, now.UTC()); err != nil {
			return fmt.Errorf("activate rental for wallet payment: %w", err)
		}
		if err := s.repo.MarkAccountRentedForWebhook(txCtx, state.AccountID, now.UTC()); err != nil {
			return fmt.Errorf("mark account rented for wallet payment: %w", err)
		}

		correlationID := fmt.Sprintf("balance:debit:rental:%d", state.RentalID)
		balanceDebitMetadata, err := marshalFinancialMetadata(map[string]any{
			"event":       "balance_debit",
			"payment_id":  strconv.FormatInt(state.PaymentID, 10),
			"rental_id":   strconv.FormatInt(state.RentalID, 10),
			"account_id":  strconv.FormatInt(state.AccountID, 10),
			"recorded_at": now.UTC().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("marshal balance debit metadata: %w", err)
		}
		if err := s.repo.RecordBalanceDebit(txCtx, FinancialLedgerEntry{
			UserID:         state.UserID,
			RentalID:       state.RentalID,
			PaymentID:      state.PaymentID,
			AccountID:      state.AccountID,
			Amount:         totalAmount,
			Currency:       state.Currency,
			Provider:       walletPaymentProvider,
			IdempotencyKey: correlationID,
			CorrelationID:  correlationID,
			Metadata:       balanceDebitMetadata,
		}); err != nil {
			return fmt.Errorf("insert balance debit ledger entry: %w", err)
		}

		if state.DepositAmount > 0 {
			if err := s.repo.CreateDepositHold(txCtx, DepositHold{
				UserID:         state.UserID,
				RentalID:       state.RentalID,
				PaymentID:      state.PaymentID,
				Amount:         state.DepositAmount,
				Currency:       state.Currency,
				HeldAt:         now.UTC(),
				IdempotencyKey: fmt.Sprintf("deposit:hold:rental:%d", state.RentalID),
			}); err != nil {
				return fmt.Errorf("create deposit hold for wallet payment: %w", err)
			}

			depositMetadata, err := marshalFinancialMetadata(map[string]any{
				"event":       "deposit_held",
				"payment_id":  strconv.FormatInt(state.PaymentID, 10),
				"rental_id":   strconv.FormatInt(state.RentalID, 10),
				"account_id":  strconv.FormatInt(state.AccountID, 10),
				"recorded_at": now.UTC().Format(time.RFC3339),
			})
			if err != nil {
				return fmt.Errorf("marshal wallet deposit metadata: %w", err)
			}
			if err := s.repo.RecordDepositHeld(txCtx, FinancialLedgerEntry{
				UserID:         state.UserID,
				RentalID:       state.RentalID,
				PaymentID:      state.PaymentID,
				AccountID:      state.AccountID,
				Amount:         state.DepositAmount,
				Currency:       state.Currency,
				Provider:       walletPaymentProvider,
				IdempotencyKey: fmt.Sprintf("deposit:hold:rental:%d", state.RentalID),
				CorrelationID:  correlationID,
				Metadata:       depositMetadata,
			}); err != nil {
				return fmt.Errorf("insert wallet deposit held ledger entry: %w", err)
			}
		}

		eventMetadata, err := json.Marshal(map[string]any{
			"event":      "wallet_payment_completed",
			"payment_id": strconv.FormatInt(state.PaymentID, 10),
			"rental_id":  strconv.FormatInt(state.RentalID, 10),
			"account_id": strconv.FormatInt(state.AccountID, 10),
		})
		if err != nil {
			return fmt.Errorf("marshal wallet security metadata: %w", err)
		}
		if err := s.repo.LogWalletSecurityEvent(txCtx, state.UserID, state.AccountID, state.RentalID, clientIP, userAgent, eventMetadata); err != nil {
			return fmt.Errorf("log wallet security event: %w", err)
		}

		oldValues, err := json.Marshal(map[string]any{
			"payment_status":   "PENDING",
			"rental_status":    "WAITING_PAYMENT",
			"account_status":   "RESERVED",
			"payment_provider": state.Provider,
		})
		if err != nil {
			return fmt.Errorf("marshal wallet audit old values: %w", err)
		}
		newValues, err := json.Marshal(map[string]any{
			"payment_status":   "SUCCESS",
			"rental_status":    "ACTIVE",
			"account_status":   "RENTED",
			"payment_provider": walletPaymentProvider,
		})
		if err != nil {
			return fmt.Errorf("marshal wallet audit new values: %w", err)
		}
		if err := s.repo.InsertAuditLog(txCtx, state.UserID, "payment", state.PaymentID, "wallet_payment_success", oldValues, newValues); err != nil {
			return fmt.Errorf("insert wallet audit log: %w", err)
		}

		result.Changed = true
		result.PaymentStatus = 2
		result.RentalStatus = 2
		result.AccountStatus = 4
		result.PaymentProvider = walletPaymentProvider
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result.Idempotent {
		result.PaymentStatus = 2
		result.RentalStatus = 2
		result.AccountStatus = 4
	}
	return result, nil
}

func (s *PaymentService) ReleaseDeposit(ctx context.Context, actorUserID int64, actorRole string, rentalID int64, now time.Time) (*DepositSettlementResult, error) {
	if actorRole != "ADMIN" {
		return nil, ErrAdminRequired
	}

	result := &DepositSettlementResult{}
	err := s.repo.WithinTransaction(ctx, func(txCtx context.Context) error {
		state, err := s.loadDepositSettlementState(txCtx, rentalID)
		if err != nil {
			return err
		}

		if err := validateDepositReleaseState(state); err != nil {
			return err
		}

		if state.HoldStatus == depositHoldStatusReleased {
			result.Status = "RELEASED"
			return nil
		}

		if err := s.repo.MarkDepositReleased(txCtx, state.HoldID, now); err != nil {
			return fmt.Errorf("mark deposit released: %w", err)
		}
		if err := s.repo.CreditUserBalance(txCtx, state.UserID, state.Amount, now); err != nil {
			return fmt.Errorf("credit user balance: %w", err)
		}

		idempotencyKey := fmt.Sprintf("deposit:release:rental:%d", state.RentalID)
		entryMetadata, err := marshalFinancialMetadata(map[string]any{
			"event":       "deposit_released_to_balance",
			"payment_id":  strconv.FormatInt(state.PaymentID, 10),
			"rental_id":   strconv.FormatInt(state.RentalID, 10),
			"account_id":  strconv.FormatInt(state.AccountID, 10),
			"actor_user":  strconv.FormatInt(actorUserID, 10),
			"recorded_at": now.Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("marshal release ledger metadata: %w", err)
		}
		if err := s.repo.RecordDepositReleasedToBalance(txCtx, FinancialLedgerEntry{
			UserID:         state.UserID,
			RentalID:       state.RentalID,
			PaymentID:      state.PaymentID,
			AccountID:      state.AccountID,
			Amount:         state.Amount,
			Currency:       state.Currency,
			IdempotencyKey: idempotencyKey,
			CorrelationID:  idempotencyKey,
			Metadata:       entryMetadata,
		}); err != nil {
			return fmt.Errorf("insert deposit release ledger entry: %w", err)
		}

		eventMetadata, err := json.Marshal(map[string]any{
			"event":      "deposit_released",
			"payment_id": strconv.FormatInt(state.PaymentID, 10),
			"rental_id":  strconv.FormatInt(state.RentalID, 10),
			"account_id": strconv.FormatInt(state.AccountID, 10),
			"actor_id":   strconv.FormatInt(actorUserID, 10),
		})
		if err != nil {
			return fmt.Errorf("marshal release security metadata: %w", err)
		}
		if err := s.repo.LogDepositSecurityEvent(txCtx, securityEventTypeDepositReleased, state.UserID, state.AccountID, state.RentalID, "api", eventMetadata); err != nil {
			return fmt.Errorf("log release security event: %w", err)
		}

		oldValues, err := json.Marshal(map[string]any{"status": "HELD"})
		if err != nil {
			return fmt.Errorf("marshal release audit old values: %w", err)
		}
		newValues, err := json.Marshal(map[string]any{"status": "RELEASED"})
		if err != nil {
			return fmt.Errorf("marshal release audit new values: %w", err)
		}
		if err := s.repo.InsertAuditLog(txCtx, actorUserID, "deposit_hold", state.HoldID, "deposit_release", oldValues, newValues); err != nil {
			return fmt.Errorf("insert release audit log: %w", err)
		}

		result.Changed = true
		result.Status = "RELEASED"
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result.Status == "" {
		result.Status = "RELEASED"
	}
	return result, nil
}

func (s *PaymentService) ForfeitDeposit(ctx context.Context, actorUserID int64, actorRole string, rentalID int64, reasonCode string, now time.Time) (*DepositSettlementResult, error) {
	if actorRole != "ADMIN" {
		return nil, ErrAdminRequired
	}
	reasonCode = strings.TrimSpace(reasonCode)
	if !isValidReasonCode(reasonCode) {
		return nil, ErrInvalidReasonCode
	}

	result := &DepositSettlementResult{}
	err := s.repo.WithinTransaction(ctx, func(txCtx context.Context) error {
		state, err := s.loadDepositSettlementState(txCtx, rentalID)
		if err != nil {
			return err
		}

		if err := validateDepositForfeitState(state); err != nil {
			return err
		}

		if state.HoldStatus == depositHoldStatusForfeited {
			result.Status = "FORFEITED"
			return nil
		}

		if err := s.repo.MarkDepositForfeited(txCtx, state.HoldID, now); err != nil {
			return fmt.Errorf("mark deposit forfeited: %w", err)
		}

		idempotencyKey := fmt.Sprintf("deposit:forfeit:rental:%d", state.RentalID)
		entryMetadata, err := marshalFinancialMetadata(map[string]any{
			"event":       "deposit_forfeited",
			"reason_code": reasonCode,
			"actor_user":  strconv.FormatInt(actorUserID, 10),
			"recorded_at": now.Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("marshal forfeit ledger metadata: %w", err)
		}
		if err := s.repo.RecordDepositForfeited(txCtx, FinancialLedgerEntry{
			UserID:         state.UserID,
			RentalID:       state.RentalID,
			PaymentID:      state.PaymentID,
			AccountID:      state.AccountID,
			Amount:         state.Amount,
			Currency:       state.Currency,
			IdempotencyKey: idempotencyKey,
			CorrelationID:  idempotencyKey,
			Metadata:       entryMetadata,
		}); err != nil {
			return fmt.Errorf("insert deposit forfeit ledger entry: %w", err)
		}

		eventMetadata, err := json.Marshal(map[string]any{
			"event":       "deposit_forfeited",
			"reason_code": reasonCode,
			"actor_id":    strconv.FormatInt(actorUserID, 10),
			"payment_id":  strconv.FormatInt(state.PaymentID, 10),
			"rental_id":   strconv.FormatInt(state.RentalID, 10),
			"account_id":  strconv.FormatInt(state.AccountID, 10),
		})
		if err != nil {
			return fmt.Errorf("marshal forfeit security metadata: %w", err)
		}
		if err := s.repo.LogDepositSecurityEvent(txCtx, securityEventTypeDepositForfeited, state.UserID, state.AccountID, state.RentalID, "api", eventMetadata); err != nil {
			return fmt.Errorf("log forfeit security event: %w", err)
		}

		oldValues, err := json.Marshal(map[string]any{"status": "HELD"})
		if err != nil {
			return fmt.Errorf("marshal forfeit audit old values: %w", err)
		}
		newValues, err := json.Marshal(map[string]any{"status": "FORFEITED", "reason_code": reasonCode})
		if err != nil {
			return fmt.Errorf("marshal forfeit audit new values: %w", err)
		}
		if err := s.repo.InsertAuditLog(txCtx, actorUserID, "deposit_hold", state.HoldID, "deposit_forfeit", oldValues, newValues); err != nil {
			return fmt.Errorf("insert forfeit audit log: %w", err)
		}

		result.Changed = true
		result.Status = "FORFEITED"
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result.Status == "" {
		result.Status = "FORFEITED"
	}
	return result, nil
}

func (s *PaymentService) loadDepositSettlementState(ctx context.Context, rentalID int64) (*DepositSettlementState, error) {
	state, err := s.repo.LockDepositSettlementState(ctx, rentalID)
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, ErrDepositHoldNotFound) {
		return nil, fmt.Errorf("lock deposit settlement state: %w", err)
	}

	eligibility, loadErr := s.repo.LoadDepositSettlementEligibility(ctx, rentalID)
	if loadErr != nil {
		return nil, fmt.Errorf("load deposit settlement eligibility: %w", loadErr)
	}
	if !eligibility.RentalExists {
		return nil, ErrDepositHoldNotFound
	}
	return nil, ErrDepositSettlementNotAllowed
}

func validateDepositReleaseState(state *DepositSettlementState) error {
	if state.HoldStatus == depositHoldStatusForfeited {
		return ErrDepositAlreadySettled
	}
	if state.HoldStatus == depositHoldStatusReleased {
		return nil
	}
	if state.HoldStatus != depositHoldStatusHeld {
		return ErrDepositSettlementNotAllowed
	}
	if state.PaymentStatus != 2 {
		return ErrDepositSettlementNotAllowed
	}
	if state.RentalStatus != 3 && state.RentalStatus != 4 {
		return ErrDepositSettlementNotAllowed
	}
	return nil
}

func validateDepositForfeitState(state *DepositSettlementState) error {
	if state.HoldStatus == depositHoldStatusReleased {
		return ErrDepositAlreadySettled
	}
	if state.HoldStatus == depositHoldStatusForfeited {
		return nil
	}
	if state.HoldStatus != depositHoldStatusHeld {
		return ErrDepositSettlementNotAllowed
	}
	if state.PaymentStatus != 2 {
		return ErrDepositSettlementNotAllowed
	}
	if state.RentalStatus != 3 && state.RentalStatus != 4 {
		return ErrDepositSettlementNotAllowed
	}
	return nil
}

func isValidReasonCode(reasonCode string) bool {
	if len(reasonCode) == 0 || len(reasonCode) > 64 {
		return false
	}
	for _, ch := range reasonCode {
		if ch >= 'a' && ch <= 'z' {
			continue
		}
		if ch >= '0' && ch <= '9' {
			continue
		}
		if ch == '_' || ch == '-' {
			continue
		}
		return false
	}
	return true
}

func (s *PaymentService) loadWebhookState(ctx context.Context, req WebhookRequest) (*WebhookPaymentState, error) {
	if req.PaymentID != "" {
		paymentID, err := strconv.ParseInt(req.PaymentID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid payment_id format: %w", err)
		}
		state, err := s.repo.LockPaymentForWebhookByID(ctx, paymentID)
		if err == nil {
			return state, nil
		}
		if !errors.Is(err, ErrPaymentNotFound) {
			return nil, err
		}
	}

	if strings.TrimSpace(req.ExternalTransactionID) == "" {
		return nil, ErrPaymentNotFound
	}

	return s.loadWebhookStateByExternalTransaction(ctx, webhookPaymentProvider, strings.TrimSpace(req.ExternalTransactionID))
}

func (s *PaymentService) loadWebhookStateByExternalTransaction(ctx context.Context, provider, externalTransactionID string) (*WebhookPaymentState, error) {
	state, err := s.repo.LockPaymentForWebhookByExternalTransaction(ctx, provider, externalTransactionID)
	if err != nil {
		return nil, err
	}
	if state.Provider != provider {
		return nil, ErrPaymentNotFound
	}
	return state, nil
}
