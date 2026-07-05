package game

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

func (h *Handler) ListGames(w http.ResponseWriter, r *http.Request) {
	params := ParseAndValidateQueryParams(r)

	games, total, err := h.service.ListGames(r.Context(), params.Page, params.PageSize, params.Search)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to retrieve game catalog")
		return
	}

	gameResponses := make([]GameResponse, 0, len(games))
	for _, g := range games {
		gameResponses = append(gameResponses, GameResponse{
			ID:          g.ID,
			SteamAppID:  g.SteamAppID,
			Name:        g.Name,
			HeaderImage: g.HeaderImage,
		})
	}

	totalPages := int(total) / params.PageSize
	if int(total)%params.PageSize != 0 {
		totalPages++
	}

	res := CatalogResponse{
		Games: gameResponses,
		Pagination: PaginationInfo{
			Page:       params.Page,
			PageSize:   params.PageSize,
			TotalItems: total,
			TotalPages: totalPages,
		},
	}

	shared_response.JSON(w, http.StatusOK, res)
}

func (h *Handler) GetGame(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("gameId")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid game ID format")
		return
	}

	g, err := h.service.GetGameByID(r.Context(), id)
	if err != nil {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Game not found")
		return
	}

	shared_response.JSON(w, http.StatusOK, g)
}
