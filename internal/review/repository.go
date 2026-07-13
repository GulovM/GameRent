package review

import "context"

type Repository interface {
	Create(ctx context.Context, input CreateInput) (int64, error)
	ListByAccount(ctx context.Context, accountID int64) ([]Review, error)
}

type CreateInput struct {
	RentalID  int64
	UserID    int64
	AccountID int64
	Rating    int
	Comment   string
}
