package user

import (
	"context"
	"time"
)

type Service interface {
	GetUserByID(ctx context.Context, id int64) (*User, error)
	UpdateUser(ctx context.Context, id int64, firstName, lastName string) (*User, error)
	ListUsers(ctx context.Context) ([]*User, error)
}

type PostgresService struct {
	repo Repository
}

func NewPostgresService(repo Repository) *PostgresService {
	return &PostgresService{repo: repo}
}

func (s *PostgresService) GetUserByID(ctx context.Context, id int64) (*User, error) {
	return s.repo.GetUser(ctx, id)
}

func (s *PostgresService) UpdateUser(ctx context.Context, id int64, firstName, lastName string) (*User, error) {
	u, err := s.repo.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}

	u.FirstName = firstName
	u.LastName = lastName
	u.UpdatedAt = time.Now()

	if err := s.repo.UpdateUser(ctx, u); err != nil {
		return nil, err
	}

	return u, nil
}

func (s *PostgresService) ListUsers(ctx context.Context) ([]*User, error) {
	return s.repo.ListUsers(ctx, 100, 0)
}
