package payment

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"

	"go.uber.org/zap"
	shared_response "rent_game_accs/internal/shared/response"
)

const maxWebhookBodyBytes int64 = 16 << 10

type Handler struct {
	service Service
	log     *zap.Logger
}

func NewHandler(service Service, log *zap.Logger) *Handler {
	return &Handler{
		service: service,
		log:     log,
	}
}

func (h *Handler) Webhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			h.log.Warn("payment webhook rejected", zap.String("reason", "body_too_large"))
			shared_response.Error(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "Webhook payload exceeds the maximum size")
			return
		}
		h.log.Warn("payment webhook rejected", zap.String("reason", "body_read_failed"))
		shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid webhook request")
		return
	}
	defer r.Body.Close()
	if len(bodyBytes) == 0 {
		h.log.Warn("payment webhook rejected", zap.String("reason", "empty_body"))
		shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid webhook payload")
		return
	}

	signatureValues := r.Header.Values("X-Payment-Signature")
	signature := ""
	if len(signatureValues) == 1 {
		signature = signatureValues[0]
	}
	if !h.service.VerifySignature(bodyBytes, signature) {
		h.log.Warn("payment webhook rejected", zap.String("reason", "invalid_signature"), zap.String("client_ip", getClientIP(r)))
		shared_response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid request signature")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		shared_response.Error(w, http.StatusUnsupportedMediaType, "INVALID_REQUEST", "Content-Type must be application/json")
		return
	}

	var req WebhookRequest
	if err := rejectDuplicateWebhookFields(bodyBytes); err != nil {
		h.log.Warn("payment webhook rejected", zap.String("reason", "invalid_json"))
		shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid webhook payload")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.log.Warn("payment webhook rejected", zap.String("reason", "invalid_json"))
		shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON payload")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.log.Warn("payment webhook rejected", zap.String("reason", "trailing_json"))
		shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid webhook payload")
		return
	}

	clientIP := getClientIP(r)
	userAgent := r.Header.Get("User-Agent")
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}

	result, err := h.service.ProcessWebhook(r.Context(), req, clientIP, userAgent)
	if err != nil {
		if errors.Is(err, ErrWebhookMissingIdentifier) || errors.Is(err, ErrWebhookMissingExternalTxID) || errors.Is(err, ErrWebhookInvalidPayload) {
			h.log.Warn("payment webhook rejected", zap.String("reason", "invalid_payload"))
			shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid webhook payload")
			return
		}
		if errors.Is(err, ErrPaymentNotFound) {
			h.log.Warn("payment webhook rejected", zap.String("reason", "payment_not_found"))
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Payment not found")
			return
		}
		if errors.Is(err, ErrWebhookNotSuccessful) || errors.Is(err, ErrWebhookInvalidTransition) || errors.Is(err, ErrWebhookExternalTxMismatch) ||
			errors.Is(err, ErrWebhookIdentifierMismatch) || errors.Is(err, ErrWebhookFinancialMismatch) || errors.Is(err, ErrWebhookProviderUnsupported) ||
			errors.Is(err, ErrRentalNotEligible) || errors.Is(err, ErrAccountNotReserved) {
			h.log.Warn("payment webhook rejected", zap.String("reason", "webhook_conflict"))
			shared_response.Error(w, http.StatusConflict, "PAYMENT_FAILED", "Webhook does not match an eligible payment")
			return
		}
		h.log.Error("payment webhook processing failed", zap.String("reason", "internal_error"), zap.Error(err))
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to process payment webhook")
		return
	}

	if result.Idempotent {
		h.log.Info("payment webhook replay processed idempotently", zap.Int64("payment_id", result.PaymentID), zap.Int64("rental_id", result.RentalID))
	} else {
		h.log.Info("payment webhook processed", zap.Int64("payment_id", result.PaymentID), zap.Int64("rental_id", result.RentalID))
	}
	shared_response.JSON(w, http.StatusOK, WebhookResponse{
		Status:  "success",
		Message: "Payment processed",
	})
}

func getClientIP(r *http.Request) string {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return strings.TrimSpace(ip)
}

func rejectDuplicateWebhookFields(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return errors.New("webhook payload must be an object")
	}
	seen := make(map[string]struct{})
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := keyToken.(string)
		if !ok {
			return errors.New("invalid webhook object key")
		}
		if _, exists := seen[key]; exists {
			return errors.New("duplicate webhook field")
		}
		seen[key] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') {
		return errors.New("invalid webhook object")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing webhook data")
	}
	return nil
}
