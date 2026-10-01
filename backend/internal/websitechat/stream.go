package websitechat

import (
	"context"
	"fmt"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
	"net/http"
	"time"
)

// Streams carry invalidations only. Reconnect always triggers an authorized
// snapshot, including when a Redis notification was lost during an outage.
// A short lifetime bounds permission-revocation latency and works with the
// API's 30-second write timeout without keeping a PostgreSQL connection open.
func (h *Handler) events(operator bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !operator && widgetapi.ValidOrigin(r.Header.Get("Origin")) {
			widgetapi.SetCORSHeaders(w, r.Header.Get("Origin"))
		}
		s, err := h.authorize(r, operator, auth.PermissionChatRead)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		defer func() { _ = s.tx.Rollback(r.Context()) }()
		if err = h.limit(r.Context(), s.tenantID+":"+s.projectID+":"+s.actorID+":stream", 120); err != nil {
			h.fail(w, r, err)
			return
		}
		if err = s.tx.Commit(r.Context()); err != nil {
			h.fail(w, r, err)
			return
		}
		endsAt := time.Now().Add(20 * time.Second)
		if !s.expires.IsZero() && s.expires.Before(endsAt) {
			endsAt = s.expires
		}
		if !time.Now().Before(endsAt) {
			return
		}
		streamContext, cancel := context.WithDeadline(r.Context(), endsAt)
		defer cancel()
		channels := []string{visitorChannel(s), settingsChannel(s)}
		if operator {
			channels = []string{operatorChannel(s), operatorChannel(s) + ":presence"}
		}
		sub := h.options.Redis.Subscribe(streamContext, channels...)
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
				if operator && message.Channel == operatorChannel(s)+":presence" {
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
