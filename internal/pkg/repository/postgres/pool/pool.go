package pkg_postgres_pool

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"rent_game_accs/internal/pkg/monitoring"
)

type Pool interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Close()

	OpTimeout() time.Duration
}

type ConnectionPool struct {
	*pgxpool.Pool
	opTimeout time.Duration
}

type monitoredRow struct {
	pgx.Row
	start time.Time
}

func (mr *monitoredRow) Scan(dest ...any) error {
	err := mr.Row.Scan(dest...)
	duration := time.Since(mr.start).Seconds()
	monitoring.DbQueryDuration.WithLabelValues("query_row").Observe(duration)
	return err
}

func (p *ConnectionPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	start := time.Now()
	rows, err := p.Pool.Query(ctx, sql, args...)
	duration := time.Since(start).Seconds()
	monitoring.DbQueryDuration.WithLabelValues("query").Observe(duration)
	return rows, err
}

func (p *ConnectionPool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	start := time.Now()
	row := p.Pool.QueryRow(ctx, sql, args...)
	return &monitoredRow{Row: row, start: start}
}

func (p *ConnectionPool) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	start := time.Now()
	tag, err := p.Pool.Exec(ctx, sql, arguments...)
	duration := time.Since(start).Seconds()
	monitoring.DbQueryDuration.WithLabelValues("exec").Observe(duration)
	return tag, err
}

func NewConnectionPool(ctx context.Context, config PostgresConfig) (*ConnectionPool, *sql.DB, error) {
	connectionString := config.PostgresDSN()

	pgxconfig, err := pgxpool.ParseConfig(connectionString)
	if err != nil {
		return nil, nil, fmt.Errorf("parse pgxconfig: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, pgxconfig)
	if err != nil {
		return nil, nil, fmt.Errorf("create pgxpool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, nil, fmt.Errorf("pgxpool ping: %w", err)
	}

	db, err := connectWithRetry(connectionString, 5, 2*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("connect db: %w", err)
	}

	return &ConnectionPool{
		Pool:      pool,
		opTimeout: config.Timeout,
	}, db, nil
}

func (p *ConnectionPool) OpTimeout() time.Duration {
	return p.opTimeout
}

func connectWithRetry(dsn string, maxAttempts int, delay time.Duration) (*sql.DB, error) {
	var db *sql.DB
	var err error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		fmt.Printf("Try to connect to DB='%d' from '%d'...", attempt, maxAttempts)

		db, err = sql.Open("pgx", dsn)
		if err == nil {
			err = db.Ping()
			if err == nil {
				fmt.Println("Successfully connect to PostgreSQL!")
				db.SetMaxOpenConns(25)
				db.SetMaxIdleConns(25)
				db.SetConnMaxLifetime(5 * time.Minute)
				return db, nil
			}
		}

		fmt.Printf("DB is unavailable. Retry after %v... Error: %v", delay, err)
		time.Sleep(delay)
	}

	return nil, err
}
