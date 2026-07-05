package auth

import (
	"errors"
	"strings"
	"unicode"
)

func (r *RegisterRequest) Validate() error {
	r.Email = strings.TrimSpace(strings.ToLower(r.Email))
	r.FirstName = strings.TrimSpace(r.FirstName)
	r.LastName = strings.TrimSpace(r.LastName)

	if err := ValidateEmail(r.Email); err != nil {
		return err
	}
	if err := validatePassword(r.Password); err != nil {
		return err
	}
	if err := validateName("first name", r.FirstName); err != nil {
		return err
	}
	if err := validateName("last name", r.LastName); err != nil {
		return err
	}
	return nil
}

func (r *LoginRequest) Validate() error {
	r.Email = strings.TrimSpace(strings.ToLower(r.Email))
	if err := ValidateEmail(r.Email); err != nil {
		return err
	}
	if len(r.Password) == 0 {
		return errors.New("password is required")
	}
	return nil
}

func (r *RefreshRequest) Validate() error {
	if len(r.RefreshToken) == 0 {
		return errors.New("refresh token is required")
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < 8 {
		return ErrPasswordTooShort
	}

	var hasLetter, hasDigit bool
	for _, ch := range password {
		if unicode.IsLetter(ch) {
			hasLetter = true
		}
		if unicode.IsDigit(ch) {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return errors.New("password must contain at least one letter and one digit")
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
