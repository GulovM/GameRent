package user

import (
	"net/http"
	"strconv"

	"go.uber.org/zap"
	pkg_http_request "rent_game_accs/internal/pkg/transport/http/request"
	shared_middleware "rent_game_accs/internal/shared/middleware"
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

func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid user ID format")
		return
	}
	if requesterID := shared_middleware.GetUserID(r.Context()); requesterID == 0 || (requesterID != id && !shared_middleware.IsAdmin(r.Context())) {
		shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", "You can access only your own profile")
		return
	}

	u, err := h.service.GetUserByID(r.Context(), id)
	if err != nil {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	res := UserResponse{
		ID:         u.ID,
		Email:      u.Email,
		FirstName:  u.FirstName,
		LastName:   u.LastName,
		TrustScore: u.TrustScore,
		TrustLevel: string(u.TrustLevel()),
		Role:       string(u.Role),
		IsBlocked:  u.IsBlocked,
		CreatedAt:  u.CreatedAt,
		UpdatedAt:  u.UpdatedAt,
	}

	shared_response.JSON(w, http.StatusOK, res)
}

func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid user ID format")
		return
	}
	if requesterID := shared_middleware.GetUserID(r.Context()); requesterID == 0 || (requesterID != id && !shared_middleware.IsAdmin(r.Context())) {
		shared_response.Error(w, http.StatusForbidden, "FORBIDDEN", "You can update only your own profile")
		return
	}

	var req UpdateUserRequest
	if err := pkg_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	u, err := h.service.UpdateUser(r.Context(), id, req.FirstName, req.LastName)
	if err != nil {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	res := UserResponse{
		ID:         u.ID,
		Email:      u.Email,
		FirstName:  u.FirstName,
		LastName:   u.LastName,
		TrustScore: u.TrustScore,
		TrustLevel: string(u.TrustLevel()),
		Role:       string(u.Role),
		IsBlocked:  u.IsBlocked,
		CreatedAt:  u.CreatedAt,
		UpdatedAt:  u.UpdatedAt,
	}

	shared_response.JSON(w, http.StatusOK, res)
}
