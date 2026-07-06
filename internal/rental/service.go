package rental

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"rent_game_accs/internal/account"
	"rent_game_accs/internal/shared/database"
	"rent_game_accs/internal/user"
)

var (
	ErrAccountNotAvailable     = errors.New("account is not available for rent")
	ErrInsufficientBalance     = errors.New("insufficient balance")
	ErrUserBlocked             = errors.New("user is blocked")
	ErrCredentialsNotAvailable = errors.New("rental credentials are not available")
)

type Service struct {
	rentalRepo  Repository
	accountRepo account.Repository
	userRepo    user.Repository
	paymentRepo PaymentRepository
	txManager   database.TxManager
}

type PaymentRepository interface {
	CreatePendingPayment(ctx context.Context, rentalID, userID int64, amount int64, currency string) (int64, error)
}

type RentalCredentials struct {
	Login     string
	Password  string
	SteamID64 string
}

type CancelRentalResult struct {
	Changed bool
}

func NewService(
	rentalRepo Repository,
	accountRepo account.Repository,
	userRepo user.Repository,
	paymentRepo PaymentRepository,
	txManager database.TxManager,
) *Service {
	return &Service{
		rentalRepo:  rentalRepo,
		accountRepo: accountRepo,
		userRepo:    userRepo,
		paymentRepo: paymentRepo,
		txManager:   txManager,
	}
}

func (s *Service) RentAccount(ctx context.Context, userID, accountID int64, duration time.Duration, now time.Time) (*Rental, error) {
	var rental *Rental

	err := s.txManager.WithinTransaction(ctx, func(ctx context.Context) error {

		acc, err := s.accountRepo.GetAccountForUpdate(ctx, accountID)
		if err != nil {
			return err
		}

		if acc.Status != account.StatusAvailable {
			return ErrAccountNotAvailable
		}

		u, err := s.userRepo.GetUser(ctx, userID)
		if err != nil {
			return err
		}

		if u.IsBlocked {
			return ErrUserBlocked
		}

		hours := int64(duration.Hours())
		if hours == 0 {
			hours = 1
		}

		totalPriceAmount := acc.HourlyPrice.Amount * hours
		pricePaid, err := NewMoney(totalPriceAmount, acc.HourlyPrice.Currency)
		if err != nil {
			return err
		}

		depositPaid, err := NewMoney(acc.DepositAmount.Amount, acc.DepositAmount.Currency)
		if err != nil {
			return err
		}

		if u.Balance < totalPriceAmount+depositPaid.Amount {
			return ErrInsufficientBalance
		}

		if err := acc.Reserve(now); err != nil {
			return err
		}

		if err := s.accountRepo.UpdateAccount(ctx, acc); err != nil {
			return err
		}

		period, err := NewRentalPeriod(now, now.Add(duration))
		if err != nil {
			return err
		}

		newRental, err := NewRental(userID, accountID, period, pricePaid, depositPaid, now)
		if err != nil {
			return err
		}
		newRental.ID = generateRandomID()
		if err := newRental.PrepareForPayment(now); err != nil {
			return err
		}

		if err := s.rentalRepo.CreateRental(ctx, newRental); err != nil {
			if isRentalAvailabilityConflict(err) {
				return ErrAccountNotAvailable
			}
			return err
		}

		if s.paymentRepo == nil {
			return fmt.Errorf("payment repository is not configured")
		}
		if _, err := s.paymentRepo.CreatePendingPayment(ctx, newRental.ID, userID, totalPriceAmount+depositPaid.Amount, acc.HourlyPrice.Currency); err != nil {
			return fmt.Errorf("failed to create pending payment: %w", err)
		}

		rental = newRental
		return nil
	})

	if err != nil {
		return nil, err
	}
	return rental, nil
}

func (s *Service) GetRentalCredentials(ctx context.Context, userID, rentalID int64, now time.Time) (*RentalCredentials, error) {
	now = now.UTC()
	rec, err := s.rentalRepo.GetRentalCredentials(ctx, rentalID, userID, now)
	if err != nil {
		if errors.Is(err, ErrRentalNotFound) {
			return nil, ErrCredentialsNotAvailable
		}
		return nil, err
	}

	if rec.RentalStatus != StatusActive || rec.AccountStatus != int16(account.StatusRented) {
		return nil, ErrCredentialsNotAvailable
	}
	if !rec.PaymentExpiresAt.After(now) {
		return nil, ErrCredentialsNotAvailable
	}

	password, err := s.accountRepo.Decrypt(rec.EncryptedPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credentials: %w", err)
	}

	return &RentalCredentials{
		Login:     rec.Login,
		Password:  password,
		SteamID64: rec.SteamID64,
	}, nil
}

func (s *Service) CancelRental(ctx context.Context, userID, rentalID int64, reason string, now time.Time) (*CancelRentalResult, error) {
	result := &CancelRentalResult{}

	err := s.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		changed, err := s.rentalRepo.CancelWaitingPaymentRental(ctx, rentalID, userID, reason, now.UTC())
		if err != nil {
			return fmt.Errorf("cancel rental: %w", err)
		}
		result.Changed = changed
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func generateRandomID() int64 {
	n, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	return n.Int64() + 1
}

func isRentalAvailabilityConflict(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == "uq_rental_account_waiting_or_active"
}
