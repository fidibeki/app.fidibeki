package testimonial

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("testimonial resource not found")
	// ErrTokenInvalid covers an unknown, expired, cancelled or already-submitted
	// token. The caller must not distinguish between them in a response: telling
	// an anonymous caller that a token exists but expired is more than they are
	// owed.
	ErrTokenInvalid = errors.New("testimonial token is not usable")
	// ErrNotPublishable is returned when a wall publish would include a
	// testimonial that fails the publishability predicate.
	ErrNotPublishable = errors.New("testimonial is not publishable")
	ErrWallEmpty      = errors.New("a wall must contain at least one publishable testimonial")
	ErrConflict       = errors.New("testimonial resource changed concurrently")
)

// Source records where a testimonial came from.
type Source string

const (
	SourceFeedback Source = "feedback"
	SourceForm     Source = "form"
	SourceManual   Source = "manual"
)

// RequestStatus is the lifecycle of a collection or re-consent invitation.
type RequestStatus string

const (
	RequestPending   RequestStatus = "pending"
	RequestOpened    RequestStatus = "opened"
	RequestSubmitted RequestStatus = "submitted"
	RequestExpired   RequestStatus = "expired"
	RequestCancelled RequestStatus = "cancelled"
)

// RequestKind separates the first ask from a re-approval of an edited quote.
type RequestKind string

const (
	RequestCollection RequestKind = "collection"
	RequestReconsent  RequestKind = "reconsent"
)

// WallLayout is how the embed arranges its cards.
type WallLayout string

const (
	LayoutGrid     WallLayout = "grid"
	LayoutCarousel WallLayout = "carousel"
	LayoutMarquee  WallLayout = "marquee"
	LayoutList     WallLayout = "list"
	LayoutMasonry  WallLayout = "masonry"
	LayoutSingle   WallLayout = "single"
	// LayoutCustom means the customer renders the wall themselves from the
	// public JSON. The embed still works — it falls back to the grid — but the
	// backoffice hands them the endpoint rather than a script tag.
	LayoutCustom WallLayout = "custom"
)

func (l WallLayout) Valid() bool {
	switch l {
	case LayoutGrid, LayoutCarousel, LayoutMarquee, LayoutList, LayoutMasonry,
		LayoutSingle, LayoutCustom:
		return true
	}
	return false
}

// WallStatus is whether a wall is published to the public internet.
type WallStatus string

const (
	WallDraft     WallStatus = "draft"
	WallPublished WallStatus = "published"
)

// Candidate is one scored feedback in the Suggested queue.
type Candidate struct {
	FeedbackID        string             `json:"feedback_id"`
	ProjectID         string             `json:"project_id"`
	PublicID          string             `json:"public_id"`
	Text              string             `json:"text"`
	Rating            *int               `json:"rating"`
	BaseScore         float64            `json:"base_score"`
	DecayedScore      float64            `json:"decayed_score"`
	ScoreVersion      int                `json:"score_version"`
	Eligibility       Eligibility        `json:"eligibility"`
	Reasons           []string           `json:"reasons"`
	IneligibleReasons []string           `json:"ineligible_reasons"`
	Signals           map[string]float64 `json:"signals"`
	SuggestedExcerpt  *string            `json:"suggested_excerpt"`
	SuggestedHeadline *string            `json:"suggested_headline"`
	ReporterEmail     *string            `json:"reporter_email"`
	ContactPermitted  bool               `json:"contact_permitted"`
	DismissedAt       *time.Time         `json:"dismissed_at"`
	CreatedAt         time.Time          `json:"created_at"`
	ComputedAt        time.Time          `json:"computed_at"`
}

// Testimonial is the aggregate as the backoffice sees it.
type Testimonial struct {
	ID            string        `json:"id"`
	PublicID      string        `json:"public_id"`
	ProjectID     *string       `json:"project_id"`
	Source        Source        `json:"source"`
	FeedbackID    *string       `json:"source_feedback_id"`
	RequestID     *string       `json:"request_id"`
	ConsentedText string        `json:"consented_text"`
	Quote         string        `json:"quote"`
	Headline      *string       `json:"headline"`
	Author        AuthorDetails `json:"author"`
	Rating        *int          `json:"rating"`
	Status        Status        `json:"status"`
	ConsentState  ConsentState  `json:"consent_state"`
	Scopes        ConsentScopes `json:"consent_scopes"`
	ReviewNote    *string       `json:"review_note"`
	// Nil when there is no recording. Present so the library can say what is
	// holding a card back rather than showing an approved testimonial that
	// silently refuses to publish.
	VideoStatus   *string    `json:"video_status,omitempty"`
	VideoReviewed *bool      `json:"video_reviewed,omitempty"`
	TagIDs        []string   `json:"tag_ids"`
	CollectedAt   *time.Time `json:"collected_at"`
	ApprovedAt    *time.Time `json:"approved_at"`
	RevokedAt     *time.Time `json:"revoked_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// QuoteIsConsented reports whether the displayed quote still matches what the
// author agreed to.
func (t Testimonial) QuoteIsConsented() bool {
	return QuoteMatchesConsent(t.ConsentedText, t.Quote)
}

// Publishability derives the predicate inputs this aggregate can answer for
// itself. Media readiness and video review are supplied by the caller, which
// has loaded them.
func (t Testimonial) Publishability(mediaReady, hasVideo, videoReviewed bool) Publishability {
	return Publishability{
		Status:              t.Status,
		ConsentState:        t.ConsentState,
		Scopes:              t.Scopes,
		QuoteMatchesConsent: t.QuoteIsConsented(),
		MediaReady:          mediaReady,
		HasVideo:            hasVideo,
		VideoReviewed:       videoReviewed,
	}
}

// Request is a collection or re-consent invitation.
type Request struct {
	ID             string        `json:"id"`
	ProjectID      *string       `json:"project_id"`
	FeedbackID     *string       `json:"feedback_id"`
	TestimonialID  *string       `json:"testimonial_id"`
	Kind           RequestKind   `json:"kind"`
	RecipientEmail string        `json:"recipient_email"`
	Status         RequestStatus `json:"status"`
	Prompt         *string       `json:"prompt"`
	Locale         *string       `json:"locale"`
	ProposedQuote  *string       `json:"proposed_quote"`
	ExpiresAt      time.Time     `json:"expires_at"`
	SentAt         *time.Time    `json:"sent_at"`
	OpenedAt       *time.Time    `json:"opened_at"`
	SubmittedAt    *time.Time    `json:"submitted_at"`
	CreatedAt      time.Time     `json:"created_at"`
}

// Usable reports whether a token may still be acted on.
func (r Request) Usable(now time.Time) bool {
	if r.Status != RequestPending && r.Status != RequestOpened {
		return false
	}
	return now.Before(r.ExpiresAt)
}

// Wall is a curated, ordered collection the customer publishes.
type Wall struct {
	ID             string     `json:"id"`
	PublicID       string     `json:"public_id"`
	ProjectID      *string    `json:"project_id"`
	Name           string     `json:"name"`
	Slug           string     `json:"slug"`
	Layout         WallLayout `json:"layout"`
	Theme          Theme      `json:"theme"`
	ShowRating     bool       `json:"show_rating"`
	ShowAvatar     bool       `json:"show_avatar"`
	ShowCompany    bool       `json:"show_company"`
	ShowVideo      bool       `json:"show_video"`
	MaxItems       int        `json:"max_items"`
	AllowedOrigins []string   `json:"allowed_origins"`
	Status         WallStatus `json:"status"`
	ItemCount      int        `json:"item_count"`
	PublishedAt    *time.Time `json:"published_at"`
	// HasUnpublishedChanges is what lets the screen offer publishing only when
	// there is something to publish, rather than permanently or never.
	HasUnpublishedChanges bool      `json:"has_unpublished_changes"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// Theme is the customer's visual configuration, mirrored into the embed as CSS
// custom properties.
type Theme struct {
	Accent     string `json:"accent,omitempty"`
	Radius     string `json:"radius,omitempty"`
	FontFamily string `json:"font_family,omitempty"`
	Mode       string `json:"mode,omitempty"`
}

// Invalid returns why the theme cannot be stored, or "" when it can.
//
// These values end up as CSS custom property values on a customer's own page.
// The embed already escapes them, so this is not the thing standing between us
// and injected CSS — it is here so a value that could only ever have been a
// mistake is refused at the door instead of being published and then wondered
// about.
func (t Theme) Invalid() string {
	for _, value := range []string{t.Accent, t.Radius, t.FontFamily} {
		if len([]rune(value)) > 120 {
			return "values must be at most 120 characters"
		}
		if strings.ContainsAny(value, ";{}<>") || strings.Contains(value, "/*") {
			return "values must not contain CSS punctuation"
		}
	}
	switch t.Mode {
	case "", "auto", "light", "dark":
	default:
		return "mode must be auto, light or dark"
	}
	return ""
}

// WallItem is explicit membership: the platform suggests, the customer decides.
type WallItem struct {
	TestimonialID string `json:"testimonial_id"`
	Position      int    `json:"position"`
	Pinned        bool   `json:"pinned"`
}

// SnapshotCard is one rendered testimonial inside a published payload. Only
// consented fields are present, because the builder projects through
// ConsentScopes before writing.
type SnapshotCard struct {
	ID       string        `json:"id"`
	Quote    string        `json:"quote"`
	Headline string        `json:"headline,omitempty"`
	Rating   *int          `json:"rating,omitempty"`
	Author   AuthorDetails `json:"author"`
	Tags     []string      `json:"tags,omitempty"`
}

// SnapshotPayload is exactly what the public endpoint serves and what the embed
// consumes. It is stored verbatim so a read is one row, not a six-table join.
type SnapshotPayload struct {
	SchemaVersion int            `json:"schema_version"`
	WallPublicID  string         `json:"wall"`
	Name          string         `json:"name"`
	Layout        WallLayout     `json:"layout"`
	Theme         Theme          `json:"theme"`
	ShowRating    bool           `json:"show_rating"`
	ShowAvatar    bool           `json:"show_avatar"`
	ShowCompany   bool           `json:"show_company"`
	ShowVideo     bool           `json:"show_video"`
	Cards         []SnapshotCard `json:"cards"`
	GeneratedAt   time.Time      `json:"generated_at"`
}

// SnapshotSchemaVersion lets the embed refuse a payload it does not understand
// rather than rendering a half-broken wall.
const SnapshotSchemaVersion = 1

// Snapshot is the stored, published payload plus its cache identity.
type Snapshot struct {
	WallID      string
	Version     int
	Payload     SnapshotPayload
	ETag        string
	IsCurrent   bool
	IsStale     bool
	ItemCount   int
	PublishedAt time.Time
}
