package cache

import (
	"context"
	"fmt"

	"github.com/fidibeki/app.fidibeki/backend/internal/redisconn"
	"github.com/redis/go-redis/v9"
)

type Redis struct {
	client *redis.Client
}

func Open(databaseURL string, caCertificates ...string) (*Redis, error) {
	options, err := redisconn.ClientOptions(databaseURL, caCertificates...)
	if err != nil {
		return nil, fmt.Errorf("parse Redis URL: %w", err)
	}
	return &Redis{client: redis.NewClient(options)}, nil
}

func (r *Redis) Check(ctx context.Context) error {
	if err := r.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping Redis: %w", err)
	}
	return nil
}

func (r *Redis) Close() error {
	if err := r.client.Close(); err != nil {
		return fmt.Errorf("close Redis: %w", err)
	}
	return nil
}

func (r *Redis) Client() *redis.Client {
	return r.client
}
