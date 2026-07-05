package pkg_http_utils

import (
	"fmt"
	"net/http"
	pkg_errors "rent_game_accs/internal/pkg/errors"
	"strconv"
)

func GetIntQueryParam(r *http.Request, key string) (*int, error) {
	param := r.URL.Query().Get(key)
	if param == "" {
		return nil, nil
	}

	val, err := strconv.Atoi(param)
	if err != nil {
		return nil, fmt.Errorf(
			"param %s by key'%s' not a valid integer: %v: %w",
			param,
			key,
			err,
			pkg_errors.ErrInvalidArgument,
		)
	}

	return &val, nil
}
