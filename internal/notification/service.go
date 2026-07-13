package notification

import "context"

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) ListByUser(ctx context.Context, userID int64) ([]Notification, error) {
	return s.repository.ListByUser(ctx, userID)
}

func (s *Service) MarkRead(ctx context.Context, notificationID, userID int64) (bool, error) {
	return s.repository.MarkRead(ctx, notificationID, userID)
}
