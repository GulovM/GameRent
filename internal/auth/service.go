package auth

import (
	"context"
)

type Service interface {
	Register(ctx context.Context, email, password, firstName, lastName string) (*User, string, string, error)
	Login(ctx context.Context, email, password string) (string, string, error)
	Refresh(ctx context.Context, refreshToken string) (string, string, error)
	Logout(ctx context.Context, refreshToken string) error
	Me(ctx context.Context, userID int64) (*User, error)
}
