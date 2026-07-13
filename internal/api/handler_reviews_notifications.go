package api

import (
	"encoding/json"
	"net/http"
	"rent_game_accs/internal/review"
	shared_middleware "rent_game_accs/internal/shared/middleware"
	shared_response "rent_game_accs/internal/shared/response"
)

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
	id, err := h.reviewService.Create(r.Context(), review.CreateInput{
		RentalID: req.RentalID, UserID: userID, AccountID: req.AccountID, Rating: req.Rating, Comment: req.Comment,
	})
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
	items, err := h.reviewService.ListByAccount(r.Context(), accountID)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, map[string]any{"id": item.ID, "user_id": item.UserID, "account_id": item.AccountID, "rating": item.Rating, "comment": item.Comment, "created_at": item.CreatedAt})
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"reviews": response})
}

func (h *Handler) ListMyNotifications(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	items, err := h.notificationService.ListByUser(r.Context(), userID)
	if err != nil {
		shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	response := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response = append(response, map[string]any{"id": item.ID, "type": item.Type, "title": item.Title, "body": item.Body, "read": item.Read, "created_at": item.CreatedAt})
	}
	shared_response.JSON(w, http.StatusOK, map[string]any{"notifications": response})
}

func (h *Handler) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	userID := shared_middleware.GetUserID(r.Context())
	id, ok := pathID(w, r, "notificationId")
	if !ok {
		return
	}
	updated, _ := h.notificationService.MarkRead(r.Context(), id, userID)
	if !updated {
		shared_response.Error(w, http.StatusNotFound, "NOT_FOUND", "Notification not found")
		return
	}
	shared_response.JSON(w, http.StatusOK, map[string]string{"message": "Notification marked as read"})
}

func (h *Handler) FavoriteOK(w http.ResponseWriter, r *http.Request) {
	shared_response.JSON(w, http.StatusOK, map[string]string{"message": "Favorites are accepted in local MVP but not persisted"})
}
