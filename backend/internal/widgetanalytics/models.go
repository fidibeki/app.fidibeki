package widgetanalytics

import "time"

type EventType string

const (
	EventMatched     EventType = "matched"
	EventImpression  EventType = "impression"
	EventInteraction EventType = "interaction"
	EventSubmitted   EventType = "submitted"
	EventDismissed   EventType = "dismissed"
)

func (eventType EventType) ClientWritable() bool {
	return eventType == EventMatched || eventType == EventImpression ||
		eventType == EventInteraction || eventType == EventDismissed
}

type EventInput struct {
	EventID    string    `json:"event_id"`
	EventType  EventType `json:"event_type"`
	VisitorID  string    `json:"visitor_id"`
	PageViewID string    `json:"page_view_id"`
	RuleID     string    `json:"rule_id"`
	PageURL    string    `json:"page_url"`
	OccurredAt time.Time `json:"occurred_at"`
}

type EventValues struct {
	EventID    string
	TenantID   string
	ProjectID  string
	RuleID     string
	RuleName   string
	EventType  EventType
	VisitorID  string
	PageViewID string
	PageURL    string
	PageKey    []byte
	FeedbackID *string
	OccurredAt time.Time
	ReceivedAt time.Time
}

type FieldErrors map[string]string

type IngestResult struct {
	AcceptedEventIDs  []string `json:"accepted_event_ids"`
	DuplicateEventIDs []string `json:"duplicate_event_ids"`
}

type Summary struct {
	Matched         int64    `json:"matched"`
	Impressions     int64    `json:"impressions"`
	Interactions    int64    `json:"interactions"`
	Submissions     int64    `json:"submissions"`
	Dismissals      int64    `json:"dismissals"`
	ImpressionRate  *float64 `json:"impression_rate"`
	InteractionRate *float64 `json:"interaction_rate"`
	CompletionRate  *float64 `json:"completion_rate"`
	SubmitRate      *float64 `json:"submit_rate"`
}

func (summary *Summary) CalculateRates() {
	summary.ImpressionRate = ratio(summary.Impressions, summary.Matched)
	summary.InteractionRate = ratio(summary.Interactions, summary.Impressions)
	summary.CompletionRate = ratio(summary.Submissions, summary.Interactions)
	summary.SubmitRate = ratio(summary.Submissions, summary.Impressions)
}

func ratio(numerator, denominator int64) *float64 {
	if denominator == 0 {
		return nil
	}
	value := float64(numerator) / float64(denominator)
	return &value
}
