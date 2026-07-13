package account

import (
	"context"
	"errors"
	"math"

	shared_authorization "rent_game_accs/internal/shared/authorization"
	"rent_game_accs/internal/shared/database"
)

func validateAdminPricing(hourlyPrice, depositAmount int64) error {
	if hourlyPrice <= 0 || depositAmount < 0 || hourlyPrice > math.MaxInt64/720 {
		return ErrPricingOutOfRange
	}
	if hourlyPrice*720 > math.MaxInt64-depositAmount {
		return ErrPricingOutOfRange
	}
	return nil
}

var (
	ErrAdminAuthorization = errors.New("current administrator authorization is required")
	ErrPricingOutOfRange  = errors.New("account pricing exceeds the supported range")
)

type AdminAccountInput struct {
	SteamID64       string
	SteamLogin      string
	SteamPassword   string
	PricePerHour    int64
	SecurityDeposit int64
}

type AdminAccountUpdate struct {
	PricePerHour    *int64
	SecurityDeposit *int64
}

type AdminService struct {
	repo AdminRepository
	tx   database.TxManager
}

func NewAdminService(repo AdminRepository, tx database.TxManager) *AdminService {
	return &AdminService{repo: repo, tx: tx}
}

func (s *AdminService) ListAccounts(ctx context.Context) ([]*Account, error) {
	return s.repo.SearchAccounts(ctx, 100000, 0, "", 0, 0, 0, "")
}

func (s *AdminService) CreateAccount(ctx context.Context, actorUserID int64, input AdminAccountInput) (int64, error) {
	if err := validateAdminPricing(input.PricePerHour, input.SecurityDeposit); err != nil {
		return 0, err
	}
	encrypted, err := s.repo.Encrypt(input.SteamPassword)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := shared_authorization.RequireCurrentAdminForMutation(txCtx, database.GetTxOrPool(txCtx, nil), actorUserID); err != nil {
			if errors.Is(err, shared_authorization.ErrCurrentAdminRequired) {
				return ErrAdminAuthorization
			}
			return err
		}
		var createErr error
		id, createErr = s.repo.CreateAdminAccount(txCtx, input.SteamID64, input.SteamLogin, encrypted, input.PricePerHour, input.SecurityDeposit)
		return createErr
	})
	return id, err
}

func (s *AdminService) UpdateAccount(ctx context.Context, actorUserID, accountID int64, input AdminAccountUpdate) error {
	return s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := shared_authorization.RequireCurrentAdminForMutation(txCtx, database.GetTxOrPool(txCtx, nil), actorUserID); err != nil {
			if errors.Is(err, shared_authorization.ErrCurrentAdminRequired) {
				return ErrAdminAuthorization
			}
			return err
		}
		current, err := s.repo.GetAccountForUpdate(txCtx, accountID)
		if err != nil {
			return err
		}
		price, deposit := current.HourlyPrice.Amount, current.DepositAmount.Amount
		if input.PricePerHour != nil {
			price = *input.PricePerHour
		}
		if input.SecurityDeposit != nil {
			deposit = *input.SecurityDeposit
		}
		if err := validateAdminPricing(price, deposit); err != nil {
			return err
		}
		return s.repo.UpdateAdminPricing(txCtx, accountID, input.PricePerHour, input.SecurityDeposit)
	})
}
