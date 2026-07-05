package rental

import (
	"context"
)

type Repository interface {
	CreateRental(ctx context.Context, r *Rental) error
	GetRental(ctx context.Context, id int64) (*Rental, error)
}
