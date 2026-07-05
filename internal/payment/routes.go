package payment

import (
	pkg_http_server "rent_game_accs/internal/pkg/transport/http/server"
)

func (h *Handler) Routes() []pkg_http_server.Route {
	return []pkg_http_server.Route{
		pkg_http_server.NewRoute("POST", "/payments/webhook", h.Webhook),
	}
}
