package widgetanalytics

import (
	"net/http/httptest"
	"testing"
)

func TestParseAnalyticsQueryDefaultsAndCanonicalizesPage(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest("GET", "/v1/analytics/widget?page=https%3A%2F%2Fexample.com%2Fchart%2F%3Fasset%3DBAI%23price", nil)

	query, fields := parseAnalyticsQuery(request.URL.Query())

	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if query.Range != "30d" || query.RangeDays != 30 || query.PageURL != "https://example.com/chart" || len(query.PageKey) != 32 {
		t.Fatalf("query = %#v", query)
	}
}

func TestParseAnalyticsQueryAcceptsSupportedFilters(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest("GET", "/v1/analytics/widget?range=90d&project_id=00000000-0000-4000-8000-000000000001&rule_id=00000000-0000-4000-8000-000000000002&page=https%3A%2F%2Fexample.com%2Fpricing", nil)

	query, fields := parseAnalyticsQuery(request.URL.Query())

	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if query.RangeDays != 90 || query.ProjectID == "" || query.RuleID == "" || query.PageURL != "https://example.com/pricing" {
		t.Fatalf("query = %#v", query)
	}
}

func TestParseAnalyticsQueryNormalizesAcceptedUUIDForms(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest("GET", "/v1/analytics/widget?project_id=urn%3Auuid%3A00000000-0000-4000-8000-000000000001&rule_id=%7B00000000-0000-4000-8000-000000000002%7D", nil)

	query, fields := parseAnalyticsQuery(request.URL.Query())

	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if query.ProjectID != "00000000-0000-4000-8000-000000000001" ||
		query.RuleID != "00000000-0000-4000-8000-000000000002" {
		t.Fatalf("normalized query = %#v", query)
	}
}

func TestAnalyticsTenantConcurrencyIsBoundedAndReleased(t *testing.T) {
	t.Parallel()
	limiter := newAnalyticsTenantConcurrency()
	if !limiter.acquire("tenant-a") || !limiter.acquire("tenant-a") {
		t.Fatal("first two tenant analytics slots were not acquired")
	}
	if limiter.acquire("tenant-a") {
		t.Fatal("third tenant analytics slot was acquired")
	}
	if !limiter.acquire("tenant-b") {
		t.Fatal("independent tenant analytics slot was not acquired")
	}
	limiter.release("tenant-a")
	if !limiter.acquire("tenant-a") {
		t.Fatal("released tenant analytics slot was not reacquired")
	}
}

func TestParseAnalyticsQueryRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		query string
		field string
	}{
		{name: "range", query: "range=14d", field: "range"},
		{name: "project", query: "project_id=not-a-uuid", field: "project_id"},
		{name: "rule", query: "rule_id=not-a-uuid", field: "rule_id"},
		{name: "relative page", query: "page=%2Fchart", field: "page"},
		{name: "non-http page", query: "page=javascript%3Aalert%281%29", field: "page"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest("GET", "/v1/analytics/widget?"+test.query, nil)
			_, fields := parseAnalyticsQuery(request.URL.Query())
			if fields[test.field] == "" {
				t.Fatalf("fields = %#v", fields)
			}
		})
	}
}
