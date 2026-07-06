package scheduler

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	repo "rent_game_accs/internal/repository/postgres"
	"rent_game_accs/internal/shared/clock"
)

type WaitingPaymentCleanupRepository interface {
	GetExpiredWaitingPaymentReservations(ctx context.Context, now time.Time) ([]repo.ExpiredWaitingPaymentReservation, error)
	ExpireWaitingPaymentReservation(ctx context.Context, paymentID int64, now time.Time) (bool, error)
}

func NewExpiredWaitingPaymentCleanupWorker(
	r WaitingPaymentCleanupRepository,
	clk clock.Clock,
	log *zap.Logger,
) Task {
	return func(ctx context.Context) error {
		now := clk.Now()
		log.Debug("running waiting payment cleanup", zap.Time("check_time", now))

		reservations, err := r.GetExpiredWaitingPaymentReservations(ctx, now)
		if err != nil {
			return fmt.Errorf("failed to fetch expired waiting payment reservations: %w", err)
		}

		if len(reservations) == 0 {
			log.Debug("no expired waiting payment reservations found")
			return nil
		}

		log.Info("found expired waiting payment reservations", zap.Int("count", len(reservations)))

		for _, reservation := range reservations {
			log.Info("expiring waiting payment reservation",
				zap.Int64("payment_id", reservation.PaymentID),
				zap.Int64("rental_id", reservation.RentalID),
				zap.Int64("account_id", reservation.AccountID),
			)

			processed, err := r.ExpireWaitingPaymentReservation(ctx, reservation.PaymentID, now)
			if err != nil {
				log.Error("failed to expire waiting payment reservation",
					zap.Int64("payment_id", reservation.PaymentID),
					zap.Error(err),
				)
				continue
			}

			if !processed {
				log.Debug("waiting payment reservation was already handled or locked",
					zap.Int64("payment_id", reservation.PaymentID),
				)
				continue
			}

			log.Info("successfully expired waiting payment reservation",
				zap.Int64("payment_id", reservation.PaymentID),
				zap.Int64("rental_id", reservation.RentalID),
			)
		}

		return nil
	}
}
