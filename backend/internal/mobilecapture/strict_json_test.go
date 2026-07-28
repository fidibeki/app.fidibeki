package mobilecapture

import (
	"strings"
	"testing"
)

func TestRejectDuplicateJSONKeysAtEveryNestingLevel(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		`{"client_feedback_id":"one","client_feedback_id":"two"}`,
		`{"responses":{"rating":1,"rating":2}}`,
		`{"attachments":[{"kind":"screenshot","kind":"video"}]}`,
		`{"consent":{"granted":true,"nested":{"locale":"en","locale":"pt"}}}`,
	} {
		if err := rejectDuplicateJSONKeys([]byte(input)); err == nil {
			t.Errorf("rejectDuplicateJSONKeys(%s) error = nil", input)
		}
	}
}

func TestRejectDuplicateJSONKeysAcceptsDistinctObjectsAndBoundsDepth(t *testing.T) {
	t.Parallel()
	if err := rejectDuplicateJSONKeys([]byte(
		`{"responses":{"rating":1},"attachments":[{"kind":"screenshot"},{"kind":"screenshot"}]}`,
	)); err != nil {
		t.Fatalf("distinct object keys rejected: %v", err)
	}
	deep := strings.Repeat(`{"a":`, maximumJSONDepth+2) + `null` +
		strings.Repeat(`}`, maximumJSONDepth+2)
	if err := rejectDuplicateJSONKeys([]byte(deep)); err == nil {
		t.Fatal("excessive JSON nesting passed")
	}
}
