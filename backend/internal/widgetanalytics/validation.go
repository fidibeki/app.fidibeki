package widgetanalytics

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/google/uuid"
)

const maximumBatchEvents = 20

func ValidateEvents(
	inputs []EventInput,
	configuration project.PublicConfiguration,
	origin string,
	now time.Time,
) ([]EventValues, FieldErrors) {
	fields := FieldErrors{}
	if len(inputs) < 1 || len(inputs) > maximumBatchEvents {
		fields["events"] = "must contain between 1 and 20 events"
		return nil, fields
	}
	now = now.UTC()
	rules := make(map[string]project.WidgetRule, len(configuration.Rules))
	for _, rule := range configuration.Rules {
		rules[rule.ID] = rule
	}
	originPage, originErr := project.CanonicalizePage(origin)
	seenEventIDs := make(map[string]struct{}, len(inputs))
	values := make([]EventValues, len(inputs))
	for index, input := range inputs {
		prefix := fmt.Sprintf("events[%d].", index)
		if _, err := uuid.Parse(input.EventID); err != nil {
			fields[prefix+"event_id"] = "must be a UUID"
		} else if _, exists := seenEventIDs[input.EventID]; exists {
			fields[prefix+"event_id"] = "must be unique within the batch"
		} else {
			seenEventIDs[input.EventID] = struct{}{}
		}
		if !input.EventType.ClientWritable() {
			fields[prefix+"event_type"] = "must be matched, impression, interaction, or dismissed"
		}
		if _, err := uuid.Parse(input.VisitorID); err != nil {
			fields[prefix+"visitor_id"] = "must be a UUID"
		}
		if _, err := uuid.Parse(input.PageViewID); err != nil {
			fields[prefix+"page_view_id"] = "must be a UUID"
		}
		rule, ruleExists := rules[input.RuleID]
		if _, err := uuid.Parse(input.RuleID); err != nil || !ruleExists {
			fields[prefix+"rule_id"] = "must identify an enabled project rule"
		}
		page, err := project.ProjectPrivacySafeLocation(strings.TrimSpace(input.PageURL), rule)
		if err != nil || len(page.Target) > 4096 || originErr != nil || !targetsShareOrigin(page.Target, originPage.Target) {
			fields[prefix+"page_url"] = "must be an allowed canonical page on the request origin"
		}
		occurredAt := input.OccurredAt.UTC()
		if input.OccurredAt.IsZero() || occurredAt.Before(now.Add(-24*time.Hour)) ||
			occurredAt.After(now.Add(5*time.Minute)) {
			fields[prefix+"occurred_at"] = "must be within the last 24 hours and no more than 5 minutes in the future"
		}
		pageKey := sha256.Sum256([]byte(page.Target))
		values[index] = EventValues{
			EventID: input.EventID, TenantID: configuration.TenantID, ProjectID: configuration.ProjectID,
			RuleID: input.RuleID, RuleName: rule.Name, EventType: input.EventType,
			VisitorID: input.VisitorID, PageViewID: input.PageViewID,
			PageURL: page.Target, PageKey: pageKey[:], OccurredAt: occurredAt, ReceivedAt: now,
		}
	}
	if len(fields) > 0 {
		return nil, fields
	}
	return values, nil
}

func ValidateSubmissionEvent(
	input EventInput,
	configuration project.PublicConfiguration,
	expectedRuleID string,
	expectedPageURL string,
	now time.Time,
) (EventValues, FieldErrors) {
	fields := FieldErrors{}
	expectedPage, err := project.CanonicalizePage(strings.TrimSpace(expectedPageURL))
	if err != nil {
		fields["page_url"] = "must match the submitted metadata page"
		return EventValues{}, fields
	}
	parsedExpected, err := url.Parse(expectedPage.Target)
	if err != nil {
		fields["page_url"] = "must match the submitted metadata page"
		return EventValues{}, fields
	}
	candidate := input
	candidate.EventType = EventMatched
	values, indexed := ValidateEvents(
		[]EventInput{candidate}, configuration,
		parsedExpected.Scheme+"://"+parsedExpected.Host, now,
	)
	for name, message := range indexed {
		fields[strings.TrimPrefix(name, "events[0].")] = message
	}
	if input.RuleID != expectedRuleID {
		fields["rule_id"] = "must match widget_rule_id"
	}
	if len(values) != 1 || values[0].PageURL != expectedPage.Target {
		fields["page_url"] = "must match the submitted metadata page"
	}
	if len(fields) > 0 {
		return EventValues{}, fields
	}
	value := values[0]
	value.EventType = EventSubmitted
	return value, nil
}

func targetsShareOrigin(left, right string) bool {
	leftURL, leftErr := url.Parse(left)
	rightURL, rightErr := url.Parse(right)
	return leftErr == nil && rightErr == nil &&
		leftURL.Scheme == rightURL.Scheme && leftURL.Host == rightURL.Host
}
