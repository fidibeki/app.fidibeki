package support

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/redis/go-redis/v9"
)

const PresenceTTL = 60 * time.Second
const adminChannel = "support:admin:updates"
const adminPresenceChannel = "support:admin:presence"
const presenceIndex = "support:presence:index"

type Presence struct {
	TenantID  string    `json:"tenant_id"`
	MemberID  string    `json:"member_id"`
	TabID     string    `json:"-"`
	Section   string    `json:"section"`
	State     string    `json:"state"`
	LastSeen  time.Time `json:"last_seen"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Workspace string    `json:"workspace"`
}

type Live struct{ Redis redis.UniversalClient }

func memberChannel(tenantID, memberID string) string {
	return "support:member:" + tenantID + ":" + memberID
}
func presenceKey(tenantID, memberID, tabID string) string {
	return "support:presence:" + tenantID + ":" + memberID + ":" + tabID
}

func (l Live) Touch(ctx context.Context, p Presence) error {
	p.LastSeen = time.Now().UTC()
	encoded, err := json.Marshal(p)
	if err != nil {
		return err
	}
	key := presenceKey(p.TenantID, p.MemberID, p.TabID)
	_, err = l.Redis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, key, encoded, PresenceTTL)
		pipe.ZRemRangeByScore(ctx, presenceIndex, "-inf", strconv.FormatInt(p.LastSeen.Add(-PresenceTTL).UnixMilli(), 10))
		pipe.ZAdd(ctx, presenceIndex, redis.Z{Score: float64(p.LastSeen.UnixMilli()), Member: key})
		pipe.Expire(ctx, presenceIndex, 2*PresenceTTL)
		pipe.Publish(ctx, adminPresenceChannel, "change")
		return nil
	})
	return err
}

func (l Live) Leave(ctx context.Context, tenantID, memberID, tabID string) error {
	key := presenceKey(tenantID, memberID, tabID)
	_, err := l.Redis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, key)
		pipe.ZRem(ctx, presenceIndex, key)
		pipe.Publish(ctx, adminPresenceChannel, "change")
		return nil
	})
	return err
}

// Bound both Redis work and the identity join. No names, emails or raw URLs are
// stored in Redis. Each member appears once even with multiple browser tabs.
func (l Live) Online(ctx context.Context, q db.Querier) ([]Presence, bool, error) {
	keys, err := l.Redis.ZRevRangeByScore(ctx, presenceIndex, &redis.ZRangeBy{
		Min: strconv.FormatInt(time.Now().Add(-PresenceTTL).UnixMilli(), 10), Max: "+inf", Offset: 0, Count: 501,
	}).Result()
	if err != nil {
		return nil, false, err
	}
	result := []Presence{}
	if len(keys) == 0 {
		return result, false, nil
	}
	truncated := len(keys) > 500
	if truncated {
		keys = keys[:500]
	}
	values, err := l.Redis.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, false, err
	}
	positions := map[string]int{}
	ids := []string{}
	for _, value := range values {
		encoded, ok := value.(string)
		if !ok {
			continue
		}
		var p Presence
		if json.Unmarshal([]byte(encoded), &p) != nil {
			continue
		}
		if i, ok := positions[p.MemberID]; ok {
			if p.State == "active" && result[i].State != "active" {
				result[i] = p
			}
			continue
		}
		positions[p.MemberID] = len(result)
		ids = append(ids, p.MemberID)
		result = append(result, p)
	}
	if len(ids) == 0 {
		return result, truncated, nil
	}
	rows, err := q.Query(ctx, `SELECT m.id,m.tenant_id,COALESCE(NULLIF(m.name,''),split_part(m.email::text,'@',1)),m.email::text,t.name
        FROM tenant_members m JOIN tenants t ON t.id=m.tenant_id WHERE m.id=ANY($1::uuid[])
        AND m.status='active' AND t.deletion_requested_at IS NULL
        AND (t.status='active' OR (t.status='trial' AND t.trial_ends_at>now()))`, ids)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	valid := map[string]bool{}
	for rows.Next() {
		var id, tenantID, name, email, workspace string
		if err := rows.Scan(&id, &tenantID, &name, &email, &workspace); err != nil {
			return nil, false, err
		}
		i := positions[id]
		if result[i].TenantID != tenantID {
			continue
		}
		result[i].Name = name
		result[i].Email = email
		result[i].Workspace = workspace
		valid[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	filtered := []Presence{}
	for _, p := range result {
		if valid[p.MemberID] {
			filtered = append(filtered, p)
		}
	}
	return filtered, truncated, nil
}

func (l Live) Notify(ctx context.Context, tenantID, memberID string) error {
	_, err := l.Redis.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Publish(ctx, adminChannel, "change")
		pipe.Publish(ctx, memberChannel(tenantID, memberID), "change")
		return nil
	})
	if err != nil {
		return fmt.Errorf("notify support: %w", err)
	}
	return nil
}
