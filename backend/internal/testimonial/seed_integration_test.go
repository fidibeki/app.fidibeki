package testimonial

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// TestSeedProducesAWorkingDemo is the guard on the environment itself.
//
// The seed exists so someone can look at this feature working. If it produced a
// wall that served nothing, or suggested the material it is supposed to refuse,
// the demo would be worse than no demo: it would misrepresent what the feature
// does.
func TestSeedProducesAWorkingDemo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := newTestimonialEnvironment(ctx, t)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	result, err := Seed(ctx, env.pools.Admin, Store{}, env.tenantID, env.projectID, now)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if result.WallPublicID == "" || result.Published == 0 {
		t.Fatalf("seed result = %#v, want a published wall", result)
	}
	if result.FeedbackCount != len(DemoFeedback()) {
		t.Fatalf("seeded %d feedbacks, want %d", result.FeedbackCount, len(DemoFeedback()))
	}

	// The corpus is mixed on purpose. If everything were suggested, the demo
	// would prove nothing about the gates.
	if result.Suggested >= result.FeedbackCount {
		t.Fatalf("suggested %d of %d; the refusals are the point of the corpus",
			result.Suggested, result.FeedbackCount)
	}
	if result.Suggested == 0 {
		t.Fatal("nothing was suggested; the demo would show an empty queue")
	}

	// The wall must actually serve over the public endpoint, because that is what
	// the mock customer site fetches.
	server := env.publicServer(t)
	defer server.Close()
	response, body := getWall(t, server.URL, result.WallPublicID, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("seeded wall status = %d, want 200 (%s)", response.StatusCode, body)
	}
	var payload SnapshotPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode seeded payload: %v", err)
	}
	if len(payload.Cards) != result.Published {
		t.Fatalf("served %d cards, published %d", len(payload.Cards), result.Published)
	}
	for _, card := range payload.Cards {
		if card.Quote == "" {
			t.Errorf("a seeded card has no quote: %#v", card)
		}
	}
	// One of the seeded testimonials withholds company consent, so at least one
	// card must arrive without a company even though the seed supplied one.
	withheld := false
	for _, card := range payload.Cards {
		if card.Author.Name != "" && card.Author.Company == "" {
			withheld = true
		}
	}
	if !withheld {
		t.Error("no seeded card exercises a withheld consent scope")
	}

	// Running it twice must refresh rather than duplicate, so an operator can
	// re-run it without cleaning up first.
	second, err := Seed(ctx, env.pools.Admin, Store{}, env.tenantID, env.projectID, now)
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if second.WallPublicID != result.WallPublicID {
		t.Fatalf("second seed made a new wall %q, want %q", second.WallPublicID, result.WallPublicID)
	}
	if second.FeedbackCount != result.FeedbackCount || second.Published != result.Published {
		t.Fatalf("second seed = %#v, want the same shape as %#v", second, result)
	}
}
