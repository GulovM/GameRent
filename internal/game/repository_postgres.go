package game

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"rent_game_accs/internal/shared/database"
)

var (
	ErrGameNotFound = errors.New("game not found")
)

type Repository interface {
	CreateGame(ctx context.Context, g *Game) error
	GetGameByID(ctx context.Context, id int64) (*Game, error)
	GetGameBySteamAppID(ctx context.Context, appID int) (*Game, error)
	UpdateGame(ctx context.Context, g *Game) error
	ListGames(ctx context.Context, limit, offset int, search string) ([]*Game, error)
}

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) CreateGame(ctx context.Context, g *Game) error {
	db := database.GetTxOrPool(ctx, r.pool)

	devBytes, err := json.Marshal(g.Developers)
	if err != nil {
		return err
	}
	pubBytes, err := json.Marshal(g.Publishers)
	if err != nil {
		return err
	}
	genBytes, err := json.Marshal(g.Genres)
	if err != nil {
		return err
	}

	query := `INSERT INTO games (
		id, name, steam_app_id, 
		short_description, header_image, release_date, 
		developers, publishers, genres, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

	_, err = db.Exec(ctx, query,
		g.ID, g.Name, g.SteamAppID,
		g.ShortDescription, g.HeaderImage, g.ReleaseDate,
		devBytes, pubBytes, genBytes, g.CreatedAt, g.UpdatedAt,
	)
	return err
}

func (r *PostgresRepository) GetGameByID(ctx context.Context, id int64) (*Game, error) {
	db := database.GetTxOrPool(ctx, r.pool)

	query := `SELECT 
		id, name, COALESCE(steam_app_id, 0), COALESCE(short_description, ''), COALESCE(header_image, ''), 
		release_date, COALESCE(developers, '[]'::jsonb), COALESCE(publishers, '[]'::jsonb), COALESCE(genres, '[]'::jsonb), created_at, updated_at 
		FROM games WHERE id = $1`

	var g Game
	var devBytes, pubBytes, genBytes []byte

	err := db.QueryRow(ctx, query, id).Scan(
		&g.ID, &g.Name, &g.SteamAppID, &g.ShortDescription, &g.HeaderImage,
		&g.ReleaseDate, &devBytes, &pubBytes, &genBytes, &g.CreatedAt, &g.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, ErrGameNotFound
	}
	if err != nil {
		return nil, err
	}

	if len(devBytes) > 0 {
		_ = json.Unmarshal(devBytes, &g.Developers)
	}
	if len(pubBytes) > 0 {
		_ = json.Unmarshal(pubBytes, &g.Publishers)
	}
	if len(genBytes) > 0 {
		_ = json.Unmarshal(genBytes, &g.Genres)
	}

	return &g, nil
}

func (r *PostgresRepository) GetGameBySteamAppID(ctx context.Context, appID int) (*Game, error) {
	db := database.GetTxOrPool(ctx, r.pool)

	query := `SELECT 
		id, name, COALESCE(steam_app_id, 0), COALESCE(short_description, ''), COALESCE(header_image, ''), 
		release_date, COALESCE(developers, '[]'::jsonb), COALESCE(publishers, '[]'::jsonb), COALESCE(genres, '[]'::jsonb), created_at, updated_at 
		FROM games WHERE steam_app_id = $1`

	var g Game
	var devBytes, pubBytes, genBytes []byte

	err := db.QueryRow(ctx, query, appID).Scan(
		&g.ID, &g.Name, &g.SteamAppID, &g.ShortDescription, &g.HeaderImage,
		&g.ReleaseDate, &devBytes, &pubBytes, &genBytes, &g.CreatedAt, &g.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, ErrGameNotFound
	}
	if err != nil {
		return nil, err
	}

	if len(devBytes) > 0 {
		_ = json.Unmarshal(devBytes, &g.Developers)
	}
	if len(pubBytes) > 0 {
		_ = json.Unmarshal(pubBytes, &g.Publishers)
	}
	if len(genBytes) > 0 {
		_ = json.Unmarshal(genBytes, &g.Genres)
	}

	return &g, nil
}

func (r *PostgresRepository) UpdateGame(ctx context.Context, g *Game) error {
	db := database.GetTxOrPool(ctx, r.pool)

	devBytes, err := json.Marshal(g.Developers)
	if err != nil {
		return err
	}
	pubBytes, err := json.Marshal(g.Publishers)
	if err != nil {
		return err
	}
	genBytes, err := json.Marshal(g.Genres)
	if err != nil {
		return err
	}

	query := `UPDATE games SET 
		name = $1, steam_app_id = $2, 
		short_description = $3, header_image = $4, release_date = $5, 
		developers = $6, publishers = $7, genres = $8, updated_at = $9 
		WHERE id = $10`

	_, err = db.Exec(ctx, query,
		g.Name, g.SteamAppID,
		g.ShortDescription, g.HeaderImage, g.ReleaseDate,
		devBytes, pubBytes, genBytes, g.UpdatedAt, g.ID,
	)
	return err
}

func (r *PostgresRepository) ListGames(ctx context.Context, limit, offset int, search string) ([]*Game, error) {
	db := database.GetTxOrPool(ctx, r.pool)

	var rows pgx.Rows
	var err error

	if search != "" {
		query := `SELECT 
			id, name, COALESCE(steam_app_id, 0), COALESCE(short_description, ''), COALESCE(header_image, ''), 
			release_date, COALESCE(developers, '[]'::jsonb), COALESCE(publishers, '[]'::jsonb), COALESCE(genres, '[]'::jsonb), created_at, updated_at 
			FROM games WHERE name ILIKE $1 ORDER BY name ASC LIMIT $2 OFFSET $3`
		rows, err = db.Query(ctx, query, "%"+search+"%", limit, offset)
	} else {
		query := `SELECT 
			id, name, COALESCE(steam_app_id, 0), COALESCE(short_description, ''), COALESCE(header_image, ''), 
			release_date, COALESCE(developers, '[]'::jsonb), COALESCE(publishers, '[]'::jsonb), COALESCE(genres, '[]'::jsonb), created_at, updated_at 
			FROM games ORDER BY name ASC LIMIT $1 OFFSET $2`
		rows, err = db.Query(ctx, query, limit, offset)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []*Game
	for rows.Next() {
		var g Game
		var devBytes, pubBytes, genBytes []byte

		err := rows.Scan(
			&g.ID, &g.Name, &g.SteamAppID, &g.ShortDescription, &g.HeaderImage,
			&g.ReleaseDate, &devBytes, &pubBytes, &genBytes, &g.CreatedAt, &g.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if len(devBytes) > 0 {
			_ = json.Unmarshal(devBytes, &g.Developers)
		}
		if len(pubBytes) > 0 {
			_ = json.Unmarshal(pubBytes, &g.Publishers)
		}
		if len(genBytes) > 0 {
			_ = json.Unmarshal(genBytes, &g.Genres)
		}

		games = append(games, &g)
	}

	return games, rows.Err()
}
