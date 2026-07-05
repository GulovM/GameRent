package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"go.uber.org/zap"
	shared_logger "rent_game_accs/internal/shared/logger"
	shared_response "rent_game_accs/internal/shared/response"
)

func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log := shared_logger.FromContext(r.Context())

				log.Error("panic recovered",
					zap.Any("error", err),
					zap.String("stack", string(debug.Stack())),
				)

				shared_response.Error(
					w,
					http.StatusInternalServerError,
					"INTERNAL_SERVER_ERROR",
					fmt.Sprintf("An unexpected error occurred: %v", err),
				)
			}
		}()

		next.ServeHTTP(w, r)
	})
}
