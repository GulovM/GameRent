package pkg_redis

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

type RedisConfig struct {
	RedisHost     string `envconfig:"HOST" default:"localhost"`
	RedisPort     string `envconfig:"PORT" default:"6379"`
	RedisPassword string `envconfig:"PASSWORD" default:""`
}

func NewRedisConfig() (RedisConfig, error) {
	var config RedisConfig

	if err := envconfig.Process("REDIS", &config); err != nil {
		return RedisConfig{}, fmt.Errorf("process envconfig: %w", err)
	}

	return config, nil
}

func NewRedisConfigMust() RedisConfig {
	config, err := NewRedisConfig()
	if err != nil {
		err = fmt.Errorf("get Redis connection config: %w", err)
		panic(err)
	}

	return config
}

func (c *RedisConfig) RedisAddr() string {
	return fmt.Sprintf("%s:%s", c.RedisHost, c.RedisPort)
}
