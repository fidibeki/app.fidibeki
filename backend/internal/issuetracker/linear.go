package issuetracker

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// LinearEndpoint is Linear's single GraphQL endpoint.
const LinearEndpoint = "https://api.linear.app/graphql"

// LinearClient talks to Linear's GraphQL API with a workspace API key. Linear has no per-site
// URL, so the connection stores none.
type LinearClient struct {
	HTTP HTTPDoer
	// Endpoint overrides the GraphQL endpoint in tests. Empty means LinearEndpoint.
	Endpoint string
}

// linearPriorities maps Fidibeki priorities onto Linear's fixed priority scale, where 0 means
// "no priority" and 1 is the most urgent.
var linearPriorities = map[string]int{
	"urgent": 1,
	"high":   2,
	"medium": 3,
	"low":    4,
}

const linearViewerQuery = `query { viewer { id name email } organization { name urlKey } }`

const linearTeamsQuery = `query {
  teams(first: 50) {
    nodes { id key name projects(first: 50) { nodes { id name } } }
  }
}`

const linearCreateMutation = `mutation CreateIssue($input: IssueCreateInput!) {
  issueCreate(input: $input) { success issue { id identifier url } }
}`

func (c LinearClient) Validate(ctx context.Context, credentials Credentials) (Account, error) {
	var payload struct {
		Viewer struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"viewer"`
		Organization struct {
			Name string `json:"name"`
		} `json:"organization"`
	}
	if err := c.call(ctx, credentials, linearViewerQuery, nil, &payload); err != nil {
		return Account{}, err
	}
	label := strings.TrimSpace(payload.Organization.Name)
	person := strings.TrimSpace(payload.Viewer.Name)
	if person == "" {
		person = strings.TrimSpace(payload.Viewer.Email)
	}
	switch {
	case label != "" && person != "":
		label += " (" + person + ")"
	case label == "":
		label = person
	}
	if label == "" {
		return Account{}, providerError(ErrorCodeUnauthorized,
			"the stored credential did not resolve a Linear account", 0, false)
	}
	return Account{Label: label}, nil
}

func (c LinearClient) Destinations(ctx context.Context, credentials Credentials) ([]Destination, error) {
	var payload struct {
		Teams struct {
			Nodes []struct {
				ID       string `json:"id"`
				Key      string `json:"key"`
				Name     string `json:"name"`
				Projects struct {
					Nodes []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"projects"`
			} `json:"nodes"`
		} `json:"teams"`
	}
	if err := c.call(ctx, credentials, linearTeamsQuery, nil, &payload); err != nil {
		return nil, err
	}
	destinations := make([]Destination, 0, len(payload.Teams.Nodes))
	for _, team := range payload.Teams.Nodes {
		projects := make([]DestinationRef, 0, len(team.Projects.Nodes))
		for _, project := range team.Projects.Nodes {
			projects = append(projects, DestinationRef{ID: project.ID, Name: project.Name})
		}
		destinations = append(destinations, Destination{
			ID: team.ID, Key: team.Key, Name: team.Name, Projects: projects,
		})
	}
	return destinations, nil
}

func (c LinearClient) CreateIssue(
	ctx context.Context,
	credentials Credentials,
	request CreateRequest,
) (CreateResult, error) {
	if request.DestinationID == "" {
		return CreateResult{}, providerError(ErrorCodeNoDestination,
			"a Linear team must be configured first", 0, false)
	}
	input := map[string]any{
		"teamId": request.DestinationID,
		"title":  truncate(request.Title, 255),
	}
	if description := strings.TrimSpace(request.Description); description != "" {
		input["description"] = description
	}
	if priority, ok := linearPriorities[request.Priority]; ok {
		input["priority"] = priority
	}
	if request.SubDestination != "" {
		input["projectId"] = request.SubDestination
	}
	var payload struct {
		IssueCreate struct {
			Success bool `json:"success"`
			Issue   struct {
				ID         string `json:"id"`
				Identifier string `json:"identifier"`
				URL        string `json:"url"`
			} `json:"issue"`
		} `json:"issueCreate"`
	}
	variables := map[string]any{"input": input}
	if err := c.call(ctx, credentials, linearCreateMutation, variables, &payload); err != nil {
		return CreateResult{}, err
	}
	created := payload.IssueCreate
	if !created.Success || created.Issue.ID == "" || created.Issue.URL == "" {
		return CreateResult{}, providerError(ErrorCodeMalformed,
			"Linear accepted the request without returning an issue", 0, true)
	}
	return CreateResult{
		ID:  created.Issue.ID,
		Key: created.Issue.Identifier,
		URL: created.Issue.URL,
	}, nil
}

func (c LinearClient) call(
	ctx context.Context,
	credentials Credentials,
	query string,
	variables map[string]any,
	destination any,
) error {
	token := strings.TrimSpace(credentials.APIToken)
	if token == "" {
		return providerError(ErrorCodeUnauthorized, "the Linear API key is missing", 0, false)
	}
	body := map[string]any{"query": query}
	if len(variables) > 0 {
		body["variables"] = variables
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return providerError(ErrorCodeRejected, "the request could not be encoded", 0, false)
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = LinearEndpoint
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return providerError(ErrorCodeRejected, "the Linear endpoint is not usable", 0, false)
	}
	request.Header.Set("Authorization", token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	status, responseBody, err := send(ctx, c.HTTP, request)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return statusError(status, linearTransportDetail(responseBody))
	}
	// GraphQL reports application errors inside a 200 response, so the envelope is inspected
	// before the data is decoded.
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message    string `json:"message"`
			Extensions struct {
				Code string `json:"code"`
				Type string `json:"type"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return providerError(ErrorCodeMalformed, "the Linear response could not be decoded", status, true)
	}
	if len(envelope.Errors) > 0 {
		messages := make([]string, 0, len(envelope.Errors))
		authentication := false
		for _, item := range envelope.Errors {
			messages = append(messages, item.Message)
			code := strings.ToLower(item.Extensions.Code + " " + item.Extensions.Type)
			if strings.Contains(code, "authentication") || strings.Contains(code, "unauthorized") {
				authentication = true
			}
		}
		detail := strings.Join(messages, "; ")
		if authentication {
			return providerError(ErrorCodeUnauthorized, fallback(detail,
				"the stored credential was rejected"), status, false)
		}
		return providerError(ErrorCodeRejected, fallback(detail,
			"Linear rejected the request"), status, false)
	}
	if destination == nil {
		return nil
	}
	if len(envelope.Data) == 0 {
		return providerError(ErrorCodeMalformed, "the Linear response contained no data", status, true)
	}
	if err := json.Unmarshal(envelope.Data, destination); err != nil {
		return providerError(ErrorCodeMalformed, "the Linear response could not be decoded", status, true)
	}
	return nil
}

// linearTransportDetail extracts an error message from a non-2xx Linear response, which is not
// always valid GraphQL.
func linearTransportDetail(body []byte) string {
	var payload struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	if payload.Message != "" {
		return payload.Message
	}
	messages := make([]string, 0, len(payload.Errors))
	for _, item := range payload.Errors {
		messages = append(messages, item.Message)
	}
	return strings.Join(messages, "; ")
}
