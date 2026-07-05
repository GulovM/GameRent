package auth

import (
	"errors"
	"net/mail"
	"strings"
	"time"
)

var (
	ErrInvalidEmail        = errors.New("invalid email address")
	ErrPasswordTooShort    = errors.New("password must be at least 8 characters long")
	ErrUserBlocked         = errors.New("user is blocked")
	ErrEmailNotVerified    = errors.New("email is not verified")
	ErrTokenExpired        = errors.New("refresh token is expired")
	ErrTokenAlreadyRevoked = errors.New("refresh token is already revoked")
)

type UserRole string

const (
	RoleAdmin UserRole = "ADMIN"
	RoleRent  UserRole = "RENT"
)

type User struct {
	ID            int64
	Email         string
	PasswordHash  string
	FirstName     string
	LastName      string
	Role          UserRole
	EmailVerified bool
	IsBlocked     bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time
}

func NewUser(email, passwordHash string, now time.Time) (*User, error) {
	u := &User{
		Email:         email,
		PasswordHash:  passwordHash,
		CreatedAt:     now,
		UpdatedAt:     now,
		Role:          RoleRent,
		EmailVerified: false,
		IsBlocked:     false,
	}
	if err := u.Validate(); err != nil {
		return nil, err
	}
	return u, nil
}

func (u *User) Validate() error {
	if err := ValidateEmail(u.Email); err != nil {
		return err
	}
	if len(u.PasswordHash) == 0 {
		return errors.New("password hash cannot be empty")
	}
	return nil
}

func ValidateEmail(email string) error {
	email = strings.TrimSpace(strings.ToLower(email))
	if len(email) == 0 {
		return ErrInvalidEmail
	}
	_, err := mail.ParseAddress(email)
	if err != nil {
		return ErrInvalidEmail
	}

	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ErrInvalidEmail
	}
	return nil
}

func (u *User) CanAuthenticate() error {
	if u.IsBlocked {
		return ErrUserBlocked
	}
	if !u.EmailVerified {
		return ErrEmailNotVerified
	}
	return nil
}

type RefreshToken struct {
	ID        int64
	UserID    int64
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

func NewRefreshToken(userID int64, tokenHash string, duration time.Duration, now time.Time) (*RefreshToken, error) {
	if userID <= 0 {
		return nil, errors.New("user ID must be positive")
	}
	if len(tokenHash) == 0 {
		return nil, errors.New("token hash cannot be empty")
	}
	if duration <= 0 {
		return nil, errors.New("token duration must be positive")
	}
	return &RefreshToken{
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: now.Add(duration),
		CreatedAt: now,
	}, nil
}

func (rt *RefreshToken) IsExpired(now time.Time) bool {
	return now.After(rt.ExpiresAt)
}

func (rt *RefreshToken) IsRevoked() bool {
	return rt.RevokedAt != nil
}

func (rt *RefreshToken) Revoke(now time.Time) error {
	if rt.IsRevoked() {
		return ErrTokenAlreadyRevoked
	}
	rt.RevokedAt = &now
	return nil
}
