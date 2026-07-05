package payment

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"strconv"
	"testing"
)

type mockRepository struct {
	withinTxFunc      func(ctx context.Context, fn func(ctx context.Context) error) error
	updatePaymentFunc func(ctx context.Context, paymentID int64, extTxID string) (int64, int64, int64, string, error)
	activateFunc      func(ctx context.Context, rentalID int64) (int64, error)
	markRentedFunc    func(ctx context.Context, accountID int64) (string, []byte, string, error)
	logSecurityFunc   func(ctx context.Context, userID, accountID, rentalID int64, clientIP, userAgent string, metadata []byte) error
}

func (m *mockRepository) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if m.withinTxFunc != nil {
		return m.withinTxFunc(ctx, fn)
	}
	return fn(ctx)
}

func (m *mockRepository) UpdatePaymentSuccess(ctx context.Context, paymentID int64, extTxID string) (int64, int64, int64, string, error) {
	return m.updatePaymentFunc(ctx, paymentID, extTxID)
}

func (m *mockRepository) ActivateRental(ctx context.Context, rentalID int64) (int64, error) {
	return m.activateFunc(ctx, rentalID)
}

func (m *mockRepository) MarkAccountRented(ctx context.Context, accountID int64) (string, []byte, string, error) {
	return m.markRentedFunc(ctx, accountID)
}

func (m *mockRepository) LogSecurityEvent(ctx context.Context, userID, accountID, rentalID int64, clientIP, userAgent string, metadata []byte) error {
	return m.logSecurityFunc(ctx, userID, accountID, rentalID, clientIP, userAgent, metadata)
}

func encryptHelper(plaintext string, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
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
	return gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

func TestPaymentService_ProcessWebhook_Success(t *testing.T) {
	encryptionKey := []byte("super-secret-32-byte-key-for-aes")
	os.Setenv("ENCRYPTION_KEY", string(encryptionKey))
	defer os.Unsetenv("ENCRYPTION_KEY")

	rawPassword := "decrypted-steam-pass-99"
	encPassword, err := encryptHelper(rawPassword, encryptionKey)
	if err != nil {
		t.Fatalf("failed to encrypt password: %v", err)
	}

	paymentID := int64(101)
	rentalID := int64(202)
	userID := int64(303)
	accountID := int64(404)
	extTxID := "ext-tx-12345"

	repo := &mockRepository{
		updatePaymentFunc: func(ctx context.Context, pID int64, txID string) (int64, int64, int64, string, error) {
			if pID != paymentID {
				t.Errorf("expected paymentID %d, got %d", paymentID, pID)
			}
			if txID != extTxID {
				t.Errorf("expected external tx ID %s, got %s", extTxID, txID)
			}
			return rentalID, userID, 1500, "USD", nil
		},
		activateFunc: func(ctx context.Context, rID int64) (int64, error) {
			if rID != rentalID {
				t.Errorf("expected rentalID %d, got %d", rentalID, rID)
			}
			return accountID, nil
		},
		markRentedFunc: func(ctx context.Context, aID int64) (string, []byte, string, error) {
			if aID != accountID {
				t.Errorf("expected accountID %d, got %d", accountID, aID)
			}
			return "steam_user_login", encPassword, "76561197960287930", nil
		},
		logSecurityFunc: func(ctx context.Context, uID, aID, rID int64, clientIP, userAgent string, metadata []byte) error {
			if uID != userID || aID != accountID || rID != rentalID {
				t.Errorf("mismatched IDs in security log")
			}
			if clientIP != "192.168.1.10" || userAgent != "Go-Test" {
				t.Errorf("mismatched metadata: IP %s, UA %s", clientIP, userAgent)
			}
			return nil
		},
	}

	service := NewPaymentService(repo)
	req := WebhookRequest{
		PaymentID:             strconv.FormatInt(paymentID, 10),
		ExternalTransactionID: extTxID,
		Status:                "success",
	}

	res, err := service.ProcessWebhook(context.Background(), req, "192.168.1.10", "Go-Test")
	if err != nil {
		t.Fatalf("ProcessWebhook failed: %v", err)
	}

	if res.RentalID != rentalID {
		t.Errorf("expected rentalID %d, got %d", rentalID, res.RentalID)
	}
	if res.AccountID != accountID {
		t.Errorf("expected accountID %d, got %d", accountID, res.AccountID)
	}
	if res.SteamLogin != "steam_user_login" {
		t.Errorf("expected steam login 'steam_user_login', got %q", res.SteamLogin)
	}
	if res.SteamPassword != rawPassword {
		t.Errorf("expected steam password %q, got %q", rawPassword, res.SteamPassword)
	}
	if res.SteamID64 != "76561197960287930" {
		t.Errorf("expected steam ID '76561197960287930', got %q", res.SteamID64)
	}
}

func TestPaymentService_ProcessWebhook_DatabaseErrorRollback(t *testing.T) {
	repo := &mockRepository{
		updatePaymentFunc: func(ctx context.Context, pID int64, txID string) (int64, int64, int64, string, error) {
			return 0, 0, 0, "", errors.New("db error")
		},
	}

	service := NewPaymentService(repo)
	req := WebhookRequest{
		PaymentID:             "101",
		ExternalTransactionID: "tx-fail",
		Status:                "success",
	}

	_, err := service.ProcessWebhook(context.Background(), req, "127.0.0.1", "Go-Test")
	if err == nil {
		t.Fatalf("expected error from ProcessWebhook, got nil")
	}
}
