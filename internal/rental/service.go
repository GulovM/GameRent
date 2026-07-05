package rental

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"time"

	"rent_game_accs/internal/account"
	"rent_game_accs/internal/shared/database"
	"rent_game_accs/internal/user"
)

var (
	ErrAccountNotAvailable = errors.New("account is not available for rent")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrUserBlocked         = errors.New("user is blocked")
)

type Service struct {
	rentalRepo  Repository
	accountRepo account.Repository
	userRepo    user.Repository
	txManager   database.TxManager
}

func NewService(
	rentalRepo Repository,
	accountRepo account.Repository,
	userRepo user.Repository,
	txManager database.TxManager,
) *Service {
	return &Service{
		rentalRepo:  rentalRepo,
		accountRepo: accountRepo,
		userRepo:    userRepo,
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
		if err := acc.Rent(now); err != nil {
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
		newRental.Status = StatusActive

		if err := s.rentalRepo.CreateRental(ctx, newRental); err != nil {
			return err
		}

		rental = newRental
		return nil
	})

	if err != nil {
		return nil, err
	}
	return rental, nil
}

func generateRandomID() int64 {
	n, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	return n.Int64() + 1
}
