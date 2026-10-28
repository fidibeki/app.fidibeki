package websitechat

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/project"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

const presenceTTL = 60 * time.Second

type Presence struct {
	Locale   string    `json:"locale,omitempty"`
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	PageURL  string    `json:"page_url"`
	State    string    `json:"state"`
	LastSeen time.Time `json:"last_seen"`
}

func operatorChannel(s *scope) string {
	return "website-chat:operators:" + s.tenantID + ":" + s.projectID
}
func settingsChannel(s *scope) string {
	return "website-chat:settings:" + s.tenantID + ":" + s.projectID
}
func visitorChannel(s *scope) string {
	return "website-chat:visitor:" + s.tenantID + ":" + s.projectID + ":" + s.visitorID
}
func presenceIndex(s *scope) string {
	return "website-chat:presence:" + s.tenantID + ":" + s.projectID + ":index"
}
func presenceKey(s *scope, tab string) string {
	return "website-chat:presence:" + s.tenantID + ":" + s.projectID + ":" + s.visitorID + ":" + tab
}
func (h *Handler) presence(r *http.Request, s *scope) (any, error) {
	var input struct {
		Locale  string `json:"locale"`
		TabID   string `json:"tab_id"`
		PageURL string `json:"page_url"`
		State   string `json:"state"`
	}
	if decode(r, &input) != nil || !validID(input.TabID) || (input.State != "active" && input.State != "idle") || len(input.PageURL) > 2048 {
		return nil, invalid()
	}
	locale, valid := normalizeVisitorLocale(input.Locale)
	if !valid {
		return nil, invalid()
	}
	u, err := url.Parse(input.PageURL)
	if err != nil || u.User != nil || u.Scheme+"://"+u.Host != s.origin {
		return nil, invalid()
	}
	safe := s.origin + "/"
	// Only paths explicitly configured by this website owner may leave the page.
	// Unmatched paths project to the origin, using the existing privacy contract.
	for _, rule := range s.rules {
		if !rule.Enabled || (rule.DefinitionStatus != "" && rule.DefinitionStatus != project.DefinitionActive) {
			continue
		}
		location, err := project.ProjectPrivacySafeLocation(input.PageURL, rule)
		if err == nil && len(location.Target) > len(safe) {
			safe = location.Target
		}
	}
	// A heartbeat must not change inbox order or extend conversation retention.
	if locale != "" {
		if _, err = s.tx.Exec(r.Context(), `UPDATE website_chat_visitors SET locale=$2 WHERE id=$1 AND locale<>$2`, s.visitorID, locale); err != nil {
			return nil, err
		}
	}
	p := Presence{Locale: locale, ID: s.visitorID, PageURL: safe, State: input.State, LastSeen: time.Now().UTC()}
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	key := presenceKey(s, input.TabID)
	_, err = h.options.Redis.TxPipelined(r.Context(), func(pipe redis.Pipeliner) error {
		pipe.Set(r.Context(), key, data, presenceTTL)
		pipe.ZRemRangeByScore(r.Context(), presenceIndex(s), "-inf", strconv.FormatInt(p.LastSeen.Add(-presenceTTL).UnixMilli(), 10))
		pipe.ZAdd(r.Context(), presenceIndex(s), redis.Z{Score: float64(p.LastSeen.UnixMilli()), Member: key})
		pipe.Expire(r.Context(), presenceIndex(s), 2*presenceTTL)
		pipe.Publish(r.Context(), operatorChannel(s)+":presence", "change")
		return nil
	})
	return nil, err
}
func (h *Handler) leave(r *http.Request, s *scope) (any, error) {
	tab := chi.URLParam(r, "tabID")
	if !validID(tab) {
		return nil, invalid()
	}
	key := presenceKey(s, tab)
	_, err := h.options.Redis.TxPipelined(r.Context(), func(p redis.Pipeliner) error {
		p.Del(r.Context(), key)
		p.ZRem(r.Context(), presenceIndex(s), key)
		p.Publish(r.Context(), operatorChannel(s)+":presence", "change")
		return nil
	})
	return nil, err
}
func (h *Handler) online(r *http.Request, s *scope) (any, error) {
	result := []Presence{}
	response := func(truncated bool) any { return map[string]any{"visitors": result, "truncated": truncated} }
	if !s.enabled {
		return response(false), nil
	}
	keys, err := h.options.Redis.ZRevRangeByScore(r.Context(), presenceIndex(s), &redis.ZRangeBy{Min: strconv.FormatInt(time.Now().Add(-presenceTTL).UnixMilli(), 10), Max: "+inf", Count: 501}).Result()
	if err != nil {
		return nil, err
	}
	truncated := len(keys) > 500
	if truncated {
		keys = keys[:500]
	}
	if len(keys) == 0 {
		return response(false), nil
	}
	values, err := h.options.Redis.MGet(r.Context(), keys...).Result()
	if err != nil {
		return nil, err
	}
	byID := map[string]Presence{}
	ids := []string{}
	for _, value := range values {
		raw, ok := value.(string)
		if !ok {
			continue
		}
		var p Presence
		if json.Unmarshal([]byte(raw), &p) != nil {
			continue
		}
		old, exists := byID[p.ID]
		if exists {
			if p.State == "active" && old.State != "active" {
				byID[p.ID] = p
			}
			continue
		}
		byID[p.ID] = p
		ids = append(ids, p.ID)
	}
	if len(ids) == 0 {
		return response(truncated), nil
	}
	rows, err := s.tx.Query(r.Context(), `SELECT id,locale FROM website_chat_visitors WHERE id=ANY($1::uuid[]) AND expires_at>now()`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	valid := map[string]string{}
	for rows.Next() {
		var id, locale string
		if err = rows.Scan(&id, &locale); err != nil {
			return nil, err
		}
		valid[id] = locale
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if locale, exists := valid[id]; exists {
			p := byID[id]
			p.Locale = locale
			p.Name = "Visitor " + id[:8]
			result = append(result, p)
		}
	}
	return response(truncated), nil
}
