package middleware

import (
	"context"
	"net/http"
	"strings"

	shared_auth "rent_game_accs/internal/shared/auth"
	shared_logger "rent_game_accs/internal/shared/logger"
	shared_response "rent_game_accs/internal/shared/response"
)

type contextKey string

const UserIDKey contextKey = "user_id"
const UserRoleKey contextKey = "user_role"

func Auth(secret string, log *shared_logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				shared_response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authorization header is required")
				return
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				shared_response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authorization header must be Bearer token")
				return
			}

			token := parts[1]
			claims, err := shared_auth.ValidateToken(token, secret)
			if err != nil {
				shared_response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid or expired token")
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
			ctx = context.WithValue(ctx, UserRoleKey, claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUserRole(ctx context.Context) string {
	if role, ok := ctx.Value(UserRoleKey).(string); ok {
		return role
	}
	return ""
}

func IsAdmin(ctx context.Context) bool {
	return GetUserRole(ctx) == "ADMIN"
}

func GetUserID(ctx context.Context) int64 {
	if id, ok := ctx.Value(UserIDKey).(int64); ok {
		return id
	}
	return 0
}
