package payment

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"

	"go.uber.org/zap"
	shared_response "rent_game_accs/internal/shared/response"
)

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

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		h.log.Error("failed to read webhook body", zap.Error(err))
		shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to read request body")
		return
	}
	defer r.Body.Close()

	signature := r.Header.Get("X-Payment-Signature")
	if !h.service.VerifySignature(bodyBytes, signature) {
		h.log.Warn("invalid webhook signature received", zap.String("signature", signature))
		shared_response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid request signature")
		return
	}

	var req WebhookRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		h.log.Error("failed to parse webhook payload", zap.Error(err))
		shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON payload")
		return
	}

	clientIP := getClientIP(r)
	userAgent := r.Header.Get("User-Agent")

	h.log.Info("processing payment webhook",
		zap.String("payment_id", req.PaymentID),
		zap.String("rental_id", req.RentalID),
		zap.String("status", req.Status),
		zap.String("client_ip", clientIP),
	)

	result, err := h.service.ProcessWebhook(r.Context(), req, clientIP, userAgent)
	if err != nil {
		if err == ErrPaymentAlreadyProcessed {

			h.log.Warn("payment already processed", zap.String("payment_id", req.PaymentID))
			shared_response.JSON(w, http.StatusOK, WebhookResponse{
				Status:  "success",
				Message: "Payment already processed",
			})
			return
		}

		h.log.Error("failed to process payment webhook", zap.Error(err))
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	res := WebhookResponse{
		Status: "success",
		Credentials: &CredentialsPayload{
			Login:     result.SteamLogin,
			Password:  result.SteamPassword,
			SteamID64: result.SteamID64,
		},
	}

	shared_response.JSON(w, http.StatusOK, res)
}

func getClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.TrimSpace(strings.Split(ip, ",")[0])
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return strings.TrimSpace(ip)
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return strings.TrimSpace(ip)
}
