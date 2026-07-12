package user

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	pkg_http_server "rent_game_accs/internal/pkg/transport/http/server"
	shared_logger "rent_game_accs/internal/shared/logger"
	shared_middleware "rent_game_accs/internal/shared/middleware"
)

func (h *Handler) Routes(jwtSecret string, log *shared_logger.Logger, pool *pgxpool.Pool) []pkg_http_server.Route {
	authMw := shared_middleware.Auth(jwtSecret, log, pool)

	return []pkg_http_server.Route{
		pkg_http_server.NewRoute("GET", "/users/{id}", wrap(h.GetProfile, authMw)),
		pkg_http_server.NewRoute("PATCH", "/users/{id}", wrap(h.UpdateProfile, authMw)),
	}
}

func wrap(h http.HandlerFunc, mws ...func(http.Handler) http.Handler) http.HandlerFunc {
	var final http.Handler = h
	for i := len(mws) - 1; i >= 0; i-- {
		final = mws[i](final)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		final.ServeHTTP(w, r)
	})
}
