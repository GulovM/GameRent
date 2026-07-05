package pkg_redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"rent_game_accs/internal/pkg/monitoring"
)

type redisHook struct{}

func (redisHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (redisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		duration := time.Since(start).Seconds()
		monitoring.RedisOpDuration.WithLabelValues(cmd.Name()).Observe(duration)
		return err
	}
}

func (redisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		duration := time.Since(start).Seconds()
		monitoring.RedisOpDuration.WithLabelValues("pipeline").Observe(duration)
		return err
	}
}

func InitRedis(ctx context.Context, cfg RedisConfig) (*redis.Client, error) {
	rdb, err := initRedis(ctx, &cfg)
	if err != nil {
		return nil, fmt.Errorf("redis connection: %w", err)
	}
	return rdb, nil
}

func initRedis(ctx context.Context, cfg *RedisConfig) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr(),
		Password: cfg.RedisPassword,
		DB:       0,
	})
	rdb.AddHook(redisHook{})

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return rdb, nil
}
