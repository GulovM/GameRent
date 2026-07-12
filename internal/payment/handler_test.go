package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const testWebhookSecret = "test-only-webhook-secret-at-least-32-bytes"

type webhookBoundaryService struct {
	Service
	verifier     *PaymentService
	processCalls int
	result       *WebhookResult
	err          error
}

func (s *webhookBoundaryService) VerifySignature(payload []byte, signature string) bool {
	return s.verifier.VerifySignature(payload, signature)
}

func (s *webhookBoundaryService) ProcessWebhook(context.Context, WebhookRequest, string, string) (*WebhookResult, error) {
	s.processCalls++
	return s.result, s.err
}

func signWebhookBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func newWebhookBoundaryHandler(t *testing.T) (*Handler, *webhookBoundaryService, *observer.ObservedLogs) {
	t.Helper()
	verifier, err := NewPaymentServiceWithWebhookSecret(newMockRepository(nil), testWebhookSecret)
	if err != nil {
		t.Fatalf("create webhook verifier: %v", err)
	}
	service := &webhookBoundaryService{verifier: verifier, result: &WebhookResult{Processed: true}}
	core, logs := observer.New(zap.DebugLevel)
	return NewHandler(service, zap.New(core)), service, logs
}

func performWebhookRequest(handler *Handler, body []byte, signature, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/payments/webhook", bytes.NewReader(body))
	if signature != "" {
		req.Header.Set("X-Payment-Signature", signature)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	handler.Webhook(recorder, req)
	return recorder
}

func TestWebhookHandlerRejectsMissingMalformedAndInvalidSignaturesWithoutLoggingThem(t *testing.T) {
	body := []byte(`{"payment_id":"1","rental_id":"2","external_transaction_id":"ext-1","provider":"internal","amount":100,"currency":"USD","status":"success"}`)
	validSignature := signWebhookBody(testWebhookSecret, body)
	tests := []struct {
		name      string
		signature string
	}{
		{name: "missing"},
		{name: "malformed", signature: "not-hex"},
		{name: "wrong digest", signature: strings.Repeat("0", sha256.Size*2)},
		{name: "uppercase rejected", signature: strings.ToUpper(validSignature)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, service, logs := newWebhookBoundaryHandler(t)
			response := performWebhookRequest(handler, body, tt.signature, "application/json")
			if response.Code != http.StatusUnauthorized || service.processCalls != 0 {
				t.Fatalf("signature rejection status=%d process_calls=%d body=%s", response.Code, service.processCalls, response.Body.String())
			}
			for _, entry := range logs.All() {
				logged := entry.Message + fmt.Sprint(entry.ContextMap())
				if tt.signature != "" && strings.Contains(logged, tt.signature) {
					t.Fatalf("raw signature was logged: %s", tt.signature)
				}
				if strings.Contains(entry.Message, string(body)) {
					t.Fatal("raw webhook body was logged")
				}
			}
		})
	}
}

func TestWebhookHandlerRejectsEmptyAndDuplicateSignatureHeaders(t *testing.T) {
	body := []byte(`{"payment_id":"1","rental_id":"2","external_transaction_id":"ext-1","provider":"internal","amount":100,"currency":"USD","status":"success"}`)
	for _, signatures := range [][]string{{""}, {signWebhookBody(testWebhookSecret, body), signWebhookBody(testWebhookSecret, body)}} {
		handler, service, _ := newWebhookBoundaryHandler(t)
		req := httptest.NewRequest(http.MethodPost, "/payments/webhook", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header["X-Payment-Signature"] = signatures
		response := httptest.NewRecorder()
		handler.Webhook(response, req)
		if response.Code != http.StatusUnauthorized || service.processCalls != 0 {
			t.Fatalf("unsafe signature header accepted values=%v status=%d calls=%d", signatures, response.Code, service.processCalls)
		}
	}
}

func TestWebhookHandlerVerifiesExactRawBody(t *testing.T) {
	body := []byte(`{"payment_id":"1","rental_id":"2","external_transaction_id":"ext-1","provider":"internal","amount":100,"currency":"USD","status":"success"}`)
	handler, service, _ := newWebhookBoundaryHandler(t)
	response := performWebhookRequest(handler, body, signWebhookBody(testWebhookSecret, body), "application/json")
	if response.Code != http.StatusOK || service.processCalls != 1 {
		t.Fatalf("valid webhook status=%d calls=%d body=%s", response.Code, service.processCalls, response.Body.String())
	}

	mutatedBody := append([]byte(" "), body...)
	handler, service, _ = newWebhookBoundaryHandler(t)
	response = performWebhookRequest(handler, mutatedBody, signWebhookBody(testWebhookSecret, body), "application/json")
	if response.Code != http.StatusUnauthorized || service.processCalls != 0 {
		t.Fatalf("signature for different raw bytes was accepted: status=%d calls=%d", response.Code, service.processCalls)
	}
}

func TestWebhookHandlerRejectsUnsafeBodiesBeforeProcessing(t *testing.T) {
	validBody := []byte(`{"payment_id":"1","rental_id":"2","external_transaction_id":"ext-1","provider":"internal","amount":100,"currency":"USD","status":"success"}`)
	tests := []struct {
		name        string
		body        []byte
		contentType string
		status      int
	}{
		{name: "empty", body: nil, contentType: "application/json", status: http.StatusBadRequest},
		{name: "oversized", body: bytes.Repeat([]byte("x"), int(maxWebhookBodyBytes)+1), contentType: "application/json", status: http.StatusRequestEntityTooLarge},
		{name: "malformed", body: []byte(`{"payment_id":`), contentType: "application/json", status: http.StatusBadRequest},
		{name: "unknown field", body: []byte(`{"payment_id":"1","rental_id":"2","external_transaction_id":"ext-1","provider":"internal","amount":100,"currency":"USD","status":"success","unexpected":true}`), contentType: "application/json", status: http.StatusBadRequest},
		{name: "duplicate field", body: []byte(`{"payment_id":"1","payment_id":"2","rental_id":"2","external_transaction_id":"ext-1","provider":"internal","amount":100,"currency":"USD","status":"success"}`), contentType: "application/json", status: http.StatusBadRequest},
		{name: "trailing data", body: append(append([]byte{}, validBody...), []byte(` {}`)...), contentType: "application/json", status: http.StatusBadRequest},
		{name: "wrong content type", body: validBody, contentType: "text/plain", status: http.StatusUnsupportedMediaType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, service, _ := newWebhookBoundaryHandler(t)
			response := performWebhookRequest(handler, tt.body, signWebhookBody(testWebhookSecret, tt.body), tt.contentType)
			if response.Code != tt.status || service.processCalls != 0 {
				t.Fatalf("unsafe body status=%d want=%d calls=%d response=%s", response.Code, tt.status, service.processCalls, response.Body.String())
			}
		})
	}
}

func TestWebhookHandlerMapsLifecycleRaceFailuresToConflict(t *testing.T) {
	body := []byte(`{"payment_id":"1","rental_id":"2","external_transaction_id":"ext-1","provider":"internal","amount":100,"currency":"USD","status":"success"}`)
	for _, serviceErr := range []error{
		fmt.Errorf("activate rental: %w", ErrRentalNotEligible),
		fmt.Errorf("mark account rented: %w", ErrAccountNotReserved),
	} {
		handler, service, _ := newWebhookBoundaryHandler(t)
		service.err = serviceErr
		response := performWebhookRequest(handler, body, signWebhookBody(testWebhookSecret, body), "application/json")
		if response.Code != http.StatusConflict || service.processCalls != 1 {
			t.Fatalf("lifecycle race status=%d calls=%d response=%s", response.Code, service.processCalls, response.Body.String())
		}
	}
}

func TestWebhookSecretValidationAndFailClosedVerification(t *testing.T) {
	for _, secret := range []string{"", "   ", "short", "payment-webhook-secret-placeholder", "local-payment-webhook-secret", strings.Repeat("a", 32), " " + testWebhookSecret} {
		if err := ValidateWebhookSecret(secret); err == nil {
			t.Fatalf("unsafe webhook secret accepted")
		}
		if service, err := NewPaymentServiceWithWebhookSecret(newMockRepository(nil), secret); err == nil || service != nil {
			t.Fatalf("unsafe webhook service configuration accepted")
		}
	}
	service, err := NewPaymentServiceWithWebhookSecret(newMockRepository(nil), testWebhookSecret)
	if err != nil {
		t.Fatalf("explicit test webhook secret rejected: %v", err)
	}
	body := []byte("raw-body")
	if service.VerifySignature(body, "") || service.VerifySignature(body, "00") || !service.VerifySignature(body, signWebhookBody(testWebhookSecret, body)) {
		t.Fatal("webhook verifier did not fail closed")
	}
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "")
	if NewPaymentService(newMockRepository(nil)).VerifySignature(body, signWebhookBody(testWebhookSecret, body)) {
		t.Fatal("environment constructor accepted an unconfigured secret")
	}
}
