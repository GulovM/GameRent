package auth

import (
	"errors"
	"net/http"

	"go.uber.org/zap"
	pkg_http_request "rent_game_accs/internal/pkg/transport/http/request"
	shared_logger "rent_game_accs/internal/shared/logger"
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

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := pkg_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	user, accessToken, refreshToken, err := h.service.Register(r.Context(), req.Email, req.Password, req.FirstName, req.LastName)
	if err != nil {
		h.log.Warn("registration failed", zap.Error(err))
		shared_response.Error(w, http.StatusConflict, "REGISTRATION_FAILED", err.Error())
		return
	}

	res := RegisterResponse{
		User: UserResponse{
			ID:            user.ID,
			Email:         user.Email,
			FirstName:     user.FirstName,
			LastName:      user.LastName,
			Role:          string(user.Role),
			EmailVerified: user.EmailVerified,
			IsBlocked:     user.IsBlocked,
		},
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}

	shared_response.JSON(w, http.StatusCreated, res)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := pkg_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	accessToken, refreshToken, err := h.service.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		h.log.Warn("login failed", zap.Error(err), zap.String("email", req.Email))
		shared_response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid email or password")
		return
	}

	res := LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}

	shared_response.JSON(w, http.StatusOK, res)
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := pkg_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	accessToken, refreshToken, err := h.service.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		h.log.Warn("token refresh failed", zap.Error(err))
		if errors.Is(err, ErrTokenExpired) || errors.Is(err, ErrTokenAlreadyRevoked) {
			shared_response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
			return
		}
		shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	res := RefreshResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}

	shared_response.JSON(w, http.StatusOK, res)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := pkg_http_request.DecodeAndValidateRequest(r, &req); err != nil {
		shared_response.Error(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	if err := h.service.Logout(r.Context(), req.RefreshToken); err != nil {
		h.log.Warn("logout failed", zap.Error(err))
		shared_response.Error(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	shared_response.JSON(w, http.StatusOK, map[string]string{"message": "Logged out successfully"})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	if userID == 0 {
		shared_response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized")
		return
	}

	user, err := h.service.Me(r.Context(), userID)
	if err != nil {
		shared_logger.FromContext(r.Context()).Warn("fetch current user failed", zap.Error(err), zap.Int64("user_id", userID))
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "User not found")
		return
	}

	res := UserResponse{
		ID:            user.ID,
		Email:         user.Email,
		FirstName:     user.FirstName,
		LastName:      user.LastName,
		Role:          string(user.Role),
		EmailVerified: user.EmailVerified,
		IsBlocked:     user.IsBlocked,
	}

	shared_response.JSON(w, http.StatusOK, res)
}
