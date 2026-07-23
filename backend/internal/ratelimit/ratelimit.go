package ratelimit

import (
	"context"
	"time"

	redisrate "github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
)

type Window struct {
	Limit  int
	Period time.Duration
}

type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
}

type Limiter interface {
	Allow(context.Context, string, Window) (Decision, error)
}

type CostLimiter interface {
	AllowN(context.Context, string, Window, int) (Decision, error)
}

type RedisLimiter struct {
	limiter *redisrate.Limiter
}

func NewRedisLimiter(client redis.UniversalClient) RedisLimiter {
	return RedisLimiter{limiter: redisrate.NewLimiter(client)}
}

func (l RedisLimiter) Allow(ctx context.Context, key string, window Window) (Decision, error) {
	return l.AllowN(ctx, key, window, 1)
}

func (l RedisLimiter) AllowN(
	ctx context.Context,
	key string,
	window Window,
	cost int,
) (Decision, error) {
	result, err := l.limiter.AllowN(ctx, key, redisrate.Limit{
		Rate: window.Limit, Burst: window.Limit, Period: window.Period,
	}, cost)
	if err != nil {
		return Decision{}, err
	}
	return Decision{
		Allowed: result.Allowed > 0, Limit: result.Limit.Rate,
		Remaining: result.Remaining, RetryAfter: result.RetryAfter,
	}, nil
}
