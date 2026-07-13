package review

import "context"

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, input CreateInput) (int64, error) {
	return s.repository.Create(ctx, input)
}

func (s *Service) ListByAccount(ctx context.Context, accountID int64) ([]Review, error) {
	return s.repository.ListByAccount(ctx, accountID)
}
