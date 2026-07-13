package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"rent_game_accs/internal/account"
	"rent_game_accs/internal/payment"
	repo_postgres "rent_game_accs/internal/repository/postgres"
	shared_authorization "rent_game_accs/internal/shared/authorization"
	shared_middleware "rent_game_accs/internal/shared/middleware"
	shared_response "rent_game_accs/internal/shared/response"
	"strconv"
)

func (h *Handler) AdminListAccounts(w http.ResponseWriter, r *http.Request) {
	if !admin(w, r) {
		return
	}
	accounts, err := h.adminAccountService.ListAccounts(r.Context())
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load accounts")
		return
	}
	items := make([]map[string]any, 0, len(accounts))
	for _, item := range accounts {
		items = append(items, map[string]any{"id": item.ID, "steam_id64": item.Credentials.SteamID64, "status": item.Status, "hourly_price": item.HourlyPrice.Amount, "deposit_amount": item.DepositAmount.Amount})
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
	id, err := h.adminAccountService.CreateAccount(r.Context(), shared_middleware.GetUserID(r.Context()), account.AdminAccountInput{SteamID64: req.SteamID64, SteamLogin: req.SteamLogin, SteamPassword: req.SteamPassword, PricePerHour: req.PricePerHour, SecurityDeposit: req.SecurityDeposit})
	if err != nil {
		switch {
		case errors.Is(err, account.ErrAdminAuthorization):
			shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", "Current administrator authorization is required")
		case errors.Is(err, account.ErrPricingOutOfRange):
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Account pricing exceeds the supported range")
		default:
			shared_response.Error(w, http.StatusConflict, "CREATE_FAILED", "Account could not be created")
		}
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
	err := h.adminAccountService.UpdateAccount(r.Context(), shared_middleware.GetUserID(r.Context()), id, account.AdminAccountUpdate{PricePerHour: req.PricePerHour, SecurityDeposit: req.SecurityDeposit})
	if err != nil {
		switch {
		case errors.Is(err, account.ErrAdminAuthorization):
			shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", "Current administrator authorization is required")
		case errors.Is(err, account.ErrAccountNotFound):
			shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Account not found")
		case errors.Is(err, account.ErrPricingOutOfRange):
			shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Account pricing exceeds the supported range")
		default:
			shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update account")
		}
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
