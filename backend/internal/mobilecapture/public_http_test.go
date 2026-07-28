package mobilecapture

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestPublicMobileSubmissionRejectsUnknownFieldsRecursively(t *testing.T) {
	t.Parallel()
	token := TokenPrefix + strings.Repeat("A", 43)
	for _, testCase := range []struct {
		name string
		body string
	}{
		{
			name: "top level",
			body: `{
				"client_feedback_id":"10000000-0000-4000-8000-000000000001",
				"responses":{},"unknown":true
			}`,
		},
		{
			name: "nested screenshot",
			body: `{
				"client_feedback_id":"10000000-0000-4000-8000-000000000001",
				"responses":{},
				"screenshot":{
					"client_attachment_id":"20000000-0000-4000-8000-000000000002",
					"content_type":"image/png","size_bytes":2048,"uri":"file:///secret"
				}
			}`,
		},
		{
			name: "nested consent",
			body: `{
				"client_feedback_id":"10000000-0000-4000-8000-000000000001",
				"responses":{},
				"consent":{
					"purpose":"feedback_updates","notice_version":"v1",
					"controller_profile_version":1,"widget_definition_version":1,
					"locale":"en","granted":true,"device_locale":"pt"
				}
			}`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			router := chi.NewRouter()
			RegisterPublicRoutes(router, PublicHTTPOptions{})
			request := httptest.NewRequest(
				http.MethodPost, "/v1/mobile/feedback",
				strings.NewReader(testCase.body),
			)
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}
