package rental

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"rent_game_accs/internal/account"
	"rent_game_accs/internal/shared/database"
	"rent_game_accs/internal/user"
)

var (
	ErrAccountNotAvailable        = errors.New("account is not available for rent")
	ErrInsufficientBalance        = errors.New("insufficient balance")
	ErrUserBlocked                = errors.New("user is blocked")
	ErrCredentialsNotAvailable    = errors.New("rental credentials are not available")
	ErrInvalidRentalPricing       = errors.New("invalid rental pricing")
	ErrRentalPriceOverflow        = errors.New("rental price exceeds supported range")
	ErrRentalAccessDenied         = errors.New("rental access denied")
	ErrCustomerQueriesUnavailable = errors.New("customer rental queries are not configured")
)

func CalculateRentalTotal(hourlyPrice, depositAmount, hours int64) (rentalPrice int64, total int64, err error) {
	if hourlyPrice <= 0 || depositAmount < 0 || hours <= 0 {
		return 0, 0, ErrInvalidRentalPricing
	}
	if hourlyPrice > math.MaxInt64/hours {
		return 0, 0, ErrRentalPriceOverflow
	}
	rentalPrice = hourlyPrice * hours
	if rentalPrice > math.MaxInt64-depositAmount {
		return 0, 0, ErrRentalPriceOverflow
	}
	return rentalPrice, rentalPrice + depositAmount, nil
}

func (s *Service) ListCustomerRentals(ctx context.Context, userID int64) ([]CustomerRental, error) {
	if s.customerRepo == nil {
		return nil, ErrCustomerQueriesUnavailable
	}
	return s.customerRepo.ListCustomerRentals(ctx, userID)
}

func (s *Service) GetCustomerRental(ctx context.Context, userID, rentalID int64) (*CustomerRental, error) {
	if s.customerRepo == nil {
		return nil, ErrCustomerQueriesUnavailable
	}
	rent, err := s.customerRepo.GetCustomerRental(ctx, rentalID)
	if err != nil {
		return nil, err
	}
	if rent.UserID != userID {
		return nil, ErrRentalAccessDenied
	}
	return rent, nil
}

func (s *Service) QuoteRental(ctx context.Context, accountID, durationHours int64) (*RentalQuote, int64, error) {
	if s.customerRepo == nil {
		return nil, 0, ErrCustomerQueriesUnavailable
	}
	quote, err := s.customerRepo.GetRentalQuote(ctx, accountID)
	if err != nil {
		return nil, 0, err
	}
	_, total, err := CalculateRentalTotal(quote.HourlyPrice, quote.DepositAmount, durationHours)
	if err != nil {
		return nil, 0, err
	}
	return quote, total, nil
}

type Service struct {
	rentalRepo   Repository
	customerRepo CustomerRepository
	accountRepo  account.Repository
	userRepo     user.Repository
	paymentRepo  PaymentRepository
	txManager    database.TxManager
}

type PaymentRepository interface {
	CreatePendingPayment(ctx context.Context, rentalID, userID int64, amount int64, currency string) (int64, error)
}

type RentalCredentials struct {
	AccountID int64
	Login     string
	Password  string
	SteamID64 string
}

type CredentialRequestContext struct {
	IPAddress string
	UserAgent string
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
	customerRepo, _ := rentalRepo.(CustomerRepository)
	return &Service{
		rentalRepo:   rentalRepo,
		customerRepo: customerRepo,
		accountRepo:  accountRepo,
		userRepo:     userRepo,
		paymentRepo:  paymentRepo,
		txManager:    txManager,
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

		totalPriceAmount, paymentAmount, err := CalculateRentalTotal(acc.HourlyPrice.Amount, acc.DepositAmount.Amount, hours)
		if err != nil {
			return err
		}
		pricePaid, err := NewMoney(totalPriceAmount, acc.HourlyPrice.Currency)
		if err != nil {
			return err
		}

		depositPaid, err := NewMoney(acc.DepositAmount.Amount, acc.DepositAmount.Currency)
		if err != nil {
			return err
		}

		if u.Balance < paymentAmount {
			return ErrInsufficientBalance
		}

		if err := acc.Reserve(now); err != nil {
			return err
		}

		if err := s.accountRepo.ReserveAccount(ctx, acc.ID, now); err != nil {
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
		if _, err := s.paymentRepo.CreatePendingPayment(ctx, newRental.ID, userID, paymentAmount, acc.HourlyPrice.Currency); err != nil {
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

func (s *Service) GetRentalCredentials(ctx context.Context, userID, rentalID int64, requestContext CredentialRequestContext, now time.Time) (*RentalCredentials, error) {
	now = now.UTC()
	var credentials *RentalCredentials
	err := s.txManager.WithinTransaction(ctx, func(ctx context.Context) error {
		rec, err := s.rentalRepo.GetRentalCredentials(ctx, rentalID, userID, now)
		if err != nil {
			if errors.Is(err, ErrRentalNotFound) {
				return ErrCredentialsNotAvailable
			}
			return err
		}

		if rec.RentalStatus != StatusActive || rec.AccountStatus != int16(account.StatusRented) {
			return ErrCredentialsNotAvailable
		}

		password, err := s.accountRepo.Decrypt(rec.EncryptedPassword)
		if err != nil {
			return fmt.Errorf("failed to decrypt credentials: %w", err)
		}

		if err := s.rentalRepo.RecordCredentialIssued(ctx, CredentialIssueEvent{
			UserID:    userID,
			AccountID: rec.AccountID,
			RentalID:  rentalID,
			IPAddress: requestContext.IPAddress,
			UserAgent: requestContext.UserAgent,
			CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("record credential issuance: %w", err)
		}

		credentials = &RentalCredentials{
			AccountID: rec.AccountID,
			Login:     rec.Login,
			Password:  password,
			SteamID64: rec.SteamID64,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return credentials, nil
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
