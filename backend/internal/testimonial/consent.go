package testimonial

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Status is the editorial lifecycle of a testimonial.
type Status string

const (
	StatusDraft         Status = "draft"
	StatusPendingReview Status = "pending_review"
	StatusApproved      Status = "approved"
	StatusArchived      Status = "archived"
)

// ConsentState is the author's permission, tracked separately from Status.
//
// The two are orthogonal on purpose. A customer approving a testimonial says
// nothing about whether its author still consents, and conflating the two is how
// something whose consent was withdrawn yesterday goes live today.
type ConsentState string

const (
	ConsentPending          ConsentState = "pending"
	ConsentGranted          ConsentState = "granted"
	ConsentPendingReconsent ConsentState = "pending_reconsent"
	ConsentRevoked          ConsentState = "revoked"
	ConsentExpired          ConsentState = "expired"
)

// ConsentScopes records what the author agreed to publish, field by field.
// One blanket checkbox is not enough: people will publish their words but not
// their face, and revoking a face should not retract the words.
type ConsentScopes struct {
	Quote    bool `json:"quote"`
	Identity bool `json:"identity"`
	Company  bool `json:"company"`
	Avatar   bool `json:"avatar"`
	Video    bool `json:"video"`
}

// AuthorDetails is everything a card could show about the person.
type AuthorDetails struct {
	Name       string `json:"name,omitempty"`
	Role       string `json:"role,omitempty"`
	Company    string `json:"company,omitempty"`
	CompanyURL string `json:"company_url,omitempty"`
	AvatarURL  string `json:"avatar_url,omitempty"`
	VideoURL   string `json:"video_url,omitempty"`
	PosterURL  string `json:"poster_url,omitempty"`
	// The rendition's own shape, so the embed can hold the right box before a
	// single byte of video has loaded. Without it a portrait recording either
	// arrives into a landscape frame and gets letterboxed, or the card jumps
	// when the real dimensions turn up.
	VideoWidth  int `json:"video_width,omitempty"`
	VideoHeight int `json:"video_height,omitempty"`
}

// Project drops every field the author did not consent to publish. The snapshot
// builder calls this, so an ungranted scope cannot reach a payload by being
// forgotten at a call site.
func (d AuthorDetails) Project(scopes ConsentScopes) AuthorDetails {
	projected := AuthorDetails{}
	if scopes.Identity {
		projected.Name = d.Name
		projected.Role = d.Role
	}
	if scopes.Company {
		projected.Company = d.Company
		projected.CompanyURL = d.CompanyURL
	}
	if scopes.Avatar {
		projected.AvatarURL = d.AvatarURL
	}
	if scopes.Video {
		projected.VideoURL = d.VideoURL
		projected.PosterURL = d.PosterURL
		projected.VideoWidth = d.VideoWidth
		projected.VideoHeight = d.VideoHeight
	}
	return projected
}

// QuoteMatchesConsent reports whether the displayed quote is still the text the
// author agreed to, allowing only differences a reader cannot perceive.
//
// An earlier design allowed any contiguous substring, on the reasoning that
// shortening a quote is safe. It is not. "I did not think this would work, but
// it does" contains "I did not think this would work", which is a contiguous
// substring and the opposite claim. Every contrastive construction has the same
// hole, in every language.
//
// Detecting contrastive conjunctions lexically was considered and rejected: this
// platform serves en, pt, pt-PT and pt-AO, so it would need but/however/though
// and mas/porém/contudo/no entanto, and a false negative publishes a
// defamatory-by-omission quote. The rule is therefore exact match, and edits go
// back to the author for a one-click re-approval.
func QuoteMatchesConsent(consented, quote string) bool {
	return normaliseQuote(consented) == normaliseQuote(quote)
}

// normaliseQuote removes only what a reader cannot see: Unicode composition
// differences, surrounding whitespace, and runs of inner whitespace.
func normaliseQuote(value string) string {
	return strings.Join(strings.Fields(norm.NFC.String(value)), " ")
}

// Publishability is every condition that must hold before a testimonial can
// appear in a published snapshot. It exists as one struct with one method so the
// predicate cannot be partially re-implemented at a second call site.
type Publishability struct {
	Status              Status
	ConsentState        ConsentState
	Scopes              ConsentScopes
	QuoteMatchesConsent bool
	// MediaReady is false while any referenced asset is still being verified or
	// transcoded, or has been quarantined or rejected.
	MediaReady bool
	HasVideo   bool
	// VideoReviewed records that a member holding testimonial:publish actually
	// watched it. Upload-time scanning cannot judge whether a bystander in the
	// frame consented to appear.
	VideoReviewed bool
}

// Publishable is the only thing the snapshot builder consults.
func (p Publishability) Publishable() bool {
	if p.Status != StatusApproved || p.ConsentState != ConsentGranted {
		return false
	}
	// A testimonial without a consented quote is not a testimonial. Allowing one
	// would produce a card carrying a name, a company and a video of someone
	// saying words they never agreed to publish.
	if !p.Scopes.Quote || !p.QuoteMatchesConsent {
		return false
	}
	if !p.MediaReady {
		return false
	}
	return !p.HasVideo || p.VideoReviewed
}
