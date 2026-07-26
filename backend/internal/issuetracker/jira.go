package issuetracker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// JiraClient talks to the Jira Cloud REST v3 API using the workspace's own site URL and an
// account email plus API token, sent as HTTP basic authentication.
type JiraClient struct {
	HTTP HTTPDoer
}

// jiraPriorityNames maps Fidibeki priorities onto the names in Jira's default priority scheme.
var jiraPriorityNames = map[string]string{
	"low":    "Low",
	"medium": "Medium",
	"high":   "High",
	"urgent": "Highest",
}

func (c JiraClient) Validate(ctx context.Context, credentials Credentials) (Account, error) {
	var identity struct {
		AccountID    string `json:"accountId"`
		DisplayName  string `json:"displayName"`
		EmailAddress string `json:"emailAddress"`
	}
	if err := c.call(ctx, credentials, http.MethodGet, "/rest/api/3/myself", nil, &identity); err != nil {
		return Account{}, err
	}
	label := strings.TrimSpace(identity.DisplayName)
	if label == "" {
		label = strings.TrimSpace(identity.EmailAddress)
	}
	if label == "" {
		label = strings.TrimSpace(credentials.Email)
	}
	return Account{Label: label}, nil
}

func (c JiraClient) Destinations(ctx context.Context, credentials Credentials) ([]Destination, error) {
	var page struct {
		Values []struct {
			ID   string `json:"id"`
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"values"`
	}
	path := "/rest/api/3/project/search?maxResults=50&orderBy=name"
	if err := c.call(ctx, credentials, http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	destinations := make([]Destination, 0, len(page.Values))
	for _, project := range page.Values {
		issueTypes, err := c.issueTypes(ctx, credentials, project.ID)
		if err != nil {
			// A project whose create metadata is not readable is still worth listing; the
			// administrator simply cannot select it as a destination.
			issueTypes = nil
		}
		destinations = append(destinations, Destination{
			ID: project.ID, Key: project.Key, Name: project.Name, IssueTypes: issueTypes,
		})
	}
	return destinations, nil
}

func (c JiraClient) issueTypes(
	ctx context.Context,
	credentials Credentials,
	projectID string,
) ([]DestinationRef, error) {
	var metadata struct {
		IssueTypes []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Subtask bool   `json:"subtask"`
		} `json:"issueTypes"`
	}
	path := fmt.Sprintf("/rest/api/3/issue/createmeta/%s/issuetypes", projectID)
	if err := c.call(ctx, credentials, http.MethodGet, path, nil, &metadata); err != nil {
		return nil, err
	}
	types := make([]DestinationRef, 0, len(metadata.IssueTypes))
	for _, issueType := range metadata.IssueTypes {
		if issueType.Subtask {
			// A subtask cannot be created without a parent, so it is never a valid destination.
			continue
		}
		types = append(types, DestinationRef{ID: issueType.ID, Name: issueType.Name})
	}
	return types, nil
}

func (c JiraClient) CreateIssue(
	ctx context.Context,
	credentials Credentials,
	request CreateRequest,
) (CreateResult, error) {
	if request.DestinationID == "" || request.IssueTypeID == "" {
		return CreateResult{}, providerError(ErrorCodeNoDestination,
			"a Jira project and issue type must be configured first", 0, false)
	}
	result, err := c.createIssue(ctx, credentials, request, true)
	var failure *ProviderError
	if err != nil && errors.As(err, &failure) && failure.Status == http.StatusBadRequest &&
		strings.Contains(strings.ToLower(failure.Message), "priority") {
		// Priority is not on every Jira project's create screen. Rather than fail an otherwise
		// valid issue, retry once without it; the Fidibeki issue keeps the priority either way.
		return c.createIssue(ctx, credentials, request, false)
	}
	return result, err
}

func (c JiraClient) createIssue(
	ctx context.Context,
	credentials Credentials,
	request CreateRequest,
	withPriority bool,
) (CreateResult, error) {
	fields := map[string]any{
		"project":   map[string]string{"id": request.DestinationID},
		"issuetype": map[string]string{"id": request.IssueTypeID},
		"summary":   truncate(request.Title, 255),
	}
	if description := strings.TrimSpace(request.Description); description != "" {
		fields["description"] = atlassianDocument(description)
	}
	if name, ok := jiraPriorityNames[request.Priority]; ok && withPriority {
		fields["priority"] = map[string]string{"name": name}
	}
	var created struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	body := map[string]any{"fields": fields}
	if err := c.call(ctx, credentials, http.MethodPost, "/rest/api/3/issue", body, &created); err != nil {
		return CreateResult{}, err
	}
	if created.ID == "" || created.Key == "" {
		return CreateResult{}, providerError(ErrorCodeMalformed,
			"Jira accepted the request without returning an issue key", 0, true)
	}
	return CreateResult{
		ID:  created.ID,
		Key: created.Key,
		URL: strings.TrimRight(credentials.SiteURL, "/") + "/browse/" + created.Key,
	}, nil
}

func (c JiraClient) call(
	ctx context.Context,
	credentials Credentials,
	method string,
	path string,
	body any,
	destination any,
) error {
	site := strings.TrimRight(strings.TrimSpace(credentials.SiteURL), "/")
	if site == "" {
		return providerError(ErrorCodeRejected, "the Jira site URL is missing", 0, false)
	}
	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return providerError(ErrorCodeRejected, "the request could not be encoded", 0, false)
		}
		payload = encoded
	}
	request, err := http.NewRequestWithContext(ctx, method, site+path, bytes.NewReader(payload))
	if err != nil {
		return providerError(ErrorCodeRejected, "the Jira site URL is not usable", 0, false)
	}
	credential := base64.StdEncoding.EncodeToString(
		[]byte(strings.TrimSpace(credentials.Email) + ":" + strings.TrimSpace(credentials.APIToken)),
	)
	request.Header.Set("Authorization", "Basic "+credential)
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	status, responseBody, err := send(ctx, c.HTTP, request)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return statusError(status, jiraErrorDetail(responseBody))
	}
	if destination == nil {
		return nil
	}
	if err := json.Unmarshal(responseBody, destination); err != nil {
		return providerError(ErrorCodeMalformed, "the Jira response could not be decoded", status, true)
	}
	return nil
}

// jiraErrorDetail extracts Jira's own error text so the workspace sees why the call failed.
func jiraErrorDetail(body []byte) string {
	var payload struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	details := make([]string, 0, len(payload.ErrorMessages)+len(payload.Errors))
	details = append(details, payload.ErrorMessages...)
	for field, message := range payload.Errors {
		details = append(details, field+": "+message)
	}
	return strings.Join(details, "; ")
}

// atlassianDocument renders plain text as an Atlassian Document Format doc, which is the only
// description format the v3 create endpoint accepts. Empty lines are dropped because ADF text
// nodes may not be empty.
func atlassianDocument(text string) map[string]any {
	paragraphs := make([]any, 0)
	for line := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		paragraphs = append(paragraphs, map[string]any{
			"type":    "paragraph",
			"content": []any{map[string]any{"type": "text", "text": line}},
		})
	}
	if len(paragraphs) == 0 {
		paragraphs = append(paragraphs, map[string]any{"type": "paragraph"})
	}
	return map[string]any{"type": "doc", "version": 1, "content": paragraphs}
}
