package activity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRecordAppliesDefaultsAndPersistsUTCData(t *testing.T) {
	t.Parallel()
	createdAt := time.Date(2026, 7, 22, 23, 15, 0, 0, time.FixedZone("west", -2*60*60))
	query := &activityQuerier{}

	id, err := Record(context.Background(), query, RecordInput{
		TenantID: "tenant-id", IssueID: "issue-id", Kind: "issue.created", CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if id == "" || !strings.Contains(query.statement, "INSERT INTO issue_activities") {
		t.Fatalf("Record() id = %q, statement = %q", id, query.statement)
	}
	if got := query.arguments[0]; got != id {
		t.Errorf("persisted id = %v, want %s", got, id)
	}
	if got := query.arguments[5]; got != VisibilityInternal {
		t.Errorf("visibility = %v, want %s", got, VisibilityInternal)
	}
	if got := query.arguments[6]; got != "{}" {
		t.Errorf("data = %v, want {}", got)
	}
	if got := query.arguments[7].(time.Time); !got.Equal(createdAt) || got.Location() != time.UTC {
		t.Errorf("created_at = %v, want UTC %v", got, createdAt.UTC())
	}
}

func TestRecordPreservesExplicitValuesAndReturnsFailures(t *testing.T) {
	t.Parallel()
	actorID := "member-id"
	query := &activityQuerier{}
	id, err := Record(context.Background(), query, RecordInput{
		ID: "activity-id", TenantID: "tenant-id", IssueID: "issue-id", ActorMemberID: &actorID,
		Kind: "reporter.responded", Visibility: VisibilityReporter,
		Data: map[string]any{"response": "More detail"}, CreatedAt: time.Now(),
	})
	if err != nil || id != "activity-id" {
		t.Fatalf("Record() = %q, %v", id, err)
	}
	if query.arguments[3] != &actorID || query.arguments[5] != VisibilityReporter ||
		query.arguments[6] != `{"response":"More detail"}` {
		t.Errorf("persisted arguments = %#v", query.arguments)
	}

	if _, err := Record(context.Background(), query, RecordInput{Data: map[string]any{"bad": make(chan int)}}); err == nil ||
		!strings.Contains(err.Error(), "encode issue activity data") {
		t.Errorf("marshal error = %v", err)
	}
	query.execError = errors.New("database unavailable")
	if _, err := Record(context.Background(), query, RecordInput{ID: "failed"}); err == nil ||
		!strings.Contains(err.Error(), "record issue activity") {
		t.Errorf("database error = %v", err)
	}
}

func TestScanEventBuildsActorAndRejectsInvalidRows(t *testing.T) {
	t.Parallel()
	createdAt := time.Date(2026, 7, 23, 1, 0, 0, 0, time.UTC)
	name := "Alex"
	row := activityRow{values: []any{
		"activity-id", "issue-id", "issue.updated", VisibilityInternal,
		[]byte(`{"changes":{"priority":{"to":"high"}}}`), createdAt,
		stringPointer("member-id"), &name, stringPointer("alex@example.test"),
	}}
	event, err := scanEvent(row)
	if err != nil {
		t.Fatalf("scanEvent() error = %v", err)
	}
	if event.ID != "activity-id" || event.Actor == nil || event.Actor.ID != "member-id" ||
		event.Actor.Name == nil || *event.Actor.Name != "Alex" || event.Data["changes"] == nil {
		t.Errorf("event = %#v", event)
	}

	if _, err := scanEvent(activityRow{err: pgx.ErrNoRows}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("not-found error = %v", err)
	}
	if _, err := scanEvent(activityRow{err: errors.New("scan failed")}); err == nil ||
		!strings.Contains(err.Error(), "scan issue activity") {
		t.Errorf("scan error = %v", err)
	}
	invalid := row
	invalid.values = append([]any(nil), row.values...)
	invalid.values[4] = []byte(`{"broken"`)
	if _, err := scanEvent(invalid); err == nil || !strings.Contains(err.Error(), "decode issue activity data") {
		t.Errorf("decode error = %v", err)
	}
}

func TestGetIssueScopesQueryAndWrapsErrors(t *testing.T) {
	t.Parallel()
	query := &activityQuerier{row: activityRow{values: []any{
		"activity-id", "issue-id", "issue.comment_created", VisibilityInternal,
		[]byte(`{}`), time.Now(), nil, nil, nil,
	}}}
	event, err := GetIssue(context.Background(), query, "tenant-id", "issue-id", "activity-id")
	if err != nil || event.ID != "activity-id" {
		t.Fatalf("GetIssue() = %#v, %v", event, err)
	}
	if !strings.Contains(query.statement, "activity.tenant_id = $1") || len(query.arguments) != 3 {
		t.Errorf("query = %q, arguments = %#v", query.statement, query.arguments)
	}
	query.row = activityRow{err: pgx.ErrNoRows}
	if _, err := GetIssue(context.Background(), query, "tenant-id", "issue-id", "missing"); err == nil ||
		!strings.Contains(err.Error(), "get issue activity") {
		t.Errorf("GetIssue() error = %v", err)
	}
}

func TestListIssueReturnsOrderedEventsAndWrapsRowFailures(t *testing.T) {
	t.Parallel()
	createdAt := time.Date(2026, 7, 23, 1, 0, 0, 0, time.UTC)
	query := &activityQuerier{rows: &activityRows{rows: []activityRow{
		{values: []any{"first", "issue-id", "issue.created", VisibilityInternal, []byte(`{}`), createdAt, nil, nil, nil}},
		{values: []any{"second", "issue-id", "reporter.responded", VisibilityReporter, []byte(`{"response":"context"}`), createdAt.Add(time.Minute), nil, nil, nil}},
	}}}
	events, err := ListIssue(context.Background(), query, "tenant-id", "issue-id")
	if err != nil || len(events) != 2 || events[0].ID != "first" || events[1].ID != "second" {
		t.Fatalf("ListIssue() = %#v, %v", events, err)
	}
	if !strings.Contains(query.statement, "ORDER BY activity.created_at, activity.id") ||
		len(query.arguments) != 2 {
		t.Errorf("query = %q, arguments = %#v", query.statement, query.arguments)
	}

	query.queryError = errors.New("database unavailable")
	if _, err := ListIssue(context.Background(), query, "tenant-id", "issue-id"); err == nil ||
		!strings.Contains(err.Error(), "list issue activities") {
		t.Errorf("query error = %v", err)
	}
	query.queryError = nil
	query.rows = &activityRows{rows: []activityRow{{err: errors.New("scan failed")}}}
	if _, err := ListIssue(context.Background(), query, "tenant-id", "issue-id"); err == nil ||
		!strings.Contains(err.Error(), "scan issue activity") {
		t.Errorf("scan error = %v", err)
	}
	query.rows = &activityRows{terminalError: errors.New("connection interrupted")}
	if _, err := ListIssue(context.Background(), query, "tenant-id", "issue-id"); err == nil ||
		!strings.Contains(err.Error(), "iterate issue activities") {
		t.Errorf("iteration error = %v", err)
	}
}

type activityQuerier struct {
	statement  string
	arguments  []any
	execError  error
	row        pgx.Row
	rows       pgx.Rows
	queryError error
}

func (q *activityQuerier) Exec(_ context.Context, statement string, arguments ...any) (pgconn.CommandTag, error) {
	q.statement, q.arguments = statement, arguments
	return pgconn.CommandTag{}, q.execError
}

func (q *activityQuerier) Query(_ context.Context, statement string, arguments ...any) (pgx.Rows, error) {
	q.statement, q.arguments = statement, arguments
	return q.rows, q.queryError
}

func (q *activityQuerier) QueryRow(_ context.Context, statement string, arguments ...any) pgx.Row {
	q.statement, q.arguments = statement, arguments
	return q.row
}

type activityRow struct {
	values []any
	err    error
}

type activityRows struct {
	rows          []activityRow
	index         int
	current       *activityRow
	closed        bool
	terminalError error
}

func (rows *activityRows) Close()                                  { rows.closed = true }
func (rows *activityRows) Err() error                              { return rows.terminalError }
func (*activityRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (*activityRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (rows *activityRows) Next() bool {
	if rows.index >= len(rows.rows) {
		rows.closed = true
		return false
	}
	rows.current = &rows.rows[rows.index]
	rows.index++
	return true
}
func (rows *activityRows) Scan(destinations ...any) error {
	if rows.current == nil {
		return errors.New("Scan called before Next")
	}
	return rows.current.Scan(destinations...)
}
func (*activityRows) Values() ([]any, error) { return nil, errors.New("unexpected Values call") }
func (*activityRows) RawValues() [][]byte    { return nil }
func (*activityRows) Conn() *pgx.Conn        { return nil }

func (row activityRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != len(row.values) {
		return errors.New("unexpected scan destination count")
	}
	for index, value := range row.values {
		switch destination := destinations[index].(type) {
		case *string:
			*destination = value.(string)
		case *[]byte:
			*destination = value.([]byte)
		case *time.Time:
			*destination = value.(time.Time)
		case **string:
			if value == nil {
				*destination = nil
			} else {
				*destination = value.(*string)
			}
		default:
			return errors.New("unexpected scan destination type")
		}
	}
	return nil
}

func stringPointer(value string) *string {
	return &value
}
