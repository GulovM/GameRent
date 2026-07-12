package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
	ErrWebhookInvalidPayload       = errors.New("invalid webhook payload")
	ErrWebhookIdentifierMismatch   = errors.New("webhook identifiers do not match the stored payment")
	ErrWebhookFinancialMismatch    = errors.New("webhook financial facts do not match the stored payment")
	ErrWebhookProviderUnsupported  = errors.New("webhook provider is not supported")
	ErrInvalidWebhookSecret        = errors.New("PAYMENT_WEBHOOK_SECRET must be an explicit non-placeholder value of at least 32 bytes without surrounding whitespace")
	ErrPaymentAlreadyProcessed     = errors.New("payment already processed")
	ErrRentalNotEligible           = errors.New("rental is not eligible for activation")
	ErrAccountNotReserved          = errors.New("account is not reserved")
	ErrFinancialUserNotFound       = errors.New("financial user not found")
	ErrWalletPaymentNotFound       = errors.New("wallet payment rental not found")
	ErrWalletPaymentNotAllowed     = errors.New("wallet payment is not allowed")
	ErrWalletPaymentExpired        = errors.New("wallet payment has expired")
	ErrWalletInsufficientBalance   = errors.New("insufficient wallet balance")
	ErrWalletRefundNotFound        = errors.New("wallet refund rental not found")
	ErrWalletRefundNotAllowed      = errors.New("wallet refund is not allowed")
	ErrDepositHoldNotFound         = errors.New("deposit hold not found")
	ErrDepositSettlementNotAllowed = errors.New("deposit settlement is not allowed")
	ErrDepositAlreadySettled       = errors.New("deposit is already settled")
	ErrAdminRequired               = errors.New("admin role is required")
	ErrInvalidReasonCode           = errors.New("invalid reason_code")
	ErrInvalidLedgerPagination     = errors.New("invalid ledger pagination")
	ErrInvalidRefundPagination     = errors.New("invalid refund pagination")
	ErrInvalidAdminRentalFilters   = errors.New("invalid admin rental filters")
	ErrAdminRentalNotFound         = errors.New("admin rental not found")
)

type Service interface {
	ProcessWebhook(ctx context.Context, req WebhookRequest, clientIP, userAgent string) (*WebhookResult, error)
	VerifySignature(payload []byte, signature string) bool
	GetUserBalance(ctx context.Context, userID int64) (*UserBalance, error)
	ListUserLedger(ctx context.Context, userID int64, page, pageSize int) (*UserLedgerPage, error)
	ListUserRefunds(ctx context.Context, userID int64, page, pageSize int) (*UserRefundPage, error)
	ListAdminRentals(ctx context.Context, filters AdminRentalListFilter) (*AdminRentalPage, error)
	GetAdminRentalDetail(ctx context.Context, rentalID int64) (*AdminRentalDetail, error)
	AdjustAdminBalance(ctx context.Context, actorUserID int64, actorRole string, targetUserID int64, input AdminBalanceAdjustmentInput, clientIP, userAgent string, now time.Time) (*AdminBalanceAdjustmentResult, error)
	PayRentalWithBalance(ctx context.Context, userID, rentalID int64, clientIP, userAgent string, now time.Time) (*WalletPaymentResult, error)
	RefundWalletPayment(ctx context.Context, actorUserID int64, actorRole string, rentalID int64, reasonCode string, now time.Time) (*WalletRefundResult, error)
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

type UserRefundPage struct {
	Entries    []PublicRefundEntry
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

type WalletRefundResult struct {
	Changed         bool
	Idempotent      bool
	RefundID        int64
	Status          string
	PrincipalAmount int64
	DepositAmount   int64
	TotalAmount     int64
	DepositStatus   string
}

type WalletRefundReasonOption struct {
	Code  string
	Label string
}

type PaymentService struct {
	repo          Repository
	webhookSecret string
}

var walletRefundReasonOptions = []WalletRefundReasonOption{
	{Code: "SERVICE_UNAVAILABLE", Label: "Service unavailable"},
	{Code: "ACCOUNT_INVALID", Label: "Account invalid"},
	{Code: "ADMIN_CORRECTION", Label: "Admin correction"},
}

var walletRefundReasonCodeSet = func() map[string]struct{} {
	result := make(map[string]struct{}, len(walletRefundReasonOptions))
	for _, option := range walletRefundReasonOptions {
		result[option.Code] = struct{}{}
	}
	return result
}()

func NewPaymentService(repo Repository) *PaymentService {
	return &PaymentService{
		repo:          repo,
		webhookSecret: os.Getenv("PAYMENT_WEBHOOK_SECRET"),
	}
}

func NewPaymentServiceWithWebhookSecret(repo Repository, webhookSecret string) (*PaymentService, error) {
	if err := ValidateWebhookSecret(webhookSecret); err != nil {
		return nil, err
	}
	return &PaymentService{repo: repo, webhookSecret: webhookSecret}, nil
}

func ValidateWebhookSecret(webhookSecret string) error {
	if webhookSecret == "" || strings.TrimSpace(webhookSecret) != webhookSecret || len([]byte(webhookSecret)) < 32 {
		return ErrInvalidWebhookSecret
	}
	normalized := strings.ToLower(webhookSecret)
	for _, unsafe := range []string{"placeholder", "change-me", "changeme", "default", "example", "local-payment-webhook-secret", "your-secret", "<generate"} {
		if strings.Contains(normalized, unsafe) {
			return ErrInvalidWebhookSecret
		}
	}
	uniqueBytes := make(map[byte]struct{})
	for index := 0; index < len(webhookSecret); index++ {
		uniqueBytes[webhookSecret[index]] = struct{}{}
	}
	if len(uniqueBytes) < 8 {
		return ErrInvalidWebhookSecret
	}
	return nil
}

func WalletRefundReasonOptions() []WalletRefundReasonOption {
	options := make([]WalletRefundReasonOption, len(walletRefundReasonOptions))
	copy(options, walletRefundReasonOptions)
	return options
}

func IsAllowedWalletRefundReasonCode(reasonCode string) bool {
	reasonCode = strings.TrimSpace(reasonCode)
	if !isSafeReasonCode(reasonCode) {
		return false
	}
	_, ok := walletRefundReasonCodeSet[reasonCode]
	return ok
}

func (s *PaymentService) VerifySignature(payload []byte, signature string) bool {
	if ValidateWebhookSecret(s.webhookSecret) != nil || len(signature) != sha256.Size*2 {
		return false
	}
	for _, character := range signature {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	providedSignature, err := hex.DecodeString(signature)
	if err != nil || len(providedSignature) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, []byte(s.webhookSecret))
	_, _ = mac.Write(payload)
	return hmac.Equal(providedSignature, mac.Sum(nil))
}

func (s *PaymentService) ProcessWebhook(ctx context.Context, req WebhookRequest, clientIP, userAgent string) (*WebhookResult, error) {
	paymentID, rentalID, err := validateWebhookRequest(req)
	if err != nil {
		return nil, err
	}
	if req.Status != "success" {
		return nil, ErrWebhookNotSuccessful
	}

	now := time.Now()
	result := &WebhookResult{}

	err = s.repo.WithinTransaction(ctx, func(txCtx context.Context) error {
		state, err := s.loadWebhookState(txCtx, paymentID, req.Provider, req.ExternalTransactionID)
		if err != nil {
			return err
		}
		if state.PaymentID != paymentID || state.RentalID != rentalID {
			return ErrWebhookIdentifierMismatch
		}
		if state.Provider != webhookPaymentProvider || state.Provider != req.Provider {
			return ErrWebhookProviderUnsupported
		}
		if state.Amount != req.Amount || state.Currency != req.Currency {
			return ErrWebhookFinancialMismatch
		}
		if state.RentalPrice <= 0 || state.DepositAmount < 0 || state.RentalPrice > math.MaxInt64-state.DepositAmount || state.RentalPrice+state.DepositAmount != state.Amount {
			return ErrWebhookFinancialMismatch
		}

		result.PaymentID = state.PaymentID
		result.RentalID = state.RentalID
		result.AccountID = state.AccountID

		if state.Status == 2 {
			if state.ExternalTransactionID != req.ExternalTransactionID {
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
		if state.RentalStatus != 1 || state.AccountStatus != 3 {
			return fmt.Errorf("%w: payment=%d rental=%d account=%d", ErrWebhookInvalidTransition, state.Status, state.RentalStatus, state.AccountStatus)
		}

		if err := s.repo.MarkPaymentSuccessful(txCtx, state.PaymentID, req.ExternalTransactionID); err != nil {
			if errors.Is(err, ErrPaymentAlreadyProcessed) {
				updated, lockErr := s.loadWebhookStateByExternalTransaction(txCtx, state.Provider, req.ExternalTransactionID)
				if lockErr != nil {
					return lockErr
				}
				if updated.PaymentID != state.PaymentID || updated.RentalID != state.RentalID || updated.Provider != state.Provider ||
					updated.ExternalTransactionID != req.ExternalTransactionID || updated.Amount != state.Amount || updated.Currency != state.Currency {
					return ErrWebhookIdentifierMismatch
				}
				if updated.Status != 2 || updated.RentalStatus != 2 || updated.AccountStatus != 4 {
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

	totalAmount := state.Amount
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

func (s *PaymentService) ListUserRefunds(ctx context.Context, userID int64, page, pageSize int) (*UserRefundPage, error) {
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return nil, ErrInvalidRefundPagination
	}

	offset := (page - 1) * pageSize
	entries, err := s.repo.ListUserRefundEntries(ctx, userID, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("list user refunds: %w", err)
	}
	total, err := s.repo.CountUserRefundEntries(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("count user refunds: %w", err)
	}

	return &UserRefundPage{
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

func (s *PaymentService) RefundWalletPayment(ctx context.Context, actorUserID int64, actorRole string, rentalID int64, reasonCode string, now time.Time) (*WalletRefundResult, error) {
	if actorRole != "ADMIN" {
		return nil, ErrAdminRequired
	}
	reasonCode = strings.TrimSpace(reasonCode)
	if !IsAllowedWalletRefundReasonCode(reasonCode) {
		return nil, ErrInvalidReasonCode
	}

	result := &WalletRefundResult{}
	err := s.repo.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.RequireCurrentAdmin(txCtx, actorUserID); err != nil {
			if errors.Is(err, ErrAdminRequired) {
				return err
			}
			return fmt.Errorf("authorize current admin for wallet refund: %w", err)
		}
		state, err := s.repo.LockWalletRefundState(txCtx, rentalID)
		if err != nil {
			if errors.Is(err, ErrWalletRefundNotFound) {
				return err
			}
			return fmt.Errorf("lock wallet refund state: %w", err)
		}

		principalAmount, depositAmount, err := validateWalletRefundEligibility(state)
		if err != nil {
			return err
		}

		refundKey := fmt.Sprintf("refund:wallet:full:rental:%d", state.RentalID)
		correlationID := refundKey
		refundMetadata, err := marshalFinancialMetadata(map[string]any{
			"event":               "wallet_full_refund",
			"payment_id":          strconv.FormatInt(state.PaymentID, 10),
			"rental_id":           strconv.FormatInt(state.RentalID, 10),
			"account_id":          strconv.FormatInt(state.AccountID, 10),
			"reason_code":         reasonCode,
			"actor_user":          strconv.FormatInt(actorUserID, 10),
			"deposit_hold_status": walletRefundDepositStatus(state.HoldStatus, state.DepositAmount, state.HasDepositHold),
			"recorded_at":         now.UTC().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("marshal wallet refund metadata: %w", err)
		}

		requestedByUserID := &actorUserID
		refundRecord, _, err := s.repo.CreateRefund(txCtx, RefundRecord{
			PaymentID:         state.PaymentID,
			RentalID:          state.RentalID,
			UserID:            state.UserID,
			AccountID:         state.AccountID,
			SourceType:        refundSourceTypeWallet,
			RefundKind:        refundKindFull,
			Status:            refundStatusRequested,
			ReasonCode:        reasonCode,
			RequestedByUserID: requestedByUserID,
			RequestedByRole:   actorRole,
			AmountPrincipal:   principalAmount,
			AmountDeposit:     depositAmount,
			AmountTotal:       principalAmount + depositAmount,
			Currency:          state.Currency,
			IdempotencyKey:    refundKey,
			CorrelationID:     correlationID,
			Metadata:          refundMetadata,
		})
		if err != nil {
			return fmt.Errorf("create refund record: %w", err)
		}

		result.RefundID = refundRecord.ID
		result.PrincipalAmount = refundRecord.AmountPrincipal
		result.DepositAmount = refundRecord.AmountDeposit
		result.TotalAmount = refundRecord.AmountTotal
		result.DepositStatus = walletRefundDepositStatus(state.HoldStatus, state.DepositAmount, state.HasDepositHold)

		if refundRecord.Status == refundStatusCompleted {
			result.Idempotent = true
			result.Status = "COMPLETED"
			if state.DepositAmount > 0 && state.HasDepositHold && state.HoldStatus == depositHoldStatusHeld {
				result.DepositStatus = "REFUNDED"
			}
			return nil
		}

		totals, err := s.repo.LoadCompletedRefundTotals(txCtx, state.PaymentID)
		if err != nil {
			return fmt.Errorf("load completed refund totals: %w", err)
		}
		if totals.Principal+principalAmount > state.RentalPrice {
			return ErrWalletRefundNotAllowed
		}
		if totals.Deposit+depositAmount > maxRefundableDeposit(state) {
			return ErrWalletRefundNotAllowed
		}

		totalAmount := principalAmount + depositAmount
		if err := s.repo.CreditUserBalance(txCtx, state.UserID, totalAmount, now.UTC()); err != nil {
			return fmt.Errorf("credit refund amount to balance: %w", err)
		}

		if depositAmount > 0 {
			if err := s.repo.MarkDepositRefunded(txCtx, state.HoldID, refundRecord.ID, now.UTC()); err != nil {
				return fmt.Errorf("mark deposit refunded: %w", err)
			}
			result.DepositStatus = "REFUNDED"
		}

		principalMetadata, err := marshalFinancialMetadata(map[string]any{
			"event":       "balance_refund_credit",
			"refund_id":   strconv.FormatInt(refundRecord.ID, 10),
			"payment_id":  strconv.FormatInt(state.PaymentID, 10),
			"rental_id":   strconv.FormatInt(state.RentalID, 10),
			"account_id":  strconv.FormatInt(state.AccountID, 10),
			"reason_code": reasonCode,
			"recorded_at": now.UTC().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("marshal principal refund metadata: %w", err)
		}
		if err := s.repo.RecordBalanceRefundCredit(txCtx, FinancialLedgerEntry{
			UserID:         state.UserID,
			RentalID:       state.RentalID,
			PaymentID:      state.PaymentID,
			AccountID:      state.AccountID,
			Amount:         principalAmount,
			Currency:       state.Currency,
			Provider:       walletPaymentProvider,
			IdempotencyKey: fmt.Sprintf("refund:wallet:principal:full:rental:%d", state.RentalID),
			CorrelationID:  correlationID,
			Metadata:       principalMetadata,
		}); err != nil {
			return fmt.Errorf("insert principal refund ledger entry: %w", err)
		}

		if depositAmount > 0 {
			depositMetadata, err := marshalFinancialMetadata(map[string]any{
				"event":       "deposit_refund_credit",
				"refund_id":   strconv.FormatInt(refundRecord.ID, 10),
				"payment_id":  strconv.FormatInt(state.PaymentID, 10),
				"rental_id":   strconv.FormatInt(state.RentalID, 10),
				"account_id":  strconv.FormatInt(state.AccountID, 10),
				"reason_code": reasonCode,
				"recorded_at": now.UTC().Format(time.RFC3339),
			})
			if err != nil {
				return fmt.Errorf("marshal deposit refund metadata: %w", err)
			}
			if err := s.repo.RecordDepositRefundCredit(txCtx, FinancialLedgerEntry{
				UserID:         state.UserID,
				RentalID:       state.RentalID,
				PaymentID:      state.PaymentID,
				AccountID:      state.AccountID,
				Amount:         depositAmount,
				Currency:       state.Currency,
				Provider:       walletPaymentProvider,
				IdempotencyKey: fmt.Sprintf("refund:wallet:deposit:full:rental:%d", state.RentalID),
				CorrelationID:  correlationID,
				Metadata:       depositMetadata,
			}); err != nil {
				return fmt.Errorf("insert deposit refund ledger entry: %w", err)
			}
		}

		if err := s.repo.MarkRefundCompleted(txCtx, refundRecord.ID, now.UTC()); err != nil {
			return fmt.Errorf("mark refund completed: %w", err)
		}

		eventMetadata, err := json.Marshal(map[string]any{
			"event":              "wallet_refund_completed",
			"refund_id":          strconv.FormatInt(refundRecord.ID, 10),
			"payment_id":         strconv.FormatInt(state.PaymentID, 10),
			"rental_id":          strconv.FormatInt(state.RentalID, 10),
			"account_id":         strconv.FormatInt(state.AccountID, 10),
			"actor_id":           strconv.FormatInt(actorUserID, 10),
			"reason_code":        reasonCode,
			"principal_refunded": principalAmount > 0,
			"deposit_refunded":   depositAmount > 0,
			"deposit_status":     result.DepositStatus,
		})
		if err != nil {
			return fmt.Errorf("marshal refund security metadata: %w", err)
		}
		if err := s.repo.LogRefundSecurityEvent(txCtx, state.UserID, state.AccountID, state.RentalID, "api", eventMetadata); err != nil {
			return fmt.Errorf("log refund security event: %w", err)
		}

		oldValues, err := json.Marshal(map[string]any{
			"status":         "REQUESTED",
			"principal":      principalAmount,
			"deposit":        depositAmount,
			"deposit_status": walletRefundDepositStatus(state.HoldStatus, state.DepositAmount, state.HasDepositHold),
		})
		if err != nil {
			return fmt.Errorf("marshal refund audit old values: %w", err)
		}
		newValues, err := json.Marshal(map[string]any{
			"status":         "COMPLETED",
			"principal":      principalAmount,
			"deposit":        depositAmount,
			"deposit_status": result.DepositStatus,
			"reason_code":    reasonCode,
		})
		if err != nil {
			return fmt.Errorf("marshal refund audit new values: %w", err)
		}
		if err := s.repo.InsertAuditLog(txCtx, actorUserID, "refund", refundRecord.ID, "wallet_refund_completed", oldValues, newValues); err != nil {
			return fmt.Errorf("insert refund audit log: %w", err)
		}

		result.Changed = true
		result.Status = "COMPLETED"
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result.Status == "" {
		result.Status = "COMPLETED"
	}
	return result, nil
}

func (s *PaymentService) ReleaseDeposit(ctx context.Context, actorUserID int64, actorRole string, rentalID int64, now time.Time) (*DepositSettlementResult, error) {
	if actorRole != "ADMIN" {
		return nil, ErrAdminRequired
	}

	result := &DepositSettlementResult{}
	err := s.repo.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.RequireCurrentAdmin(txCtx, actorUserID); err != nil {
			if errors.Is(err, ErrAdminRequired) {
				return err
			}
			return fmt.Errorf("authorize current admin for deposit release: %w", err)
		}
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
	if !isSafeReasonCode(reasonCode) {
		return nil, ErrInvalidReasonCode
	}

	result := &DepositSettlementResult{}
	err := s.repo.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.RequireCurrentAdmin(txCtx, actorUserID); err != nil {
			if errors.Is(err, ErrAdminRequired) {
				return err
			}
			return fmt.Errorf("authorize current admin for deposit forfeit: %w", err)
		}
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
	if state.HoldStatus == depositHoldStatusRefunded {
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
	if state.HoldStatus == depositHoldStatusRefunded {
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

func isSafeReasonCode(reasonCode string) bool {
	if len(reasonCode) == 0 || len(reasonCode) > 64 {
		return false
	}
	for _, ch := range reasonCode {
		if ch >= 'a' && ch <= 'z' {
			continue
		}
		if ch >= 'A' && ch <= 'Z' {
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

func validateWalletRefundEligibility(state *WalletRefundState) (int64, int64, error) {
	if state.Provider != walletPaymentProvider {
		return 0, 0, ErrWalletRefundNotAllowed
	}
	if state.PaymentStatus != 2 {
		return 0, 0, ErrWalletRefundNotAllowed
	}
	if state.RentalStatus != 3 && state.RentalStatus != 4 {
		return 0, 0, ErrWalletRefundNotAllowed
	}
	principalAmount := state.RentalPrice
	if principalAmount <= 0 {
		return 0, 0, ErrWalletRefundNotAllowed
	}

	if state.DepositAmount <= 0 {
		return principalAmount, 0, nil
	}
	if !state.HasDepositHold {
		return 0, 0, ErrWalletRefundNotAllowed
	}

	switch state.HoldStatus {
	case depositHoldStatusHeld:
		return principalAmount, state.HoldAmount, nil
	case depositHoldStatusReleased, depositHoldStatusForfeited, depositHoldStatusRefunded:
		return principalAmount, 0, nil
	default:
		return 0, 0, ErrWalletRefundNotAllowed
	}
}

func maxRefundableDeposit(state *WalletRefundState) int64 {
	if state.DepositAmount <= 0 {
		return 0
	}
	if state.HasDepositHold && state.HoldStatus == depositHoldStatusHeld {
		return state.HoldAmount
	}
	return 0
}

func walletRefundDepositStatus(holdStatus int16, depositAmount int64, hasDepositHold bool) string {
	if depositAmount <= 0 {
		return "NONE"
	}
	if !hasDepositHold {
		return "NONE"
	}
	switch holdStatus {
	case depositHoldStatusHeld:
		return "HELD"
	case depositHoldStatusReleased:
		return "RELEASED"
	case depositHoldStatusForfeited:
		return "FORFEITED"
	case depositHoldStatusRefunded:
		return "REFUNDED"
	default:
		return "UNKNOWN"
	}
}

func validateWebhookRequest(req WebhookRequest) (paymentID, rentalID int64, err error) {
	if req.PaymentID == "" || len(req.PaymentID) > 19 || req.RentalID == "" || len(req.RentalID) > 19 ||
		req.ExternalTransactionID == "" || len(req.ExternalTransactionID) > 128 || !isSafeWebhookExternalTransactionID(req.ExternalTransactionID) ||
		req.Provider != webhookPaymentProvider || req.Amount <= 0 || len(req.Currency) != 3 || strings.TrimSpace(req.Currency) != req.Currency ||
		req.Status == "" || len(req.Status) > 16 || strings.TrimSpace(req.Status) != req.Status {
		return 0, 0, ErrWebhookInvalidPayload
	}
	paymentID, err = strconv.ParseInt(req.PaymentID, 10, 64)
	if err != nil || paymentID <= 0 {
		return 0, 0, ErrWebhookInvalidPayload
	}
	rentalID, err = strconv.ParseInt(req.RentalID, 10, 64)
	if err != nil || rentalID <= 0 {
		return 0, 0, ErrWebhookInvalidPayload
	}
	return paymentID, rentalID, nil
}

func isSafeWebhookExternalTransactionID(value string) bool {
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' || character == ':' {
			continue
		}
		return false
	}
	return true
}

func (s *PaymentService) loadWebhookState(ctx context.Context, paymentID int64, provider, externalTransactionID string) (*WebhookPaymentState, error) {
	stateByExternalID, err := s.loadWebhookStateByExternalTransaction(ctx, provider, externalTransactionID)
	if err == nil {
		if stateByExternalID.PaymentID != paymentID {
			return nil, ErrWebhookIdentifierMismatch
		}
		return stateByExternalID, nil
	}
	if !errors.Is(err, ErrPaymentNotFound) {
		return nil, err
	}
	return s.repo.LockPaymentForWebhookByID(ctx, paymentID)
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
