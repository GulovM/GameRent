package game

import "context"

type Service interface {
	ListGames(ctx context.Context, page, pageSize int, search string) ([]*Game, int64, error)
	GetGameByID(ctx context.Context, id int64) (*Game, error)
}

type PostgresService struct {
	repo Repository
}

func NewPostgresService(repo Repository) *PostgresService {
	return &PostgresService{repo: repo}
}

func (s *PostgresService) ListGames(ctx context.Context, page, pageSize int, search string) ([]*Game, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize
	games, err := s.repo.ListGames(ctx, pageSize, offset, search)
	if err != nil {
		return nil, 0, err
	}

	total := int64(offset + len(games))
	if len(games) == pageSize {
		total++
	}

	return games, total, nil
}

func (s *PostgresService) GetGameByID(ctx context.Context, id int64) (*Game, error) {
	return s.repo.GetGameByID(ctx, id)
}
