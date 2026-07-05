package account

import (
	pkg_http_server "rent_game_accs/internal/pkg/transport/http/server"
)

func (h *Handler) Routes() []pkg_http_server.Route {
	return []pkg_http_server.Route{
		pkg_http_server.NewRoute("GET", "/accounts", h.ListAccounts),
		pkg_http_server.NewRoute("GET", "/accounts/{accountId}", h.GetAccount),
		pkg_http_server.NewRoute("GET", "/accounts/{accountId}/availability", h.GetAvailability),
	}
}
