package user

import (
	"errors"
	"strings"
	"unicode"
)

func (r *UpdateUserRequest) Validate() error {
	r.FirstName = strings.TrimSpace(r.FirstName)
	r.LastName = strings.TrimSpace(r.LastName)

	if err := validateName("first name", r.FirstName); err != nil {
		return err
	}
	if err := validateName("last name", r.LastName); err != nil {
		return err
	}
	return nil
}

func validateName(field, value string) error {
	if value == "" {
		return errors.New(field + " is required")
	}
	if len([]rune(value)) < 2 || len([]rune(value)) > 50 {
		return errors.New(field + " length must be between 2 and 50 characters")
	}
	for _, ch := range value {
		if unicode.IsLetter(ch) || ch == '-' || ch == '\'' || ch == ' ' {
			continue
		}
		return errors.New(field + " can contain only letters, spaces, hyphen and apostrophe")
	}
	return nil
}
