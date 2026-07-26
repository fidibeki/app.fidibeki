package testimonial

import "testing"

func TestQuoteMatchesConsentAcceptsOnlyCosmeticDifferences(t *testing.T) {
	t.Parallel()
	const consented = "We cut our triage time from 3 days to 20 minutes."
	tests := []struct {
		name  string
		quote string
		want  bool
	}{
		{name: "identical", quote: consented, want: true},
		{name: "leading and trailing whitespace", quote: "  " + consented + "\n", want: true},
		{name: "collapsed inner whitespace", quote: "We cut our triage time from 3 days  to 20 minutes.", want: true},
		{name: "decomposed unicode normalises", quote: "Reduzimos a triagem á tarde.", want: false},
		{name: "different wording", quote: "We cut our triage time a lot.", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := QuoteMatchesConsent(consented, test.quote); got != test.want {
				t.Fatalf("QuoteMatchesConsent(%q) = %t, want %t", test.quote, got, test.want)
			}
		})
	}
}

// The rule that replaced "shortening is free". Every case here was legal under
// the substring rule and every one of them changes what the author said.
func TestQuoteMatchesConsentRejectsMeaningInvertingTruncation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		consented string
		quote     string
	}{
		{
			name:      "english contrastive clause dropped",
			consented: "I did not think this would work, but it does.",
			quote:     "I did not think this would work",
		},
		{
			name:      "portuguese contrastive clause dropped",
			consented: "Nao achei que isto fosse funcionar, mas funciona.",
			quote:     "Nao achei que isto fosse funcionar",
		},
		{
			name:      "price objection stripped of its resolution",
			consented: "It is not cheap, but it is worth every cent.",
			quote:     "It is not cheap",
		},
		{
			name:      "second sentence dropped",
			consented: "Support was slow at first. Then they fixed everything in an hour.",
			quote:     "Support was slow at first.",
		},
		{
			name:      "innocuous trailing trim is still an edit",
			consented: "The migration lost nothing at all.",
			quote:     "The migration lost nothing",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if QuoteMatchesConsent(test.consented, test.quote) {
				t.Fatalf("QuoteMatchesConsent accepted a meaning-changing edit: %q -> %q",
					test.consented, test.quote)
			}
		})
	}
}

func TestPublishableRequiresEveryCondition(t *testing.T) {
	t.Parallel()
	base := func() Publishability {
		return Publishability{
			Status:              StatusApproved,
			ConsentState:        ConsentGranted,
			Scopes:              ConsentScopes{Quote: true, Identity: true},
			QuoteMatchesConsent: true,
			MediaReady:          true,
			VideoReviewed:       true,
			HasVideo:            false,
		}
	}
	if !base().Publishable() {
		t.Fatalf("the base case must be publishable: %#v", base())
	}

	tests := []struct {
		name   string
		mutate func(*Publishability)
	}{
		{name: "not approved", mutate: func(p *Publishability) { p.Status = StatusPendingReview }},
		{name: "archived", mutate: func(p *Publishability) { p.Status = StatusArchived }},
		{name: "consent revoked", mutate: func(p *Publishability) { p.ConsentState = ConsentRevoked }},
		{name: "consent awaiting reconsent", mutate: func(p *Publishability) { p.ConsentState = ConsentPendingReconsent }},
		{name: "consent expired", mutate: func(p *Publishability) { p.ConsentState = ConsentExpired }},
		{
			// A card with a name, a company and a video but no consented quote is
			// not a testimonial, and the video would be words the author never
			// agreed to publish.
			name:   "quote scope not granted",
			mutate: func(p *Publishability) { p.Scopes.Quote = false },
		},
		{name: "quote drifted from consent", mutate: func(p *Publishability) { p.QuoteMatchesConsent = false }},
		{name: "media not ready", mutate: func(p *Publishability) { p.MediaReady = false }},
		{
			name: "video present but unreviewed",
			mutate: func(p *Publishability) {
				p.HasVideo = true
				p.VideoReviewed = false
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := base()
			test.mutate(&state)
			if state.Publishable() {
				t.Fatalf("%s must not be publishable: %#v", test.name, state)
			}
		})
	}
}

// Video review only gates when there is a video to review.
func TestPublishableIgnoresVideoReviewWithoutVideo(t *testing.T) {
	t.Parallel()
	state := Publishability{
		Status:              StatusApproved,
		ConsentState:        ConsentGranted,
		Scopes:              ConsentScopes{Quote: true},
		QuoteMatchesConsent: true,
		MediaReady:          true,
		HasVideo:            false,
		VideoReviewed:       false,
	}
	if !state.Publishable() {
		t.Fatalf("a text-only testimonial must not wait on video review: %#v", state)
	}
}

func TestConsentScopesProjectionHidesUngrantedFields(t *testing.T) {
	t.Parallel()
	full := AuthorDetails{
		Name:       "Ana Marques",
		Role:       "Head of Support",
		Company:    "Acme",
		CompanyURL: "https://acme.test",
		AvatarURL:  "https://cdn.test/a.jpg",
		VideoURL:   "https://cdn.test/a.mp4",
	}
	scopes := ConsentScopes{Quote: true, Identity: false, Company: true, Avatar: false, Video: true}
	got := full.Project(scopes)
	if got.Name != "" || got.Role != "" {
		t.Errorf("identity was not granted but name/role survived: %#v", got)
	}
	if got.Company != "Acme" || got.CompanyURL != "https://acme.test" {
		t.Errorf("company was granted but did not survive: %#v", got)
	}
	if got.AvatarURL != "" {
		t.Errorf("avatar was not granted but survived: %#v", got)
	}
	if got.VideoURL == "" {
		t.Errorf("video was granted but did not survive: %#v", got)
	}
}

// Revoking one scope must not unpublish the rest.
func TestConsentScopesProjectionKeepsTheQuoteWhenOnlyAvatarIsRevoked(t *testing.T) {
	t.Parallel()
	details := AuthorDetails{Name: "Ana", AvatarURL: "https://cdn.test/a.jpg"}
	got := details.Project(ConsentScopes{Quote: true, Identity: true, Avatar: false})
	if got.Name != "Ana" {
		t.Errorf("identity was granted but name was dropped: %#v", got)
	}
	if got.AvatarURL != "" {
		t.Errorf("avatar was revoked but survived: %#v", got)
	}
}
