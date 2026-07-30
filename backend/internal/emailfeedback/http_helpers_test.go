package emailfeedback

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/feedback"
)

func TestPublicRequestDecodersRejectAmbiguousBodies(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		body   string
		ok     bool
		status int
	}{
		{name: "empty", body: "", ok: true, status: http.StatusOK},
		{name: "empty object", body: "{}", ok: true, status: http.StatusOK},
		{name: "unexpected field", body: `{"rating":5}`, status: http.StatusBadRequest},
	} {
		t.Run("optional "+test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodPost, "/email-feedback/token/opt-out", strings.NewReader(test.body),
			)
			if got := decodeOptionalEmptyJSON(response, request); got != test.ok {
				t.Fatalf("decodeOptionalEmptyJSON() = %t, want %t", got, test.ok)
			}
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}

	for _, test := range []struct {
		name   string
		body   string
		ok     bool
		status int
	}{
		{name: "valid", body: `{"name":"purchase"}`, ok: true, status: http.StatusOK},
		{name: "unknown field", body: `{"name":"purchase","extra":true}`, status: http.StatusBadRequest},
		{name: "multiple values", body: `{"name":"purchase"} {}`, status: http.StatusBadRequest},
	} {
		t.Run("JSON "+test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodPost, "/email-feedback/campaigns", strings.NewReader(test.body),
			)
			var payload struct {
				Name string `json:"name"`
			}
			if got := decodeJSON(response, request, &payload); got != test.ok {
				t.Fatalf("decodeJSON() = %t, want %t", got, test.ok)
			}
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}

func TestCollectionErrorsRemainPublicAndBounded(t *testing.T) {
	t.Parallel()

	validation := &collectionValidationError{fields: []feedback.FieldError{{
		Field: "rating", Message: "must be between 1 and 5",
	}}}
	if validation.Error() != "email feedback collection is invalid" {
		t.Fatalf("collectionValidationError.Error() = %q", validation.Error())
	}

	for _, test := range []struct {
		name   string
		err    error
		status int
		code   api.ErrorCode
	}{
		{
			name: "unavailable token", err: ErrInvitationPublicToken,
			status: http.StatusNotFound, code: api.CodeNotFound,
		},
		{
			name: "invalid response", err: validation,
			status: http.StatusUnprocessableEntity, code: api.CodeValidation,
		},
		{
			name: "plan limit", err: &feedback.PlanLimitError{Limit: 100, Usage: 100},
			status: http.StatusPaymentRequired, code: api.CodePlanLimitExceeded,
		},
		{
			name: "internal dependency", err: errors.New("database unavailable"),
			status: http.StatusServiceUnavailable, code: api.CodeServiceUnavailable,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/email-feedback/token", nil)
			handler := &publicHTTPHandler{}
			handler.writeCollectionError(response, request, test.err)

			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if response.Header().Get("Cache-Control") != "no-store" ||
				response.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatalf("public error cache headers = %#v", response.Header())
			}
			var envelope api.ErrorEnvelope
			if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if envelope.Error.Code != test.code {
				t.Fatalf("error code = %q, want %q", envelope.Error.Code, test.code)
			}
		})
	}
}
