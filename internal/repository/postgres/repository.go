package repo_postgres

import pkg_postgres_pool "rent_game_accs/internal/pkg/repository/postgres/pool"

type Repository struct {
	pool pkg_postgres_pool.Pool
}

func NewRepository(
	pool pkg_postgres_pool.Pool,
) *Repository {
	return &Repository{pool: pool}
}
