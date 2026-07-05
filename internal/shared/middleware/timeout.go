package middleware

import (
	"encoding/json"
	"net/http"
	"time"

	shared_response "rent_game_accs/internal/shared/response"
)

func Timeout(duration time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		errResp := shared_response.Response{
			Success: false,
			Error: &shared_response.ErrorData{
				Code:    "REQUEST_TIMEOUT",
				Message: "The request exceeded the allowed time limit.",
			},
		}

		errJSON, _ := json.Marshal(errResp)

		return http.TimeoutHandler(next, duration, string(errJSON))
	}
}
