package abuse

import (
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestParseDiagnosticsQueryDefaultsAndBoundsFilters(t *testing.T) {
	t.Parallel()

	defaults, fields := parseDiagnosticsQuery(url.Values{})
	if len(fields) != 0 || defaults.Range != "7d" || defaults.RangeHours != 168 {
		t.Fatalf("default query = %#v, fields = %#v", defaults, fields)
	}
	request := httptest.NewRequest("GET",
		"/v1/abuse/diagnostics?range=24h&project_id=%7B00000000-0000-4000-8000-000000000001%7D",
		nil,
	)
	filtered, fields := parseDiagnosticsQuery(request.URL.Query())
	if len(fields) != 0 || filtered.RangeHours != 24 ||
		filtered.ProjectID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("filtered query = %#v, fields = %#v", filtered, fields)
	}
}

func TestParseDiagnosticsQueryRejectsUnknownRangeAndProject(t *testing.T) {
	t.Parallel()

	query, fields := parseDiagnosticsQuery(url.Values{
		"range": {"90d"}, "project_id": {"not-a-project"},
	})
	if query.RangeHours != 168 || fields["range"] == "" || fields["project_id"] == "" {
		t.Fatalf("query = %#v, fields = %#v", query, fields)
	}
}

func TestSummarizeDiagnosticsReturnsEveryOperationalCategory(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	workspace := summarizeDiagnostics(
		DiagnosticsQuery{Range: "24h", RangeHours: 24, ProjectID: "project"},
		start,
		end,
		[]DiagnosticPoint{
			{Category: CategoryRateLimit, Count: 3},
			{Category: CategoryProvider, Count: 2},
		},
	)
	if workspace.Total != 5 || len(workspace.Totals) != 6 || len(workspace.Trend) != 2 ||
		workspace.Filters.ProjectID != "project" {
		t.Fatalf("workspace = %#v", workspace)
	}
	if workspace.Totals[0].Category != CategoryRateLimit || workspace.Totals[0].Count != 3 ||
		workspace.Totals[5].Category != CategoryProvider || workspace.Totals[5].Count != 2 {
		t.Errorf("totals = %#v", workspace.Totals)
	}
}
