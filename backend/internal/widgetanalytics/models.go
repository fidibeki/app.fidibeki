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

type AnalyticsQuery struct {
	Range     string
	RangeDays int
	ProjectID string
	RuleID    string
	PageURL   string
	PageKey   []byte
	Now       time.Time
}

type Period struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Timezone  string `json:"timezone"`
}

type AppliedFilters struct {
	ProjectID string `json:"project_id,omitempty"`
	RuleID    string `json:"rule_id,omitempty"`
	PageURL   string `json:"page,omitempty"`
}

type TrendPoint struct {
	Date         string `json:"date"`
	Impressions  int64  `json:"impressions"`
	Interactions int64  `json:"interactions"`
	Submissions  int64  `json:"submissions"`
}

type Breakdown struct {
	ProjectID   string `json:"project_id,omitempty"`
	ProjectName string `json:"project_name,omitempty"`
	RuleID      string `json:"rule_id,omitempty"`
	RuleName    string `json:"rule_name,omitempty"`
	PageURL     string `json:"page_url,omitempty"`
	Deleted     bool   `json:"deleted,omitempty"`
	Summary
}

type ProjectOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type RuleOption struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Deleted   bool   `json:"deleted"`
}

type PageOption struct {
	URL string `json:"url"`
}

type AnalyticsOptions struct {
	Projects []ProjectOption `json:"projects"`
	Rules    []RuleOption    `json:"rules"`
	Pages    []PageOption    `json:"pages"`
}

type ResultMetadata struct {
	Total     int64 `json:"total"`
	Returned  int   `json:"returned"`
	Limit     int   `json:"limit"`
	Truncated bool  `json:"truncated"`
}

type AnalyticsMetadata struct {
	Projects       ResultMetadata `json:"projects"`
	Rules          ResultMetadata `json:"rules"`
	Pages          ResultMetadata `json:"pages"`
	ProjectOptions ResultMetadata `json:"project_options"`
	RuleOptions    ResultMetadata `json:"rule_options"`
	PageOptions    ResultMetadata `json:"page_options"`
}

type Workspace struct {
	Range    string            `json:"range"`
	Period   Period            `json:"period"`
	Filters  AppliedFilters    `json:"filters"`
	Summary  Summary           `json:"summary"`
	Trend    []TrendPoint      `json:"trend"`
	Projects []Breakdown       `json:"projects"`
	Rules    []Breakdown       `json:"rules"`
	Pages    []Breakdown       `json:"pages"`
	Options  AnalyticsOptions  `json:"options"`
	Metadata AnalyticsMetadata `json:"metadata"`
}

func (workspace *Workspace) CalculateRates() {
	workspace.Summary.CalculateRates()
	for index := range workspace.Projects {
		workspace.Projects[index].Summary.CalculateRates()
	}
	for index := range workspace.Rules {
		workspace.Rules[index].Summary.CalculateRates()
	}
	for index := range workspace.Pages {
		workspace.Pages[index].Summary.CalculateRates()
	}
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
