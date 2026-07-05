package account

import (
	"net/http"
	"strconv"

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

func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	params := ParseAndValidateQueryParams(r)

	accounts, total, err := h.service.ListAccounts(r.Context(), params)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to retrieve accounts")
		return
	}

	accResponses := make([]AccountResponse, 0, len(accounts))
	for _, acc := range accounts {
		games := make([]AccountGameResponse, 0, len(acc.Games))
		for _, g := range acc.Games {
			games = append(games, AccountGameResponse{
				GameID:     g.Game.ID,
				Name:       g.Game.Name,
				SteamAppID: g.Game.SteamAppID,
				Playtime:   g.PlaytimeMinutes,
			})
		}

		accResponses = append(accResponses, AccountResponse{
			ID:        acc.ID,
			SteamID64: acc.Credentials.SteamID64,
			Status:    acc.Status.String(),
			PricePerHour: MoneyResponse{
				Amount:   acc.HourlyPrice.Amount,
				Currency: acc.HourlyPrice.Currency,
			},
			SecurityDeposit: MoneyResponse{
				Amount:   acc.DepositAmount.Amount,
				Currency: acc.DepositAmount.Currency,
			},
			Games: games,
		})
	}

	totalPages := int(total) / params.PageSize
	if int(total)%params.PageSize != 0 {
		totalPages++
	}

	res := PaginatedAccountsResponse{
		Accounts: accResponses,
		Pagination: PaginationInfo{
			Page:       params.Page,
			PageSize:   params.PageSize,
			TotalItems: total,
			TotalPages: totalPages,
		},
	}

	shared_response.JSON(w, http.StatusOK, res)
}

func (h *Handler) GetAccount(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("accountId")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid account ID format")
		return
	}

	acc, err := h.service.GetAccountByID(r.Context(), id)
	if err != nil {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Account not found")
		return
	}

	games := make([]AccountGameResponse, 0, len(acc.Games))
	for _, g := range acc.Games {
		games = append(games, AccountGameResponse{
			GameID:     g.Game.ID,
			Name:       g.Game.Name,
			SteamAppID: g.Game.SteamAppID,
			Playtime:   g.PlaytimeMinutes,
		})
	}

	res := AccountResponse{
		ID:        acc.ID,
		SteamID64: acc.Credentials.SteamID64,
		Status:    acc.Status.String(),
		PricePerHour: MoneyResponse{
			Amount:   acc.HourlyPrice.Amount,
			Currency: acc.HourlyPrice.Currency,
		},
		SecurityDeposit: MoneyResponse{
			Amount:   acc.DepositAmount.Amount,
			Currency: acc.DepositAmount.Currency,
		},
		Games: games,
	}

	shared_response.JSON(w, http.StatusOK, res)
}

func (h *Handler) GetAvailability(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("accountId")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid account ID format")
		return
	}

	available, err := h.service.CheckAvailability(r.Context(), id)
	if err != nil {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Account not found")
		return
	}

	shared_response.JSON(w, http.StatusOK, map[string]interface{}{
		"account_id": id,
		"available":  available,
	})
}
