package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"rent_game_accs/internal/account"
	"rent_game_accs/internal/game"
	"rent_game_accs/internal/payment"
	pkg_http_request "rent_game_accs/internal/pkg/transport/http/request"
	pkg_http_server "rent_game_accs/internal/pkg/transport/http/server"
	"rent_game_accs/internal/rental"
	repo_postgres "rent_game_accs/internal/repository/postgres"
	shared_authorization "rent_game_accs/internal/shared/authorization"
	shared_logger "rent_game_accs/internal/shared/logger"
	shared_middleware "rent_game_accs/internal/shared/middleware"
	shared_response "rent_game_accs/internal/shared/response"
)

type SteamSyncRepository interface {
	GetAccountSyncDetails(ctx context.Context, accountID int64) (string, string, error)
	SyncAccountGamesAsCurrentAdmin(ctx context.Context, actorUserID, accountID int64, games []repo_postgres.AccountGameSyncInfo) error
	DisableAccountIfIdleAsCurrentAdmin(ctx context.Context, actorUserID, accountID int64) error
}

type Handler struct {
	pool           *pgxpool.Pool
	rentalService  *rental.Service
	paymentService payment.Service
	accountRepo    account.Repository
	steamClient    game.SteamClient
	steamSyncRepo  SteamSyncRepository
}

func NewHandler(
	pool *pgxpool.Pool,
	rentalService *rental.Service,
	paymentService payment.Service,
	accountRepo account.Repository,
	steamClient game.SteamClient,
	steamSyncRepo SteamSyncRepository,
) *Handler {
	return &Handler{
		pool:           pool,
		rentalService:  rentalService,
		paymentService: paymentService,
		accountRepo:    accountRepo,
		steamClient:    steamClient,
		steamSyncRepo:  steamSyncRepo,
	}
}

func (h *Handler) Routes(jwtSecret string, log *shared_logger.Logger) []pkg_http_server.Route {
	authMw := shared_middleware.Auth(jwtSecret, log, h.pool)
	return []pkg_http_server.Route{
		pkg_http_server.NewRoute("GET", "/me/balance", wrap(h.GetMyBalance, authMw)),
		pkg_http_server.NewRoute("GET", "/me/ledger", wrap(h.ListMyLedger, authMw)),
		pkg_http_server.NewRoute("GET", "/me/refunds", wrap(h.ListMyRefunds, authMw)),
		pkg_http_server.NewRoute("GET", "/me/rentals", wrap(h.ListMyRentals, authMw)),
		pkg_http_server.NewRoute("GET", "/me/rentals/{rentalId}/credentials", wrap(h.GetMyRentalCredentials, authMw)),
		pkg_http_server.NewRoute("POST", "/me/rentals/{rentalId}/pay-with-balance", wrap(h.PayRentalWithBalance, authMw)),
		pkg_http_server.NewRoute("GET", "/me/payments", wrap(h.ListMyPayments, authMw)),
		pkg_http_server.NewRoute("GET", "/me/notifications", wrap(h.ListMyNotifications, authMw)),
		pkg_http_server.NewRoute("POST", "/rentals", wrap(h.CreateRental, authMw)),
		pkg_http_server.NewRoute("GET", "/rentals", wrap(h.ListMyRentals, authMw)),
		pkg_http_server.NewRoute("GET", "/rentals/{rentalId}", wrap(h.GetRental, authMw)),
		pkg_http_server.NewRoute("POST", "/rentals/{rentalId}/cancel", wrap(h.CancelRental, authMw)),
		pkg_http_server.NewRoute("POST", "/rentals/calculate", wrap(h.CalculateRental, authMw)),
		pkg_http_server.NewRoute("POST", "/rentals/{id}/extend", wrap(h.ExtendRental, authMw)),
		pkg_http_server.NewRoute("POST", "/payments", wrap(h.CreatePayment, authMw)),
		pkg_http_server.NewRoute("GET", "/payments", wrap(h.ListMyPayments, authMw)),
		pkg_http_server.NewRoute("GET", "/payments/{paymentId}", wrap(h.GetPayment, authMw)),
		pkg_http_server.NewRoute("POST", "/reviews", wrap(h.CreateReview, authMw)),
		pkg_http_server.NewRoute("GET", "/accounts/{accountId}/reviews", h.ListAccountReviews),
		pkg_http_server.NewRoute("GET", "/notifications", wrap(h.ListMyNotifications, authMw)),
		pkg_http_server.NewRoute("PATCH", "/notifications/{notificationId}/read", wrap(h.MarkNotificationRead, authMw)),
		pkg_http_server.NewRoute("POST", "/accounts/{id}/favorite", wrap(h.FavoriteOK, authMw)),
		pkg_http_server.NewRoute("DELETE", "/accounts/{id}/favorite", wrap(h.FavoriteOK, authMw)),
		pkg_http_server.NewRoute("GET", "/admin/accounts", wrap(h.AdminListAccounts, authMw)),
		pkg_http_server.NewRoute("GET", "/admin/rentals", wrap(h.AdminListRentals, authMw)),
		pkg_http_server.NewRoute("GET", "/admin/rentals/{rentalId}", wrap(h.AdminGetRentalDetail, authMw)),
		pkg_http_server.NewRoute("GET", "/admin/refund-reason-codes", wrap(h.AdminRefundReasonCodes, authMw)),
		pkg_http_server.NewRoute("POST", "/admin/accounts", wrap(h.AdminCreateAccount, authMw)),
		pkg_http_server.NewRoute("PATCH", "/admin/accounts/{accountId}", wrap(h.AdminUpdateAccount, authMw)),
		pkg_http_server.NewRoute("POST", "/admin/accounts/{accountId}/sync", wrap(h.AdminSyncAccount, authMw)),
		pkg_http_server.NewRoute("POST", "/admin/rentals/{rentalId}/wallet-refund", wrap(h.AdminWalletRefund, authMw)),
		pkg_http_server.NewRoute("POST", "/admin/rentals/{rentalId}/deposit/release", wrap(h.AdminReleaseDeposit, authMw)),
		pkg_http_server.NewRoute("POST", "/admin/rentals/{rentalId}/deposit/forfeit", wrap(h.AdminForfeitDeposit, authMw)),
		pkg_http_server.NewRoute("GET", "/admin/users", wrap(h.AdminListUsers, authMw)),
		pkg_http_server.NewRoute("PATCH", "/admin/users/{userId}", wrap(h.AdminUpdateUser, authMw)),
		pkg_http_server.NewRoute("POST", "/admin/users/{userId}/balance-adjustments", wrap(h.AdminAdjustUserBalance, authMw)),
		pkg_http_server.NewRoute("GET", "/admin/audit-logs", wrap(h.AdminAuditLogs, authMw)),
	}
}

func wrap(h http.HandlerFunc, mws ...func(http.Handler) http.Handler) http.HandlerFunc {
	var final http.Handler = h
	for i := len(mws) - 1; i >= 0; i-- {
		final = mws[i](final)
	}
	return func(w http.ResponseWriter, r *http.Request) { final.ServeHTTP(w, r) }
}

type createRentalRequest struct {
	AccountID     int64 `json:"account_id"`
	DurationHours int   `json:"duration_hours"`
}

func (r *createRentalRequest) Validate() error {
	if r.AccountID <= 0 {
		return errText("account_id is required")
	}
	if r.DurationHours < 1 || r.DurationHours > 720 {
		return errText("duration_hours must be between 1 and 720")
	}
	return nil
}

type errText string

func (e errText) Error() string { return string(e) }

func (h *Handler) CreateRental(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	var req createRentalRequest
	if err := pkg_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	rent, err := h.rentalService.RentAccount(r.Context(), userID, req.AccountID, time.Duration(req.DurationHours)*time.Hour, time.Now().UTC())
	if err != nil {
		shared_response.Error(w, http.StatusConflict, "RENTAL_FAILED", err.Error())
		return
	}
	shared_response.JSON(w, http.StatusCreated, rentalDTO(rent))
}

func (h *Handler) ListMyRentals(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	rows, err := h.pool.Query(r.Context(), `
		SELECT
			r.id,
			r.user_id,
			r.account_id,
			r.status,
			r.start_at,
			r.end_at,
			r.payment_expires_at,
			r.rental_price,
			r.deposit_amount,
			COALESCE(d.status, 0),
			COALESCE(rf.status, 0),
			COALESCE(rf.amount_total, 0),
			rf.processed_at
		FROM rentals r
		LEFT JOIN deposit_holds d ON d.rental_id = r.id
		LEFT JOIN LATERAL (
			SELECT status, amount_total, processed_at
			FROM refunds
			WHERE rental_id = r.id AND user_id = r.user_id
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		) rf ON TRUE
		WHERE r.user_id = $1
		ORDER BY r.created_at DESC`, userID)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id, uid, accountID, price, deposit int64
		var status, depositHoldStatus, refundStatusCode int16
		var start, end, paymentExpiresAt time.Time
		var refundTotalAmount int64
		var refundProcessedAt sql.NullTime
		if err := rows.Scan(&id, &uid, &accountID, &status, &start, &end, &paymentExpiresAt, &price, &deposit, &depositHoldStatus, &refundStatusCode, &refundTotalAmount, &refundProcessedAt); err != nil {
			shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		items = append(items, rentalMap(id, uid, accountID, status, start, end, paymentExpiresAt, price, deposit, publicDepositStatus(deposit, depositHoldStatus), publicRefundSummary(refundStatusCode, refundTotalAmount, refundProcessedAt)))
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"rentals": items})
}

func (h *Handler) GetRental(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	id, ok := pathID(w, r, "rentalId")
	if !ok {
		return
	}
	var uid, accountID, price, deposit int64
	var status, depositHoldStatus, refundStatusCode int16
	var start, end, paymentExpiresAt time.Time
	var refundTotalAmount int64
	var refundProcessedAt sql.NullTime
	err := h.pool.QueryRow(r.Context(), `
		SELECT
			r.user_id,
			r.account_id,
			r.status,
			r.start_at,
			r.end_at,
			r.payment_expires_at,
			r.rental_price,
			r.deposit_amount,
			COALESCE(d.status, 0),
			COALESCE(rf.status, 0),
			COALESCE(rf.amount_total, 0),
			rf.processed_at
		FROM rentals r
		LEFT JOIN deposit_holds d ON d.rental_id = r.id
		LEFT JOIN LATERAL (
			SELECT status, amount_total, processed_at
			FROM refunds
			WHERE rental_id = r.id AND user_id = r.user_id
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		) rf ON TRUE
		WHERE r.id = $1`, id).Scan(&uid, &accountID, &status, &start, &end, &paymentExpiresAt, &price, &deposit, &depositHoldStatus, &refundStatusCode, &refundTotalAmount, &refundProcessedAt)
	if err != nil {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Rental not found")
		return
	}
	if uid != userID {
		shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", "You can access only your rentals")
		return
	}
	shared_response.JSON(w, http.StatusOK, rentalMap(id, uid, accountID, status, start, end, paymentExpiresAt, price, deposit, publicDepositStatus(deposit, depositHoldStatus), publicRefundSummary(refundStatusCode, refundTotalAmount, refundProcessedAt)))
}

func (h *Handler) GetMyBalance(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	balance, err := h.paymentService.GetUserBalance(r.Context(), userID)
	if err != nil {
		if errors.Is(err, payment.ErrFinancialUserNotFound) {
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Balance not found")
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load balance")
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{
		"available_balance": balance.AvailableBalance,
		"currency":          balance.Currency,
	})
}

func (h *Handler) ListMyLedger(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	page, pageSize, ok := ledgerPaginationParams(w, r)
	if !ok {
		return
	}

	ledgerPage, err := h.paymentService.ListUserLedger(r.Context(), userID, page, pageSize)
	if err != nil {
		if errors.Is(err, payment.ErrInvalidLedgerPagination) {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "page and page_size must be positive; page_size must be <= 100")
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load ledger")
		return
	}

	totalPages := int(ledgerPage.TotalItems) / ledgerPage.PageSize
	if int(ledgerPage.TotalItems)%ledgerPage.PageSize != 0 {
		totalPages++
	}
	if ledgerPage.TotalItems == 0 {
		totalPages = 0
	}

	items := make([]map[string]any, 0, len(ledgerPage.Entries))
	for _, entry := range ledgerPage.Entries {
		dto := map[string]any{
			"id":           entry.ID,
			"entry_type":   entry.EntryType,
			"amount":       entry.Amount,
			"currency":     entry.Currency,
			"created_at":   entry.CreatedAt,
			"display_type": entry.DisplayType,
		}
		if entry.RentalID != nil {
			dto["rental_id"] = *entry.RentalID
		}
		if entry.PaymentID != nil {
			dto["payment_id"] = *entry.PaymentID
		}
		items = append(items, dto)
	}

	shared_response.JSON(w, http.StatusOK, map[string]any{
		"entries": items,
		"pagination": map[string]any{
			"page":        ledgerPage.Page,
			"page_size":   ledgerPage.PageSize,
			"total_items": ledgerPage.TotalItems,
			"total_pages": totalPages,
		},
	})
}

func (h *Handler) ListMyRefunds(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	page, pageSize, ok := ledgerPaginationParams(w, r)
	if !ok {
		return
	}

	refundPage, err := h.paymentService.ListUserRefunds(r.Context(), userID, page, pageSize)
	if err != nil {
		if errors.Is(err, payment.ErrInvalidRefundPagination) {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "page and page_size must be positive; page_size must be <= 100")
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load refunds")
		return
	}

	totalPages := int(refundPage.TotalItems) / refundPage.PageSize
	if int(refundPage.TotalItems)%refundPage.PageSize != 0 {
		totalPages++
	}
	if refundPage.TotalItems == 0 {
		totalPages = 0
	}

	items := make([]map[string]any, 0, len(refundPage.Entries))
	for _, entry := range refundPage.Entries {
		dto := map[string]any{
			"id":               entry.ID,
			"rental_id":        entry.RentalID,
			"payment_id":       entry.PaymentID,
			"status":           entry.Status,
			"principal_amount": entry.PrincipalAmount,
			"deposit_amount":   entry.DepositAmount,
			"total_amount":     entry.TotalAmount,
			"currency":         entry.Currency,
			"created_at":       entry.CreatedAt,
			"processed_at":     entry.ProcessedAt,
		}
		if entry.ReasonCode != nil {
			dto["reason_code"] = *entry.ReasonCode
		}
		items = append(items, dto)
	}

	shared_response.JSON(w, http.StatusOK, map[string]any{
		"refunds": items,
		"pagination": map[string]any{
			"page":        refundPage.Page,
			"page_size":   refundPage.PageSize,
			"total_items": refundPage.TotalItems,
			"total_pages": totalPages,
		},
	})
}

type rentalCredentialsResponse struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

func (h *Handler) GetMyRentalCredentials(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	rentalID, ok := pathID(w, r, "rentalId")
	if !ok {
		return
	}

	creds, err := h.rentalService.GetRentalCredentials(r.Context(), userID, rentalID, rental.CredentialRequestContext{
		IPAddress: clientIPFromRequest(r),
		UserAgent: r.UserAgent(),
	}, time.Now().UTC())
	if err != nil {
		if errors.Is(err, rental.ErrCredentialsNotAvailable) {
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Rental not found")
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load rental credentials")
		return
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	shared_response.JSON(w, http.StatusOK, rentalCredentialsResponse{
		Login:    creds.Login,
		Password: creds.Password,
	})
}

func (h *Handler) PayRentalWithBalance(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	rentalID, ok := pathID(w, r, "rentalId")
	if !ok {
		return
	}

	result, err := h.paymentService.PayRentalWithBalance(r.Context(), userID, rentalID, clientIPFromRequest(r), r.Header.Get("User-Agent"), time.Now().UTC())
	if err != nil {
		if errors.Is(err, payment.ErrWalletPaymentNotFound) {
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Rental not found")
			return
		}
		if errors.Is(err, payment.ErrWalletInsufficientBalance) {
			shared_response.Error(w, http.StatusConflict, "PAYMENT_FAILED", "Insufficient balance")
			return
		}
		if errors.Is(err, payment.ErrWalletPaymentExpired) || errors.Is(err, payment.ErrWalletPaymentNotAllowed) {
			shared_response.Error(w, http.StatusConflict, "PAYMENT_FAILED", "Rental cannot be paid with balance")
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to pay rental with balance")
		return
	}

	shared_response.JSON(w, http.StatusOK, map[string]any{
		"changed":          result.Changed,
		"idempotent":       result.Idempotent,
		"payment_id":       result.PaymentID,
		"rental_id":        result.RentalID,
		"account_id":       result.AccountID,
		"payment_status":   result.PaymentStatus,
		"rental_status":    result.RentalStatus,
		"account_status":   result.AccountStatus,
		"payment_provider": result.PaymentProvider,
	})
}

func (h *Handler) CancelRental(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	id, ok := pathID(w, r, "rentalId")
	if !ok {
		return
	}

	_, err := h.rentalService.CancelRental(r.Context(), userID, id, "cancelled by user", time.Now().UTC())
	if err != nil {
		if errors.Is(err, rental.ErrCannotCancel) || errors.Is(err, rental.ErrRentalNotFound) {
			shared_response.Error(w, http.StatusConflict, "CANCEL_FAILED", "Rental cannot be cancelled")
			return
		}
		shared_response.Error(w, http.StatusConflict, "CANCEL_FAILED", "Rental cannot be cancelled")
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]string{"message": "Rental cancelled"})
}

func (h *Handler) CalculateRental(w http.ResponseWriter, r *http.Request) {
	var req createRentalRequest
	if err := pkg_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	var hourly, deposit int64
	err := h.pool.QueryRow(r.Context(), `SELECT hourly_price, deposit_amount FROM accounts WHERE id=$1 AND deleted_at IS NULL`, req.AccountID).Scan(&hourly, &deposit)
	if err != nil {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Account not found")
		return
	}
	_, total, err := rental.CalculateRentalTotal(hourly, deposit, int64(req.DurationHours))
	if err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Configured rental price exceeds the supported range")
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"price_per_hour": money(hourly), "security_deposit": money(deposit), "duration_hours": req.DurationHours, "total_price": money(total)})
}

func (h *Handler) ExtendRental(w http.ResponseWriter, r *http.Request) {
	shared_response.Error(w, http.StatusNotImplemented, "EXTENSION_NOT_SUPPORTED", "Paid rental extension is not supported")
}

func (h *Handler) CreatePayment(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	var req struct {
		RentalID int64 `json:"rental_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RentalID <= 0 {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "rental_id is required")
		return
	}
	var amount int64
	err := h.pool.QueryRow(r.Context(), `SELECT rental_price + deposit_amount FROM rentals WHERE id=$1 AND user_id=$2`, req.RentalID, userID).Scan(&amount)
	if err != nil {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Rental not found")
		return
	}

	var id int64
	var currency string
	var status int16
	err = h.pool.QueryRow(r.Context(), `SELECT id, amount, currency, status FROM payments WHERE rental_id=$1 AND user_id=$2`, req.RentalID, userID).Scan(&id, &amount, &currency, &status)
	if err != nil {
		shared_response.Error(w, http.StatusConflict, "PAYMENT_FAILED", "Payment record is unavailable for this rental")
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"id": id, "rental_id": req.RentalID, "amount": amount, "currency": currency, "status": status})
}

func (h *Handler) ListMyPayments(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	rows, err := h.pool.Query(r.Context(), `SELECT id, rental_id, amount, currency, status, created_at FROM payments WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id, rentalID, amount int64
		var currency string
		var status int16
		var created time.Time
		_ = rows.Scan(&id, &rentalID, &amount, &currency, &status, &created)
		items = append(items, map[string]any{"id": id, "rental_id": rentalID, "amount": amount, "currency": currency, "status": status, "created_at": created})
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"payments": items})
}

func (h *Handler) GetPayment(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	id, ok := pathID(w, r, "paymentId")
	if !ok {
		return
	}
	var rentalID, amount int64
	var currency string
	var status int16
	err := h.pool.QueryRow(r.Context(), `SELECT rental_id, amount, currency, status FROM payments WHERE id=$1 AND user_id=$2`, id, userID).Scan(&rentalID, &amount, &currency, &status)
	if err != nil {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Payment not found")
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"id": id, "rental_id": rentalID, "amount": amount, "currency": currency, "status": status})
}

func (h *Handler) CreateReview(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	var req struct {
		AccountID int64  `json:"account_id"`
		RentalID  int64  `json:"rental_id"`
		Rating    int    `json:"rating"`
		Comment   string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AccountID <= 0 || req.RentalID <= 0 || req.Rating < 1 || req.Rating > 5 {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "account_id, rental_id and rating 1..5 are required")
		return
	}
	var id int64
	err := h.pool.QueryRow(r.Context(), `INSERT INTO reviews (rental_id,user_id,account_id,rating,comment) VALUES ($1,$2,$3,$4,$5) RETURNING id`, req.RentalID, userID, req.AccountID, req.Rating, req.Comment).Scan(&id)
	if err != nil {
		shared_response.Error(w, http.StatusConflict, "REVIEW_FAILED", err.Error())
		return
	}
	shared_response.JSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (h *Handler) ListAccountReviews(w http.ResponseWriter, r *http.Request) {
	accountID, ok := pathID(w, r, "accountId")
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT id,user_id,rating,comment,created_at FROM reviews WHERE account_id=$1 ORDER BY created_at DESC`, accountID)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id, userID int64
		var rating int16
		var comment string
		var created time.Time
		_ = rows.Scan(&id, &userID, &rating, &comment, &created)
		items = append(items, map[string]any{"id": id, "user_id": userID, "account_id": accountID, "rating": rating, "comment": comment, "created_at": created})
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"reviews": items})
}

func (h *Handler) ListMyNotifications(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	rows, err := h.pool.Query(r.Context(), `SELECT id,type,title,body,is_read,created_at FROM notifications WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id int64
		var typ int16
		var title, body string
		var read bool
		var created time.Time
		_ = rows.Scan(&id, &typ, &title, &body, &read, &created)
		items = append(items, map[string]any{"id": id, "type": typ, "title": title, "body": body, "read": read, "created_at": created})
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"notifications": items})
}

func (h *Handler) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	id, ok := pathID(w, r, "notificationId")
	if !ok {
		return
	}
	tag, _ := h.pool.Exec(r.Context(), `UPDATE notifications SET is_read=true WHERE id=$1 AND user_id=$2`, id, userID)
	if tag.RowsAffected() == 0 {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Notification not found")
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]string{"message": "Notification marked as read"})
}

func (h *Handler) FavoriteOK(w http.ResponseWriter, r *http.Request) {
	shared_response.JSON(w, http.StatusOK, map[string]string{"message": "Favorites are accepted in local MVP but not persisted"})
}

func (h *Handler) AdminListAccounts(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT id, steam_id64, status, hourly_price, deposit_amount FROM accounts ORDER BY created_at DESC`)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id, hourly, deposit int64
		var steam string
		var status int16
		_ = rows.Scan(&id, &steam, &status, &hourly, &deposit)
		items = append(items, map[string]any{"id": id, "steam_id64": steam, "status": status, "hourly_price": hourly, "deposit_amount": deposit})
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"accounts": items})
}

func (h *Handler) AdminListRentals(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	filters, ok := adminRentalListFilters(w, r)
	if !ok {
		return
	}
	result, err := h.paymentService.ListAdminRentals(r.Context(), filters)
	if err != nil {
		if errors.Is(err, payment.ErrInvalidAdminRentalFilters) {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load admin rentals")
		return
	}
	items := make([]map[string]any, 0, len(result.Rentals))
	for _, item := range result.Rentals {
		items = append(items, adminRentalEntryDTO(item))
	}

	totalPages := int(result.TotalItems) / result.PageSize
	if int(result.TotalItems)%result.PageSize != 0 {
		totalPages++
	}
	if result.TotalItems == 0 {
		totalPages = 0
	}

	shared_response.JSON(w, http.StatusOK, map[string]any{
		"rentals": items,
		"summary": adminRentalSummaryDTO(result.Summary),
		"pagination": map[string]any{
			"page":        result.Page,
			"page_size":   result.PageSize,
			"total_items": result.TotalItems,
			"total_pages": totalPages,
		},
	})
}

func (h *Handler) AdminGetRentalDetail(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}

	rentalID, err := strconv.ParseInt(r.PathValue("rentalId"), 10, 64)
	if err != nil || rentalID <= 0 {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "rentalId must be a positive integer")
		return
	}

	detail, err := h.paymentService.GetAdminRentalDetail(r.Context(), rentalID)
	if err != nil {
		if errors.Is(err, payment.ErrInvalidAdminRentalFilters) {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			return
		}
		if errors.Is(err, payment.ErrAdminRentalNotFound) {
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Rental not found")
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load admin rental detail")
		return
	}

	shared_response.JSON(w, http.StatusOK, adminRentalDetailDTO(detail))
}

func (h *Handler) AdminRefundReasonCodes(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}

	items := make([]map[string]string, 0, len(payment.WalletRefundReasonOptions()))
	for _, option := range payment.WalletRefundReasonOptions() {
		items = append(items, map[string]string{
			"code":  option.Code,
			"label": option.Label,
		})
	}

	shared_response.JSON(w, http.StatusOK, map[string]any{
		"reason_codes": items,
	})
}

func (h *Handler) AdminCreateAccount(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	var req struct {
		SteamID64       string `json:"steam_id64"`
		SteamLogin      string `json:"steam_login"`
		SteamPassword   string `json:"steam_password"`
		PricePerHour    int64  `json:"price_per_hour"`
		SecurityDeposit int64  `json:"security_deposit"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid account creation payload")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid account creation payload")
		return
	}
	if req.SteamID64 == "" || req.SteamLogin == "" || req.SteamPassword == "" || req.PricePerHour <= 0 || req.SecurityDeposit < 0 {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "steam credentials, positive price_per_hour, and non-negative security_deposit are required")
		return
	}
	if _, _, err := rental.CalculateRentalTotal(req.PricePerHour, req.SecurityDeposit, 720); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Account pricing exceeds the supported range")
		return
	}
	encrypted, err := h.accountRepo.Encrypt(req.SteamPassword)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	var id int64
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create account")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	if err := shared_authorization.RequireCurrentAdminForMutation(r.Context(), tx, shared_middleware.GetUserID(r.Context())); err != nil {
		if errors.Is(err, shared_authorization.ErrCurrentAdminRequired) {
			shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", "Current administrator authorization is required")
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to authorize account creation")
		return
	}
	err = tx.QueryRow(r.Context(), `INSERT INTO accounts (steam_id64,login,encrypted_password,hourly_price,deposit_amount,status,steam_guard_enabled,inventory_verified,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,2,true,true,NOW(),NOW()) RETURNING id`, req.SteamID64, req.SteamLogin, encrypted, req.PricePerHour, req.SecurityDeposit).Scan(&id)
	if err != nil {
		shared_response.Error(w, http.StatusConflict, "CREATE_FAILED", "Account could not be created")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create account")
		return
	}

	gamesCount, syncErr := h.syncSteamLibrary(r.Context(), shared_middleware.GetUserID(r.Context()), id)
	if syncErr != nil {
		shared_response.JSON(w, http.StatusCreated, map[string]any{
			"id":          id,
			"games_count": gamesCount,
			"sync_error":  syncErr.Error(),
		})
		return
	}

	shared_response.JSON(w, http.StatusCreated, map[string]any{"id": id, "games_count": gamesCount})
}

func (h *Handler) AdminUpdateAccount(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	id, ok := pathID(w, r, "accountId")
	if !ok {
		return
	}
	var req struct {
		PricePerHour    *int64 `json:"price_per_hour"`
		SecurityDeposit *int64 `json:"security_deposit"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid account update payload")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid account update payload")
		return
	}
	if req.PricePerHour != nil && *req.PricePerHour <= 0 {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "price_per_hour must be positive")
		return
	}
	if req.SecurityDeposit != nil && *req.SecurityDeposit < 0 {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "security_deposit must be non-negative")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update account")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	if err := shared_authorization.RequireCurrentAdminForMutation(r.Context(), tx, shared_middleware.GetUserID(r.Context())); err != nil {
		if errors.Is(err, shared_authorization.ErrCurrentAdminRequired) {
			shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", "Current administrator authorization is required")
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to authorize account update")
		return
	}
	var currentPrice, currentDeposit int64
	err = tx.QueryRow(r.Context(), `SELECT hourly_price, deposit_amount FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&currentPrice, &currentDeposit)
	if errors.Is(err, pgx.ErrNoRows) {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Account not found")
		return
	}
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update account")
		return
	}
	mergedPrice, mergedDeposit := currentPrice, currentDeposit
	if req.PricePerHour != nil {
		mergedPrice = *req.PricePerHour
	}
	if req.SecurityDeposit != nil {
		mergedDeposit = *req.SecurityDeposit
	}
	if _, _, err := rental.CalculateRentalTotal(mergedPrice, mergedDeposit, 720); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Account pricing exceeds the supported range")
		return
	}
	tag, err := tx.Exec(r.Context(), `UPDATE accounts SET hourly_price=COALESCE($1,hourly_price), deposit_amount=COALESCE($2,deposit_amount), updated_at=NOW() WHERE id=$3 AND deleted_at IS NULL`, req.PricePerHour, req.SecurityDeposit, id)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update account")
		return
	}
	if tag.RowsAffected() == 0 {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Account not found")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update account")
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]string{"message": "Account updated"})
}

func (h *Handler) AdminSyncAccount(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	id, ok := pathID(w, r, "accountId")
	if !ok {
		return
	}

	gamesCount, err := h.syncSteamLibrary(r.Context(), shared_middleware.GetUserID(r.Context()), id)
	if err != nil {
		if errors.Is(err, shared_authorization.ErrCurrentAdminRequired) {
			shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", "Current administrator authorization is required")
			return
		}
		if errors.Is(err, repo_postgres.ErrAccountLifecycleConflict) {
			shared_response.Error(w, http.StatusConflict, "ACCOUNT_LIFECYCLE_CONFLICT", "Account cannot be disabled while a rental is waiting for payment or active")
			return
		}
		shared_response.Error(w, http.StatusBadGateway, "STEAM_SYNC_FAILED", err.Error())
		return
	}

	shared_response.JSON(w, http.StatusOK, map[string]any{"message": "Account library synced", "games_count": gamesCount})
}

func (h *Handler) syncSteamLibrary(ctx context.Context, actorUserID, accountID int64) (int, error) {
	if h.steamClient == nil || h.steamSyncRepo == nil {
		return 0, fmt.Errorf("steam synchronization is not configured")
	}

	login, steamID64, err := h.steamSyncRepo.GetAccountSyncDetails(ctx, accountID)
	if err != nil {
		return 0, fmt.Errorf("failed to load Steam account details: %w", err)
	}
	if steamID64 == "" {
		return 0, fmt.Errorf("account %d has empty steam_id64", accountID)
	}

	vacBanned, err := h.steamClient.CheckVACBans(ctx, steamID64)
	if err != nil {
		return 0, fmt.Errorf("failed to check VAC bans for %s: %w", login, err)
	}
	if vacBanned {
		if banErr := h.steamSyncRepo.DisableAccountIfIdleAsCurrentAdmin(ctx, actorUserID, accountID); banErr != nil {
			return 0, fmt.Errorf("account is VAC banned and could not be disabled: %w", banErr)
		}
		return 0, fmt.Errorf("account is VAC banned and was disabled")
	}

	games, err := h.steamClient.GetOwnedGames(ctx, steamID64)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch owned games for %s: %w", login, err)
	}
	if err := h.steamSyncRepo.SyncAccountGamesAsCurrentAdmin(ctx, actorUserID, accountID, games); err != nil {
		return 0, fmt.Errorf("failed to persist Steam library: %w", err)
	}

	return len(games), nil
}

func (h *Handler) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT id,email,COALESCE(first_name,''),COALESCE(last_name,''),role,trust_score,is_blocked,balance FROM users WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id, balance int64
		var email, first, last, role string
		var trust int
		var blocked bool
		_ = rows.Scan(&id, &email, &first, &last, &role, &trust, &blocked, &balance)
		items = append(items, map[string]any{"id": id, "email": email, "first_name": first, "last_name": last, "role": role, "trust_score": trust, "is_blocked": blocked, "balance": balance})
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"users": items})
}

func (h *Handler) AdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	id, ok := pathID(w, r, "userId")
	if !ok {
		return
	}
	var req struct {
		TrustScore *int    `json:"trust_score"`
		IsBlocked  *bool   `json:"is_blocked"`
		Role       *string `json:"role"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid admin user update payload")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid admin user update payload")
		return
	}
	if req.Role != nil && *req.Role != "ADMIN" && *req.Role != "RENT" {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "role must be ADMIN or RENT")
		return
	}
	if req.TrustScore != nil && (*req.TrustScore < 0 || *req.TrustScore > 1000) {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "trust_score must be between 0 and 1000")
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update user")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	actorUserID := shared_middleware.GetUserID(r.Context())
	if err := shared_authorization.RequireCurrentAdminForMutation(r.Context(), tx, actorUserID); err != nil {
		if errors.Is(err, shared_authorization.ErrCurrentAdminRequired) {
			shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", "Current administrator authorization is required")
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to authorize user update")
		return
	}
	if actorUserID == id {
		shared_response.Error(w, http.StatusConflict, "ADMIN_USER_UPDATE_FORBIDDEN", "Administrators cannot change their own administrative user state")
		return
	}
	var oldRole string
	var oldBlocked bool
	if err := tx.QueryRow(r.Context(), `SELECT role, is_blocked FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&oldRole, &oldBlocked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "User not found")
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update user")
		return
	}

	tag, err := tx.Exec(r.Context(), `UPDATE users SET trust_score=COALESCE($1,trust_score), is_blocked=COALESCE($2,is_blocked), role=COALESCE($3,role), updated_at=NOW() WHERE id=$4 AND deleted_at IS NULL`, req.TrustScore, req.IsBlocked, req.Role, id)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update user")
		return
	}
	if tag.RowsAffected() == 0 {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "User not found")
		return
	}
	newRole, newBlocked := oldRole, oldBlocked
	if req.Role != nil {
		newRole = *req.Role
	}
	if req.IsBlocked != nil {
		newBlocked = *req.IsBlocked
	}
	if newRole != oldRole || newBlocked != oldBlocked {
		if _, err := tx.Exec(r.Context(), `UPDATE refresh_tokens SET revoked_at=NOW() WHERE user_id=$1 AND revoked_at IS NULL`, id); err != nil {
			shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to revoke user sessions")
			return
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO audit_logs (actor_user_id, entity_type, entity_id, action, old_values, new_values, created_at) VALUES ($1, 'USER', $2, 'USER_SECURITY_STATE_UPDATED', jsonb_build_object('role',$3::text,'is_blocked',$4::boolean), jsonb_build_object('role',$5::text,'is_blocked',$6::boolean), NOW())`, actorUserID, id, oldRole, oldBlocked, newRole, newBlocked); err != nil {
			shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to audit user update")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update user")
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]string{"message": "User updated"})
}

type adminBalanceAdjustmentRequest struct {
	Amount         int64  `json:"amount"`
	Currency       string `json:"currency"`
	ReasonCode     string `json:"reason_code"`
	Comment        string `json:"comment"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (h *Handler) AdminAdjustUserBalance(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}

	targetUserID, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil || targetUserID <= 0 {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "userId must be a positive integer")
		return
	}

	var req adminBalanceAdjustmentRequest
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid balance adjustment payload")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Invalid balance adjustment payload")
		return
	}

	result, err := h.paymentService.AdjustAdminBalance(
		r.Context(),
		shared_middleware.GetUserID(r.Context()),
		shared_middleware.GetUserRole(r.Context()),
		targetUserID,
		payment.AdminBalanceAdjustmentInput{
			Amount:         req.Amount,
			Currency:       req.Currency,
			ReasonCode:     req.ReasonCode,
			Comment:        req.Comment,
			IdempotencyKey: req.IdempotencyKey,
		},
		clientIPFromRequest(r),
		r.Header.Get("User-Agent"),
		time.Now().UTC(),
	)
	if err != nil {
		switch {
		case errors.Is(err, payment.ErrAdminRequired):
			shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", err.Error())
		case errors.Is(err, payment.ErrFinancialUserNotFound):
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "User not found")
		case errors.Is(err, payment.ErrBalanceAdjustmentAmountRequired),
			errors.Is(err, payment.ErrBalanceAdjustmentCurrencyUnsupported),
			errors.Is(err, payment.ErrBalanceAdjustmentReasonRequired),
			errors.Is(err, payment.ErrBalanceAdjustmentCommentTooLong),
			errors.Is(err, payment.ErrBalanceAdjustmentIdempotencyRequired),
			errors.Is(err, payment.ErrBalanceAdjustmentOverflow):
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		case errors.Is(err, payment.ErrBalanceAdjustmentWouldBeNegative),
			errors.Is(err, payment.ErrBalanceAdjustmentIdempotencyConflict):
			shared_response.Error(w, http.StatusConflict, "BALANCE_ADJUSTMENT_FAILED", err.Error())
		default:
			shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to adjust user balance")
		}
		return
	}

	status := http.StatusCreated
	if result.IdempotentReplay {
		status = http.StatusOK
	}
	shared_response.JSON(w, status, map[string]any{
		"adjustment_id":     result.AdjustmentID,
		"user_id":           result.UserID,
		"previous_balance":  result.PreviousBalance,
		"new_balance":       result.NewBalance,
		"amount":            result.Amount,
		"currency":          result.Currency,
		"ledger_entry_id":   result.LedgerEntryID,
		"idempotency_key":   result.IdempotencyKey,
		"idempotent_replay": result.IdempotentReplay,
		"created_at":        result.CreatedAt,
	})
}

func (h *Handler) AdminAuditLogs(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT id, actor_user_id, entity_type, entity_id, action, created_at FROM audit_logs ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id, entityID int64
		var actor *int64
		var entity, action string
		var created time.Time
		_ = rows.Scan(&id, &actor, &entity, &entityID, &action, &created)
		items = append(items, map[string]any{"id": id, "actor_user_id": actor, "entity_type": entity, "entity_id": entityID, "action": action, "created_at": created})
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"audit_logs": items})
}

type adminForfeitDepositRequest struct {
	ReasonCode string `json:"reason_code"`
}

func (r *adminForfeitDepositRequest) Validate() error {
	r.ReasonCode = strings.TrimSpace(r.ReasonCode)
	if len(r.ReasonCode) == 0 || len(r.ReasonCode) > 64 {
		return errText("reason_code must be between 1 and 64 characters")
	}
	for _, ch := range r.ReasonCode {
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
		return errText("reason_code may contain only letters, digits, underscores and hyphens")
	}
	return nil
}

type adminWalletRefundRequest struct {
	ReasonCode string `json:"reason_code"`
}

func (r *adminWalletRefundRequest) Validate() error {
	r.ReasonCode = strings.TrimSpace(r.ReasonCode)
	if !payment.IsAllowedWalletRefundReasonCode(r.ReasonCode) {
		return errText("reason_code must be one of the supported wallet refund reason codes")
	}
	return nil
}

func (h *Handler) AdminWalletRefund(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	rentalID, ok := pathID(w, r, "rentalId")
	if !ok {
		return
	}
	var req adminWalletRefundRequest
	if err := pkg_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	result, err := h.paymentService.RefundWalletPayment(r.Context(), shared_middleware.GetUserID(r.Context()), shared_middleware.GetUserRole(r.Context()), rentalID, req.ReasonCode, time.Now().UTC())
	if err != nil {
		if errors.Is(err, payment.ErrAdminRequired) {
			shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", err.Error())
			return
		}
		if errors.Is(err, payment.ErrInvalidReasonCode) {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			return
		}
		if errors.Is(err, payment.ErrWalletRefundNotFound) {
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			return
		}
		if errors.Is(err, payment.ErrWalletRefundNotAllowed) || errors.Is(err, payment.ErrDepositAlreadySettled) {
			shared_response.Error(w, http.StatusConflict, "WALLET_REFUND_FAILED", err.Error())
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal error")
		return
	}

	shared_response.JSON(w, http.StatusOK, map[string]any{
		"changed":          result.Changed,
		"idempotent":       result.Idempotent,
		"status":           result.Status,
		"principal_amount": money(result.PrincipalAmount),
		"deposit_amount":   money(result.DepositAmount),
		"total_amount":     money(result.TotalAmount),
		"deposit_status":   result.DepositStatus,
	})
}

func (h *Handler) AdminReleaseDeposit(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	rentalID, ok := pathID(w, r, "rentalId")
	if !ok {
		return
	}
	result, err := h.paymentService.ReleaseDeposit(r.Context(), shared_middleware.GetUserID(r.Context()), shared_middleware.GetUserRole(r.Context()), rentalID, time.Now().UTC())
	if err != nil {
		if errors.Is(err, payment.ErrAdminRequired) {
			shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", err.Error())
			return
		}
		if errors.Is(err, payment.ErrDepositHoldNotFound) {
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			return
		}
		if errors.Is(err, payment.ErrDepositSettlementNotAllowed) || errors.Is(err, payment.ErrDepositAlreadySettled) {
			shared_response.Error(w, http.StatusConflict, "DEPOSIT_SETTLEMENT_FAILED", err.Error())
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"changed": result.Changed, "status": result.Status})
}

func (h *Handler) AdminForfeitDeposit(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	rentalID, ok := pathID(w, r, "rentalId")
	if !ok {
		return
	}
	var req adminForfeitDepositRequest
	if err := pkg_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	result, err := h.paymentService.ForfeitDeposit(r.Context(), shared_middleware.GetUserID(r.Context()), shared_middleware.GetUserRole(r.Context()), rentalID, req.ReasonCode, time.Now().UTC())
	if err != nil {
		if errors.Is(err, payment.ErrAdminRequired) {
			shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", err.Error())
			return
		}
		if errors.Is(err, payment.ErrInvalidReasonCode) {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			return
		}
		if errors.Is(err, payment.ErrDepositHoldNotFound) {
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			return
		}
		if errors.Is(err, payment.ErrDepositSettlementNotAllowed) || errors.Is(err, payment.ErrDepositAlreadySettled) {
			shared_response.Error(w, http.StatusConflict, "DEPOSIT_SETTLEMENT_FAILED", err.Error())
			return
		}
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"changed": result.Changed, "status": result.Status})
}

func admin(w http.ResponseWriter, r *http.Request) bool {
	if !shared_middleware.IsAdmin(r.Context()) {
		shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", "Admin access is required")
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid ID")
		return 0, false
	}
	return id, true
}

func rentalDTO(rent *rental.Rental) map[string]any {
	return rentalMap(
		rent.ID,
		rent.UserID,
		rent.AccountID,
		int16(rent.Status),
		rent.Period.StartAt,
		rent.Period.EndAt,
		rent.CreatedAt.Add(15*time.Minute),
		rent.RentalPrice.Amount,
		rent.DepositAmount.Amount,
		publicDepositStatus(rent.DepositAmount.Amount, 0),
		nil,
	)
}

func adminRentalSummaryDTO(s payment.AdminRentalSummary) map[string]any {
	return map[string]any{
		"total_count":                  s.TotalCount,
		"eligible_wallet_refund_count": s.EligibleWalletRefundCount,
		"rental_status_counts":         s.RentalStatusCounts,
		"payment_status_counts":        s.PaymentStatusCounts,
		"refund_status_counts":         s.RefundStatusCounts,
	}
}

func adminRentalEntryDTO(item payment.AdminRentalEntry) map[string]any {
	dto := rentalMap(
		item.ID,
		item.UserID,
		item.AccountID,
		item.Status,
		item.StartedAt,
		item.ExpiresAt,
		item.PaymentExpiresAt,
		item.RentalPrice,
		item.SecurityDeposit,
		item.DepositStatus,
		&publicRentalRefundSummary{
			HasRefund:   item.HasRefund,
			Status:      item.RefundStatus,
			TotalAmount: item.RefundTotal,
			ProcessedAt: item.ProcessedAt,
		},
	)
	if item.PaymentID > 0 {
		dto["payment_id"] = item.PaymentID
	}
	dto["payment_status"] = item.PaymentStatus
	dto["payment_provider"] = item.PaymentProvider
	return dto
}

func adminRentalDetailDTO(detail *payment.AdminRentalDetail) map[string]any {
	result := map[string]any{
		"rental": map[string]any{
			"id":                 detail.Rental.ID,
			"user_id":            detail.Rental.UserID,
			"account_id":         detail.Rental.AccountID,
			"status":             detail.Rental.Status,
			"start_at":           detail.Rental.StartAt,
			"end_at":             detail.Rental.EndAt,
			"rental_price":       money(detail.Rental.RentalPrice),
			"deposit_amount":     money(detail.Rental.DepositAmount),
			"payment_expires_at": detail.Rental.PaymentExpiresAt,
			"created_at":         detail.Rental.CreatedAt,
			"updated_at":         detail.Rental.UpdatedAt,
		},
		"payment": nil,
		"deposit": nil,
		"refund_summary": map[string]any{
			"count":                    detail.RefundSummary.Count,
			"latest_refund_status":     detail.RefundSummary.LatestStatus,
			"total_refunded_principal": money(detail.RefundSummary.TotalRefundedPrimary),
			"total_refunded_deposit":   money(detail.RefundSummary.TotalRefundedDeposit),
			"latest_processed_at":      detail.RefundSummary.LatestProcessedAt,
		},
		"ledger_summary": map[string]any{
			"counts_by_display_type": detail.LedgerSummary.CountsByDisplayType,
			"totals_by_display_type": adminLedgerTotalsDTO(detail.LedgerSummary.TotalsByDisplayType),
			"latest_entries":         adminLedgerEntriesDTO(detail.LedgerSummary.LatestEntries),
		},
		"support_flags": map[string]any{
			"eligible_wallet_refund":        detail.SupportFlags.EligibleWalletRefund,
			"refund_ineligible_reason":      detail.SupportFlags.RefundIneligibleReason,
			"has_active_credentials_access": detail.SupportFlags.HasActiveCredentials,
			"payment_window_expired":        detail.SupportFlags.PaymentWindowExpired,
		},
	}
	if detail.Payment != nil {
		result["payment"] = map[string]any{
			"id":         detail.Payment.ID,
			"status":     detail.Payment.Status,
			"provider":   detail.Payment.Provider,
			"amount":     detail.Payment.Amount,
			"currency":   detail.Payment.Currency,
			"created_at": detail.Payment.CreatedAt,
		}
	}
	if detail.Deposit != nil {
		result["deposit"] = map[string]any{
			"amount":       detail.Deposit.Amount,
			"currency":     detail.Deposit.Currency,
			"status":       detail.Deposit.Status,
			"held_at":      detail.Deposit.HeldAt,
			"released_at":  detail.Deposit.ReleasedAt,
			"forfeited_at": detail.Deposit.ForfeitedAt,
			"refunded_at":  detail.Deposit.RefundedAt,
		}
	}
	return result
}

func adminLedgerTotalsDTO(totals map[string]int64) map[string]any {
	result := make(map[string]any, len(totals))
	for key, value := range totals {
		result[key] = money(value)
	}
	return result
}

func adminLedgerEntriesDTO(entries []payment.AdminRentalSafeLedgerEntry) []map[string]any {
	items := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		items = append(items, map[string]any{
			"id":           entry.ID,
			"display_type": entry.DisplayType,
			"amount":       entry.Amount,
			"currency":     entry.Currency,
			"created_at":   entry.CreatedAt,
		})
	}
	return items
}

type publicRentalRefundSummary struct {
	HasRefund   bool
	Status      string
	TotalAmount int64
	ProcessedAt *time.Time
}

func rentalMap(id, userID, accountID int64, status int16, start, end, paymentExpiresAt time.Time, price, deposit int64, depositStatus string, refundSummary *publicRentalRefundSummary) map[string]any {
	dto := map[string]any{
		"id":                 id,
		"user_id":            userID,
		"account_id":         accountID,
		"status":             status,
		"started_at":         start,
		"expires_at":         end,
		"payment_expires_at": paymentExpiresAt,
		"rental_price":       money(price),
		"security_deposit":   money(deposit),
		"deposit_status":     depositStatus,
		"total_price":        money(price + deposit),
	}
	if refundSummary == nil {
		dto["has_refund"] = false
		dto["refund_status"] = "NONE"
		dto["refund_total_amount"] = money(0)
		return dto
	}
	dto["has_refund"] = refundSummary.HasRefund
	dto["refund_status"] = refundSummary.Status
	dto["refund_total_amount"] = money(refundSummary.TotalAmount)
	if refundSummary.ProcessedAt != nil {
		dto["processed_at"] = *refundSummary.ProcessedAt
	}
	return dto
}

func publicRefundSummary(statusCode int16, amount int64, processedAt sql.NullTime) *publicRentalRefundSummary {
	if statusCode == 0 {
		return nil
	}
	summary := &publicRentalRefundSummary{
		HasRefund:   true,
		Status:      publicRefundStatus(statusCode),
		TotalAmount: amount,
	}
	if processedAt.Valid {
		processed := processedAt.Time
		summary.ProcessedAt = &processed
	}
	return summary
}

func publicRefundStatus(statusCode int16) string {
	switch statusCode {
	case 1:
		return "REQUESTED"
	case 2:
		return "COMPLETED"
	case 3:
		return "FAILED"
	default:
		return "UNKNOWN"
	}
}

func publicDepositStatus(depositAmount int64, holdStatus int16) string {
	if depositAmount <= 0 {
		return "NONE"
	}
	if holdStatus == 0 {
		return "NONE"
	}
	switch holdStatus {
	case 1:
		return "HELD"
	case 2:
		return "RELEASED"
	case 3:
		return "FORFEITED"
	case 4:
		return "REFUNDED"
	default:
		return "UNKNOWN"
	}
}

func ledgerPaginationParams(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	page := 1
	pageSize := 20

	if pageRaw := strings.TrimSpace(r.URL.Query().Get("page")); pageRaw != "" {
		parsed, err := strconv.Atoi(pageRaw)
		if err != nil || parsed <= 0 {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "page must be a positive integer")
			return 0, 0, false
		}
		page = parsed
	}

	if pageSizeRaw := strings.TrimSpace(r.URL.Query().Get("page_size")); pageSizeRaw != "" {
		parsed, err := strconv.Atoi(pageSizeRaw)
		if err != nil || parsed <= 0 {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "page_size must be a positive integer")
			return 0, 0, false
		}
		pageSize = parsed
	}

	return page, pageSize, true
}

func adminPaginationParams(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	page, pageSize, ok := ledgerPaginationParams(w, r)
	if !ok {
		return 0, 0, false
	}
	if pageSize > 100 {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "page_size must be <= 100")
		return 0, 0, false
	}
	return page, pageSize, true
}

func adminRentalListFilters(w http.ResponseWriter, r *http.Request) (payment.AdminRentalListFilter, bool) {
	page, pageSize, ok := adminPaginationParams(w, r)
	if !ok {
		return payment.AdminRentalListFilter{}, false
	}

	filters := payment.AdminRentalListFilter{
		RentalStatus:    strings.TrimSpace(r.URL.Query().Get("rental_status")),
		PaymentStatus:   strings.TrimSpace(r.URL.Query().Get("payment_status")),
		PaymentProvider: strings.TrimSpace(r.URL.Query().Get("payment_provider")),
		DepositStatus:   strings.TrimSpace(r.URL.Query().Get("deposit_status")),
		RefundStatus:    strings.TrimSpace(r.URL.Query().Get("refund_status")),
		Page:            page,
		PageSize:        pageSize,
	}

	if raw := strings.TrimSpace(r.URL.Query().Get("eligible_wallet_refund")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "eligible_wallet_refund must be true or false")
			return payment.AdminRentalListFilter{}, false
		}
		filters.EligibleWalletRefund = &value
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("user_id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "user_id must be a positive integer")
			return payment.AdminRentalListFilter{}, false
		}
		filters.UserID = value
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("rental_id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "rental_id must be a positive integer")
			return payment.AdminRentalListFilter{}, false
		}
		filters.RentalID = value
	}

	return filters, true
}

func clientIPFromRequest(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.TrimSpace(strings.Split(ip, ",")[0])
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return strings.TrimSpace(ip)
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return strings.TrimSpace(ip)
}

func money(amount int64) map[string]any {
	return map[string]any{"amount": amount, "currency": "USD"}
}

func PaymentSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
