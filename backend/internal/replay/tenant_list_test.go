package replay

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseTenantSessionListOptions(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		request := httptest.NewRequest("GET", "/v1/sessions", nil)
		options, fields := parseTenantSessionListOptions(request)
		if len(fields) != 0 {
			t.Fatalf("fields = %#v", fields)
		}
		if options.Limit != 50 || options.ProjectID != "" || options.Status != "" ||
			options.Feedback != "" || options.Cursor != nil {
			t.Fatalf("options = %#v", options)
		}
	})

	t.Run("accepts valid filters", func(t *testing.T) {
		request := httptest.NewRequest("GET",
			"/v1/sessions?project_id=7d4a51dd-3f3a-4a4e-9c5d-14f1de8a3b9b&status=ready&feedback=unlinked&limit=5",
			nil)
		options, fields := parseTenantSessionListOptions(request)
		if len(fields) != 0 {
			t.Fatalf("fields = %#v", fields)
		}
		if options.ProjectID != "7d4a51dd-3f3a-4a4e-9c5d-14f1de8a3b9b" ||
			options.Status != "ready" || options.Feedback != "unlinked" || options.Limit != 5 {
			t.Fatalf("options = %#v", options)
		}
	})

	t.Run("rejects invalid filters", func(t *testing.T) {
		request := httptest.NewRequest("GET",
			"/v1/sessions?project_id=nope&status=bogus&feedback=maybe&limit=0&cursor=@@",
			nil)
		_, fields := parseTenantSessionListOptions(request)
		for _, field := range []string{"project_id", "status", "feedback", "limit", "cursor"} {
			if fields[field] == "" {
				t.Fatalf("missing validation for %s: %#v", field, fields)
			}
		}
	})

	t.Run("rejects limit above 100", func(t *testing.T) {
		request := httptest.NewRequest("GET", "/v1/sessions?limit=101", nil)
		_, fields := parseTenantSessionListOptions(request)
		if fields["limit"] == "" {
			t.Fatalf("fields = %#v", fields)
		}
	})
}

func TestTenantSessionCursorRoundTrip(t *testing.T) {
	session := Session{
		ID:        "9b8ad1de-4a3f-4a83-8a2e-0a8f4f3f6d21",
		StartedAt: time.Date(2026, 8, 1, 10, 30, 0, 0, time.UTC),
	}
	encoded, err := encodeTenantSessionCursor(session)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	decoded, err := decodeTenantSessionCursor(encoded)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if decoded.ID != session.ID || !decoded.StartedAt.Equal(session.StartedAt) {
		t.Fatalf("decoded = %#v", decoded)
	}

	for name, value := range map[string]string{
		"not base64":     "@@",
		"not json":       "bm9wZQ",
		"unknown fields": "eyJ2IjoxLCJzdGFydGVkX2F0IjoiMjAyNi0wOC0wMVQxMDozMDowMFoiLCJpZCI6IjliOGFkMWRlLTRhM2YtNGE4My04YTJlLTBhOGY0ZjNmNmQyMSIsImV4dHJhIjoxfQ",
		"bad uuid":       "eyJ2IjoxLCJzdGFydGVkX2F0IjoiMjAyNi0wOC0wMVQxMDozMDowMFoiLCJpZCI6Im5vcGUifQ",
		"zero time":      "eyJ2IjoxLCJzdGFydGVkX2F0IjoiMDAwMS0wMS0wMVQwMDowMDowMFoiLCJpZCI6IjliOGFkMWRlLTRhM2YtNGE4My04YTJlLTBhOGY0ZjNmNmQyMSJ9",
		"wrong version":  "eyJ2IjoyLCJzdGFydGVkX2F0IjoiMjAyNi0wOC0wMVQxMDozMDowMFoiLCJpZCI6IjliOGFkMWRlLTRhM2YtNGE4My04YTJlLTBhOGY0ZjNmNmQyMSJ9",
	} {
		if _, err := decodeTenantSessionCursor(value); err == nil {
			t.Fatalf("decode %s cursor succeeded", name)
		}
	}
}
