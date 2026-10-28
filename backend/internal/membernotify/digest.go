package membernotify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/email"
)

// Cadences is the processing order of one scheduler pass.
var Cadences = []string{CadenceImmediate, CadenceDaily, CadenceWeekly}

// Suppression returns why an address must not receive optional product mail, or
// an empty string when it may. Optional notifications stop on both bounces and
// complaints: a complaint is an explicit "stop sending me this class of mail",
// and this class is exactly the optional one. Mandatory account, security, and
// billing messages are composed elsewhere and are not gated on this function.
func Suppression(bounced, complained bool) string {
	switch {
	case complained:
		return "recipient reported optional notifications as spam"
	case bounced:
		return "recipient address previously bounced"
	default:
		return ""
	}
}

// DigestSource is the persistence the scheduler drives. DigestStore is the
// production implementation; the interface exists so the delivery decisions
// (claim, suppress, skip, retry) can be exercised without a database.
type DigestSource interface {
	Candidates(ctx context.Context, cadence string, limit int) ([]Candidate, error)
	Collect(ctx context.Context, candidate Candidate, cadence string, window Window) ([]Section, error)
	Claim(
		ctx context.Context, candidate Candidate, cadence string, window Window, now time.Time,
	) (string, bool, error)
	MarkSent(ctx context.Context, deliveryID string, eventCount int, now time.Time) error
	MarkSuppressed(ctx context.Context, deliveryID string, eventCount int, reason string) error
	MarkFailed(ctx context.Context, deliveryID string, message string) error
}

// Scheduler turns preferences into delivered digests. It owns no transport: it
// hands composed messages to the existing retryable email queue, which keeps a
// provider outage from consuming digest windows.
type Scheduler struct {
	Store         DigestSource
	Email         email.Enqueuer
	Tokens        *auth.JWTManager
	PublicBaseURL string
	BaseDomain    string
	AuthFrom      string
	MaxMembers    int
	Logger        *slog.Logger
	Now           func() time.Time
}

// Run processes every cadence on an interval. The interval must be at most a
// minute for immediate delivery to feel immediate; daily and weekly windows are
// no-ops between their boundaries.
func (s Scheduler) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Minute
	}
	s.executeLogged(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.executeLogged(ctx)
		}
	}
}

func (s Scheduler) executeLogged(ctx context.Context) {
	if err := s.Execute(ctx); err != nil && s.Logger != nil {
		s.Logger.ErrorContext(ctx, "member notification digest failed", "error", err)
	}
}

// Execute runs one pass over every cadence. One member's failure is recorded and
// skipped rather than abandoning the pass.
func (s Scheduler) Execute(ctx context.Context) error {
	if s.Email == nil || s.Store == nil {
		return errors.New("digest scheduler email queue and store are required")
	}
	var joined error
	for _, cadence := range Cadences {
		if err := s.process(ctx, cadence); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func (s Scheduler) process(ctx context.Context, cadence string) error {
	candidates, err := s.Store.Candidates(ctx, cadence, s.MaxMembers)
	if err != nil {
		return err
	}
	if s.MaxMembers > 0 && len(candidates) == s.MaxMembers && s.Logger != nil {
		// Say so rather than letting a truncated pass look like a complete one.
		// The next pass picks up the remainder because delivered windows advance
		// their own cursors.
		s.Logger.WarnContext(
			ctx, "member notification digest pass hit its member limit",
			"cadence", cadence, "limit", s.MaxMembers,
		)
	}
	var joined error
	for _, candidate := range candidates {
		if err := s.processCandidate(ctx, candidate, cadence); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func (s Scheduler) processCandidate(
	ctx context.Context,
	candidate Candidate,
	cadence string,
) error {
	window, due := NextWindow(candidate.Preferences, cadence, candidate.Cursor, s.now())
	if !due {
		return nil
	}
	sections, err := s.Store.Collect(ctx, candidate, cadence, window)
	if err != nil {
		return err
	}
	total := 0
	for _, section := range sections {
		total += section.Total
	}
	// An empty window is never claimed, so it leaves no ledger row, sends no
	// mail, and does not advance the cursor past events that have yet to happen.
	if total == 0 {
		return nil
	}
	deliveryID, claimed, err := s.Store.Claim(ctx, candidate, cadence, window, s.now())
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	if reason := Suppression(candidate.Bounced, candidate.Complained); reason != "" {
		return s.Store.MarkSuppressed(ctx, deliveryID, total, reason)
	}
	message, err := s.message(candidate, cadence, window, sections, total, deliveryID)
	if err != nil {
		markErr := s.Store.MarkFailed(ctx, deliveryID, err.Error())
		return errors.Join(fmt.Errorf("compose member digest: %w", err), markErr)
	}
	if err := s.Email.Enqueue(ctx, message); err != nil {
		markErr := s.Store.MarkFailed(ctx, deliveryID, err.Error())
		return errors.Join(fmt.Errorf("enqueue member digest: %w", err), markErr)
	}
	return s.Store.MarkSent(ctx, deliveryID, total, s.now())
}

func (s Scheduler) message(
	candidate Candidate,
	cadence string,
	window Window,
	sections []Section,
	total int,
	deliveryID string,
) (email.Message, error) {
	logoURL, err := email.BrandLogoURL(s.PublicBaseURL)
	if err != nil {
		return email.Message{}, err
	}
	settingsURL, err := WorkspaceURL(
		s.PublicBaseURL, s.BaseDomain, candidate.TenantSlug, "/settings/notifications",
	)
	if err != nil {
		return email.Message{}, err
	}
	issuesURL, err := WorkspaceURL(s.PublicBaseURL, s.BaseDomain, candidate.TenantSlug, "/issues")
	if err != nil {
		return email.Message{}, err
	}
	unsubscribeURL, err := s.unsubscribeURL(candidate)
	if err != nil {
		return email.Message{}, err
	}
	heading := digestHeading(cadence, total)
	body := []string{digestIntroduction(cadence, candidate.TenantName, window, candidate.Preferences)}
	metadata := make([]email.Metadata, 0, len(sections)*(maxSectionEvents+1))
	for _, section := range sections {
		metadata = append(metadata, email.Metadata{
			Label: sectionLabel(section.Category), Value: sectionSummary(section),
		})
		for _, event := range section.Events {
			metadata = append(metadata, email.Metadata{
				Label: event.Reference, Value: eventSummary(event),
			})
		}
	}
	// The idempotency key is the claimed delivery row, so a duplicated hand-off
	// collapses at the queue instead of producing a second email.
	message := email.Message{
		From: s.AuthFrom, To: candidate.Email, Subject: digestSubject(cadence, candidate, total),
		IdempotencyKey: "member-digest:" + deliveryID,
	}
	return email.Compose(message, email.Content{
		Preheader: heading, Heading: heading, Workspace: candidate.TenantName,
		Body: body, Metadata: metadata, LogoURL: logoURL,
		Action: &email.Link{Label: "Open workspace", URL: issuesURL},
		Footer: "You received this optional notification because of your notification " +
			"preferences in this workspace. Account, security, and billing messages are " +
			"sent separately and are not affected by these settings.",
		FooterLinks: []email.Link{
			{Label: "Notification settings", URL: settingsURL},
			{Label: "Turn these off", URL: unsubscribeURL},
		},
	})
}

func (s Scheduler) unsubscribeURL(candidate Candidate) (string, error) {
	if s.Tokens == nil {
		return "", errors.New("digest scheduler unsubscribe tokens are required")
	}
	token, _, err := s.Tokens.IssueMemberNotificationUnsubscribe(
		candidate.MemberID, candidate.TenantID,
	)
	if err != nil {
		return "", fmt.Errorf("member unsubscribe token: %w", err)
	}
	return strings.TrimRight(s.PublicBaseURL, "/") +
		UnsubscribePath + "?token=" + url.QueryEscape(token), nil
}

func (s Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func digestSubject(cadence string, candidate Candidate, total int) string {
	switch cadence {
	case CadenceDaily:
		return candidate.TenantName + ": daily digest"
	case CadenceWeekly:
		return candidate.TenantName + ": weekly digest"
	default:
		if total == 1 {
			return candidate.TenantName + ": 1 update for you"
		}
		return candidate.TenantName + ": " + strconv.Itoa(total) + " updates for you"
	}
}

func digestHeading(cadence string, total int) string {
	label := describeCadence(cadence)
	if total == 1 {
		return "1 update in your " + label
	}
	return strconv.Itoa(total) + " updates in your " + label
}

func digestIntroduction(
	cadence string,
	workspace string,
	window Window,
	preferences Preferences,
) string {
	location := preferences.Location()
	layout := "2 Jan 15:04"
	if cadence == CadenceWeekly {
		layout = "2 Jan"
	}
	return fmt.Sprintf(
		"Activity in %s between %s and %s (%s).",
		workspace,
		window.Start.In(location).Format(layout),
		window.End.In(location).Format(layout),
		preferences.Timezone,
	)
}

func sectionLabel(category string) string {
	return map[string]string{
		CategoryAssignments:     "Assigned to you",
		CategoryMentions:        "Mentions",
		CategoryComments:        "Comments",
		CategoryIssueChanges:    "Issue changes",
		CategoryFeedbackSummary: "New feedback",
	}[category]
}

func sectionSummary(section Section) string {
	if section.Total == 1 {
		return "1 update"
	}
	summary := strconv.Itoa(section.Total) + " updates"
	if section.Remainder > 0 {
		summary += ", showing the first " + strconv.Itoa(len(section.Events))
	}
	return summary
}

func eventSummary(event Event) string {
	parts := make([]string, 0, 3)
	if title := strings.TrimSpace(event.Title); title != "" {
		parts = append(parts, title)
	}
	if detail := strings.TrimSpace(event.Detail); detail != "" {
		parts = append(parts, detail)
	}
	if actor := strings.TrimSpace(event.Actor); actor != "" {
		parts = append(parts, "by "+actor)
	}
	return strings.Join(parts, " — ")
}

// WorkspaceURL builds an absolute workspace URL for one tenant slug, carrying the
// current environment's scheme and port so local, staging, and production links
// all resolve.
func WorkspaceURL(publicBaseURL, baseDomain, slug, path string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(publicBaseURL))
	if err != nil || !base.IsAbs() || base.Host == "" {
		return "", errors.New("public base URL is invalid")
	}
	if strings.TrimSpace(baseDomain) == "" || strings.TrimSpace(slug) == "" {
		return "", errors.New("workspace host is incomplete")
	}
	host := slug + "." + baseDomain
	if port := base.Port(); port != "" {
		host += ":" + port
	}
	base.Host = host
	base.Path = path
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}
