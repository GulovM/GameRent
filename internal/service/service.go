package service

import (
	"context"
	"rent_game_accs/internal/domain"
)

type Repository interface{}

type Service struct {
	repo Repository
}

type UsersRepository interface {
	CreateUser(ctx context.Context, user domain.User) (domain.User, error)
	GetUsers(ctx context.Context, limit, offset *int) ([]domain.User, error)
	GetUser(ctx context.Context, id int) (domain.User, error)
	DeleteUser(ctx context.Context, id int) error
	PatchUser(ctx context.Context, id int, patch domain.User) (domain.User, error)
}

type UsersService struct {
	usersRepository UsersRepository
}

func NewUsersService(usersRepository UsersRepository) UsersService {
	return UsersService{usersRepository: usersRepository}
}
