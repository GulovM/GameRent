package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"rent_game_accs/internal/account"
	pkg_http_request "rent_game_accs/internal/pkg/transport/http/request"
	pkg_http_server "rent_game_accs/internal/pkg/transport/http/server"
	"rent_game_accs/internal/rental"
	shared_logger "rent_game_accs/internal/shared/logger"
	shared_middleware "rent_game_accs/internal/shared/middleware"
	shared_response "rent_game_accs/internal/shared/response"
)

type Handler struct {
	pool          *pgxpool.Pool
	rentalService *rental.Service
	accountRepo   account.Repository
}

func NewHandler(pool *pgxpool.Pool, rentalService *rental.Service, accountRepo account.Repository) *Handler {
	return &Handler{pool: pool, rentalService: rentalService, accountRepo: accountRepo}
}

func (h *Handler) Routes(jwtSecret string, log *shared_logger.Logger) []pkg_http_server.Route {
	authMw := shared_middleware.Auth(jwtSecret, log)
	return []pkg_http_server.Route{
		pkg_http_server.NewRoute("GET", "/me/rentals", wrap(h.ListMyRentals, authMw)),
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
		pkg_http_server.NewRoute("POST", "/admin/accounts", wrap(h.AdminCreateAccount, authMw)),
		pkg_http_server.NewRoute("PATCH", "/admin/accounts/{accountId}", wrap(h.AdminUpdateAccount, authMw)),
		pkg_http_server.NewRoute("POST", "/admin/accounts/{accountId}/sync", wrap(h.AdminSyncAccount, authMw)),
		pkg_http_server.NewRoute("GET", "/admin/users", wrap(h.AdminListUsers, authMw)),
		pkg_http_server.NewRoute("PATCH", "/admin/users/{userId}", wrap(h.AdminUpdateUser, authMw)),
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
	rent, err := h.rentalService.RentAccount(r.Context(), userID, req.AccountID, time.Duration(req.DurationHours)*time.Hour, time.Now())
	if err != nil {
		shared_response.Error(w, http.StatusConflict, "RENTAL_FAILED", err.Error())
		return
	}
	_, _ = h.pool.Exec(r.Context(), `INSERT INTO payments (rental_id, user_id, payment_type, status, amount, currency) VALUES ($1, $2, 1, 1, $3, 'USD')`, rent.ID, userID, rent.RentalPrice.Amount+rent.DepositAmount.Amount)
	shared_response.JSON(w, http.StatusCreated, rentalDTO(rent))
}

func (h *Handler) ListMyRentals(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	rows, err := h.pool.Query(r.Context(), `SELECT id, user_id, account_id, status, start_at, end_at, rental_price, deposit_amount FROM rentals WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id, uid, accountID, price, deposit int64
		var status int16
		var start, end time.Time
		if err := rows.Scan(&id, &uid, &accountID, &status, &start, &end, &price, &deposit); err != nil {
			shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		items = append(items, rentalMap(id, uid, accountID, status, start, end, price, deposit))
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
	var status int16
	var start, end time.Time
	err := h.pool.QueryRow(r.Context(), `SELECT user_id, account_id, status, start_at, end_at, rental_price, deposit_amount FROM rentals WHERE id=$1`, id).Scan(&uid, &accountID, &status, &start, &end, &price, &deposit)
	if err != nil {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Rental not found")
		return
	}
	if uid != userID && userID != 1 {
		shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", "You can access only your rentals")
		return
	}
	shared_response.JSON(w, http.StatusOK, rentalMap(id, uid, accountID, status, start, end, price, deposit))
}

func (h *Handler) CancelRental(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	id, ok := pathID(w, r, "rentalId")
	if !ok {
		return
	}
	tag, err := h.pool.Exec(r.Context(), `UPDATE rentals SET status=5, cancellation_reason='cancelled by user', actual_finished_at=NOW(), updated_at=NOW() WHERE id=$1 AND user_id=$2 AND status IN (1,2)`, id, userID)
	if err != nil || tag.RowsAffected() == 0 {
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
	shared_response.JSON(w, http.StatusOK, map[string]any{"price_per_hour": money(hourly), "security_deposit": money(deposit), "duration_hours": req.DurationHours, "total_price": money(hourly*int64(req.DurationHours) + deposit)})
}

type extendRentalRequest struct {
	DurationHours int `json:"duration_hours"`
}

func (r *extendRentalRequest) Validate() error {
	if r.DurationHours < 1 || r.DurationHours > 720 {
		return errText("duration_hours must be between 1 and 720")
	}
	return nil
}

func (h *Handler) ExtendRental(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req extendRentalRequest
	if err := pkg_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	tag, err := h.pool.Exec(r.Context(), `UPDATE rentals SET end_at=end_at + ($1::TEXT || ' hours')::INTERVAL, updated_at=NOW() WHERE id=$2 AND user_id=$3 AND status=2`, req.DurationHours, id, userID)
	if err != nil || tag.RowsAffected() == 0 {
		shared_response.Error(w, http.StatusConflict, "EXTEND_FAILED", "Rental cannot be extended")
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]string{"message": "Rental extended"})
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
	err = h.pool.QueryRow(r.Context(), `INSERT INTO payments (rental_id, user_id, payment_type, status, amount, currency) VALUES ($1,$2,1,1,$3,'USD') RETURNING id`, req.RentalID, userID, amount).Scan(&id)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	shared_response.JSON(w, http.StatusCreated, map[string]any{"id": id, "rental_id": req.RentalID, "amount": amount, "currency": "USD", "status": "Waiting"})
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SteamID64 == "" || req.SteamLogin == "" || req.SteamPassword == "" || req.PricePerHour <= 0 {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "steam credentials and positive price_per_hour are required")
		return
	}
	encrypted, err := h.accountRepo.Encrypt(req.SteamPassword)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	var id int64
	err = h.pool.QueryRow(r.Context(), `INSERT INTO accounts (steam_id64,login,encrypted_password,hourly_price,deposit_amount,status,steam_guard_enabled,inventory_verified,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,2,true,true,NOW(),NOW()) RETURNING id`, req.SteamID64, req.SteamLogin, encrypted, req.PricePerHour, req.SecurityDeposit).Scan(&id)
	if err != nil {
		shared_response.Error(w, http.StatusConflict, "CREATE_FAILED", err.Error())
		return
	}
	shared_response.JSON(w, http.StatusCreated, map[string]any{"id": id})
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
		Status          *int16 `json:"status"`
		PricePerHour    *int64 `json:"price_per_hour"`
		SecurityDeposit *int64 `json:"security_deposit"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	_, err := h.pool.Exec(r.Context(), `UPDATE accounts SET status=COALESCE($1,status), hourly_price=COALESCE($2,hourly_price), deposit_amount=COALESCE($3,deposit_amount), updated_at=NOW() WHERE id=$4`, req.Status, req.PricePerHour, req.SecurityDeposit, id)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
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
	_, _ = h.pool.Exec(r.Context(), `UPDATE accounts SET library_synced_at=NOW(), updated_at=NOW() WHERE id=$1`, id)
	shared_response.JSON(w, http.StatusOK, map[string]string{"message": "Account sync marked"})
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
		Balance    *int64  `json:"balance"`
		Role       *string `json:"role"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Role != nil && *req.Role != "ADMIN" && *req.Role != "RENT" {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "role must be ADMIN or RENT")
		return
	}
	_, err := h.pool.Exec(r.Context(), `UPDATE users SET trust_score=COALESCE($1,trust_score), is_blocked=COALESCE($2,is_blocked), balance=COALESCE($3,balance), role=COALESCE($4,role), updated_at=NOW() WHERE id=$5`, req.TrustScore, req.IsBlocked, req.Balance, req.Role, id)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]string{"message": "User updated"})
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
	return rentalMap(rent.ID, rent.UserID, rent.AccountID, int16(rent.Status), rent.Period.StartAt, rent.Period.EndAt, rent.RentalPrice.Amount, rent.DepositAmount.Amount)
}

func rentalMap(id, userID, accountID int64, status int16, start, end time.Time, price, deposit int64) map[string]any {
	return map[string]any{"id": id, "user_id": userID, "account_id": accountID, "status": status, "started_at": start, "expires_at": end, "rental_price": money(price), "security_deposit": money(deposit), "total_price": money(price + deposit)}
}

func money(amount int64) map[string]any {
	return map[string]any{"amount": amount, "currency": "USD"}
}

func PaymentSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
