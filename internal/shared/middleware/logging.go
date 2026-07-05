package middleware

import (
	"net/http"
	"time"

	"go.uber.org/zap"
	shared_logger "rent_game_accs/internal/shared/logger"
)

type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriterWrapper) Write(b []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func Logging(log *shared_logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			reqID := GetRequestID(r.Context())

			var userID string
			if val := r.Context().Value("user_id"); val != nil {
				if id, ok := val.(string); ok {
					userID = id
				}
			}

			ip := r.RemoteAddr
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				ip = xff
			}

			wrapper := &responseWriterWrapper{ResponseWriter: w, statusCode: http.StatusOK}

			ctxLogger := log.With(
				zap.String("request_id", reqID),
				zap.String("ip_address", ip),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
			)
			if userID != "" {
				ctxLogger = ctxLogger.With(zap.String("user_id", userID))
			}

			ctx := shared_logger.ToContext(r.Context(), ctxLogger)

			next.ServeHTTP(wrapper, r.WithContext(ctx))

			latency := time.Since(start)

			ctxLogger.Info("http request completed",
				zap.Int("status_code", wrapper.statusCode),
				zap.Duration("latency", latency),
			)
		})
	}
}
