package rental

import (
	"context"
	"time"
)

type Repository interface {
	CreateRental(ctx context.Context, r *Rental) error
	GetRental(ctx context.Context, id int64) (*Rental, error)
	GetRentalCredentials(ctx context.Context, rentalID, userID int64, now time.Time) (*RentalCredentialsRecord, error)
	RecordCredentialIssued(ctx context.Context, event CredentialIssueEvent) error
	CancelWaitingPaymentRental(ctx context.Context, rentalID, userID int64, reason string, now time.Time) (bool, error)
}

type CustomerRepository interface {
	ListCustomerRentals(ctx context.Context, userID int64) ([]CustomerRental, error)
	GetCustomerRental(ctx context.Context, rentalID int64) (*CustomerRental, error)
	GetRentalQuote(ctx context.Context, accountID int64) (*RentalQuote, error)
}

type CustomerRental struct {
	ID, UserID, AccountID                         int64
	Status, DepositHoldStatus, RefundStatus       int16
	StartAt, EndAt, PaymentExpiresAt              time.Time
	RentalPrice, DepositAmount, RefundTotalAmount int64
	RefundProcessedAt                             *time.Time
}

type RentalQuote struct {
	HourlyPrice   int64
	DepositAmount int64
}

type CredentialIssueEvent struct {
	UserID    int64
	AccountID int64
	RentalID  int64
	IPAddress string
	UserAgent string
	CreatedAt time.Time
}

type RentalCredentialsRecord struct {
	RentalID          int64
	UserID            int64
	AccountID         int64
	RentalStatus      RentalStatus
	AccountStatus     int16
	PaymentExpiresAt  time.Time
	PaymentID         int64
	Login             string
	EncryptedPassword []byte
	SteamID64         string
}
