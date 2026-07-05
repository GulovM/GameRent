package pkg_http_middleware

import (
	"context"
	"net/http"
	pkg_logger "rent_game_accs/internal/pkg/logger"
	"rent_game_accs/internal/pkg/monitoring"
	pkg_http_response "rent_game_accs/internal/pkg/transport/http/response"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	requestIDHeader = "X-Request-ID"
)

func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get(requestIDHeader)
			if requestID == "" {
				requestID = uuid.NewString()
			}

			r.Header.Set(requestIDHeader, requestID)
			w.Header().Set(requestIDHeader, requestID)

			next.ServeHTTP(w, r)
		})
	}
}

func Logger(log *pkg_logger.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get(requestIDHeader)

			l := log.With(
				zap.String("request_id", requestID),
				zap.String("url", r.URL.String()),
			)

			ctx := context.WithValue(r.Context(), "log", l)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func Panic() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			log := pkg_logger.FromContext(ctx)
			responseHandler := pkg_http_response.NewHTTPResponseHandler(log, w)
			defer func() {
				if p := recover(); p != nil {
					responseHandler.PanicResponse(
						p,
						"during handle HTTP request go unexpected panic",
					)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

func Trace() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			log := pkg_logger.FromContext(ctx)
			rw := pkg_http_response.NewResponseWriter(w)
			before := time.Now()
			log.Debug(
				">>>> incoming HTTP request",
				zap.String("http_method", r.Method),
				zap.Time("time", before.UTC()),
			)

			next.ServeHTTP(rw, r)

			log.Debug(
				"<<<< done HTTP request",
				zap.Int("status_code", rw.GetStatusCodeOrPanic()),
				zap.Duration("latency", time.Now().Sub(before)),
			)
		})
	}
}

func Metrics() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if path == "/metrics" || path == "/healthz" || path == "/health/live" || path == "/health/ready" {
				next.ServeHTTP(w, r)
				return
			}

			rw := pkg_http_response.NewResponseWriter(w)
			before := time.Now()

			next.ServeHTTP(rw, r)

			duration := time.Since(before).Seconds()
			statusCode := rw.GetStatusCode()
			statusStr := strconv.Itoa(statusCode)

			monitoring.HttpRequestsTotal.WithLabelValues(r.Method, path, statusStr).Inc()
			monitoring.HttpRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
		})
	}
}
