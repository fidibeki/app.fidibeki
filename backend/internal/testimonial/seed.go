package testimonial

import (
	"context"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
)

// SeedFeedback is one piece of demo material.
//
// The set below is deliberately mixed. Seeding only glowing quotes would make
// the Suggested queue look clever when it is doing nothing: the useful demo is
// watching it refuse the ones it should.
type SeedFeedback struct {
	PublicID    string
	Text        string
	Rating      int
	Status      string
	Moderation  string
	Flags       []string
	Reachable   bool
	Note        string
	Testimonial *SeedTestimonial
}

// SeedTestimonial is an author's consented answer, for the material that should
// end up on the demo wall.
type SeedTestimonial struct {
	Name     string
	Role     string
	Company  string
	Headline string
	Scopes   ConsentScopes
}

// DemoFeedback is the corpus the seed inserts.
//
// Five of these must never be suggested, and each is a different reason, so the
// demo shows the gates rather than asserting they exist.
func DemoFeedback() []SeedFeedback {
	return []SeedFeedback{
		{
			PublicID: "fb_SEEDgood1", Rating: 5, Status: "open", Moderation: "completed", Reachable: true,
			Text: "We rolled this out across three teams and it cut our feedback triage from 3 days " +
				"to about 20 minutes per week. Support answered every question within the hour.",
			Note: "strong: specific, warm, reachable author",
			Testimonial: &SeedTestimonial{
				Name: "Ana Marques", Role: "Head of Support", Company: "Northwind",
				Headline: "Triage went from days to minutes",
				Scopes:   ConsentScopes{Quote: true, Identity: true, Company: true},
			},
		},
		{
			PublicID: "fb_SEEDgood2", Rating: 5, Status: "open", Moderation: "completed", Reachable: true,
			Text: "Onboarding a new engineer used to take a full afternoon of hand-holding. It is " +
				"now under 20 minutes and nobody has asked me for help since we switched.",
			Note: "strong: concrete before-and-after",
			Testimonial: &SeedTestimonial{
				Name: "Tiago Fonseca", Role: "Engineering Manager", Company: "Meridian",
				Scopes: ConsentScopes{Quote: true, Identity: true, Company: true},
			},
		},
		{
			PublicID: "fb_SEEDgood3", Rating: 4, Status: "open", Moderation: "completed", Reachable: true,
			Text: "Reduzimos a triagem de feedback de 3 dias para 20 minutos por semana, menos 90% " +
				"do tempo, e a equipa respondeu a tudo em menos de uma hora.",
			Note: "strong, in Portuguese: proves the specificity signal is language-neutral",
			Testimonial: &SeedTestimonial{
				Name: "Inês Cardoso", Role: "Product Lead", Company: "Aurora",
				Scopes: ConsentScopes{Quote: true, Identity: true},
			},
		},
		{
			PublicID: "fb_SEEDthin1", Rating: 5, Status: "open", Moderation: "completed", Reachable: true,
			Text: "Nice! Really good. We like it a lot, keep it up please and thank you.",
			Note: "five stars and worthless: no specificity, so it ranks low rather than being refused",
		},
		{
			PublicID: "fb_SEEDpii01", Rating: 5, Status: "open", Moderation: "completed", Reachable: true,
			Flags: []string{"pii"},
			Text: "Brilliant product, saved us 3 days a week. Call me on +351 900 000 000 or mail " +
				"ana.marques@northwind.test if you want to talk about it.",
			Note: "refused: glowing, and carries contact details",
		},
		{
			PublicID: "fb_SEEDangry", Rating: 1, Status: "open", Moderation: "completed", Reachable: true,
			Text: "Fantastic work losing our data for the third time this quarter. Truly world " +
				"class. I have never been so impressed by anything in my entire career.",
			Note: "refused: sarcasm that reads positive on tone alone",
		},
		{
			PublicID: "fb_SEEDunmod", Rating: 5, Status: "open", Moderation: "pending", Reachable: true,
			Text: "This genuinely cut our onboarding from three days to twenty minutes for every " +
				"single new team member we brought on this quarter.",
			Note: "refused: would be strong, but has not completed moderation",
		},
		{
			PublicID: "fb_SEEDspam1", Rating: 5, Status: "spam", Moderation: "completed", Reachable: false,
			Text: "Best tool ever, cut our costs by 90% in one week, everyone should buy it now.",
			Note: "refused: marked spam",
		},
	}
}

// SeedResult reports what the seed produced, so an operator can point the mock
// site at it without going digging in the database.
type SeedResult struct {
	TenantID      string
	ProjectID     string
	WallPublicID  string
	FeedbackCount int
	Suggested     int
	Published     int
}

// Seed installs the demo corpus, scores it, publishes a wall, and returns the
// wall's public identifier.
//
// It is idempotent by feedback public id, so running it twice on the same
// workspace refreshes rather than duplicating.
func Seed(
	ctx context.Context,
	query db.Querier,
	store Store,
	tenantID, projectID string,
	now time.Time,
) (SeedResult, error) {
	result := SeedResult{TenantID: tenantID, ProjectID: projectID}
	corpus := DemoFeedback()
	ids := make(map[string]string, len(corpus))

	for _, item := range corpus {
		var id string
		err := query.QueryRow(ctx, `
			INSERT INTO feedbacks (
				tenant_id, project_id, public_id, client_feedback_id, rating, text, page_url,
				status, text_process_status, text_moderation_flags, reporter_email,
				notify_reporter, language
			) VALUES (
				$1, $2, $3, gen_random_uuid(), $4, $5, 'https://northwind.test/pricing',
				$6::feedback_status, $7::processing_status, $8, $9, $10, 'en'
			)
			ON CONFLICT (tenant_id, public_id) DO UPDATE SET
				text = EXCLUDED.text, rating = EXCLUDED.rating, status = EXCLUDED.status,
				text_process_status = EXCLUDED.text_process_status,
				text_moderation_flags = EXCLUDED.text_moderation_flags
			RETURNING id
		`, tenantID, projectID, item.PublicID, item.Rating, item.Text, item.Status,
			item.Moderation, item.Flags, seedEmail(item), item.Reachable).Scan(&id)
		if err != nil {
			return result, fmt.Errorf("seed feedback %s: %w", item.PublicID, err)
		}
		ids[item.PublicID] = id
		result.FeedbackCount++
	}

	// Score through the real sweep rather than writing candidate rows directly,
	// so the demo exercises the code path a deployment actually runs.
	maintainer := ScoringMaintainer{Store: store, Database: query, Now: func() time.Time { return now }}
	if err := maintainer.MaintainOnce(ctx); err != nil {
		return result, fmt.Errorf("seed scoring sweep: %w", err)
	}
	suggested, err := store.ListCandidates(ctx, query, tenantID, 100)
	if err != nil {
		return result, fmt.Errorf("seed candidate list: %w", err)
	}
	result.Suggested = len(suggested)

	wallID, wallPublicID, err := seedWall(ctx, query, store, tenantID, now)
	if err != nil {
		return result, err
	}
	result.WallPublicID = wallPublicID

	items := make([]WallItem, 0, 3)
	for _, item := range corpus {
		if item.Testimonial == nil {
			continue
		}
		testimonialID, err := seedTestimonial(ctx, query, store, tenantID, projectID,
			ids[item.PublicID], item, now)
		if err != nil {
			return result, err
		}
		items = append(items, WallItem{TestimonialID: testimonialID})
	}
	if err := store.SetWallItems(ctx, query, tenantID, wallID, items); err != nil {
		return result, fmt.Errorf("seed wall items: %w", err)
	}

	cards, err := store.LoadWallCards(ctx, query, tenantID, wallID)
	if err != nil {
		return result, fmt.Errorf("seed wall cards: %w", err)
	}
	wall, err := store.GetWall(ctx, query, tenantID, wallID)
	if err != nil {
		return result, fmt.Errorf("seed wall reload: %w", err)
	}
	payload := BuildSnapshotPayload(wall, cards, now)
	if _, err := store.PublishSnapshot(ctx, query, tenantID, wallID, payload, nil, now); err != nil {
		return result, fmt.Errorf("seed publish: %w", err)
	}
	result.Published = len(payload.Cards)
	return result, nil
}

func seedEmail(item SeedFeedback) *string {
	if !item.Reachable {
		return nil
	}
	address := "author+" + item.PublicID + "@example.test"
	return &address
}

func seedWall(
	ctx context.Context,
	query db.Querier,
	store Store,
	tenantID string,
	now time.Time,
) (string, string, error) {
	var existingID, existingPublic string
	err := query.QueryRow(ctx, `
		SELECT id, public_id FROM testimonial_walls WHERE tenant_id = $1 AND slug = 'demo'
	`, tenantID).Scan(&existingID, &existingPublic)
	if err == nil {
		return existingID, existingPublic, nil
	}
	publicID, err := GeneratePublicID(PrefixWall)
	if err != nil {
		return "", "", fmt.Errorf("seed wall id: %w", err)
	}
	wall, err := store.CreateWall(ctx, query, tenantID, publicID, WallValues{
		Name: "Demo wall", Slug: "demo", Layout: LayoutGrid, MaxItems: 12,
		ShowRating: true, ShowAvatar: true, ShowCompany: true, ShowVideo: true,
	})
	if err != nil {
		return "", "", fmt.Errorf("seed wall: %w", err)
	}
	_ = now
	return wall.ID, wall.PublicID, nil
}

func seedTestimonial(
	ctx context.Context,
	query db.Querier,
	store Store,
	tenantID, projectID, feedbackID string,
	item SeedFeedback,
	now time.Time,
) (string, error) {
	var existing string
	err := query.QueryRow(ctx, `
		SELECT id FROM testimonials
		WHERE tenant_id = $1 AND source_feedback_id = $2 AND status <> 'archived'
	`, tenantID, feedbackID).Scan(&existing)
	if err == nil {
		return existing, nil
	}

	publicID, err := GeneratePublicID(PrefixTestimonial)
	if err != nil {
		return "", fmt.Errorf("seed testimonial id: %w", err)
	}
	_, revocationHash, err := GenerateToken()
	if err != nil {
		return "", fmt.Errorf("seed revocation token: %w", err)
	}
	requestToken, requestHash, err := GenerateToken()
	if err != nil {
		return "", fmt.Errorf("seed request token: %w", err)
	}
	_ = requestToken

	request, err := store.CreateRequest(ctx, query, tenantID, RequestValues{
		ProjectID: &projectID, FeedbackID: &feedbackID, Kind: RequestCollection,
		TokenHash: requestHash, RecipientEmail: *seedEmail(item),
		ExpiresAt: now.Add(30 * 24 * time.Hour), SentAt: &now,
	})
	if err != nil {
		return "", fmt.Errorf("seed request: %w", err)
	}

	headline := item.Testimonial.Headline
	var headlinePtr *string
	if headline != "" {
		headlinePtr = &headline
	}
	created, err := store.SubmitCollection(ctx, query, tenantID, publicID, revocationHash,
		SubmissionValues{
			ProjectID: &projectID, FeedbackID: &feedbackID, RequestID: request.ID,
			Source: SourceFeedback, Quote: item.Text, Headline: headlinePtr,
			Author: AuthorDetails{
				Name: item.Testimonial.Name, Role: item.Testimonial.Role,
				Company: item.Testimonial.Company,
			},
			Scopes:  item.Testimonial.Scopes,
			Consent: ConsentEvidence{NoticeVersion: "v1", Locale: "en", UserAgentFamily: "seed"},
		}, now)
	if err != nil {
		return "", fmt.Errorf("seed submission: %w", err)
	}
	if err := store.ApproveTestimonial(ctx, query, tenantID, created.ID, "", now); err != nil {
		// Approval records the acting member; the seed has none, so approve
		// directly rather than inventing one.
		if _, updateErr := query.Exec(ctx, `
			UPDATE testimonials SET status = 'approved', approved_at = $3
			WHERE tenant_id = $1 AND id = $2
		`, tenantID, created.ID, now); updateErr != nil {
			return "", fmt.Errorf("seed approval: %w", updateErr)
		}
	}
	return created.ID, nil
}
