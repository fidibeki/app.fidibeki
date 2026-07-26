package testimonial

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestScoringSweepPopulatesTheSuggestedQueue is the test that matters most for
// whether this feature works at all in production.
//
// Every other piece can be correct and the Suggested tab still be permanently
// empty, because nothing else connects a feedback to a candidate row. This
// proves the sweep does, and that it applies the same gates the pure engine
// does when reading from real rows rather than a hand-built struct.
func TestScoringSweepPopulatesTheSuggestedQueue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := newTestimonialEnvironment(ctx, t)
	store := Store{}
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	// Alongside the seeded strong candidate, three that must not be suggested.
	seed := func(publicID, text string, rating int, status, moderation string, flags []string) {
		t.Helper()
		if _, err := env.root.Exec(ctx, `
			INSERT INTO feedbacks (
				tenant_id, project_id, public_id, client_feedback_id, rating, text, page_url,
				text_process_status, text_moderation_flags, language
			) VALUES ($1, $2, $3, gen_random_uuid(), $4, $5, 'https://example.com/x', $6::processing_status, $7, 'en')
		`, env.tenantID, env.projectID, publicID, rating, text, moderation, flags); err != nil {
			t.Fatalf("seed feedback %s: %v", publicID, err)
		}
	}
	seed("fb_PII00001",
		"Absolutely brilliant, it saved our team about 3 days a week. Reach me at ana@example.test.",
		5, "open", "completed", []string{"pii"})
	seed("fb_ANGRY001",
		"Great job losing our data for the third time this quarter, truly world class work here.",
		1, "open", "completed", []string{})
	seed("fb_UNMOD001",
		"This genuinely cut our onboarding from three days to twenty minutes for every new team.",
		5, "open", "pending", []string{})

	maintainer := ScoringMaintainer{
		Store: store, Database: env.pools.Admin,
		Now: func() time.Time { return now },
	}
	if err := maintainer.MaintainOnce(ctx); err != nil {
		t.Fatalf("scoring sweep: %v", err)
	}

	var suggested []Candidate
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		var err error
		suggested, err = store.ListCandidates(ctx, tx, env.tenantID, 50)
		if err != nil {
			t.Fatalf("list candidates: %v", err)
		}
	})

	if len(suggested) != 1 {
		for _, candidate := range suggested {
			t.Logf("suggested %s: %q", candidate.PublicID, candidate.Text)
		}
		t.Fatalf("suggested = %d, want only the clean strong candidate", len(suggested))
	}
	if suggested[0].PublicID != "fb_ABCD1234" {
		t.Fatalf("suggested %q, want the seeded strong candidate", suggested[0].PublicID)
	}
	if len(suggested[0].Reasons) == 0 {
		t.Error("a suggested candidate must arrive with the reasons that ranked it")
	}

	// Every feedback gets a row, including the refusals: the backoffice can then
	// explain why something is absent instead of silently omitting it.
	var total, ineligible int
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		if err := tx.QueryRow(ctx, `
			SELECT count(*), count(*) FILTER (WHERE eligibility = 'ineligible')
			FROM testimonial_candidates WHERE tenant_id = $1
		`, env.tenantID).Scan(&total, &ineligible); err != nil {
			t.Fatalf("count candidates: %v", err)
		}
	})
	if total != 4 || ineligible != 3 {
		t.Fatalf("candidates = %d total, %d ineligible; want 4 and 3", total, ineligible)
	}

	// The sweep is idempotent: running it again must not duplicate or rescore
	// what is already at the current version.
	if err := maintainer.MaintainOnce(ctx); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		var after int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM testimonial_candidates WHERE tenant_id = $1
		`, env.tenantID).Scan(&after); err != nil {
			t.Fatalf("recount candidates: %v", err)
		}
		if after != total {
			t.Fatalf("candidates after a second sweep = %d, want %d", after, total)
		}
	})

	// Decay must lower an older candidate without disturbing its base score.
	later := now.Add(180 * 24 * time.Hour)
	aged := ScoringMaintainer{Store: store, Database: env.pools.Admin, Now: func() time.Time { return later }}
	if err := aged.MaintainOnce(ctx); err != nil {
		t.Fatalf("aged sweep: %v", err)
	}
	env.withTenant(ctx, t, func(tx pgx.Tx) {
		var base, decayed float64
		if err := tx.QueryRow(ctx, `
			SELECT base_score, decayed_score FROM testimonial_candidates
			WHERE tenant_id = $1 AND eligibility = 'eligible'
		`, env.tenantID).Scan(&base, &decayed); err != nil {
			t.Fatalf("read decayed score: %v", err)
		}
		if decayed >= base {
			t.Fatalf("decayed = %v, base = %v; an aged candidate must rank lower", decayed, base)
		}
		if base <= 0 {
			t.Fatalf("base score = %v; decay must not overwrite the base", base)
		}
	})
}
