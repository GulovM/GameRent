package pkg_postgres_pool

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type PostgresConfig struct {
	Host     string        `envconfig:"HOST"     required:"ture"`
	Port     string        `envconfig:"PORT"                     default:"5432"`
	User     string        `envconfig:"USER"     required:"true"`
	Password string        `envconfig:"PASSWORD" required:"true"`
	Database string        `envconfig:"DB"       required:"true"`
	Timeout  time.Duration `envconfig:"TIMEOUT"  required:"true"`
}

func NewPostgresConfig() (PostgresConfig, error) {
	var config PostgresConfig

	if err := envconfig.Process("POSTGRES", &config); err != nil {
		return PostgresConfig{}, fmt.Errorf("process envconfig: %w", err)
	}

	return config, nil
}

func NewPostgresConfigMust() PostgresConfig {
	config, err := NewPostgresConfig()
	if err != nil {
		err = fmt.Errorf("get Postgres connection pool config: %w", err)
		panic(err)
	}

	return config
}

func (c *PostgresConfig) PostgresDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.User, c.Password, c.Host, c.Port, c.Database,
	)
}
