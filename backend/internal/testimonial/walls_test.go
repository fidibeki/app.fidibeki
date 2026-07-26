package testimonial

import (
	"strings"
	"testing"
	"time"
)

func approvedCard(publicID string) PublishableCard {
	rating := 5
	return PublishableCard{
		Testimonial: Testimonial{
			PublicID:      publicID,
			ConsentedText: "We cut triage from 3 days to 20 minutes.",
			Quote:         "We cut triage from 3 days to 20 minutes.",
			Rating:        &rating,
			Status:        StatusApproved,
			ConsentState:  ConsentGranted,
			Scopes:        ConsentScopes{Quote: true, Identity: true, Company: true, Avatar: true},
			Author: AuthorDetails{
				Name: "Ana Marques", Role: "Head of Support",
				Company: "Acme", CompanyURL: "https://acme.test",
			},
		},
		MediaReady: true,
		AvatarURL:  "/v1/public/media/tm_abc/hash.jpg",
	}
}

func defaultWall() Wall {
	return Wall{
		PublicID: "wl_0123456789abcdef", Name: "Homepage", Layout: LayoutGrid, MaxItems: 24,
		ShowRating: true, ShowAvatar: true, ShowCompany: true, ShowVideo: true,
	}
}

func TestBuildSnapshotPayloadDropsUnpublishableCards(t *testing.T) {
	t.Parallel()
	pending := approvedCard("ts_pending")
	pending.Status = StatusPendingReview
	revoked := approvedCard("ts_revoked")
	revoked.ConsentState = ConsentRevoked
	drifted := approvedCard("ts_drifted")
	drifted.Quote = "We cut triage a lot."
	unreviewedVideo := approvedCard("ts_video")
	unreviewedVideo.HasVideo = true
	unreviewedVideo.VideoReviewed = false

	payload := BuildSnapshotPayload(defaultWall(), []PublishableCard{
		approvedCard("ts_good"), pending, revoked, drifted, unreviewedVideo,
	}, time.Unix(0, 0))

	if len(payload.Cards) != 1 || payload.Cards[0].ID != "ts_good" {
		t.Fatalf("cards = %#v, want only the publishable one", payload.Cards)
	}
	if payload.SchemaVersion != SnapshotSchemaVersion {
		t.Errorf("schema version = %d, want %d", payload.SchemaVersion, SnapshotSchemaVersion)
	}
}

func TestBuildSnapshotPayloadProjectsThroughConsentScopes(t *testing.T) {
	t.Parallel()
	card := approvedCard("ts_scoped")
	card.Scopes = ConsentScopes{Quote: true, Identity: false, Company: true, Avatar: false}
	payload := BuildSnapshotPayload(defaultWall(), []PublishableCard{card}, time.Unix(0, 0))
	if len(payload.Cards) != 1 {
		t.Fatalf("cards = %#v, want one", payload.Cards)
	}
	author := payload.Cards[0].Author
	if author.Name != "" || author.Role != "" {
		t.Errorf("identity was not consented but survived: %#v", author)
	}
	if author.Company != "Acme" {
		t.Errorf("company was consented but did not survive: %#v", author)
	}
	if author.AvatarURL != "" {
		t.Errorf("avatar was not consented but survived: %#v", author)
	}
}

// The wall's display toggles are the customer's choice and apply on top of
// consent, never instead of it.
func TestBuildSnapshotPayloadHonoursWallDisplayToggles(t *testing.T) {
	t.Parallel()
	wall := defaultWall()
	wall.ShowRating, wall.ShowAvatar, wall.ShowCompany = false, false, false
	payload := BuildSnapshotPayload(wall, []PublishableCard{approvedCard("ts_toggle")}, time.Unix(0, 0))
	card := payload.Cards[0]
	if card.Rating != nil {
		t.Errorf("rating hidden by the wall but survived: %#v", card)
	}
	if card.Author.AvatarURL != "" || card.Author.Company != "" {
		t.Errorf("avatar/company hidden by the wall but survived: %#v", card.Author)
	}
}

func TestBuildSnapshotPayloadRespectsMaxItems(t *testing.T) {
	t.Parallel()
	wall := defaultWall()
	wall.MaxItems = 2
	cards := []PublishableCard{
		approvedCard("ts_1"), approvedCard("ts_2"), approvedCard("ts_3"),
	}
	payload := BuildSnapshotPayload(wall, cards, time.Unix(0, 0))
	if len(payload.Cards) != 2 {
		t.Fatalf("cards = %d, want the wall's max of 2", len(payload.Cards))
	}
}

func TestFilterRevokedRemovesOnlyTheNamedCards(t *testing.T) {
	t.Parallel()
	payload := SnapshotPayload{Cards: []SnapshotCard{
		{ID: "ts_keep"}, {ID: "ts_drop"}, {ID: "ts_also_keep"},
	}}
	filtered := filterRevoked(payload, map[string]struct{}{"ts_drop": {}})
	if len(filtered.Cards) != 2 {
		t.Fatalf("cards = %#v, want two survivors", filtered.Cards)
	}
	for _, card := range filtered.Cards {
		if card.ID == "ts_drop" {
			t.Fatal("a revoked card survived the filter")
		}
	}
	unchanged := filterRevoked(payload, nil)
	if len(unchanged.Cards) != 3 {
		t.Fatalf("an empty revocation set changed the payload: %#v", unchanged.Cards)
	}
}

// The allowlist is casual reuse prevention, not access control, and the
// behaviour has to match that description honestly.
func TestOriginAllowed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		origin  string
		allowed []string
		want    bool
	}{
		{name: "no allowlist permits everything", origin: "https://any.test", want: true},
		{
			name: "listed origin passes", origin: "https://acme.test",
			allowed: []string{"https://acme.test"}, want: true,
		},
		{
			name: "case and trailing path are normalised", origin: "HTTPS://Acme.test",
			allowed: []string{"https://acme.test"}, want: true,
		},
		{
			name: "unlisted origin is refused", origin: "https://evil.test",
			allowed: []string{"https://acme.test"}, want: false,
		},
		{
			// A direct fetch carries no Origin. Refusing it would imply the
			// allowlist is a security boundary, which it is not.
			name: "a direct fetch with no origin is allowed", origin: "",
			allowed: []string{"https://acme.test"}, want: true,
		},
		{
			name: "the literal null origin is treated as absent", origin: "null",
			allowed: []string{"https://acme.test"}, want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := OriginAllowed(test.origin, test.allowed); got != test.want {
				t.Fatalf("OriginAllowed(%q, %v) = %t, want %t",
					test.origin, test.allowed, got, test.want)
			}
		})
	}
}

// The snapshot embeds a stable path, never a presigned URL: a presigned URL
// expires and the snapshot does not.
func TestMediaPathIsStableAndContentAddressed(t *testing.T) {
	t.Parallel()
	got := MediaPath("tm_0123456789abcdef", "abc123", "mp4")
	want := "/v1/public/media/tm_0123456789abcdef/abc123.mp4"
	if got != want {
		t.Fatalf("MediaPath = %q, want %q", got, want)
	}
}

func TestRequestUsable(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	tests := []struct {
		name    string
		request Request
		want    bool
	}{
		{name: "pending and unexpired", request: Request{Status: RequestPending, ExpiresAt: future}, want: true},
		{name: "opened and unexpired", request: Request{Status: RequestOpened, ExpiresAt: future}, want: true},
		{name: "expired by time", request: Request{Status: RequestPending, ExpiresAt: past}},
		{name: "already submitted", request: Request{Status: RequestSubmitted, ExpiresAt: future}},
		{name: "cancelled", request: Request{Status: RequestCancelled, ExpiresAt: future}},
		{name: "marked expired", request: Request{Status: RequestExpired, ExpiresAt: future}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.request.Usable(now); got != test.want {
				t.Fatalf("Usable = %t, want %t for %#v", got, test.want, test.request)
			}
		})
	}
}

func TestWallLayoutValid(t *testing.T) {
	t.Parallel()
	for _, layout := range []WallLayout{
		LayoutGrid, LayoutCarousel, LayoutMarquee, LayoutList, LayoutMasonry,
		LayoutSingle, LayoutCustom,
	} {
		if !layout.Valid() {
			t.Errorf("%q must be a valid layout", layout)
		}
	}
	if WallLayout("spiral").Valid() {
		t.Error("an unknown layout must not validate")
	}
}

func TestThemeInvalidRefusesWhatCannotBeACSSValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		theme Theme
		want  bool
	}{
		{name: "empty", theme: Theme{}},
		{name: "ordinary", theme: Theme{Accent: "#087f73", Radius: "10px", Mode: "dark"}},
		{name: "font stack with quotes and commas", theme: Theme{FontFamily: `"Segoe UI", system-ui, sans-serif`}},
		{name: "closes the declaration", theme: Theme{Accent: "red; background: url(x)"}, want: true},
		{name: "opens a rule", theme: Theme{Radius: "10px} :host{display:none"}, want: true},
		{name: "hides the rest in a comment", theme: Theme{Accent: "red /* trailing"}, want: true},
		{name: "unknown mode", theme: Theme{Mode: "sepia"}, want: true},
		{name: "far longer than any colour", theme: Theme{Accent: strings.Repeat("a", 121)}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.theme.Invalid() != ""; got != test.want {
				t.Fatalf("Invalid() rejected = %t, want %t (reason %q)", got, test.want, test.theme.Invalid())
			}
		})
	}
}

func TestQuoteIsConsentedTracksTheEditRule(t *testing.T) {
	t.Parallel()
	item := Testimonial{
		ConsentedText: "I did not think this would work, but it does.",
		Quote:         "I did not think this would work, but it does.",
	}
	if !item.QuoteIsConsented() {
		t.Fatal("an unedited quote must count as consented")
	}
	item.Quote = "I did not think this would work"
	if item.QuoteIsConsented() {
		t.Fatal("a meaning-inverting truncation must not count as consented")
	}
	if item.Publishability(true, false, false).Publishable() {
		t.Fatal("a drifted quote must not be publishable")
	}
}
