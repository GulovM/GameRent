package rental

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidPeriod       = errors.New("rental period end time must be after start time")
	ErrPeriodTooShort      = errors.New("rental period must be at least 1 hour")
	ErrPeriodTooLong       = errors.New("rental period cannot exceed 30 days")
	ErrInvalidUserID       = errors.New("invalid user ID")
	ErrInvalidAccountID    = errors.New("invalid account ID")
	ErrInvalidPrice        = errors.New("rental price must be positive")
	ErrInvalidDeposit      = errors.New("deposit amount must be non-negative")
	ErrInvalidTransition   = errors.New("invalid rental state transition")
	ErrUnsupportedCurrency = errors.New("unsupported currency")
	ErrCannotCancel        = errors.New("cannot cancel rental in current state")
)

type RentalStatus uint8

const (
	StatusCreated        RentalStatus = 0
	StatusWaitingPayment RentalStatus = 1
	StatusActive         RentalStatus = 2
	StatusExpired        RentalStatus = 3
	StatusCompleted      RentalStatus = 4
	StatusCancelled      RentalStatus = 5
)

func (s RentalStatus) String() string {
	switch s {
	case StatusCreated:
		return "Created"
	case StatusWaitingPayment:
		return "WaitingPayment"
	case StatusActive:
		return "Active"
	case StatusExpired:
		return "Expired"
	case StatusCompleted:
		return "Completed"
	case StatusCancelled:
		return "Cancelled"
	default:
		return "Unknown"
	}
}

type Money struct {
	Amount   int64
	Currency string
}

func NewMoney(amount int64, currency string) (Money, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency != "USD" && currency != "EUR" && currency != "RUB" && currency != "TJS" {
		return Money{}, ErrUnsupportedCurrency
	}
	return Money{Amount: amount, Currency: currency}, nil
}

func (m Money) Add(o Money) (Money, error) {
	if m.Currency != o.Currency {
		return Money{}, errors.New("currency mismatch")
	}
	return Money{Amount: m.Amount + o.Amount, Currency: m.Currency}, nil
}

func (m Money) Sub(o Money) (Money, error) {
	if m.Currency != o.Currency {
		return Money{}, errors.New("currency mismatch")
	}
	return Money{Amount: m.Amount - o.Amount, Currency: m.Currency}, nil
}

type RentalPeriod struct {
	StartAt time.Time
	EndAt   time.Time
}

func NewRentalPeriod(start, end time.Time) (RentalPeriod, error) {
	p := RentalPeriod{StartAt: start, EndAt: end}
	if err := p.Validate(); err != nil {
		return RentalPeriod{}, err
	}
	return p, nil
}

func (p RentalPeriod) Validate() error {
	if !p.EndAt.After(p.StartAt) {
		return ErrInvalidPeriod
	}
	duration := p.Duration()
	if duration < time.Hour {
		return ErrPeriodTooShort
	}
	if duration > 30*24*time.Hour {
		return ErrPeriodTooLong
	}
	return nil
}

func (p RentalPeriod) Duration() time.Duration {
	return p.EndAt.Sub(p.StartAt)
}

func (p RentalPeriod) IsExpired(now time.Time) bool {
	return now.After(p.EndAt)
}

func (p RentalPeriod) RemainingTime(now time.Time) time.Duration {
	if p.IsExpired(now) {
		return 0
	}
	return p.EndAt.Sub(now)
}

type Rental struct {
	ID                 int64
	UserID             int64
	AccountID          int64
	Status             RentalStatus
	Period             RentalPeriod
	RentalPrice        Money
	DepositAmount      Money
	ActualFinishedAt   *time.Time
	CancellationReason *string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func NewRental(userID, accountID int64, period RentalPeriod, price, deposit Money, now time.Time) (*Rental, error) {
	if userID <= 0 {
		return nil, ErrInvalidUserID
	}
	if accountID <= 0 {
		return nil, ErrInvalidAccountID
	}
	if price.Amount <= 0 {
		return nil, ErrInvalidPrice
	}
	if deposit.Amount < 0 {
		return nil, ErrInvalidDeposit
	}
	if price.Currency != deposit.Currency {
		return nil, errors.New("rental price and deposit currency must match")
	}

	return &Rental{
		UserID:        userID,
		AccountID:     accountID,
		Status:        StatusCreated,
		Period:        period,
		RentalPrice:   price,
		DepositAmount: deposit,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func (r *Rental) PrepareForPayment(now time.Time) error {
	if r.Status != StatusCreated {
		return ErrInvalidTransition
	}
	r.Status = StatusWaitingPayment
	r.UpdatedAt = now
	return nil
}

func (r *Rental) Activate(now time.Time) error {
	if r.Status != StatusWaitingPayment {
		return ErrInvalidTransition
	}
	r.Status = StatusActive
	r.UpdatedAt = now
	return nil
}

func (r *Rental) Expire(now time.Time) error {
	if r.Status != StatusActive {
		return ErrInvalidTransition
	}
	r.Status = StatusExpired
	r.UpdatedAt = now
	return nil
}

func (r *Rental) Complete(now time.Time) error {
	if r.Status != StatusExpired {
		return ErrInvalidTransition
	}
	r.Status = StatusCompleted
	r.ActualFinishedAt = &now
	r.UpdatedAt = now
	return nil
}

func (r *Rental) Cancel(reason string, now time.Time) error {
	if r.Status != StatusWaitingPayment {
		return ErrCannotCancel
	}
	reasonClean := strings.TrimSpace(reason)
	r.Status = StatusCancelled
	r.CancellationReason = &reasonClean
	r.ActualFinishedAt = &now
	r.UpdatedAt = now
	return nil
}
