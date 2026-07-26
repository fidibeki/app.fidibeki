package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const slackStateKeyPrefix = "slack_install_state:"

// RedisInstallStateStore keeps install intent server-side, keyed by an unguessable token.
//
// Consume uses GETDEL so the read and the delete are one atomic step: two callbacks racing
// on the same token — a replayed URL, a double-clicked redirect — cannot both succeed. The
// payload never travels through the browser, so a user cannot rewrite the tenant it names.
type RedisInstallStateStore struct {
	Client redis.UniversalClient
}

func (s RedisInstallStateStore) Create(
	ctx context.Context, state InstallState, ttl time.Duration,
) (string, error) {
	if s.Client == nil {
		return "", errors.New("slack install state store is unavailable")
	}
	token, err := NewInstallStateToken()
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("encode slack install state: %w", err)
	}
	if err := s.Client.Set(ctx, slackStateKeyPrefix+token, encoded, ttl).Err(); err != nil {
		return "", fmt.Errorf("store slack install state: %w", err)
	}
	return token, nil
}

func (s RedisInstallStateStore) Consume(ctx context.Context, token string) (InstallState, error) {
	if s.Client == nil {
		return InstallState{}, errors.New("slack install state store is unavailable")
	}
	if token == "" {
		return InstallState{}, ErrInstallStateInvalid
	}
	encoded, err := s.Client.GetDel(ctx, slackStateKeyPrefix+token).Bytes()
	if errors.Is(err, redis.Nil) {
		return InstallState{}, ErrInstallStateInvalid
	}
	if err != nil {
		return InstallState{}, fmt.Errorf("read slack install state: %w", err)
	}
	var state InstallState
	if err := json.Unmarshal(encoded, &state); err != nil {
		return InstallState{}, ErrInstallStateInvalid
	}
	if state.TenantID == "" {
		return InstallState{}, ErrInstallStateInvalid
	}
	return state, nil
}
