// Command devseed provisions a local demo workspace for manually exercising the
// Session Replays feature against the compose stack: a growth tenant, a member
// with every permission, replay sessions in every lifecycle state (linked and
// unlinked to feedback), playable merged replay objects in minio, and a magic
// link to sign in with. Local development only — never run against a real
// environment.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	databaseURL = "postgres://postgres:postgres@127.0.0.1:5433/fidibeki?sslmode=disable"
	tenantSlug  = "acme"
	memberEmail = "acidineydias@gmail.com"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer func() { _ = connection.Close(ctx) }()

	var tenantID string
	if err := connection.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', $1, 'growth', 'active')
		ON CONFLICT (slug) DO UPDATE SET plan = 'growth', status = 'active'
		RETURNING id
	`, tenantSlug).Scan(&tenantID); err != nil {
		log.Fatalf("upsert tenant: %v", err)
	}

	var projectID string
	if err := connection.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Demo Web', 'pk_live_DEMOAAAAAAAAAAAAAAAAAA', '{demo.localhost}')
		ON CONFLICT (public_key) DO UPDATE SET name = 'Demo Web'
		RETURNING id
	`, tenantID).Scan(&projectID); err != nil {
		log.Fatalf("upsert project: %v", err)
	}

	permissions := []string{
		"project:create", "project:read", "project:update", "project:delete",
		"widget_rule:create", "widget_rule:read", "widget_rule:update", "widget_rule:delete",
		"feedback:read", "feedback:update", "feedback:delete",
		"issue:read", "issue:update", "issue:delete", "issue:merge",
		"session:read", "session:delete",
		"member:read", "member:invite", "member:update", "member:remove",
		"api_key:create", "api_key:read", "api_key:delete",
		"integration:create", "integration:read", "integration:update", "integration:delete",
		"testimonial:read", "testimonial:create", "testimonial:update", "testimonial:delete",
		"testimonial:publish",
		"billing:manage", "billing:read", "tenant:update", "tenant:read", "audit:read",
	}
	var memberID string
	if err := connection.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, name, permissions)
		VALUES ($1, $2, 'Acidiney', $3)
		ON CONFLICT (tenant_id, email) DO UPDATE SET permissions = $3
		RETURNING id
	`, tenantID, memberEmail, permissions).Scan(&memberID); err != nil {
		log.Fatalf("upsert member: %v", err)
	}
	if _, err := connection.Exec(ctx, `
		UPDATE tenants SET owner_member_id = $2, welcome_email_sent_at = now()
		WHERE id = $1
	`, tenantID, memberID); err != nil {
		log.Fatalf("set workspace owner: %v", err)
	}

	objectStorage, err := storage.Open(storage.Config{
		Endpoint:  "http://127.0.0.1:9000",
		Region:    "us-east-1",
		Bucket:    "fidibeki",
		AccessKey: "fidibeki-local",
		SecretKey: "fidibeki-local-secret",
	})
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}

	if _, err := connection.Exec(ctx, `
		DELETE FROM sessions WHERE tenant_id = $1
	`, tenantID); err != nil {
		log.Fatalf("clear previous demo sessions: %v", err)
	}
	if _, err := connection.Exec(ctx, `
		DELETE FROM feedbacks WHERE tenant_id = $1 AND public_id = 'fb_DEMOSEED'
	`, tenantID); err != nil {
		log.Fatalf("clear previous demo feedback: %v", err)
	}

	now := time.Now().UTC()
	insertSession := func(pageURL string, startedAt time.Time, events int) string {
		var id string
		if err := connection.QueryRow(ctx, `
			INSERT INTO sessions (
				tenant_id, project_id, session_id, page_url, started_at,
				rrweb_version, max_duration_seconds, max_events,
				events_count, batches_count, replay_privacy_version
			) VALUES ($1, $2, $3, $4, $5, '2.1.1', 1800, 50000, $6, 1, 2)
			RETURNING id
		`, tenantID, projectID, uuid.NewString(), pageURL, startedAt, events).Scan(&id); err != nil {
			log.Fatalf("insert session: %v", err)
		}
		return id
	}
	markReady := func(id string, startedAt time.Time, duration time.Duration) {
		key := "tenants/" + tenantID + "/sessions/" + id + "/replay.json"
		payload := replayEvents(startedAt, duration)
		if err := objectStorage.PutObject(
			ctx, key, bytes.NewReader(payload), int64(len(payload)), "application/json",
		); err != nil {
			log.Fatalf("upload replay object: %v", err)
		}
		if _, err := connection.Exec(ctx, `
			UPDATE sessions SET ended_at = started_at + $2::interval,
				merge_requested_at = started_at + $2::interval,
				merged_at = started_at + $2::interval + interval '30 seconds',
				storage_key = $3
			WHERE id = $1
		`, id, duration.String(), key); err != nil {
			log.Fatalf("mark session ready: %v", err)
		}
	}

	readyLinked := insertSession("https://demo.localhost/checkout", now.Add(-26*time.Hour), 214)
	markReady(readyLinked, now.Add(-26*time.Hour), 4*time.Minute)
	readyOne := insertSession("https://demo.localhost/pricing", now.Add(-3*time.Hour), 158)
	markReady(readyOne, now.Add(-3*time.Hour), 2*time.Minute+35*time.Second)
	readyTwo := insertSession("https://demo.localhost/onboarding", now.Add(-45*time.Minute), 96)
	markReady(readyTwo, now.Add(-45*time.Minute), 74*time.Second)

	if _, err := connection.Exec(ctx, `
		UPDATE sessions SET ended_at = started_at + interval '3 minutes',
			merge_requested_at = started_at + interval '3 minutes',
			merge_attempts = 12, failure_code = 'processing_failed'
		WHERE id = $1
	`, insertSession("https://demo.localhost/settings", now.Add(-8*time.Hour), 51)); err != nil {
		log.Fatalf("mark session failed: %v", err)
	}
	if _, err := connection.Exec(ctx, `
		UPDATE sessions SET ended_at = started_at + interval '90 seconds',
			finalize_requested_at = started_at + interval '90 seconds'
		WHERE id = $1
	`, insertSession("https://demo.localhost/search", now.Add(-20*time.Minute), 42)); err != nil {
		log.Fatalf("mark session processing: %v", err)
	}
	insertSession("https://demo.localhost/dashboard", now.Add(-4*time.Minute), 18)
	if _, err := connection.Exec(ctx, `
		UPDATE sessions SET retention_deleted_at = now()
		WHERE id = $1
	`, insertSession("https://demo.localhost/legacy", now.Add(-40*24*time.Hour), 130)); err != nil {
		log.Fatalf("mark session expired: %v", err)
	}

	var feedbackID string
	if err := connection.QueryRow(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, public_id, client_feedback_id, page_url,
			rating, text, session_id
		) VALUES ($1, $2, 'fb_DEMOSEED', gen_random_uuid(),
			'https://demo.localhost/checkout', 2,
			'The checkout total does not update when I change the quantity.', $3)
		RETURNING id
	`, tenantID, projectID, readyLinked).Scan(&feedbackID); err != nil {
		log.Fatalf("insert feedback: %v", err)
	}
	if _, err := connection.Exec(ctx, `
		UPDATE sessions SET feedback_id = $2 WHERE id = $1
	`, readyLinked, feedbackID); err != nil {
		log.Fatalf("link feedback: %v", err)
	}

	rawToken, tokenHash, err := auth.NewOpaqueToken()
	if err != nil {
		log.Fatalf("mint token: %v", err)
	}
	if _, err := connection.Exec(ctx, `
		INSERT INTO tenant_magic_links (
			tenant_id, member_id, email, invited_permissions, token_hash, expires_at
		) VALUES ($1, $2, $3, '{}', $4, $5)
	`, tenantID, memberID, memberEmail, tokenHash, now.Add(12*time.Hour)); err != nil {
		log.Fatalf("insert magic link: %v", err)
	}

	fmt.Println("tenant:", tenantID, "project:", projectID)
	fmt.Println("magic link: http://" + tenantSlug + ".localhost:3000/auth/verify#token=" + rawToken)
}

// replayEvents builds a minimal but valid rrweb recording: meta, full snapshot,
// and a stream of mouse movements so the player has a real timeline to scrub.
func replayEvents(startedAt time.Time, duration time.Duration) []byte {
	base := startedAt.UnixMilli()
	events := []map[string]any{
		{"type": 4, "data": map[string]any{
			"href": "https://demo.localhost/", "width": 1280, "height": 720,
		}, "timestamp": base},
		{"type": 2, "data": map[string]any{
			"node": map[string]any{
				"type": 0, "id": 1, "childNodes": []map[string]any{
					{"type": 1, "id": 2, "name": "html", "publicId": "", "systemId": ""},
					{"type": 2, "id": 3, "tagName": "html", "attributes": map[string]any{}, "childNodes": []map[string]any{
						{"type": 2, "id": 4, "tagName": "head", "attributes": map[string]any{}, "childNodes": []map[string]any{}},
						{"type": 2, "id": 5, "tagName": "body", "attributes": map[string]any{
							"style": "margin:0;background:#f5f7f6;font-family:system-ui,sans-serif;color:#13221d",
						}, "childNodes": []map[string]any{
							{"type": 2, "id": 6, "tagName": "main", "attributes": map[string]any{
								"style": "padding:48px;max-width:640px",
							}, "childNodes": []map[string]any{
								{"type": 2, "id": 7, "tagName": "h1", "attributes": map[string]any{}, "childNodes": []map[string]any{
									{"type": 3, "id": 8, "textContent": "Demo checkout"},
								}},
								{"type": 2, "id": 9, "tagName": "p", "attributes": map[string]any{}, "childNodes": []map[string]any{
									{"type": 3, "id": 10, "textContent": "A recorded visitor session, replayed by Fidibeki."},
								}},
							}},
						}},
					}},
				},
			},
			"initialOffset": map[string]any{"left": 0, "top": 0},
		}, "timestamp": base + 40},
	}
	steps := max(int(duration/(5*time.Second)), 2)
	for step := range steps {
		offset := int64(step+1) * duration.Milliseconds() / int64(steps+1)
		events = append(events, map[string]any{
			"type": 3,
			"data": map[string]any{
				"source": 1,
				"positions": []map[string]any{{
					"x": 120 + (step*67)%480, "y": 140 + (step*41)%320, "id": 6, "timeOffset": 0,
				}},
			},
			"timestamp": base + offset,
		})
	}
	payload, err := json.Marshal(events)
	if err != nil {
		log.Fatalf("marshal replay events: %v", err)
	}
	return payload
}
