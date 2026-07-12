package rental

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"rent_game_accs/internal/account"
	"rent_game_accs/internal/user"
)

type mockTxManager struct{}

func (m mockTxManager) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

type mockRentalRepo struct {
	createFunc         func(ctx context.Context, r *Rental) error
	cancelFunc         func(ctx context.Context, rentalID, userID int64, reason string, now time.Time) (bool, error)
	created            *Rental
	credentialsRec     *RentalCredentialsRecord
	credentialsErr     error
	credentialEvent    *CredentialIssueEvent
	credentialEventErr error
	cancelCalled       bool
	cancelRentalID     int64
	cancelUserID       int64
	cancelReason       string
	cancelChanged      bool
	cancelErr          error
}

func (m *mockRentalRepo) CreateRental(ctx context.Context, r *Rental) error {
	m.created = r
	if m.createFunc != nil {
		return m.createFunc(ctx, r)
	}
	return nil
}

func (m *mockRentalRepo) GetRental(ctx context.Context, id int64) (*Rental, error) {
	return nil, ErrRentalNotFound
}

func (m *mockRentalRepo) GetRentalCredentials(ctx context.Context, rentalID, userID int64, now time.Time) (*RentalCredentialsRecord, error) {
	if m.credentialsErr != nil {
		return nil, m.credentialsErr
	}
	return m.credentialsRec, nil
}

func (m *mockRentalRepo) RecordCredentialIssued(ctx context.Context, event CredentialIssueEvent) error {
	m.credentialEvent = &event
	return m.credentialEventErr
}

func (m *mockRentalRepo) CancelWaitingPaymentRental(ctx context.Context, rentalID, userID int64, reason string, now time.Time) (bool, error) {
	m.cancelCalled = true
	m.cancelRentalID = rentalID
	m.cancelUserID = userID
	m.cancelReason = reason
	if m.cancelFunc != nil {
		return m.cancelFunc(ctx, rentalID, userID, reason, now)
	}
	return m.cancelChanged, m.cancelErr
}

type mockAccountRepo struct {
	account       *account.Account
	updated       *account.Account
	getErr        error
	updateErr     error
	decryptFunc   func([]byte) (string, error)
	decryptCalled bool
}

func (m *mockAccountRepo) CreateAccount(ctx context.Context, a *account.Account) error { return nil }
func (m *mockAccountRepo) GetAccount(ctx context.Context, id int64) (*account.Account, error) {
	return nil, account.ErrAccountNotFound
}
func (m *mockAccountRepo) GetAccountForUpdate(ctx context.Context, id int64) (*account.Account, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.account, nil
}
func (m *mockAccountRepo) ReserveAccount(ctx context.Context, id int64, now time.Time) error {
	m.updated = m.account
	if m.updateErr != nil {
		return m.updateErr
	}
	return nil
}
func (m *mockAccountRepo) ListAccounts(ctx context.Context, limit, offset int) ([]*account.Account, error) {
	return nil, nil
}
func (m *mockAccountRepo) SearchAccounts(ctx context.Context, limit, offset int, search string, gameID int64, minPrice, maxPrice int64, status string) ([]*account.Account, error) {
	return nil, nil
}
func (m *mockAccountRepo) SyncAccountGames(ctx context.Context, accountID int64, games []account.AccountGame) error {
	return nil
}
func (m *mockAccountRepo) Encrypt(plaintext string) ([]byte, error) { return nil, nil }
func (m *mockAccountRepo) Decrypt(ciphertext []byte) (string, error) {
	m.decryptCalled = true
	if m.decryptFunc != nil {
		return m.decryptFunc(ciphertext)
	}
	return "decrypted-password", nil
}

type mockUserRepo struct {
	user *user.User
}

func (m *mockUserRepo) CreateUser(ctx context.Context, u *user.User) error { return nil }
func (m *mockUserRepo) GetUser(ctx context.Context, id int64) (*user.User, error) {
	return m.user, nil
}
func (m *mockUserRepo) GetUserByEmail(ctx context.Context, email string) (*user.User, error) {
	return nil, user.ErrInvalidEmail
}
func (m *mockUserRepo) UpdateUser(ctx context.Context, u *user.User) error { return nil }
func (m *mockUserRepo) ListUsers(ctx context.Context, limit, offset int) ([]*user.User, error) {
	return nil, nil
}

type mockPaymentRepo struct {
	createCalled bool
	rentalID     int64
	userID       int64
	amount       int64
	currency     string
	createErr    error
}

func (m *mockPaymentRepo) CreatePendingPayment(ctx context.Context, rentalID, userID int64, amount int64, currency string) (int64, error) {
	m.createCalled = true
	m.rentalID = rentalID
	m.userID = userID
	m.amount = amount
	m.currency = currency
	if m.createErr != nil {
		return 0, m.createErr
	}
	return 999, nil
}

func TestService_RentAccount_CreatesWaitingPaymentAndReservesAccount(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	creds := account.SteamCredentials{Login: "steam_login", EncryptedPassword: []byte("enc-pass"), SteamID64: "76561198000000001"}
	price, _ := account.NewMoney(200, "USD")
	deposit, _ := account.NewMoney(1000, "USD")
	acc := &account.Account{ID: 55, Credentials: creds, HourlyPrice: price, DepositAmount: deposit, Status: account.StatusAvailable}
	u := &user.User{ID: 77, Email: "buyer@example.com", Balance: 5000}

	rentalRepo := &mockRentalRepo{}
	accountRepo := &mockAccountRepo{account: acc}
	userRepo := &mockUserRepo{user: u}
	paymentRepo := &mockPaymentRepo{}
	service := NewService(rentalRepo, accountRepo, userRepo, paymentRepo, mockTxManager{})

	rent, err := service.RentAccount(context.Background(), u.ID, acc.ID, 2*time.Hour, now)
	if err != nil {
		t.Fatalf("RentAccount failed: %v", err)
	}

	if rent.Status != StatusWaitingPayment {
		t.Fatalf("expected rental status WAITING_PAYMENT, got %v", rent.Status)
	}
	if accountRepo.updated == nil || accountRepo.updated.Status != account.StatusReserved {
		t.Fatalf("expected account status RESERVED, got %+v", accountRepo.updated)
	}
	if rentalRepo.created == nil || rentalRepo.created.Status != StatusWaitingPayment {
		t.Fatalf("expected created rental to be WAITING_PAYMENT, got %+v", rentalRepo.created)
	}
	if !paymentRepo.createCalled {
		t.Fatalf("expected pending payment creation to be called")
	}
	if paymentRepo.rentalID != rent.ID || paymentRepo.userID != u.ID {
		t.Fatalf("unexpected payment refs: rental=%d user=%d", paymentRepo.rentalID, paymentRepo.userID)
	}
	if paymentRepo.amount != 1400 || paymentRepo.currency != "USD" {
		t.Fatalf("unexpected payment payload: amount=%d currency=%s", paymentRepo.amount, paymentRepo.currency)
	}
}

func TestService_RentAccount_PaymentFailureReturnsError(t *testing.T) {
	now := time.Now()
	creds := account.SteamCredentials{Login: "steam_login", EncryptedPassword: []byte("enc-pass"), SteamID64: "76561198000000001"}
	price, _ := account.NewMoney(100, "USD")
	deposit, _ := account.NewMoney(50, "USD")
	acc := &account.Account{ID: 55, Credentials: creds, HourlyPrice: price, DepositAmount: deposit, Status: account.StatusAvailable}
	u := &user.User{ID: 77, Email: "buyer@example.com", Balance: 5000}

	rentalRepo := &mockRentalRepo{}
	accountRepo := &mockAccountRepo{account: acc}
	userRepo := &mockUserRepo{user: u}
	paymentRepo := &mockPaymentRepo{createErr: errors.New("payment insert failed")}
	service := NewService(rentalRepo, accountRepo, userRepo, paymentRepo, mockTxManager{})

	_, err := service.RentAccount(context.Background(), u.ID, acc.ID, time.Hour, now)
	if err == nil {
		t.Fatalf("expected error when pending payment creation fails")
	}
}

func TestCalculateRentalTotalRejectsOverflow(t *testing.T) {
	tests := []struct {
		name    string
		hourly  int64
		deposit int64
		hours   int64
	}{
		{name: "multiplication", hourly: math.MaxInt64/2 + 1, hours: 2},
		{name: "deposit addition", hourly: math.MaxInt64, deposit: 1, hours: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := CalculateRentalTotal(tt.hourly, tt.deposit, tt.hours); !errors.Is(err, ErrRentalPriceOverflow) {
				t.Fatalf("expected ErrRentalPriceOverflow, got %v", err)
			}
		})
	}
}

func TestService_RentAccount_OverflowDoesNotCreateFinancialState(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	price, _ := account.NewMoney(math.MaxInt64/2+1, "USD")
	deposit, _ := account.NewMoney(0, "USD")
	acc := &account.Account{ID: 55, HourlyPrice: price, DepositAmount: deposit, Status: account.StatusAvailable}
	rentalRepo := &mockRentalRepo{}
	accountRepo := &mockAccountRepo{account: acc}
	paymentRepo := &mockPaymentRepo{}
	service := NewService(rentalRepo, accountRepo, &mockUserRepo{user: &user.User{ID: 77, Balance: math.MaxInt64}}, paymentRepo, mockTxManager{})

	got, err := service.RentAccount(context.Background(), 77, 55, 2*time.Hour, now)
	if !errors.Is(err, ErrRentalPriceOverflow) {
		t.Fatalf("expected ErrRentalPriceOverflow, got rental=%+v err=%v", got, err)
	}
	if accountRepo.updated != nil || rentalRepo.created != nil || paymentRepo.createCalled {
		t.Fatalf("overflow created state: account=%+v rental=%+v payment_called=%t", accountRepo.updated, rentalRepo.created, paymentRepo.createCalled)
	}
}

func TestService_GetRentalCredentials_ReturnsCredentialsOnlyForEligibleRental(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	price, _ := account.NewMoney(200, "USD")
	deposit, _ := account.NewMoney(1000, "USD")
	acc := &account.Account{ID: 55, HourlyPrice: price, DepositAmount: deposit, Status: account.StatusRented}
	accountRepo := &mockAccountRepo{account: acc, decryptFunc: func(ciphertext []byte) (string, error) {
		if string(ciphertext) != "enc-pass" {
			t.Fatalf("unexpected ciphertext passed to decrypt: %q", string(ciphertext))
		}
		return "steam_secret_password", nil
	}}
	rentalRepo := &mockRentalRepo{
		credentialsRec: &RentalCredentialsRecord{
			RentalID:          101,
			UserID:            77,
			AccountID:         55,
			RentalStatus:      StatusActive,
			AccountStatus:     int16(account.StatusRented),
			PaymentExpiresAt:  now.Add(30 * time.Minute),
			Login:             "steam_login",
			EncryptedPassword: []byte("enc-pass"),
			SteamID64:         "76561198000000001",
		},
	}
	service := NewService(rentalRepo, accountRepo, &mockUserRepo{}, &mockPaymentRepo{}, mockTxManager{})

	creds, err := service.GetRentalCredentials(context.Background(), 77, 101, CredentialRequestContext{IPAddress: "127.0.0.1", UserAgent: "test-agent"}, now)
	if err != nil {
		t.Fatalf("GetRentalCredentials failed: %v", err)
	}
	if creds.Login != "steam_login" || creds.Password != "steam_secret_password" || creds.SteamID64 != "76561198000000001" {
		t.Fatalf("unexpected credentials returned: %+v", creds)
	}
	if !accountRepo.decryptCalled {
		t.Fatalf("expected decrypt to be called after checks")
	}
	if rentalRepo.credentialEvent == nil || rentalRepo.credentialEvent.UserID != 77 || rentalRepo.credentialEvent.AccountID != 55 || rentalRepo.credentialEvent.RentalID != 101 {
		t.Fatalf("expected credential issuance event inside service flow, got %+v", rentalRepo.credentialEvent)
	}
}

func TestService_GetRentalCredentials_AuditFailureReturnsNoCredentials(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	rentalRepo := &mockRentalRepo{
		credentialsRec: &RentalCredentialsRecord{
			RentalID:          104,
			UserID:            77,
			AccountID:         55,
			RentalStatus:      StatusActive,
			AccountStatus:     int16(account.StatusRented),
			PaymentID:         900,
			PaymentExpiresAt:  now.Add(-time.Hour),
			Login:             "steam_login",
			EncryptedPassword: []byte("enc-pass"),
			SteamID64:         "76561198000000001",
		},
		credentialEventErr: errors.New("security event insert failed"),
	}
	service := NewService(rentalRepo, &mockAccountRepo{}, &mockUserRepo{}, &mockPaymentRepo{}, mockTxManager{})

	creds, err := service.GetRentalCredentials(context.Background(), 77, 104, CredentialRequestContext{}, now)
	if err == nil || !strings.Contains(err.Error(), "record credential issuance") {
		t.Fatalf("expected audit failure, got credentials=%+v err=%v", creds, err)
	}
	if creds != nil {
		t.Fatalf("audit failure disclosed credentials: %+v", creds)
	}
}

func TestService_GetRentalCredentials_DeniesIneligibleRentalWithoutDecrypt(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	accountRepo := &mockAccountRepo{decryptFunc: func(ciphertext []byte) (string, error) {
		t.Fatalf("decrypt should not be called for ineligible rental")
		return "", nil
	}}
	rentalRepo := &mockRentalRepo{
		credentialsRec: &RentalCredentialsRecord{
			RentalID:          102,
			UserID:            77,
			AccountID:         55,
			RentalStatus:      StatusWaitingPayment,
			AccountStatus:     int16(account.StatusReserved),
			PaymentExpiresAt:  now.Add(30 * time.Minute),
			Login:             "steam_login",
			EncryptedPassword: []byte("enc-pass"),
			SteamID64:         "76561198000000001",
		},
	}
	service := NewService(rentalRepo, accountRepo, &mockUserRepo{}, &mockPaymentRepo{}, mockTxManager{})

	creds, err := service.GetRentalCredentials(context.Background(), 77, 102, CredentialRequestContext{}, now)
	if !errors.Is(err, ErrCredentialsNotAvailable) {
		t.Fatalf("expected ErrCredentialsNotAvailable, got %v", err)
	}
	if creds != nil {
		t.Fatalf("expected no credentials, got %+v", creds)
	}
	if accountRepo.decryptCalled {
		t.Fatalf("expected decrypt not to be called")
	}
}

func TestService_GetRentalCredentials_DeniesExpiredRentalWithoutDecrypt(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	accountRepo := &mockAccountRepo{decryptFunc: func(ciphertext []byte) (string, error) {
		t.Fatalf("decrypt should not be called for expired rental")
		return "", nil
	}}
	rentalRepo := &mockRentalRepo{
		credentialsRec: &RentalCredentialsRecord{
			RentalID:          103,
			UserID:            77,
			AccountID:         55,
			RentalStatus:      StatusExpired,
			AccountStatus:     int16(account.StatusAvailable),
			PaymentExpiresAt:  now.Add(30 * time.Minute),
			Login:             "steam_login",
			EncryptedPassword: []byte("enc-pass"),
			SteamID64:         "76561198000000001",
		},
	}
	service := NewService(rentalRepo, accountRepo, &mockUserRepo{}, &mockPaymentRepo{}, mockTxManager{})

	creds, err := service.GetRentalCredentials(context.Background(), 77, 103, CredentialRequestContext{}, now)
	if !errors.Is(err, ErrCredentialsNotAvailable) {
		t.Fatalf("expected ErrCredentialsNotAvailable, got %v", err)
	}
	if creds != nil {
		t.Fatalf("expected no credentials, got %+v", creds)
	}
	if accountRepo.decryptCalled {
		t.Fatalf("expected decrypt not to be called")
	}
}

func TestService_CancelRental_DelegatesWaitingPaymentTransition(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	rentalRepo := &mockRentalRepo{cancelChanged: true}
	service := NewService(rentalRepo, &mockAccountRepo{}, &mockUserRepo{}, &mockPaymentRepo{}, mockTxManager{})

	result, err := service.CancelRental(context.Background(), 77, 101, "user requested", now)
	if err != nil {
		t.Fatalf("CancelRental failed: %v", err)
	}
	if result == nil || !result.Changed {
		t.Fatalf("expected changed cancel result, got %+v", result)
	}
	if !rentalRepo.cancelCalled || rentalRepo.cancelRentalID != 101 || rentalRepo.cancelUserID != 77 || rentalRepo.cancelReason != "user requested" {
		t.Fatalf("unexpected cancel repository call: %+v", rentalRepo)
	}
}

func TestService_CancelRental_RepeatedCancelNoOp(t *testing.T) {
	rentalRepo := &mockRentalRepo{cancelChanged: false}
	service := NewService(rentalRepo, &mockAccountRepo{}, &mockUserRepo{}, &mockPaymentRepo{}, mockTxManager{})

	result, err := service.CancelRental(context.Background(), 77, 101, "user requested", time.Now())
	if err != nil {
		t.Fatalf("repeated CancelRental should be a no-op: %v", err)
	}
	if result == nil || result.Changed {
		t.Fatalf("expected no-op cancel result, got %+v", result)
	}
}

func TestService_CancelRental_UsesErrorsIsForWrappedCannotCancel(t *testing.T) {
	rentalRepo := &mockRentalRepo{cancelErr: ErrCannotCancel}
	service := NewService(rentalRepo, &mockAccountRepo{}, &mockUserRepo{}, &mockPaymentRepo{}, mockTxManager{})

	_, err := service.CancelRental(context.Background(), 77, 101, "too late", time.Now())
	if !errors.Is(err, ErrCannotCancel) {
		t.Fatalf("expected wrapped ErrCannotCancel to match with errors.Is, got %v", err)
	}
}

func TestService_GetRentalCredentials_PaymentExpiredActivePaidAllowsCredentials(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	price, _ := account.NewMoney(200, "USD")
	deposit, _ := account.NewMoney(1000, "USD")
	acc := &account.Account{ID: 55, HourlyPrice: price, DepositAmount: deposit, Status: account.StatusRented}
	accountRepo := &mockAccountRepo{account: acc, decryptFunc: func(ciphertext []byte) (string, error) {
		if string(ciphertext) != "enc-pass" {
			t.Fatalf("unexpected ciphertext passed to decrypt: %q", string(ciphertext))
		}
		return "steam_secret_password", nil
	}}
	rentalRepo := &mockRentalRepo{
		credentialsRec: &RentalCredentialsRecord{
			RentalID:          101,
			UserID:            77,
			AccountID:         55,
			RentalStatus:      StatusActive,
			AccountStatus:     int16(account.StatusRented),
			PaymentExpiresAt:  now.Add(-5 * time.Minute), // Expired in the past!
			Login:             "steam_login",
			EncryptedPassword: []byte("enc-pass"),
			SteamID64:         "76561198000000001",
		},
	}
	service := NewService(rentalRepo, accountRepo, &mockUserRepo{}, &mockPaymentRepo{}, mockTxManager{})

	creds, err := service.GetRentalCredentials(context.Background(), 77, 101, CredentialRequestContext{}, now)
	if err != nil {
		t.Fatalf("GetRentalCredentials failed: %v", err)
	}
	if creds.AccountID != 55 {
		t.Fatalf("expected AccountID 55, got %d", creds.AccountID)
	}
	if creds.Login != "steam_login" || creds.Password != "steam_secret_password" || creds.SteamID64 != "76561198000000001" {
		t.Fatalf("unexpected credentials returned: %+v", creds)
	}
	if !accountRepo.decryptCalled {
		t.Fatalf("expected decrypt to be called after checks")
	}
}
