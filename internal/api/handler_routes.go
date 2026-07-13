package api

import (
	"context"
	"net/http"
	"rent_game_accs/internal/account"
	"rent_game_accs/internal/game"
	"rent_game_accs/internal/notification"
	"rent_game_accs/internal/payment"
	pkg_http_server "rent_game_accs/internal/pkg/transport/http/server"
	"rent_game_accs/internal/rental"
	repo_postgres "rent_game_accs/internal/repository/postgres"
	"rent_game_accs/internal/review"
	shared_logger "rent_game_accs/internal/shared/logger"
	"rent_game_accs/internal/user"
)

type SteamSyncRepository interface {
	GetAccountSyncDetails(ctx context.Context, accountID int64) (string, string, error)
	SyncAccountGamesAsCurrentAdmin(ctx context.Context, actorUserID, accountID int64, games []repo_postgres.AccountGameSyncInfo) error
	DisableAccountIfIdleAsCurrentAdmin(ctx context.Context, actorUserID, accountID int64) error
}

type Handler struct {
	authMiddleware      AuthMiddlewareFactory
	rentalService       *rental.Service
	paymentService      payment.Service
	adminAccountService *account.AdminService
	adminUserService    *user.AdminService
	steamClient         game.SteamClient
	steamSyncRepo       SteamSyncRepository
	reviewService       *review.Service
	notificationService *notification.Service
}

type AuthMiddlewareFactory func(secret string, log *shared_logger.Logger) func(http.Handler) http.Handler

func NewHandler(
	authMiddleware AuthMiddlewareFactory,
	rentalService *rental.Service,
	paymentService payment.Service,
	adminAccountService *account.AdminService,
	adminUserService *user.AdminService,
	steamClient game.SteamClient,
	steamSyncRepo SteamSyncRepository,
	reviewService *review.Service,
	notificationService *notification.Service,
) *Handler {
	return &Handler{
		authMiddleware:      authMiddleware,
		rentalService:       rentalService,
		paymentService:      paymentService,
		adminAccountService: adminAccountService,
		adminUserService:    adminUserService,
		steamClient:         steamClient,
		steamSyncRepo:       steamSyncRepo,
		reviewService:       reviewService,
		notificationService: notificationService,
	}
}

func (h *Handler) Routes(jwtSecret string, log *shared_logger.Logger) []pkg_http_server.Route {
	authMw := h.authMiddleware(jwtSecret, log)
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
