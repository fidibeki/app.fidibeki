package ai

import (
	"context"
	"errors"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/aiprovider"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

type ConnectionLeaser interface {
	Acquire(context.Context, string, int) (func(), error)
}
type RedisConnectionLeaser struct{ Client redis.UniversalClient }

var acquireConnection = redis.NewScript(`
 local now=redis.call('TIME');local ms=now[1]*1000+math.floor(now[2]/1000)
 redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',ms)
 if redis.call('ZCARD',KEYS[1])>=tonumber(ARGV[1]) then return 0 end
 redis.call('ZADD',KEYS[1],ms+660000,ARGV[2]);redis.call('PEXPIRE',KEYS[1],660000);return 1
`)

func (l RedisConnectionLeaser) Acquire(ctx context.Context, key string, limit int) (func(), error) {
	id := uuid.NewString()
	key = "ai-concurrency:" + key
	result, err := acquireConnection.Run(ctx, l.Client, []string{key}, limit, id).Int()
	if err != nil {
		return nil, err
	}
	if result != 1 {
		return nil, &aiprovider.Failure{Code: "connection_busy", Retryable: true, RetryAfter: 5 * time.Second}
	}
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = l.Client.ZRem(ctx, key, id).Err()
	}, nil
}

func (p Processor) resolve(ctx context.Context, tenant string, e Execution, category, run string, check func(context.Context) (bool, error)) (Analyzer, aiprovider.Secret, func(), error) {
	noop := func() {}
	if p.Factory == nil {
		if err := p.acquire(ctx); err != nil {
			return nil, aiprovider.Secret{}, noop, err
		}
		return p.Analyzer, aiprovider.Secret{}, noop, nil
	}
	var credentialVersion int64
	authorize := func(ctx context.Context) error {
		allowed, err := check(ctx)
		if err != nil {
			return err
		}
		if !allowed {
			return permanent(aiprovider.ErrDisabled)
		}
		_, err = p.Store.Database.Exec(ctx, `UPDATE ai_processing_runs SET provider_attempts=provider_attempts+1,credential_version=$2,dispatched_at=now() WHERE id=$1 AND status='processing'`, run, credentialVersion)
		return err
	}
	a, c, key, err := p.Factory.Resolve(ctx, tenant, e, category, authorize)
	if err != nil {
		return nil, key, noop, classifyConnectionError(err)
	}
	credentialVersion = key.Version
	limitKey := "gemini"
	rpm := p.RPM
	release := noop
	if e.BillingMode == aiprovider.Customer {
		limitKey = "ai:" + tenant + ":" + c.ID
		rpm = c.RPM
		if p.Leaser != nil {
			release, err = p.Leaser.Acquire(ctx, limitKey, c.Concurrency)
			if err != nil {
				return nil, key, noop, err
			}
		}
	}
	if p.Limiter != nil {
		decision, err := p.Limiter.Allow(ctx, limitKey, ratelimit.Window{Limit: rpm, Period: time.Minute})
		if err != nil {
			release()
			return nil, key, noop, err
		}
		if !decision.Allowed {
			release()
			return nil, key, noop, &aiprovider.Failure{Code: "rate_limited", Retryable: true, RetryAfter: decision.RetryAfter}
		}
	}
	return a, key, release, nil
}
func classifyConnectionError(err error) error {
	if errors.Is(err, aiprovider.ErrDisabled) || errors.Is(err, aiprovider.ErrNotFound) || errors.Is(err, aiprovider.ErrUnvalidated) || errors.Is(err, aiprovider.ErrInvalid) {
		return permanent(err)
	}
	var failure *aiprovider.Failure
	if errors.As(err, &failure) && !failure.Retryable {
		return permanent(err)
	}
	return err
}

func RetryDelay(attempt int, err error, task *asynq.Task) time.Duration {
	delay := asynq.DefaultRetryDelayFunc(attempt, err, task)
	var failure *aiprovider.Failure
	if errors.As(err, &failure) && failure.RetryAfter > delay {
		return failure.RetryAfter
	}
	return delay
}
