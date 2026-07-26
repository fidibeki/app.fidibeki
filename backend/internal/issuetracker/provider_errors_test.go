package issuetracker

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// The two unique constraints mean different things to a caller: one says another
// request is already creating this external issue, the other says it exists.
// Collapsing them would turn a retryable in-flight collision into a permanent
// "already exists", so the mapping is pinned here.
func TestTranslateLinkConflictSeparatesInFlightFromExisting(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		err  error
		want error
	}{
		"idempotency key": {
			err:  &pgconn.PgError{Code: "23505", ConstraintName: "issue_external_links_idempotency_key"},
			want: ErrLinkInFlight,
		},
		"live link": {
			err:  &pgconn.PgError{Code: "23505", ConstraintName: "idx_issue_external_links_live"},
			want: ErrLinkExists,
		},
		"another unique constraint": {
			err:  &pgconn.PgError{Code: "23505", ConstraintName: "something_else"},
			want: ErrLinkExists,
		},
	}
	for name, test := range tests {
		if got := translateLinkConflict(test.err); !errors.Is(got, test.want) {
			t.Fatalf("%s translated to %v, want %v", name, got, test.want)
		}
	}

	// Anything that is not a unique violation must stay an unexpected failure
	// rather than being reported to the member as a conflict they caused.
	for name, err := range map[string]error{
		"plain error":          errors.New("connection reset"),
		"other postgres":       &pgconn.PgError{Code: "40001", ConstraintName: "idx_issue_external_links_live"},
		"wrapped non-conflict": fmt.Errorf("query failed: %w", errors.New("timeout")),
	} {
		got := translateLinkConflict(err)
		if errors.Is(got, ErrLinkExists) || errors.Is(got, ErrLinkInFlight) {
			t.Fatalf("%s was translated into a conflict: %v", name, got)
		}
		if !errors.Is(got, errors.Unwrap(got)) && errors.Unwrap(got) == nil {
			t.Fatalf("%s lost its cause: %v", name, got)
		}
	}

	// A wrapped conflict is still a conflict: the store wraps driver errors as it
	// returns them up the call stack.
	wrapped := fmt.Errorf("insert link: %w", &pgconn.PgError{
		Code: "23505", ConstraintName: "issue_external_links_idempotency_key",
	})
	if got := translateLinkConflict(wrapped); !errors.Is(got, ErrLinkInFlight) {
		t.Fatalf("wrapped conflict translated to %v, want ErrLinkInFlight", got)
	}
}

// A failing Linear call is not always valid GraphQL, so the detail extraction has
// to survive every shape the endpoint returns without inventing a message.
func TestLinearTransportDetailHandlesEveryResponseShape(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		body string
		want string
	}{
		"graphql errors":    {body: `{"errors":[{"message":"rate limited"}]}`, want: "rate limited"},
		"several errors":    {body: `{"errors":[{"message":"first"},{"message":"second"}]}`, want: "first; second"},
		"top-level message": {body: `{"message":"unauthorized"}`, want: "unauthorized"},
		"message wins":      {body: `{"message":"gateway","errors":[{"message":"ignored"}]}`, want: "gateway"},
		"empty envelope":    {body: `{}`, want: ""},
		"not json":          {body: `<html>502 Bad Gateway</html>`, want: ""},
		"empty body":        {body: ``, want: ""},
	}
	for name, test := range tests {
		if got := linearTransportDetail([]byte(test.body)); got != test.want {
			t.Fatalf("%s detail = %q, want %q", name, got, test.want)
		}
	}
}

// The factory decides which provider a workspace actually talks to, so an
// unknown value must fail loudly instead of silently defaulting to one of them.
func TestNewClientSelectsTheProviderAndRejectsUnknownOnes(t *testing.T) {
	t.Parallel()
	jira, err := NewClient(ProviderJira, nil, "")
	if err != nil {
		t.Fatalf("jira client: %v", err)
	}
	if _, ok := jira.(JiraClient); !ok {
		t.Fatalf("jira client = %T", jira)
	}

	linear, err := NewClient(ProviderLinear, nil, "https://linear.test/graphql")
	if err != nil {
		t.Fatalf("linear client: %v", err)
	}
	linearClient, ok := linear.(LinearClient)
	if !ok {
		t.Fatalf("linear client = %T", linear)
	}
	if linearClient.Endpoint != "https://linear.test/graphql" {
		t.Fatalf("linear endpoint = %q, want the override", linearClient.Endpoint)
	}

	if _, err := NewClient(Provider("github"), nil, ""); err == nil {
		t.Fatal("an unsupported provider produced a client")
	}

	// A nil HTTP client must be filled in rather than left to panic on first use.
	if jira.(JiraClient).HTTP == nil {
		t.Fatal("the default HTTP client was not applied")
	}
	if _, ok := defaultHTTPClient().(*http.Client); !ok {
		t.Fatal("the default HTTP client is not an *http.Client")
	}
}

func TestDefaultClientFactoryUsesTheSuppliedHTTPClient(t *testing.T) {
	t.Parallel()
	doer := &recordingDoer{}
	factory := DefaultClientFactory(doer)

	client, err := factory(ProviderJira)
	if err != nil {
		t.Fatalf("factory(jira): %v", err)
	}
	if client.(JiraClient).HTTP != doer {
		t.Fatal("the factory did not pass its HTTP client through")
	}
	if _, err := factory(Provider("")); err == nil {
		t.Fatal("the factory accepted an empty provider")
	}
}

type recordingDoer struct{}

func (r *recordingDoer) Do(*http.Request) (*http.Response, error) {
	return nil, errors.New("not used")
}
