package widgetanalytics

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestStoreRejectsMixedProjectBatchBeforeDatabaseAccess(t *testing.T) {
	t.Parallel()
	events := []EventValues{
		{TenantID: "tenant-one", ProjectID: "project-one"},
		{TenantID: "tenant-one", ProjectID: "project-two"},
	}
	_, err := (Store{}).InsertEvents(context.Background(), nil, events)
	if err == nil || !strings.Contains(err.Error(), "one tenant and project") {
		t.Fatalf("InsertEvents() error = %v", err)
	}
}

func TestWorkspaceCalculatesRatesFromCounts(t *testing.T) {
	t.Parallel()
	workspace := Workspace{
		Range: "7d",
		Period: Period{
			StartDate: time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC).Format(time.DateOnly),
			EndDate:   time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC).Format(time.DateOnly),
		},
		Summary:  Summary{Matched: 20, Impressions: 10, Interactions: 5, Submissions: 2},
		Projects: []Breakdown{{Summary: Summary{Matched: 10, Impressions: 0, Interactions: 0, Submissions: 0}}},
		Rules:    []Breakdown{{Summary: Summary{Matched: 8, Impressions: 4, Interactions: 2, Submissions: 1}}},
		Pages:    []Breakdown{{Summary: Summary{Matched: 4, Impressions: 2, Interactions: 1, Submissions: 1}}},
	}

	workspace.CalculateRates()

	if workspace.Summary.InteractionRate == nil || *workspace.Summary.InteractionRate != 0.5 ||
		workspace.Summary.CompletionRate == nil || *workspace.Summary.CompletionRate != 0.4 {
		t.Fatalf("summary = %#v", workspace.Summary)
	}
	if workspace.Projects[0].InteractionRate != nil || workspace.Projects[0].SubmitRate != nil {
		t.Fatalf("zero-denominator project rates = %#v", workspace.Projects[0])
	}
	if workspace.Rules[0].SubmitRate == nil || *workspace.Rules[0].SubmitRate != 0.25 ||
		workspace.Pages[0].CompletionRate == nil || *workspace.Pages[0].CompletionRate != 1 {
		t.Fatalf("breakdown rates = rules %#v pages %#v", workspace.Rules[0], workspace.Pages[0])
	}
}
