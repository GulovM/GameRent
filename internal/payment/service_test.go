package payment

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockRepository struct {
	mu sync.Mutex

	withinTxErr error

	statesByID                    map[int64]*WebhookPaymentState
	statesByExt                   map[string]*WebhookPaymentState
	walletStatesByRental          map[int64]*WalletPaymentState
	settlementStatesByRental      map[int64]*DepositSettlementState
	settlementEligibilityByRental map[int64]*DepositSettlementEligibility

	markPaymentErr error
	activateErr    error
	rentErr        error
	ledgerErr      error
	logErr         error
	auditErr       error

	markPaymentCalls    int
	activateCalls       int
	rentCalls           int
	providerLedgerCalls int
	depositHoldCalls    int
	depositLedgerCalls  int
	depositReleaseCalls int
	depositForfeitCalls int
	balanceDebitCalls   int
	balanceCreditCalls  int
	securityEventCalls  int
	auditCalls          int
	logCalls            int

	providerLedgerEntries []FinancialLedgerEntry
	balanceDebitEntries   []FinancialLedgerEntry
	depositHolds          []DepositHold
	depositLedgerEntries  []FinancialLedgerEntry
	depositReleaseEntries []FinancialLedgerEntry
	depositForfeitEntries []FinancialLedgerEntry
}

func newMockRepository(state *WebhookPaymentState) *mockRepository {
	repo := &mockRepository{
		statesByID:                    map[int64]*WebhookPaymentState{},
		statesByExt:                   map[string]*WebhookPaymentState{},
		walletStatesByRental:          map[int64]*WalletPaymentState{},
		settlementStatesByRental:      map[int64]*DepositSettlementState{},
		settlementEligibilityByRental: map[int64]*DepositSettlementEligibility{},
	}
	if state != nil {
		repo.statesByID[state.PaymentID] = cloneWebhookState(state)
		repo.walletStatesByRental[state.RentalID] = &WalletPaymentState{
			PaymentID:             state.PaymentID,
			RentalID:              state.RentalID,
			UserID:                state.UserID,
			AccountID:             state.AccountID,
			Provider:              state.Provider,
			ExternalTransactionID: state.ExternalTransactionID,
			PaymentStatus:         state.Status,
			RentalStatus:          state.RentalStatus,
			AccountStatus:         state.AccountStatus,
			RentalPrice:           state.RentalPrice,
			DepositAmount:         state.DepositAmount,
			PaymentExpiresAt:      state.PaymentExpiresAt,
			Currency:              state.Currency,
		}
		if state.ExternalTransactionID != "" {
			repo.statesByExt[state.ExternalTransactionID] = repo.statesByID[state.PaymentID]
		}
	}
	return repo
}

func cloneWebhookState(state *WebhookPaymentState) *WebhookPaymentState {
	if state == nil {
		return nil
	}
	cp := *state
	return &cp
}

func (m *mockRepository) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	m.mu.Lock()
	snapshotByID := make(map[int64]*WebhookPaymentState, len(m.statesByID))
	snapshotByExt := make(map[string]*WebhookPaymentState, len(m.statesByExt))
	snapshotWalletStates := make(map[int64]*WalletPaymentState, len(m.walletStatesByRental))
	snapshotSettlementStates := make(map[int64]*DepositSettlementState, len(m.settlementStatesByRental))
	snapshotSettlementEligibility := make(map[int64]*DepositSettlementEligibility, len(m.settlementEligibilityByRental))
	snapshotProviderLedger := append([]FinancialLedgerEntry(nil), m.providerLedgerEntries...)
	snapshotBalanceDebitLedger := append([]FinancialLedgerEntry(nil), m.balanceDebitEntries...)
	snapshotDepositHolds := append([]DepositHold(nil), m.depositHolds...)
	snapshotDepositLedger := append([]FinancialLedgerEntry(nil), m.depositLedgerEntries...)
	snapshotDepositRelease := append([]FinancialLedgerEntry(nil), m.depositReleaseEntries...)
	snapshotDepositForfeit := append([]FinancialLedgerEntry(nil), m.depositForfeitEntries...)
	for id, state := range m.statesByID {
		snapshotByID[id] = cloneWebhookState(state)
	}
	for ext, state := range m.statesByExt {
		snapshotByExt[ext] = cloneWebhookState(state)
	}
	for rentalID, state := range m.walletStatesByRental {
		cp := *state
		snapshotWalletStates[rentalID] = &cp
	}
	for rentalID, state := range m.settlementStatesByRental {
		cp := *state
		snapshotSettlementStates[rentalID] = &cp
	}
	for rentalID, state := range m.settlementEligibilityByRental {
		cp := *state
		snapshotSettlementEligibility[rentalID] = &cp
	}
	m.mu.Unlock()

	if m.withinTxErr != nil {
		return m.withinTxErr
	}

	err := fn(ctx)
	if err != nil {
		m.mu.Lock()
		m.statesByID = snapshotByID
		m.statesByExt = snapshotByExt
		m.walletStatesByRental = snapshotWalletStates
		m.settlementStatesByRental = snapshotSettlementStates
		m.settlementEligibilityByRental = snapshotSettlementEligibility
		m.providerLedgerEntries = snapshotProviderLedger
		m.balanceDebitEntries = snapshotBalanceDebitLedger
		m.depositHolds = snapshotDepositHolds
		m.depositLedgerEntries = snapshotDepositLedger
		m.depositReleaseEntries = snapshotDepositRelease
		m.depositForfeitEntries = snapshotDepositForfeit
		m.mu.Unlock()
	}
	return err
}

func (m *mockRepository) LockWalletPaymentState(ctx context.Context, rentalID, userID int64) (*WalletPaymentState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.walletStatesByRental[rentalID]
	if !ok || state.UserID != userID {
		return nil, ErrWalletPaymentNotFound
	}
	cp := *state
	return &cp, nil
}

func (m *mockRepository) DebitUserBalance(ctx context.Context, userID, amount int64, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for rentalID, state := range m.walletStatesByRental {
		if state.UserID != userID {
			continue
		}
		if state.UserBalance < amount {
			return ErrWalletInsufficientBalance
		}
		state.UserBalance -= amount
		m.walletStatesByRental[rentalID] = state
		m.balanceDebitCalls++
		return nil
	}
	return ErrWalletPaymentNotFound
}

func (m *mockRepository) MarkPaymentSuccessfulForWallet(ctx context.Context, paymentID int64, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.statesByID[paymentID]
	if !ok {
		return ErrPaymentNotFound
	}
	if state.Status != 1 {
		return ErrPaymentAlreadyProcessed
	}
	state.Status = 2
	state.Provider = walletPaymentProvider
	state.ExternalTransactionID = ""
	for rentalID, walletState := range m.walletStatesByRental {
		if walletState.PaymentID == paymentID {
			walletState.PaymentStatus = 2
			walletState.Provider = walletPaymentProvider
			walletState.ExternalTransactionID = ""
			m.walletStatesByRental[rentalID] = walletState
			break
		}
	}
	m.markPaymentCalls++
	return nil
}

func (m *mockRepository) RecordBalanceDebit(ctx context.Context, entry FinancialLedgerEntry) error {
	if m.ledgerErr != nil {
		return m.ledgerErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.balanceDebitEntries {
		if existing.IdempotencyKey == entry.IdempotencyKey {
			return nil
		}
	}
	m.balanceDebitEntries = append(m.balanceDebitEntries, entry)
	return nil
}

func (m *mockRepository) GetUserBalance(ctx context.Context, userID int64) (*UserBalance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, state := range m.settlementStatesByRental {
		if state.UserID == userID {
			return &UserBalance{UserID: userID, AvailableBalance: state.UserBalance, Currency: "USD"}, nil
		}
	}
	return nil, ErrFinancialUserNotFound
}

func (m *mockRepository) ListUserLedgerEntries(ctx context.Context, userID int64, limit, offset int) ([]PublicLedgerEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries := make([]PublicLedgerEntry, 0, len(m.providerLedgerEntries)+len(m.depositLedgerEntries)+len(m.depositReleaseEntries)+len(m.depositForfeitEntries))
	appendEntries := func(source []FinancialLedgerEntry, entryType int16) {
		for idx, entry := range source {
			if entry.UserID != userID {
				continue
			}
			var rentalID *int64
			if entry.RentalID != 0 {
				value := entry.RentalID
				rentalID = &value
			}
			var paymentID *int64
			if entry.PaymentID != 0 {
				value := entry.PaymentID
				paymentID = &value
			}
			entries = append(entries, PublicLedgerEntry{
				ID:          int64(len(entries) + idx + 1),
				EntryType:   entryType,
				Amount:      entry.Amount,
				Currency:    entry.Currency,
				RentalID:    rentalID,
				PaymentID:   paymentID,
				CreatedAt:   time.Now(),
				DisplayType: publicLedgerDisplayType(entryType),
			})
		}
	}
	appendEntries(m.providerLedgerEntries, ledgerEntryProviderPaymentReceived)
	appendEntries(m.balanceDebitEntries, ledgerEntryBalanceDebit)
	appendEntries(m.depositLedgerEntries, ledgerEntryDepositHeld)
	appendEntries(m.depositReleaseEntries, ledgerEntryDepositReleasedBalance)
	appendEntries(m.depositForfeitEntries, ledgerEntryDepositForfeited)

	if offset >= len(entries) {
		return []PublicLedgerEntry{}, nil
	}
	end := offset + limit
	if end > len(entries) {
		end = len(entries)
	}
	return entries[offset:end], nil
}

func (m *mockRepository) CountUserLedgerEntries(ctx context.Context, userID int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var count int64
	countEntries := func(source []FinancialLedgerEntry) {
		for _, entry := range source {
			if entry.UserID == userID {
				count++
			}
		}
	}
	countEntries(m.providerLedgerEntries)
	countEntries(m.balanceDebitEntries)
	countEntries(m.depositLedgerEntries)
	countEntries(m.depositReleaseEntries)
	countEntries(m.depositForfeitEntries)
	return count, nil
}

func (m *mockRepository) LockPaymentForWebhookByID(ctx context.Context, paymentID int64) (*WebhookPaymentState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.statesByID[paymentID]
	if !ok {
		return nil, ErrPaymentNotFound
	}
	return cloneWebhookState(state), nil
}

func (m *mockRepository) LockPaymentForWebhookByExternalTransaction(ctx context.Context, provider, externalTransactionID string) (*WebhookPaymentState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.statesByExt[externalTransactionID]
	if !ok || state.Provider != provider {
		return nil, ErrPaymentNotFound
	}
	return cloneWebhookState(state), nil
}

func (m *mockRepository) MarkPaymentSuccessful(ctx context.Context, paymentID int64, externalTransactionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.statesByID[paymentID]
	if !ok {
		return ErrPaymentNotFound
	}
	if state.Status != 1 {
		return ErrPaymentAlreadyProcessed
	}
	if m.markPaymentErr != nil {
		return m.markPaymentErr
	}
	state.Status = 2
	state.ExternalTransactionID = externalTransactionID
	if externalTransactionID != "" {
		m.statesByExt[externalTransactionID] = state
	}
	m.markPaymentCalls++
	return nil
}

func (m *mockRepository) ActivateRentalForWebhook(ctx context.Context, rentalID int64, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activateErr != nil {
		return m.activateErr
	}
	for _, state := range m.statesByID {
		if state.RentalID == rentalID {
			if state.RentalStatus != 1 || now.After(state.PaymentExpiresAt) {
				return ErrRentalNotEligible
			}
			state.RentalStatus = 2
			if walletState, ok := m.walletStatesByRental[rentalID]; ok {
				walletState.RentalStatus = 2
				m.walletStatesByRental[rentalID] = walletState
			}
			m.activateCalls++
			return nil
		}
	}
	return ErrPaymentNotFound
}

func (m *mockRepository) MarkAccountRentedForWebhook(ctx context.Context, accountID int64, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.rentErr != nil {
		return m.rentErr
	}
	for _, state := range m.statesByID {
		if state.AccountID == accountID {
			if state.AccountStatus != 3 {
				return ErrWebhookInvalidTransition
			}
			state.AccountStatus = 4
			if walletState, ok := m.walletStatesByRental[state.RentalID]; ok {
				walletState.AccountStatus = 4
				m.walletStatesByRental[state.RentalID] = walletState
			}
			m.rentCalls++
			return nil
		}
	}
	return ErrPaymentNotFound
}

func (m *mockRepository) RecordProviderPaymentReceived(ctx context.Context, entry FinancialLedgerEntry) error {
	if m.ledgerErr != nil {
		return m.ledgerErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.providerLedgerEntries {
		if existing.IdempotencyKey == entry.IdempotencyKey {
			return nil
		}
	}
	m.providerLedgerEntries = append(m.providerLedgerEntries, entry)
	m.providerLedgerCalls++
	return nil
}

func (m *mockRepository) CreateDepositHold(ctx context.Context, hold DepositHold) error {
	if m.ledgerErr != nil {
		return m.ledgerErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.depositHolds {
		if existing.IdempotencyKey == hold.IdempotencyKey {
			return nil
		}
	}
	m.depositHolds = append(m.depositHolds, hold)
	m.depositHoldCalls++
	return nil
}

func (m *mockRepository) RecordDepositHeld(ctx context.Context, entry FinancialLedgerEntry) error {
	if m.ledgerErr != nil {
		return m.ledgerErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.depositLedgerEntries {
		if existing.IdempotencyKey == entry.IdempotencyKey {
			return nil
		}
	}
	m.depositLedgerEntries = append(m.depositLedgerEntries, entry)
	m.depositLedgerCalls++
	return nil
}

func (m *mockRepository) LockDepositSettlementState(ctx context.Context, rentalID int64) (*DepositSettlementState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.settlementStatesByRental[rentalID]
	if !ok {
		return nil, ErrDepositHoldNotFound
	}
	cp := *state
	return &cp, nil
}

func (m *mockRepository) LoadDepositSettlementEligibility(ctx context.Context, rentalID int64) (*DepositSettlementEligibility, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.settlementEligibilityByRental[rentalID]
	if !ok {
		return &DepositSettlementEligibility{}, nil
	}
	cp := *state
	return &cp, nil
}

func (m *mockRepository) MarkDepositReleased(ctx context.Context, holdID int64, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, state := range m.settlementStatesByRental {
		if state.HoldID == holdID {
			if state.HoldStatus != depositHoldStatusHeld {
				return ErrDepositSettlementNotAllowed
			}
			state.HoldStatus = depositHoldStatusReleased
			m.depositReleaseCalls++
			return nil
		}
	}
	return ErrDepositHoldNotFound
}

func (m *mockRepository) MarkDepositForfeited(ctx context.Context, holdID int64, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, state := range m.settlementStatesByRental {
		if state.HoldID == holdID {
			if state.HoldStatus != depositHoldStatusHeld {
				return ErrDepositSettlementNotAllowed
			}
			state.HoldStatus = depositHoldStatusForfeited
			m.depositForfeitCalls++
			return nil
		}
	}
	return ErrDepositHoldNotFound
}

func (m *mockRepository) CreditUserBalance(ctx context.Context, userID, amount int64, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, state := range m.settlementStatesByRental {
		if state.UserID == userID {
			state.UserBalance += amount
			m.balanceCreditCalls++
			return nil
		}
	}
	return ErrPaymentNotFound
}

func (m *mockRepository) RecordDepositReleasedToBalance(ctx context.Context, entry FinancialLedgerEntry) error {
	if m.ledgerErr != nil {
		return m.ledgerErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.depositReleaseEntries {
		if existing.IdempotencyKey == entry.IdempotencyKey {
			return nil
		}
	}
	m.depositReleaseEntries = append(m.depositReleaseEntries, entry)
	return nil
}

func (m *mockRepository) RecordDepositForfeited(ctx context.Context, entry FinancialLedgerEntry) error {
	if m.ledgerErr != nil {
		return m.ledgerErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.depositForfeitEntries {
		if existing.IdempotencyKey == entry.IdempotencyKey {
			return nil
		}
	}
	m.depositForfeitEntries = append(m.depositForfeitEntries, entry)
	return nil
}

func (m *mockRepository) LogDepositSecurityEvent(ctx context.Context, eventType int16, userID, accountID, rentalID int64, userAgent string, metadata []byte) error {
	if m.logErr != nil {
		return m.logErr
	}
	m.mu.Lock()
	m.securityEventCalls++
	m.mu.Unlock()
	return nil
}

func (m *mockRepository) LogWalletSecurityEvent(ctx context.Context, userID, accountID, rentalID int64, clientIP, userAgent string, metadata []byte) error {
	if m.logErr != nil {
		return m.logErr
	}
	m.mu.Lock()
	m.securityEventCalls++
	m.mu.Unlock()
	return nil
}

func (m *mockRepository) InsertAuditLog(ctx context.Context, actorUserID int64, entityType string, entityID int64, action string, oldValues, newValues []byte) error {
	if m.auditErr != nil {
		return m.auditErr
	}
	m.mu.Lock()
	m.auditCalls++
	m.mu.Unlock()
	return nil
}

func (m *mockRepository) UpdatePaymentSuccess(ctx context.Context, paymentID int64, extTxID string) (int64, int64, int64, string, error) {
	return 0, 0, 0, "", nil
}

func (m *mockRepository) ActivateRental(ctx context.Context, rentalID int64) (int64, error) {
	return 0, nil
}

func (m *mockRepository) MarkAccountRented(ctx context.Context, accountID int64) (string, []byte, string, error) {
	return "", nil, "", nil
}

func (m *mockRepository) CreatePendingPayment(ctx context.Context, rentalID, userID int64, amount int64, currency string) (int64, error) {
	return 0, nil
}

func (m *mockRepository) LogSecurityEvent(ctx context.Context, userID, accountID, rentalID int64, clientIP, userAgent string, metadata []byte) error {
	if m.logErr != nil {
		return m.logErr
	}
	m.mu.Lock()
	m.logCalls++
	m.mu.Unlock()
	return nil
}

func seedSettlementState(repo *mockRepository, state *DepositSettlementState, eligibility *DepositSettlementEligibility) {
	repo.settlementStatesByRental[state.RentalID] = state
	if eligibility != nil {
		repo.settlementEligibilityByRental[state.RentalID] = eligibility
	}
}

func seedWalletPaymentState(repo *mockRepository, state *WalletPaymentState) {
	repo.walletStatesByRental[state.RentalID] = state
	repo.statesByID[state.PaymentID] = &WebhookPaymentState{
		PaymentID:             state.PaymentID,
		RentalID:              state.RentalID,
		UserID:                state.UserID,
		AccountID:             state.AccountID,
		Provider:              state.Provider,
		ExternalTransactionID: state.ExternalTransactionID,
		Status:                state.PaymentStatus,
		Amount:                state.RentalPrice + state.DepositAmount,
		Currency:              state.Currency,
		RentalPrice:           state.RentalPrice,
		DepositAmount:         state.DepositAmount,
		PaymentExpiresAt:      state.PaymentExpiresAt,
		RentalStatus:          state.RentalStatus,
		AccountStatus:         state.AccountStatus,
	}
}

func TestPaymentService_ProcessWebhook_FirstSuccessActivatesRental(t *testing.T) {
	repo := newMockRepository(&WebhookPaymentState{
		PaymentID:        101,
		RentalID:         202,
		UserID:           303,
		AccountID:        404,
		Provider:         webhookPaymentProvider,
		Status:           1,
		Amount:           1500,
		Currency:         "USD",
		RentalPrice:      500,
		DepositAmount:    1000,
		PaymentExpiresAt: time.Now().Add(time.Hour),
		RentalStatus:     1,
		AccountStatus:    3,
	})
	service := NewPaymentService(repo)

	res, err := service.ProcessWebhook(context.Background(), WebhookRequest{
		PaymentID:             "101",
		ExternalTransactionID: "ext-tx-1",
		Status:                "success",
	}, "127.0.0.1", "Go-Test")
	if err != nil {
		t.Fatalf("ProcessWebhook failed: %v", err)
	}

	if !res.Processed || res.Idempotent {
		t.Fatalf("expected first success to process once, got %+v", res)
	}

	state := repo.statesByID[101]
	if state.Status != 2 || state.RentalStatus != 2 || state.AccountStatus != 4 {
		t.Fatalf("unexpected final state: %+v", state)
	}
	if state.ExternalTransactionID != "ext-tx-1" {
		t.Fatalf("expected external tx to be stored, got %q", state.ExternalTransactionID)
	}
	if repo.markPaymentCalls != 1 || repo.activateCalls != 1 || repo.rentCalls != 1 || repo.providerLedgerCalls != 1 || repo.depositHoldCalls != 1 || repo.depositLedgerCalls != 1 || repo.logCalls != 1 {
		t.Fatalf("expected one call to each transition, got payment=%d rental=%d account=%d providerLedger=%d depositHold=%d depositLedger=%d log=%d", repo.markPaymentCalls, repo.activateCalls, repo.rentCalls, repo.providerLedgerCalls, repo.depositHoldCalls, repo.depositLedgerCalls, repo.logCalls)
	}
	if len(repo.providerLedgerEntries) != 1 || repo.providerLedgerEntries[0].Amount != 1500 {
		t.Fatalf("expected provider payment ledger entry for total amount, got %+v", repo.providerLedgerEntries)
	}
	if len(repo.depositHolds) != 1 || repo.depositHolds[0].Amount != 1000 {
		t.Fatalf("expected one deposit hold, got %+v", repo.depositHolds)
	}
	if len(repo.depositLedgerEntries) != 1 || repo.depositLedgerEntries[0].Amount != 1000 {
		t.Fatalf("expected one deposit ledger entry, got %+v", repo.depositLedgerEntries)
	}
}

func TestPaymentService_ProcessWebhook_DuplicateReplayIsIdempotent(t *testing.T) {
	repo := newMockRepository(&WebhookPaymentState{
		PaymentID:             101,
		RentalID:              202,
		UserID:                303,
		AccountID:             404,
		Provider:              webhookPaymentProvider,
		ExternalTransactionID: "ext-tx-dup",
		Status:                2,
		Amount:                1500,
		Currency:              "USD",
		RentalPrice:           500,
		DepositAmount:         1000,
		PaymentExpiresAt:      time.Now().Add(time.Hour),
		RentalStatus:          2,
		AccountStatus:         4,
	})
	service := NewPaymentService(repo)

	res, err := service.ProcessWebhook(context.Background(), WebhookRequest{
		PaymentID:             "101",
		ExternalTransactionID: "ext-tx-dup",
		Status:                "success",
	}, "127.0.0.1", "Go-Test")
	if err != nil {
		t.Fatalf("duplicate webhook should be idempotent: %v", err)
	}
	if !res.Idempotent || !res.Processed {
		t.Fatalf("expected duplicate replay to be idempotent success, got %+v", res)
	}
	if repo.markPaymentCalls != 0 || repo.activateCalls != 0 || repo.rentCalls != 0 {
		t.Fatalf("expected no transitions on replay, got payment=%d rental=%d account=%d", repo.markPaymentCalls, repo.activateCalls, repo.rentCalls)
	}
	if repo.providerLedgerCalls != 0 || repo.depositHoldCalls != 0 || repo.depositLedgerCalls != 0 {
		t.Fatalf("expected no financial inserts on replay, got providerLedger=%d depositHold=%d depositLedger=%d", repo.providerLedgerCalls, repo.depositHoldCalls, repo.depositLedgerCalls)
	}
}

func TestPaymentService_ProcessWebhook_PaymentIDTakesPrecedenceOverExternalTransactionLookup(t *testing.T) {
	repo := newMockRepository(&WebhookPaymentState{
		PaymentID:        151,
		RentalID:         252,
		UserID:           353,
		AccountID:        454,
		Provider:         webhookPaymentProvider,
		Status:           1,
		Amount:           1500,
		Currency:         "USD",
		RentalPrice:      500,
		DepositAmount:    1000,
		PaymentExpiresAt: time.Now().Add(time.Hour),
		RentalStatus:     1,
		AccountStatus:    3,
	})
	repo.statesByExt["unrelated-ext"] = &WebhookPaymentState{
		PaymentID:             999,
		RentalID:              998,
		UserID:                997,
		AccountID:             996,
		Provider:              webhookPaymentProvider,
		ExternalTransactionID: "unrelated-ext",
		Status:                2,
		Amount:                111,
		Currency:              "USD",
		RentalPrice:           111,
		DepositAmount:         0,
		PaymentExpiresAt:      time.Now().Add(time.Hour),
		RentalStatus:          2,
		AccountStatus:         4,
	}
	service := NewPaymentService(repo)

	res, err := service.ProcessWebhook(context.Background(), WebhookRequest{
		PaymentID:             "151",
		ExternalTransactionID: "unrelated-ext",
		Status:                "success",
	}, "127.0.0.1", "Go-Test")
	if err != nil {
		t.Fatalf("ProcessWebhook failed: %v", err)
	}
	if res.PaymentID != 151 || res.RentalID != 252 || res.AccountID != 454 || !res.Processed || res.Idempotent {
		t.Fatalf("unexpected result when payment_id should win lookup: %+v", res)
	}

	state := repo.statesByID[151]
	if state.Status != 2 || state.ExternalTransactionID != "unrelated-ext" {
		t.Fatalf("expected payment selected by payment_id to be updated, got %+v", state)
	}
	if other := repo.statesByExt["unrelated-ext"]; other == nil || other.PaymentID != 151 {
		t.Fatalf("expected external transaction mapping to point at the selected payment, got %+v", other)
	}
}

func TestPaymentService_ProcessWebhook_ZeroDepositSkipsDepositRecords(t *testing.T) {
	repo := newMockRepository(&WebhookPaymentState{
		PaymentID:        111,
		RentalID:         222,
		UserID:           333,
		AccountID:        444,
		Provider:         webhookPaymentProvider,
		Status:           1,
		Amount:           500,
		Currency:         "USD",
		RentalPrice:      500,
		DepositAmount:    0,
		PaymentExpiresAt: time.Now().Add(time.Hour),
		RentalStatus:     1,
		AccountStatus:    3,
	})
	service := NewPaymentService(repo)

	_, err := service.ProcessWebhook(context.Background(), WebhookRequest{
		PaymentID:             "111",
		ExternalTransactionID: "ext-zero-deposit",
		Status:                "success",
	}, "127.0.0.1", "Go-Test")
	if err != nil {
		t.Fatalf("ProcessWebhook failed: %v", err)
	}
	if repo.providerLedgerCalls != 1 {
		t.Fatalf("expected provider ledger entry, got %d", repo.providerLedgerCalls)
	}
	if repo.depositHoldCalls != 0 || repo.depositLedgerCalls != 0 {
		t.Fatalf("expected no deposit records for zero deposit, got hold=%d ledger=%d", repo.depositHoldCalls, repo.depositLedgerCalls)
	}
}

func TestPaymentService_ProcessWebhook_RollbackOnLedgerFailure(t *testing.T) {
	repo := newMockRepository(&WebhookPaymentState{
		PaymentID:        121,
		RentalID:         222,
		UserID:           323,
		AccountID:        424,
		Provider:         webhookPaymentProvider,
		Status:           1,
		Amount:           1500,
		Currency:         "USD",
		RentalPrice:      500,
		DepositAmount:    1000,
		PaymentExpiresAt: time.Now().Add(time.Hour),
		RentalStatus:     1,
		AccountStatus:    3,
	})
	repo.ledgerErr = errors.New("ledger insert failed")
	service := NewPaymentService(repo)

	_, err := service.ProcessWebhook(context.Background(), WebhookRequest{
		PaymentID:             "121",
		ExternalTransactionID: "ext-ledger-rb",
		Status:                "success",
	}, "127.0.0.1", "Go-Test")
	if err == nil {
		t.Fatalf("expected ledger failure")
	}

	state := repo.statesByID[121]
	if state.Status != 1 || state.RentalStatus != 1 || state.AccountStatus != 3 {
		t.Fatalf("expected rollback to restore original state, got %+v", state)
	}
	if len(repo.providerLedgerEntries) != 0 || len(repo.depositHolds) != 0 || len(repo.depositLedgerEntries) != 0 {
		t.Fatalf("expected rollback to remove financial records, got provider=%d holds=%d deposit=%d", len(repo.providerLedgerEntries), len(repo.depositHolds), len(repo.depositLedgerEntries))
	}
}

func TestPaymentService_ProcessWebhook_FinancialMetadataIsSanitized(t *testing.T) {
	repo := newMockRepository(&WebhookPaymentState{
		PaymentID:        131,
		RentalID:         232,
		UserID:           333,
		AccountID:        434,
		Provider:         webhookPaymentProvider,
		Status:           1,
		Amount:           1500,
		Currency:         "USD",
		RentalPrice:      500,
		DepositAmount:    1000,
		PaymentExpiresAt: time.Now().Add(time.Hour),
		RentalStatus:     1,
		AccountStatus:    3,
	})
	service := NewPaymentService(repo)

	_, err := service.ProcessWebhook(context.Background(), WebhookRequest{
		PaymentID:             "131",
		ExternalTransactionID: "ext-metadata",
		Status:                "success",
	}, "127.0.0.1", "Go-Test")
	if err != nil {
		t.Fatalf("ProcessWebhook failed: %v", err)
	}

	metadata := repo.providerLedgerEntries[0].Metadata + repo.depositLedgerEntries[0].Metadata
	metadata = strings.ToLower(metadata)
	for _, forbidden := range []string{"credential", "token", "password", "secret", "authorization", "key"} {
		if strings.Contains(metadata, forbidden) {
			t.Fatalf("financial metadata contains forbidden term %q: %s", forbidden, metadata)
		}
	}
}

func TestPaymentService_PayRentalWithBalance_Success(t *testing.T) {
	repo := newMockRepository(nil)
	seedWalletPaymentState(repo, &WalletPaymentState{
		PaymentID:        150,
		RentalID:         250,
		UserID:           350,
		AccountID:        450,
		Provider:         webhookPaymentProvider,
		PaymentStatus:    1,
		RentalStatus:     1,
		AccountStatus:    3,
		RentalPrice:      500,
		DepositAmount:    700,
		PaymentExpiresAt: time.Now().Add(time.Hour),
		Currency:         "USD",
		UserBalance:      3000,
	})
	service := NewPaymentService(repo)

	res, err := service.PayRentalWithBalance(context.Background(), 350, 250, "127.0.0.1", "Go-Test", time.Now())
	if err != nil {
		t.Fatalf("PayRentalWithBalance failed: %v", err)
	}
	if !res.Changed || res.Idempotent || res.PaymentProvider != walletPaymentProvider {
		t.Fatalf("unexpected wallet payment result: %+v", res)
	}
	state := repo.walletStatesByRental[250]
	if state.UserBalance != 1800 || state.PaymentStatus != 2 || state.RentalStatus != 2 || state.AccountStatus != 4 {
		t.Fatalf("unexpected wallet state after payment: %+v", state)
	}
	if len(repo.balanceDebitEntries) != 1 || repo.balanceDebitEntries[0].Amount != 1200 {
		t.Fatalf("expected one balance debit ledger entry, got %+v", repo.balanceDebitEntries)
	}
	if len(repo.depositHolds) != 1 || len(repo.depositLedgerEntries) != 1 {
		t.Fatalf("expected one deposit hold and one deposit ledger entry")
	}
	if repo.securityEventCalls != 1 || repo.auditCalls != 1 || repo.balanceDebitCalls != 1 {
		t.Fatalf("expected one security event, one audit log and one balance debit, got security=%d audit=%d debit=%d", repo.securityEventCalls, repo.auditCalls, repo.balanceDebitCalls)
	}
}

func TestPaymentService_PayRentalWithBalance_InsufficientBalance(t *testing.T) {
	repo := newMockRepository(nil)
	seedWalletPaymentState(repo, &WalletPaymentState{
		PaymentID:        151,
		RentalID:         251,
		UserID:           351,
		AccountID:        451,
		Provider:         webhookPaymentProvider,
		PaymentStatus:    1,
		RentalStatus:     1,
		AccountStatus:    3,
		RentalPrice:      500,
		DepositAmount:    700,
		PaymentExpiresAt: time.Now().Add(time.Hour),
		Currency:         "USD",
		UserBalance:      900,
	})
	service := NewPaymentService(repo)

	_, err := service.PayRentalWithBalance(context.Background(), 351, 251, "127.0.0.1", "Go-Test", time.Now())
	if !errors.Is(err, ErrWalletInsufficientBalance) {
		t.Fatalf("expected ErrWalletInsufficientBalance, got %v", err)
	}
	state := repo.walletStatesByRental[251]
	if state.UserBalance != 900 || state.PaymentStatus != 1 || state.RentalStatus != 1 || state.AccountStatus != 3 {
		t.Fatalf("wallet state changed unexpectedly on insufficient balance: %+v", state)
	}
	if len(repo.balanceDebitEntries) != 0 || len(repo.depositHolds) != 0 || len(repo.depositLedgerEntries) != 0 {
		t.Fatalf("expected no financial side effects on insufficient balance")
	}
}

func TestPaymentService_PayRentalWithBalance_ReplayIsIdempotent(t *testing.T) {
	repo := newMockRepository(nil)
	seedWalletPaymentState(repo, &WalletPaymentState{
		PaymentID:        152,
		RentalID:         252,
		UserID:           352,
		AccountID:        452,
		Provider:         walletPaymentProvider,
		PaymentStatus:    2,
		RentalStatus:     2,
		AccountStatus:    4,
		RentalPrice:      500,
		DepositAmount:    300,
		PaymentExpiresAt: time.Now().Add(time.Hour),
		Currency:         "USD",
		UserBalance:      2200,
	})
	service := NewPaymentService(repo)

	res, err := service.PayRentalWithBalance(context.Background(), 352, 252, "127.0.0.1", "Go-Test", time.Now())
	if err != nil {
		t.Fatalf("replayed PayRentalWithBalance failed: %v", err)
	}
	if !res.Idempotent || res.Changed {
		t.Fatalf("expected idempotent wallet replay result, got %+v", res)
	}
	if repo.balanceDebitCalls != 0 || len(repo.balanceDebitEntries) != 0 || len(repo.depositHolds) != 0 || len(repo.depositLedgerEntries) != 0 {
		t.Fatalf("expected no duplicate balance debit or deposit records on replay")
	}
}

func TestPaymentService_PayRentalWithBalance_RollbackOnLedgerFailure(t *testing.T) {
	repo := newMockRepository(nil)
	repo.ledgerErr = errors.New("ledger insert failed")
	seedWalletPaymentState(repo, &WalletPaymentState{
		PaymentID:        153,
		RentalID:         253,
		UserID:           353,
		AccountID:        453,
		Provider:         webhookPaymentProvider,
		PaymentStatus:    1,
		RentalStatus:     1,
		AccountStatus:    3,
		RentalPrice:      500,
		DepositAmount:    300,
		PaymentExpiresAt: time.Now().Add(time.Hour),
		Currency:         "USD",
		UserBalance:      2200,
	})
	service := NewPaymentService(repo)

	_, err := service.PayRentalWithBalance(context.Background(), 353, 253, "127.0.0.1", "Go-Test", time.Now())
	if err == nil {
		t.Fatalf("expected wallet ledger failure")
	}
	state := repo.walletStatesByRental[253]
	if state.UserBalance != 2200 || state.PaymentStatus != 1 || state.RentalStatus != 1 || state.AccountStatus != 3 {
		t.Fatalf("expected rollback to preserve wallet state, got %+v", state)
	}
	if len(repo.balanceDebitEntries) != 0 || len(repo.depositHolds) != 0 || len(repo.depositLedgerEntries) != 0 || repo.auditCalls != 0 || repo.securityEventCalls != 0 {
		t.Fatalf("expected rollback to suppress wallet side effects")
	}
}

func TestPaymentService_ReleaseDeposit_Success(t *testing.T) {
	repo := newMockRepository(nil)
	seedSettlementState(repo, &DepositSettlementState{
		HoldID:        1,
		RentalID:      200,
		UserID:        300,
		AccountID:     400,
		PaymentID:     500,
		HoldStatus:    depositHoldStatusHeld,
		RentalStatus:  3,
		PaymentStatus: 2,
		Amount:        700,
		Currency:      "USD",
		UserBalance:   1000,
	}, &DepositSettlementEligibility{RentalExists: true})
	service := NewPaymentService(repo)

	res, err := service.ReleaseDeposit(context.Background(), 900, "ADMIN", 200, time.Now())
	if err != nil {
		t.Fatalf("ReleaseDeposit failed: %v", err)
	}
	if !res.Changed || res.Status != "RELEASED" {
		t.Fatalf("unexpected release result: %+v", res)
	}
	state := repo.settlementStatesByRental[200]
	if state.HoldStatus != depositHoldStatusReleased || state.UserBalance != 1700 {
		t.Fatalf("unexpected settlement state after release: %+v", state)
	}
	if len(repo.depositReleaseEntries) != 1 {
		t.Fatalf("expected one release ledger entry, got %d", len(repo.depositReleaseEntries))
	}
	if repo.auditCalls != 1 || repo.securityEventCalls != 1 || repo.balanceCreditCalls != 1 {
		t.Fatalf("expected one audit/security/balance mutation, got audit=%d security=%d balance=%d", repo.auditCalls, repo.securityEventCalls, repo.balanceCreditCalls)
	}
}

func TestPaymentService_ReleaseDeposit_ReplayIsNoOp(t *testing.T) {
	repo := newMockRepository(nil)
	seedSettlementState(repo, &DepositSettlementState{
		HoldID:        1,
		RentalID:      201,
		UserID:        301,
		AccountID:     401,
		PaymentID:     501,
		HoldStatus:    depositHoldStatusReleased,
		RentalStatus:  3,
		PaymentStatus: 2,
		Amount:        700,
		Currency:      "USD",
		UserBalance:   1700,
	}, &DepositSettlementEligibility{RentalExists: true})
	service := NewPaymentService(repo)

	res, err := service.ReleaseDeposit(context.Background(), 900, "ADMIN", 201, time.Now())
	if err != nil {
		t.Fatalf("replayed ReleaseDeposit failed: %v", err)
	}
	if res.Changed {
		t.Fatalf("expected replay to be no-op, got %+v", res)
	}
	if repo.balanceCreditCalls != 0 || repo.auditCalls != 0 || repo.securityEventCalls != 0 || len(repo.depositReleaseEntries) != 0 {
		t.Fatalf("expected no duplicate side effects on replay")
	}
}

func TestPaymentService_ForfeitDeposit_Success(t *testing.T) {
	repo := newMockRepository(nil)
	seedSettlementState(repo, &DepositSettlementState{
		HoldID:        2,
		RentalID:      210,
		UserID:        310,
		AccountID:     410,
		PaymentID:     510,
		HoldStatus:    depositHoldStatusHeld,
		RentalStatus:  3,
		PaymentStatus: 2,
		Amount:        900,
		Currency:      "USD",
		UserBalance:   1000,
	}, &DepositSettlementEligibility{RentalExists: true})
	service := NewPaymentService(repo)

	res, err := service.ForfeitDeposit(context.Background(), 901, "ADMIN", 210, "damage_confirmed", time.Now())
	if err != nil {
		t.Fatalf("ForfeitDeposit failed: %v", err)
	}
	if !res.Changed || res.Status != "FORFEITED" {
		t.Fatalf("unexpected forfeit result: %+v", res)
	}
	state := repo.settlementStatesByRental[210]
	if state.HoldStatus != depositHoldStatusForfeited || state.UserBalance != 1000 {
		t.Fatalf("unexpected settlement state after forfeit: %+v", state)
	}
	if len(repo.depositForfeitEntries) != 1 {
		t.Fatalf("expected one forfeit ledger entry, got %d", len(repo.depositForfeitEntries))
	}
	if repo.balanceCreditCalls != 0 || repo.auditCalls != 1 || repo.securityEventCalls != 1 {
		t.Fatalf("expected no balance credit and one audit/security event")
	}
}

func TestPaymentService_ForfeitDeposit_NonAdminRejected(t *testing.T) {
	service := NewPaymentService(newMockRepository(nil))

	_, err := service.ForfeitDeposit(context.Background(), 901, "RENT", 210, "damage_confirmed", time.Now())
	if !errors.Is(err, ErrAdminRequired) {
		t.Fatalf("expected ErrAdminRequired, got %v", err)
	}
}

func TestPaymentService_DepositSettlement_RollbackOnLedgerFailure(t *testing.T) {
	repo := newMockRepository(nil)
	repo.ledgerErr = errors.New("ledger insert failed")
	seedSettlementState(repo, &DepositSettlementState{
		HoldID:        3,
		RentalID:      220,
		UserID:        320,
		AccountID:     420,
		PaymentID:     520,
		HoldStatus:    depositHoldStatusHeld,
		RentalStatus:  3,
		PaymentStatus: 2,
		Amount:        800,
		Currency:      "USD",
		UserBalance:   1000,
	}, &DepositSettlementEligibility{RentalExists: true})
	service := NewPaymentService(repo)

	_, err := service.ReleaseDeposit(context.Background(), 901, "ADMIN", 220, time.Now())
	if err == nil {
		t.Fatalf("expected ledger failure")
	}
	state := repo.settlementStatesByRental[220]
	if state.HoldStatus != depositHoldStatusHeld || state.UserBalance != 1000 {
		t.Fatalf("expected rollback to preserve hold and balance, got %+v", state)
	}
	if repo.auditCalls != 0 || repo.securityEventCalls != 0 || len(repo.depositReleaseEntries) != 0 {
		t.Fatalf("expected rollback to suppress audit/security/ledger side effects")
	}
}

func TestPaymentService_DepositSettlement_OppositeSettlementRejected(t *testing.T) {
	repo := newMockRepository(nil)
	seedSettlementState(repo, &DepositSettlementState{
		HoldID:        4,
		RentalID:      230,
		UserID:        330,
		AccountID:     430,
		PaymentID:     530,
		HoldStatus:    depositHoldStatusForfeited,
		RentalStatus:  3,
		PaymentStatus: 2,
		Amount:        800,
		Currency:      "USD",
		UserBalance:   1000,
	}, &DepositSettlementEligibility{RentalExists: true})
	service := NewPaymentService(repo)

	_, err := service.ReleaseDeposit(context.Background(), 901, "ADMIN", 230, time.Now())
	if !errors.Is(err, ErrDepositAlreadySettled) {
		t.Fatalf("expected ErrDepositAlreadySettled, got %v", err)
	}
}

func TestPaymentService_ProcessWebhook_ExpiredReservationRejected(t *testing.T) {
	repo := newMockRepository(&WebhookPaymentState{
		PaymentID:        101,
		RentalID:         202,
		UserID:           303,
		AccountID:        404,
		Provider:         webhookPaymentProvider,
		Status:           1,
		Amount:           1500,
		Currency:         "USD",
		PaymentExpiresAt: time.Now().Add(-time.Minute),
		RentalStatus:     1,
		AccountStatus:    3,
	})
	service := NewPaymentService(repo)

	_, err := service.ProcessWebhook(context.Background(), WebhookRequest{
		PaymentID:             "101",
		ExternalTransactionID: "ext-expired",
		Status:                "success",
	}, "127.0.0.1", "Go-Test")
	if !errors.Is(err, ErrRentalNotEligible) {
		t.Fatalf("expected expiry rejection, got %v", err)
	}
	if state := repo.statesByID[101]; state.Status != 1 || state.RentalStatus != 1 || state.AccountStatus != 3 {
		t.Fatalf("expected rollback to preserve original state, got %+v", state)
	}
}

func TestPaymentService_ProcessWebhook_CancelledRentalRejected(t *testing.T) {
	repo := newMockRepository(&WebhookPaymentState{
		PaymentID:        101,
		RentalID:         202,
		UserID:           303,
		AccountID:        404,
		Provider:         webhookPaymentProvider,
		Status:           1,
		Amount:           1500,
		Currency:         "USD",
		PaymentExpiresAt: time.Now().Add(time.Hour),
		RentalStatus:     5,
		AccountStatus:    3,
	})
	service := NewPaymentService(repo)

	_, err := service.ProcessWebhook(context.Background(), WebhookRequest{
		PaymentID:             "101",
		ExternalTransactionID: "ext-cancelled",
		Status:                "success",
	}, "127.0.0.1", "Go-Test")
	if !errors.Is(err, ErrWebhookInvalidTransition) {
		t.Fatalf("expected invalid transition error, got %v", err)
	}
}

func TestPaymentService_ProcessWebhook_AccountNotReservedRejected(t *testing.T) {
	repo := newMockRepository(&WebhookPaymentState{
		PaymentID:        101,
		RentalID:         202,
		UserID:           303,
		AccountID:        404,
		Provider:         webhookPaymentProvider,
		Status:           1,
		Amount:           1500,
		Currency:         "USD",
		PaymentExpiresAt: time.Now().Add(time.Hour),
		RentalStatus:     1,
		AccountStatus:    2,
	})
	service := NewPaymentService(repo)

	_, err := service.ProcessWebhook(context.Background(), WebhookRequest{
		PaymentID:             "101",
		ExternalTransactionID: "ext-account",
		Status:                "success",
	}, "127.0.0.1", "Go-Test")
	if !errors.Is(err, ErrWebhookInvalidTransition) {
		t.Fatalf("expected account state error, got %v", err)
	}
}

func TestPaymentService_ProcessWebhook_RollbackOnAccountUpdateFailure(t *testing.T) {
	repo := newMockRepository(&WebhookPaymentState{
		PaymentID:        101,
		RentalID:         202,
		UserID:           303,
		AccountID:        404,
		Provider:         webhookPaymentProvider,
		Status:           1,
		Amount:           1500,
		Currency:         "USD",
		PaymentExpiresAt: time.Now().Add(time.Hour),
		RentalStatus:     1,
		AccountStatus:    3,
	})
	repo.rentErr = errors.New("account update failed")
	service := NewPaymentService(repo)

	_, err := service.ProcessWebhook(context.Background(), WebhookRequest{
		PaymentID:             "101",
		ExternalTransactionID: "ext-rb",
		Status:                "success",
	}, "127.0.0.1", "Go-Test")
	if err == nil {
		t.Fatalf("expected transaction error")
	}

	state := repo.statesByID[101]
	if state.Status != 1 || state.RentalStatus != 1 || state.AccountStatus != 3 {
		t.Fatalf("expected rollback to restore original state, got %+v", state)
	}
}
