package account

import "context"

type Service interface {
	ListAccounts(ctx context.Context, params QueryParams) ([]*Account, int64, error)
	GetAccountByID(ctx context.Context, id int64) (*Account, error)
	CheckAvailability(ctx context.Context, id int64) (bool, error)
}

type PostgresService struct {
	repo Repository
}

func NewPostgresService(repo Repository) *PostgresService {
	return &PostgresService{repo: repo}
}

func (s *PostgresService) ListAccounts(ctx context.Context, params QueryParams) ([]*Account, int64, error) {
	accounts, err := s.repo.SearchAccounts(ctx, params.PageSize, (params.Page-1)*params.PageSize, params.Search, params.GameID, params.MinPrice, params.MaxPrice, params.Status)
	if err != nil {
		return nil, 0, err
	}

	total := int64((params.Page-1)*params.PageSize + len(accounts))
	if len(accounts) == params.PageSize {
		total++
	}

	return accounts, total, nil
}

func (s *PostgresService) GetAccountByID(ctx context.Context, id int64) (*Account, error) {
	return s.repo.GetAccount(ctx, id)
}

func (s *PostgresService) CheckAvailability(ctx context.Context, id int64) (bool, error) {
	acc, err := s.repo.GetAccount(ctx, id)
	if err != nil {
		return false, err
	}
	return acc.Status == StatusAvailable, nil
}
