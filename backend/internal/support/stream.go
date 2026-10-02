package support

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Streams carry invalidations only. Reconnect always triggers an authorized
// snapshot, including when a Redis notification was lost during an outage.
// A short lifetime bounds permission-revocation latency and works with the
// API's 30-second write timeout without keeping a PostgreSQL connection open.
func (h *Handler) events(admin bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		s, err := h.authorize(r, admin, false)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		defer func() { _ = s.tx.Rollback(r.Context()) }()
		if err = h.limit(r.Context(), s, "stream", 120); err != nil {
			h.fail(w, r, err)
			return
		}
		if err = s.tx.Commit(r.Context()); err != nil {
			h.fail(w, r, err)
			return
		}
		endsAt := time.Now().Add(20 * time.Second)
		if s.claims.ExpiresAt != nil && s.claims.ExpiresAt.Before(endsAt) {
			endsAt = s.claims.ExpiresAt.Time
		}
		if !time.Now().Before(endsAt) {
			return
		}
		streamContext, cancel := context.WithDeadline(r.Context(), endsAt)
		defer cancel()
		channels := []string{memberChannel(s.actor.TenantID, s.actor.MemberID)}
		if admin {
			channels = []string{adminChannel, adminPresenceChannel}
		}
		sub := h.options.Live.Redis.Subscribe(streamContext, channels...)
		defer func() { _ = sub.Close() }()
		if _, err = sub.Receive(streamContext); err != nil {
			h.fail(w, r, err)
			return
		}
		lifetime := time.Until(endsAt)
		if lifetime <= 0 {
			return
		}
		controller := http.NewResponseController(w)
		_ = controller.SetWriteDeadline(endsAt)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Accel-Buffering", "no")
		send := func(event string) bool {
			if !time.Now().Before(endsAt) {
				return false
			}
			_, err := fmt.Fprintf(w, "event: %s\ndata: {}\n\n", event)
			if err != nil {
				return false
			}
			return controller.Flush() == nil
		}
		if !send("change") {
			return
		}
		// Coalesce bursts before waking every connected browser.
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		dirty, presenceDirty := false, false
		messages := sub.Channel()
		for {
			select {
			case <-streamContext.Done():
				return
			case message, ok := <-messages:
				if !ok {
					return
				}
				if admin && message.Channel == adminPresenceChannel {
					presenceDirty = true
				} else {
					dirty = true
				}
			case <-tick.C:
				if presenceDirty {
					if !send("presence") {
						return
					}
					presenceDirty = false
				}
				if dirty {
					if !send("change") {
						return
					}
					dirty = false
				}
			}
		}
	}
}
