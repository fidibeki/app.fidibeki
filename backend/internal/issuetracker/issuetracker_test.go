package issuetracker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateSiteURLAcceptsOnlyAJiraOrigin(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name          string
		value         string
		allowInsecure bool
		want          string
		wantOK        bool
	}{
		{name: "https site", value: "https://acme.atlassian.net", want: "https://acme.atlassian.net", wantOK: true},
		{name: "trailing slash", value: "https://acme.atlassian.net/", want: "https://acme.atlassian.net", wantOK: true},
		{name: "path is rejected", value: "https://acme.atlassian.net/jira"},
		{name: "query is rejected", value: "https://acme.atlassian.net?a=b"},
		{name: "credentials are rejected", value: "https://user:pass@acme.atlassian.net"},
		{name: "plain http is rejected", value: "http://acme.atlassian.net"},
		{name: "empty is rejected", value: "   "},
		{
			name: "loopback allowed only in tests", value: "http://127.0.0.1:9000",
			allowInsecure: true, want: "http://127.0.0.1:9000", wantOK: true,
		},
		{
			name: "container host allowed only in tests", value: "http://host.docker.internal:9099",
			allowInsecure: true, want: "http://host.docker.internal:9099", wantOK: true,
		},
		{name: "container host rejected in production", value: "http://host.docker.internal:9099"},
		{name: "remote http still rejected in tests", value: "http://example.com", allowInsecure: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ValidateSiteURL(testCase.value, testCase.allowInsecure)
			if ok != testCase.wantOK || got != testCase.want {
				t.Fatalf("ValidateSiteURL(%q) = %q, %t; want %q, %t",
					testCase.value, got, ok, testCase.want, testCase.wantOK)
			}
		})
	}
}

func TestValidateConnectSeparatesJiraAndLinearCredentials(t *testing.T) {
	t.Parallel()
	_, fields := ValidateConnect(ProviderJira, ConnectInput{
		SiteURL: "https://acme.atlassian.net", Email: "ops@acme.test", APIToken: "token",
	}, false)
	if len(fields) != 0 {
		t.Fatalf("valid Jira credential fields = %#v", fields)
	}
	_, fields = ValidateConnect(ProviderJira, ConnectInput{APIToken: "token"}, false)
	if len(fields) != 2 {
		t.Fatalf("Jira without site or email fields = %#v", fields)
	}
	_, fields = ValidateConnect(ProviderLinear, ConnectInput{APIToken: "lin_api_key"}, false)
	if len(fields) != 0 {
		t.Fatalf("valid Linear credential fields = %#v", fields)
	}
	_, fields = ValidateConnect(ProviderLinear, ConnectInput{
		SiteURL: "https://linear.app", Email: "ops@acme.test", APIToken: "lin_api_key",
	}, false)
	if len(fields) != 2 {
		t.Fatalf("Linear with Jira-only fields = %#v", fields)
	}
	_, fields = ValidateConnect(ProviderLinear, ConnectInput{}, false)
	if len(fields) != 1 || fields[0].Field != "api_token" {
		t.Fatalf("missing token fields = %#v", fields)
	}
}

func TestValidateCreateFallsBackToTheIssueTitle(t *testing.T) {
	t.Parallel()
	issue := IssueSnapshot{ID: "issue", Title: "Checkout is confusing"}
	normalized, fields := ValidateCreate(CreateInput{Provider: ProviderLinear}, issue)
	if len(fields) != 0 || normalized.Title != "Checkout is confusing" {
		t.Fatalf("normalized = %#v, fields = %#v", normalized, fields)
	}
	_, fields = ValidateCreate(CreateInput{
		Provider: ProviderLinear, Title: "Fine", Priority: "catastrophic",
	}, issue)
	if len(fields) != 1 || fields[0].Field != "priority" {
		t.Fatalf("priority fields = %#v", fields)
	}
	_, fields = ValidateCreate(CreateInput{
		Provider: "asana", Title: "Fine", IdempotencyKey: "short",
	}, issue)
	if len(fields) != 2 {
		t.Fatalf("provider and key fields = %#v", fields)
	}
}

func TestDeriveIdempotencyKeyIsStablePerReviewedRequest(t *testing.T) {
	t.Parallel()
	input := CreateInput{Provider: ProviderJira, Title: "Broken", Description: "Steps", Priority: "high"}
	first := DeriveIdempotencyKey("issue-1", input)
	if first != DeriveIdempotencyKey("issue-1", input) {
		t.Fatal("the same reviewed request derived two different keys")
	}
	edited := input
	edited.Title = "Broken checkout"
	if first == DeriveIdempotencyKey("issue-1", edited) {
		t.Fatal("an edited request reused the previous key")
	}
	if first == DeriveIdempotencyKey("issue-2", input) {
		t.Fatal("a different issue reused the previous key")
	}
}

func TestResolveDestinationChecksTheSelectionAgainstTheProvider(t *testing.T) {
	t.Parallel()
	destinations := []Destination{{
		ID: "10000", Key: "OPS", Name: "Operations",
		IssueTypes: []DestinationRef{{ID: "10001", Name: "Task"}},
		Projects:   []DestinationRef{{ID: "p1", Name: "Q3"}},
	}}
	values, fields := resolveDestination(ProviderJira, destinations, ConfigureValues{
		DestinationID: "10000", IssueTypeID: "10001",
	})
	if len(fields) != 0 || values.DestinationName != "Operations" || values.IssueTypeName != "Task" {
		t.Fatalf("resolved = %#v, fields = %#v", values, fields)
	}
	_, fields = resolveDestination(ProviderJira, destinations, ConfigureValues{DestinationID: "10000"})
	if len(fields) != 1 || fields[0].Field != "issue_type_id" {
		t.Fatalf("missing Jira issue type fields = %#v", fields)
	}
	_, fields = resolveDestination(ProviderJira, destinations, ConfigureValues{
		DestinationID: "10000", IssueTypeID: "99999",
	})
	if len(fields) != 1 || fields[0].Field != "issue_type_id" {
		t.Fatalf("unknown Jira issue type fields = %#v", fields)
	}
	values, fields = resolveDestination(ProviderLinear, destinations, ConfigureValues{
		DestinationID: "10000", SubDestinationID: "p1",
	})
	if len(fields) != 0 || values.SubDestinationName != "Q3" {
		t.Fatalf("resolved Linear = %#v, fields = %#v", values, fields)
	}
	_, fields = resolveDestination(ProviderLinear, destinations, ConfigureValues{DestinationID: "unknown"})
	if len(fields) != 1 || fields[0].Field != "destination_id" {
		t.Fatalf("unknown destination fields = %#v", fields)
	}
}

func TestJiraClientCreatesAnIssueAndBuildsABrowseLink(t *testing.T) {
	t.Parallel()
	var authorization string
	var createBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		switch {
		case r.URL.Path == "/rest/api/3/myself":
			writeJSON(w, http.StatusOK, map[string]any{
				"accountId": "acc-1", "displayName": "Ops Bot", "emailAddress": "ops@acme.test",
			})
		case r.URL.Path == "/rest/api/3/project/search":
			writeJSON(w, http.StatusOK, map[string]any{"values": []any{
				map[string]any{"id": "10000", "key": "OPS", "name": "Operations"},
			}})
		case strings.HasPrefix(r.URL.Path, "/rest/api/3/issue/createmeta/"):
			writeJSON(w, http.StatusOK, map[string]any{"issueTypes": []any{
				map[string]any{"id": "10001", "name": "Task", "subtask": false},
				map[string]any{"id": "10002", "name": "Sub-task", "subtask": true},
			}})
		case r.URL.Path == "/rest/api/3/issue" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &createBody)
			writeJSON(w, http.StatusCreated, map[string]any{"id": "20001", "key": "OPS-42"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := JiraClient{HTTP: server.Client()}
	credentials := Credentials{SiteURL: server.URL, Email: "ops@acme.test", APIToken: "token"}
	account, err := client.Validate(context.Background(), credentials)
	if err != nil || account.Label != "Ops Bot" {
		t.Fatalf("Validate = %#v, error = %v", account, err)
	}
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("ops@acme.test:token"))
	if authorization != expected {
		t.Fatalf("Authorization = %q, want %q", authorization, expected)
	}
	destinations, err := client.Destinations(context.Background(), credentials)
	if err != nil || len(destinations) != 1 || len(destinations[0].IssueTypes) != 1 {
		t.Fatalf("Destinations = %#v, error = %v", destinations, err)
	}
	if destinations[0].IssueTypes[0].Name != "Task" {
		t.Fatalf("subtask issue type was offered as a destination: %#v", destinations[0].IssueTypes)
	}
	result, err := client.CreateIssue(context.Background(), credentials, CreateRequest{
		Title: "Checkout is confusing", Description: "First line\n\nSecond line",
		Priority: "high", DestinationID: "10000", IssueTypeID: "10001",
	})
	if err != nil {
		t.Fatalf("CreateIssue error = %v", err)
	}
	if result.Key != "OPS-42" || result.URL != server.URL+"/browse/OPS-42" {
		t.Fatalf("CreateIssue result = %#v", result)
	}
	fields, _ := createBody["fields"].(map[string]any)
	priority, _ := fields["priority"].(map[string]any)
	if priority["name"] != "High" {
		t.Fatalf("Jira priority = %#v", fields["priority"])
	}
	document, _ := fields["description"].(map[string]any)
	content, _ := document["content"].([]any)
	if document["type"] != "doc" || len(content) != 2 {
		t.Fatalf("Jira description = %#v", fields["description"])
	}
}

func TestJiraClientRetriesWithoutAnUnsupportedPriorityField(t *testing.T) {
	t.Parallel()
	attempts := 0
	var lastFields map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		lastFields, _ = payload["fields"].(map[string]any)
		if _, hasPriority := lastFields["priority"]; hasPriority {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"errors": map[string]string{"priority": "Field 'priority' cannot be set."},
			})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": "20002", "key": "OPS-43"})
	}))
	defer server.Close()

	client := JiraClient{HTTP: server.Client()}
	result, err := client.CreateIssue(context.Background(),
		Credentials{SiteURL: server.URL, Email: "ops@acme.test", APIToken: "token"},
		CreateRequest{Title: "Retry", Priority: "urgent", DestinationID: "10000", IssueTypeID: "10001"},
	)
	if err != nil || result.Key != "OPS-43" || attempts != 2 {
		t.Fatalf("result = %#v, attempts = %d, error = %v", result, attempts, err)
	}
	if _, hasPriority := lastFields["priority"]; hasPriority {
		t.Fatal("the retry still carried the rejected priority field")
	}
}

func TestJiraClientTranslatesFailuresIntoActionableErrors(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name          string
		status        int
		body          map[string]any
		wantCode      string
		wantRetryable bool
		wantMessage   string
	}{
		{
			name: "rejected credential", status: http.StatusUnauthorized,
			body:     map[string]any{"errorMessages": []string{"Client must be authenticated"}},
			wantCode: ErrorCodeUnauthorized, wantMessage: "Client must be authenticated",
		},
		{
			name: "unknown project", status: http.StatusNotFound,
			body:     map[string]any{"errorMessages": []string{"Project does not exist"}},
			wantCode: ErrorCodeNotFound, wantMessage: "Project does not exist",
		},
		{
			name: "provider outage", status: http.StatusBadGateway,
			wantCode: ErrorCodeUnavailable, wantRetryable: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, testCase.status, testCase.body)
			}))
			defer server.Close()
			client := JiraClient{HTTP: server.Client()}
			_, err := client.CreateIssue(context.Background(),
				Credentials{SiteURL: server.URL, Email: "ops@acme.test", APIToken: "token"},
				CreateRequest{Title: "Broken", DestinationID: "10000", IssueTypeID: "10001"},
			)
			failure := providerFailure(t, err)
			if failure.Code != testCase.wantCode || failure.Retryable != testCase.wantRetryable {
				t.Fatalf("failure = %#v, want code %q retryable %t",
					failure, testCase.wantCode, testCase.wantRetryable)
			}
			if testCase.wantMessage != "" && failure.Message != testCase.wantMessage {
				t.Fatalf("message = %q, want %q", failure.Message, testCase.wantMessage)
			}
			if strings.Contains(failure.Message, "token") {
				t.Fatalf("failure message leaked the credential: %q", failure.Message)
			}
		})
	}
}

func TestJiraClientRefusesToCreateWithoutADestination(t *testing.T) {
	t.Parallel()
	_, err := JiraClient{HTTP: http.DefaultClient}.CreateIssue(context.Background(),
		Credentials{SiteURL: "https://acme.atlassian.net"}, CreateRequest{Title: "Broken"})
	if failure := providerFailure(t, err); failure.Code != ErrorCodeNoDestination {
		t.Fatalf("failure = %#v", failure)
	}
}

func TestLinearClientCreatesAnIssueThroughGraphQL(t *testing.T) {
	t.Parallel()
	var authorization string
	var createInput map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		var request struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &request)
		switch {
		case strings.Contains(request.Query, "viewer"):
			writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
				"viewer":       map[string]any{"name": "Ops Bot", "email": "ops@acme.test"},
				"organization": map[string]any{"name": "Acme"},
			}})
		case strings.Contains(request.Query, "teams"):
			writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
				"teams": map[string]any{"nodes": []any{map[string]any{
					"id": "team-1", "key": "OPS", "name": "Operations",
					"projects": map[string]any{"nodes": []any{
						map[string]any{"id": "project-1", "name": "Q3"},
					}},
				}}},
			}})
		default:
			createInput, _ = request.Variables["input"].(map[string]any)
			writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
				"issueCreate": map[string]any{"success": true, "issue": map[string]any{
					"id": "issue-1", "identifier": "OPS-7", "url": "https://linear.app/acme/issue/OPS-7",
				}},
			}})
		}
	}))
	defer server.Close()

	client := LinearClient{HTTP: server.Client(), Endpoint: server.URL}
	credentials := Credentials{APIToken: "lin_api_key"}
	account, err := client.Validate(context.Background(), credentials)
	if err != nil || account.Label != "Acme (Ops Bot)" {
		t.Fatalf("Validate = %#v, error = %v", account, err)
	}
	if authorization != "lin_api_key" {
		t.Fatalf("Authorization = %q, want the raw API key", authorization)
	}
	destinations, err := client.Destinations(context.Background(), credentials)
	if err != nil || len(destinations) != 1 || len(destinations[0].Projects) != 1 {
		t.Fatalf("Destinations = %#v, error = %v", destinations, err)
	}
	result, err := client.CreateIssue(context.Background(), credentials, CreateRequest{
		Title: "Checkout is confusing", Description: "Steps", Priority: "urgent",
		DestinationID: "team-1", SubDestination: "project-1",
	})
	if err != nil {
		t.Fatalf("CreateIssue error = %v", err)
	}
	if result.Key != "OPS-7" || result.URL != "https://linear.app/acme/issue/OPS-7" {
		t.Fatalf("CreateIssue result = %#v", result)
	}
	if createInput["teamId"] != "team-1" || createInput["projectId"] != "project-1" {
		t.Fatalf("Linear input = %#v", createInput)
	}
	if priority, _ := createInput["priority"].(float64); priority != 1 {
		t.Fatalf("Linear priority = %#v, want 1 for urgent", createInput["priority"])
	}
}

func TestLinearClientTreatsGraphQLErrorsAsFailures(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"errors": []any{map[string]any{
			"message":    "Authentication required, not authenticated",
			"extensions": map[string]any{"code": "AUTHENTICATION_ERROR"},
		}}})
	}))
	defer server.Close()
	client := LinearClient{HTTP: server.Client(), Endpoint: server.URL}
	_, err := client.CreateIssue(context.Background(), Credentials{APIToken: "lin_api_key"},
		CreateRequest{Title: "Broken", DestinationID: "team-1"})
	failure := providerFailure(t, err)
	if failure.Code != ErrorCodeUnauthorized || failure.Retryable {
		t.Fatalf("failure = %#v", failure)
	}
	if !strings.Contains(failure.Message, "Authentication required") {
		t.Fatalf("message = %q", failure.Message)
	}
}

func TestLinearClientRejectsAnUnsuccessfulMutation(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
			"issueCreate": map[string]any{"success": false},
		}})
	}))
	defer server.Close()
	client := LinearClient{HTTP: server.Client(), Endpoint: server.URL}
	_, err := client.CreateIssue(context.Background(), Credentials{APIToken: "lin_api_key"},
		CreateRequest{Title: "Broken", DestinationID: "team-1"})
	if failure := providerFailure(t, err); failure.Code != ErrorCodeMalformed || !failure.Retryable {
		t.Fatalf("failure = %#v", failure)
	}
}

func TestAutoCreateIdempotencyKeyIsStablePerIssueAndProvider(t *testing.T) {
	t.Parallel()
	key := AutoCreateIdempotencyKey("issue-1", ProviderJira)
	if key != AutoCreateIdempotencyKey("issue-1", ProviderJira) {
		t.Fatal("the same issue and provider derived two different keys")
	}
	if key == AutoCreateIdempotencyKey("issue-1", ProviderLinear) {
		t.Fatal("two providers share one key")
	}
	if key == AutoCreateIdempotencyKey("issue-2", ProviderJira) {
		t.Fatal("two issues share one key")
	}
}

func providerFailure(t *testing.T, err error) *ProviderError {
	t.Helper()
	if err == nil {
		t.Fatal("expected a provider error")
	}
	failure, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("error %v is not a *ProviderError", err)
	}
	return failure
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if value != nil {
		_ = json.NewEncoder(w).Encode(value)
	}
}
