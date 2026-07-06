package rental

import (
	"context"
	"time"
)

type Repository interface {
	CreateRental(ctx context.Context, r *Rental) error
	GetRental(ctx context.Context, id int64) (*Rental, error)
	GetRentalCredentials(ctx context.Context, rentalID, userID int64, now time.Time) (*RentalCredentialsRecord, error)
	CancelWaitingPaymentRental(ctx context.Context, rentalID, userID int64, reason string, now time.Time) (bool, error)
}

type RentalCredentialsRecord struct {
	RentalID          int64
	UserID            int64
	AccountID         int64
	RentalStatus      RentalStatus
	AccountStatus     int16
	PaymentExpiresAt  time.Time
	Login             string
	EncryptedPassword []byte
	SteamID64         string
}
