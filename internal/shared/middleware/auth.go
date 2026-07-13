package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	shared_auth "rent_game_accs/internal/shared/auth"
	shared_authorization "rent_game_accs/internal/shared/authorization"
	"rent_game_accs/internal/shared/database"
	shared_logger "rent_game_accs/internal/shared/logger"
	shared_response "rent_game_accs/internal/shared/response"
)

type contextKey string

const UserIDKey contextKey = "user_id"
const UserRoleKey contextKey = "user_role"

func Auth(secret string, log *shared_logger.Logger, pool *pgxpool.Pool) func(http.Handler) http.Handler {
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

			currentUser, err := shared_authorization.LoadCurrentUser(r.Context(), database.GetTxOrPool(r.Context(), pool), claims.UserID, false)
			if err != nil {
				if errors.Is(err, shared_authorization.ErrCurrentUserForbidden) {
					shared_response.Error(w, http.StatusUnauthorized, "SESSION_REVOKED", "Current session has been revoked")
					return
				}
				if log != nil {
					log.Error("failed to load current authenticated user", zap.Error(err))
				}
				shared_response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to authorize current user")
				return
			}

			effectiveRole := currentUser.Role

			if effectiveRole == "ADMIN" && claims.Role != "ADMIN" {
				effectiveRole = "RENT"
			}
			ctx := context.WithValue(r.Context(), UserIDKey, currentUser.ID)
			ctx = context.WithValue(ctx, UserRoleKey, effectiveRole)
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
