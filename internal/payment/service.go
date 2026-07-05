package payment

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

var (
	ErrPaymentAlreadyProcessed = errors.New("payment already processed or not found")
	ErrDecryptCredentials      = errors.New("failed to decrypt steam credentials")
)

type Service interface {
	ProcessWebhook(ctx context.Context, req WebhookRequest, clientIP, userAgent string) (*ActivationResult, error)
	VerifySignature(payload []byte, signature string) bool
}

type ActivationResult struct {
	RentalID      int64
	AccountID     int64
	SteamLogin    string
	SteamPassword string
	SteamID64     string
}

type PaymentService struct {
	repo          Repository
	encryptionKey []byte
	webhookSecret string
}

func NewPaymentService(repo Repository) *PaymentService {
	encKey := os.Getenv("ENCRYPTION_KEY")
	webhookSecret := os.Getenv("PAYMENT_WEBHOOK_SECRET")
	if webhookSecret == "" {
		webhookSecret = "payment-webhook-secret-placeholder"
	}

	return &PaymentService{
		repo:          repo,
		encryptionKey: []byte(encKey),
		webhookSecret: webhookSecret,
	}
}

func (s *PaymentService) VerifySignature(payload []byte, signature string) bool {
	if s.webhookSecret == "" || signature == "" {
		return true
	}
	mac := hmac.New(sha256.New, []byte(s.webhookSecret))
	mac.Write(payload)
	expectedMAC := mac.Sum(nil)
	expectedSignature := hex.EncodeToString(expectedMAC)
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

func (s *PaymentService) ProcessWebhook(ctx context.Context, req WebhookRequest, clientIP, userAgent string) (*ActivationResult, error) {
	var err error

	var targetID int64
	if req.PaymentID != "" {
		targetID, err = strconv.ParseInt(req.PaymentID, 10, 64)
	} else if req.RentalID != "" {
		targetID, err = strconv.ParseInt(req.RentalID, 10, 64)
	} else {
		return nil, errors.New("missing payment_id or rental_id in request")
	}
	if err != nil {
		return nil, fmt.Errorf("invalid ID format: %w", err)
	}

	var rentalID, userID, accountID int64
	var steamLogin, decryptedPassword, steamID64 string
	var encPassword []byte

	err = s.repo.WithinTransaction(ctx, func(txCtx context.Context) error {
		var amount int64
		var currency string
		var txErr error

		rentalID, userID, amount, currency, txErr = s.repo.UpdatePaymentSuccess(txCtx, targetID, req.ExternalTransactionID)
		if txErr != nil {
			return txErr
		}

		accountID, txErr = s.repo.ActivateRental(txCtx, rentalID)
		if txErr != nil {
			return fmt.Errorf("failed to activate rental: %w", txErr)
		}

		steamLogin, encPassword, steamID64, txErr = s.repo.MarkAccountRented(txCtx, accountID)
		if txErr != nil {
			return fmt.Errorf("failed to mark account as rented: %w", txErr)
		}

		metadata := map[string]any{
			"payment_id":     strconv.FormatInt(targetID, 10),
			"amount":         amount,
			"currency":       currency,
			"activated_at":   time.Now().Format(time.RFC3339),
			"external_tx_id": req.ExternalTransactionID,
		}
		metadataBytes, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}

		txErr = s.repo.LogSecurityEvent(txCtx, userID, accountID, rentalID, clientIP, userAgent, metadataBytes)
		if txErr != nil {
			return fmt.Errorf("failed to log security event: %w", txErr)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	decryptedPassword, err = decryptPassword(encPassword, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecryptCredentials, err)
	}

	return &ActivationResult{
		RentalID:      rentalID,
		AccountID:     accountID,
		SteamLogin:    steamLogin,
		SteamPassword: decryptedPassword,
		SteamID64:     steamID64,
	}, nil
}

func decryptPassword(ciphertext []byte, key []byte) (string, error) {
	if len(key) == 0 {
		return "", errors.New("encryption key is empty")
	}
	block, err := aes.NewCipher(key)
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
