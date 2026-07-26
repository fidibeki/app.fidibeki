// Package membernotify holds per-member, per-workspace preferences for optional
// product notifications and the scheduled digest delivery that honors them.
//
// Only optional operational events are addressable here. Authentication,
// security, account-deletion, and critical billing messages keep their own
// delivery paths (internal/auth, internal/email lifecycle, internal/billing) and
// are never consulted against a preference row, so no combination of settings can
// silence them. Reporter consent and reporter unsubscribe are a separate,
// external-facing contract that lives on the feedback and issue rows.
package membernotify

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"
)

// Optional notification categories. Each is a class of internal operational
// event a member can route independently.
const (
	CategoryAssignments     = "assignments"
	CategoryMentions        = "mentions"
	CategoryComments        = "comments"
	CategoryIssueChanges    = "issue_changes"
	CategoryFeedbackSummary = "feedback_summary"
)

// Delivery modes. A category offers only the modes that are meaningful for it,
// see SupportedModes.
const (
	ModeImmediate = "immediate"
	ModeDaily     = "daily"
	ModeWeekly    = "weekly"
	ModeOff       = "off"
)

// Digest cadences. ModeImmediate maps to CadenceImmediate, which is delivered by
// the same windowed pipeline with a short window rather than a separate path.
const (
	CadenceImmediate = "immediate"
	CadenceDaily     = "daily"
	CadenceWeekly    = "weekly"
)

// Categories lists every optional category in stable display order.
var Categories = []string{
	CategoryAssignments,
	CategoryMentions,
	CategoryComments,
	CategoryIssueChanges,
	CategoryFeedbackSummary,
}

// MandatoryClasses names the message classes that exist outside these
// preferences. They are listed so the contract is explicit and testable: none of
// them is a category, so none of them can be turned off here.
var MandatoryClasses = []string{
	"authentication", "security", "account_deletion", "billing",
}

// supportedModes constrains each category to the modes that make sense for it.
// New feedback is summarized only: without ModeImmediate, "one email per feedback
// submission" is unreachable by configuration, not merely off by default. The
// database CHECK constraints in migration 000113 mirror this map.
var supportedModes = map[string][]string{
	CategoryAssignments:     {ModeImmediate, ModeDaily, ModeWeekly, ModeOff},
	CategoryMentions:        {ModeImmediate, ModeDaily, ModeWeekly, ModeOff},
	CategoryComments:        {ModeImmediate, ModeDaily, ModeWeekly, ModeOff},
	CategoryIssueChanges:    {ModeImmediate, ModeDaily, ModeWeekly, ModeOff},
	CategoryFeedbackSummary: {ModeDaily, ModeWeekly, ModeOff},
}

// Preferences is one member's optional-notification routing inside one
// workspace. Timezone is resolved for display and digest scheduling from the
// member profile, falling back to the workspace default and then UTC; it is
// persisted on tenant_members, not here.
type Preferences struct {
	Assignments     string     `json:"assignments"`
	Mentions        string     `json:"mentions"`
	Comments        string     `json:"comments"`
	IssueChanges    string     `json:"issue_changes"`
	FeedbackSummary string     `json:"feedback_summary"`
	DigestHour      int        `json:"digest_hour"`
	DigestWeekday   int        `json:"digest_weekday"`
	Timezone        string     `json:"timezone"`
	TimezoneSource  string     `json:"timezone_source"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

// Patch carries a partial update. A nil field is left untouched, which keeps a
// narrow client from resetting settings it does not render.
type Patch struct {
	Assignments     *string `json:"assignments"`
	Mentions        *string `json:"mentions"`
	Comments        *string `json:"comments"`
	IssueChanges    *string `json:"issue_changes"`
	FeedbackSummary *string `json:"feedback_summary"`
	DigestHour      *int    `json:"digest_hour"`
	DigestWeekday   *int    `json:"digest_weekday"`
	// Timezone updates the member profile timezone. An explicit JSON null clears
	// it back to the workspace default.
	Timezone       *string `json:"timezone"`
	timezonePassed bool
}

// Defaults returns the intentionally low-noise starting point applied to members
// who never opened the settings: directly addressed work arrives immediately,
// ambient activity is digested or silent, and nothing is per-submission.
func Defaults() Preferences {
	return Preferences{
		Assignments: ModeImmediate, Mentions: ModeImmediate, Comments: ModeOff,
		IssueChanges: ModeDaily, FeedbackSummary: ModeWeekly,
		DigestHour: 8, DigestWeekday: 1,
	}
}

// SupportedModes returns the modes a category accepts, in display order.
func SupportedModes(category string) []string {
	modes, ok := supportedModes[category]
	if !ok {
		return nil
	}
	return append([]string(nil), modes...)
}

// IsOptionalCategory reports whether a name is an addressable optional category.
func IsOptionalCategory(category string) bool {
	_, ok := supportedModes[category]
	return ok
}

// Mode returns the configured mode for one category.
func (p Preferences) Mode(category string) string {
	switch category {
	case CategoryAssignments:
		return p.Assignments
	case CategoryMentions:
		return p.Mentions
	case CategoryComments:
		return p.Comments
	case CategoryIssueChanges:
		return p.IssueChanges
	case CategoryFeedbackSummary:
		return p.FeedbackSummary
	default:
		return ModeOff
	}
}

// CategoriesFor returns the categories routed to one cadence, in display order.
func (p Preferences) CategoriesFor(cadence string) []string {
	mode := map[string]string{
		CadenceImmediate: ModeImmediate, CadenceDaily: ModeDaily, CadenceWeekly: ModeWeekly,
	}[cadence]
	if mode == "" {
		return nil
	}
	selected := make([]string, 0, len(Categories))
	for _, category := range Categories {
		if p.Mode(category) == mode {
			selected = append(selected, category)
		}
	}
	return selected
}

// UsesCadence reports whether any category routes to the given cadence.
func (p Preferences) UsesCadence(cadence string) bool {
	return len(p.CategoriesFor(cadence)) > 0
}

// patchFields is the complete set of settable request fields. Anything else,
// including a mandatory message class such as "billing", is rejected outright
// rather than silently ignored.
var patchFields = map[string]struct{}{
	CategoryAssignments: {}, CategoryMentions: {}, CategoryComments: {},
	CategoryIssueChanges: {}, CategoryFeedbackSummary: {},
	"digest_hour": {}, "digest_weekday": {}, "timezone": {},
}

// UnmarshalJSON rejects unknown fields and records whether "timezone" was
// present, so an explicit null clears the member timezone while an absent key
// leaves it untouched. Strictness lives here because encoding/json's
// DisallowUnknownFields does not reach inside a custom unmarshaler.
func (p *Patch) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for field := range raw {
		if _, ok := patchFields[field]; !ok {
			return fmt.Errorf("unknown notification preference field %q", field)
		}
	}
	type patchAlias Patch
	var alias patchAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*p = Patch(alias)
	_, p.timezonePassed = raw["timezone"]
	return nil
}

// Apply validates a patch against current preferences and returns the result
// together with field-level errors keyed by request field name.
func Apply(current Preferences, patch Patch) (Preferences, map[string]string) {
	next := current
	fields := make(map[string]string)
	modes := map[string]struct {
		value  *string
		target *string
	}{
		CategoryAssignments:     {patch.Assignments, &next.Assignments},
		CategoryMentions:        {patch.Mentions, &next.Mentions},
		CategoryComments:        {patch.Comments, &next.Comments},
		CategoryIssueChanges:    {patch.IssueChanges, &next.IssueChanges},
		CategoryFeedbackSummary: {patch.FeedbackSummary, &next.FeedbackSummary},
	}
	for _, category := range Categories {
		update := modes[category]
		if update.value == nil {
			continue
		}
		value := strings.TrimSpace(*update.value)
		if !modeSupported(category, value) {
			fields[category] = "must be one of " + strings.Join(SupportedModes(category), ", ")
			continue
		}
		*update.target = value
	}
	if patch.DigestHour != nil {
		if *patch.DigestHour < 0 || *patch.DigestHour > 23 {
			fields["digest_hour"] = "must be between 0 and 23"
		} else {
			next.DigestHour = *patch.DigestHour
		}
	}
	if patch.DigestWeekday != nil {
		if *patch.DigestWeekday < 1 || *patch.DigestWeekday > 7 {
			fields["digest_weekday"] = "must be between 1 (Monday) and 7 (Sunday)"
		} else {
			next.DigestWeekday = *patch.DigestWeekday
		}
	}
	if len(fields) == 0 {
		return next, nil
	}
	return current, fields
}

// TimezonePatch resolves the requested member timezone. It returns the value to
// persist (nil clears the override), whether the profile should be written at
// all, and a field error when the value is not a usable IANA zone.
func TimezonePatch(patch Patch) (*string, bool, string) {
	if !patch.timezonePassed {
		return nil, false, ""
	}
	if patch.Timezone == nil {
		return nil, true, ""
	}
	value := strings.TrimSpace(*patch.Timezone)
	if value == "" {
		return nil, true, ""
	}
	if len(value) > 100 || value == "Local" {
		return nil, false, "must be a valid IANA timezone"
	}
	if _, err := time.LoadLocation(value); err != nil {
		return nil, false, "must be a valid IANA timezone"
	}
	return &value, true, ""
}

// Location loads the member's resolved timezone, falling back to UTC when the
// stored zone is missing from the runtime database.
func (p Preferences) Location() *time.Location {
	if p.Timezone == "" {
		return time.UTC
	}
	location, err := time.LoadLocation(p.Timezone)
	if err != nil {
		return time.UTC
	}
	return location
}

func modeSupported(category, mode string) bool {
	for _, supported := range SupportedModes(category) {
		if supported == mode {
			return true
		}
	}
	return false
}

// AuditData summarizes a preference change for the append-only audit ledger. It
// records what changed, never who else is in the workspace or any message body.
func AuditData(before, after Preferences) map[string]any {
	changes := make(map[string]any)
	for _, category := range Categories {
		if before.Mode(category) != after.Mode(category) {
			changes[category] = map[string]any{
				"from": before.Mode(category), "to": after.Mode(category),
			}
		}
	}
	if before.DigestHour != after.DigestHour {
		changes["digest_hour"] = map[string]any{
			"from": before.DigestHour, "to": after.DigestHour,
		}
	}
	if before.DigestWeekday != after.DigestWeekday {
		changes["digest_weekday"] = map[string]any{
			"from": before.DigestWeekday, "to": after.DigestWeekday,
		}
	}
	if before.Timezone != after.Timezone {
		changes["timezone"] = map[string]any{"from": before.Timezone, "to": after.Timezone}
	}
	return changes
}

func describeCadence(cadence string) string {
	switch cadence {
	case CadenceImmediate:
		return "activity"
	case CadenceDaily:
		return "daily digest"
	case CadenceWeekly:
		return "weekly digest"
	default:
		return fmt.Sprintf("%s digest", cadence)
	}
}
