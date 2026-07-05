package scheduler

import (
	"context"
	"time"

	repo "rent_game_accs/internal/repository/postgres"
	"rent_game_accs/internal/shared/clock"

	"go.uber.org/zap"
)

type ExpiredCleanupRepository interface {
	GetExpiredRentals(ctx context.Context, now time.Time) ([]repo.ExpiredRental, error)
	ExpireRental(ctx context.Context, rentalID, accountID int64) error
}

func NewExpiredCleanupWorker(
	r ExpiredCleanupRepository,
	clk clock.Clock,
	log *zap.Logger,
) Task {
	return func(ctx context.Context) error {
		now := clk.Now()
		log.Debug("running expired rentals cleanup", zap.Time("check_time", now))

		expiredRentals, err := r.GetExpiredRentals(ctx, now)
		if err != nil {
			return err
		}

		if len(expiredRentals) == 0 {
			log.Debug("no expired rentals found")
			return nil
		}

		log.Info("found expired rentals to clean up", zap.Int("count", len(expiredRentals)))

		for _, rental := range expiredRentals {
			log.Info("expiring rental session",
				zap.Int64("rental_id", rental.ID),
				zap.Int64("account_id", rental.AccountID),
			)

			err := r.ExpireRental(ctx, rental.ID, rental.AccountID)
			if err != nil {
				log.Error("failed to expire rental",
					zap.Int64("rental_id", rental.ID),
					zap.Error(err),
				)
				continue
			}

			log.Info("successfully expired rental",
				zap.Int64("rental_id", rental.ID),
			)
		}

		return nil
	}
}
