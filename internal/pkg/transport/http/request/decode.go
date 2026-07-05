package pkg_http_request

import (
	"encoding/json"
	"fmt"
	"net/http"
	pkg_errors "rent_game_accs/internal/pkg/errors"

	"github.com/go-playground/validator/v10"
)

var requestValidator = validator.New()

type validatable interface {
	Validate() error
}

func DecodeAndValidateRequest(r *http.Request, dest any) error {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		return fmt.Errorf(
			"decode json: %v: %w",
			err,
			pkg_errors.ErrInvalidArgument,
		)
	}

	v, ok := dest.(validatable)

	var err error
	if ok {
		err = v.Validate()
	} else {
		err = requestValidator.Struct(dest)
	}

	if err != nil {
		return fmt.Errorf(
			"request validation: %v: %w",
			err,
			pkg_errors.ErrInvalidArgument,
		)
	}

	return nil
}
