package replay

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestReplayEventContract pins the shared client/server event contract: every event in
// contracts/replay-events.json is emitted by rrweb, passes the widget sanitizer unchanged
// (widget/tests/unit/replay-contract.test.ts), and must be accepted by this verifier. A
// failure here means the allow-lists drifted and production finalization would 409.
func TestReplayEventContract(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../contracts/replay-events.json")
	if err != nil {
		t.Fatalf("read replay event contract: %v", err)
	}
	var contract struct {
		PageURL string `json:"page_url"`
		Events  []struct {
			Name  string          `json:"name"`
			Event json.RawMessage `json:"event"`
		} `json:"events"`
	}
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatalf("decode replay event contract: %v", err)
	}
	if len(contract.Events) == 0 {
		t.Fatal("replay event contract has no events")
	}
	serialized := make([]string, 0, len(contract.Events))
	for _, entry := range contract.Events {
		if !validRRWebEvent(entry.Event, contract.PageURL) {
			t.Errorf("verifier rejects contract event %q", entry.Name)
		}
		serialized = append(serialized, string(entry.Event))
	}
	body := "[" + strings.Join(serialized, ",") + "]"
	_, err = decodeVerifiedBatch(strings.NewReader(body), Batch{
		Index: 0, SizeBytes: int64(len(body)), EventsCount: len(contract.Events),
	}, contract.PageURL, nil)
	if err != nil {
		t.Fatalf("decodeVerifiedBatch() rejects the contract batch: %v", err)
	}
}

func TestReplayPresentationContract(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../contracts/replay-presentation.json")
	if err != nil {
		t.Fatalf("read replay presentation contract: %v", err)
	}
	var contract struct {
		Valid []struct {
			Name   string `json:"name"`
			Style  string `json:"style"`
			Inline bool   `json:"inline"`
		} `json:"valid"`
		Invalid []struct {
			Name   string `json:"name"`
			Style  string `json:"style"`
			Inline bool   `json:"inline"`
		} `json:"invalid"`
	}
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatalf("decode replay presentation contract: %v", err)
	}
	if len(contract.Valid) == 0 || len(contract.Invalid) == 0 {
		t.Fatal("replay presentation contract needs valid and invalid cases")
	}
	for _, entry := range contract.Valid {
		if !validReplayCSS(entry.Style, entry.Inline) {
			t.Errorf("validator rejects valid presentation %q: %q", entry.Name, entry.Style)
		}
	}
	for _, entry := range contract.Invalid {
		if validReplayCSS(entry.Style, entry.Inline) {
			t.Errorf("validator accepts invalid presentation %q: %q", entry.Name, entry.Style)
		}
	}
	if validReplayCSS(strings.Repeat("x", 1024*1024+1), true) {
		t.Error("validator accepts an oversized stylesheet")
	}
}
