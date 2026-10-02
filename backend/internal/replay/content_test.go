package replay

import (
	"encoding/json"
	"os"
	"testing"
)

func TestReplayNativeCSSPrivacy(t *testing.T) {
	for _, value := range []string{
		`.card { --accent: red; color: var(--accent); border: 0; font-family: Inter, sans-serif; }`,
		`@font-face { font-family: Inter; src: url("https://cdn.example.com/fonts/inter.woff2"); }`,
		`@media (width >= 640px) { .card { display: grid; } }`,
		`.w-1\/2:hover { width: 50%; }`,
		`.icon { clip-path: url(#clip); }`,
		`.card::before { content: ""; border: 1px solid red; }`,
	} {
		if !validReplayCSS(value, false) {
			t.Errorf("rejected presentation CSS: %s", value)
		}
	}
	for _, value := range []string{
		`.card { content: "Jane Doe"; }`,
		`.card { background: url("https://example.com/avatar/jane.png"); }`,
		`.card { --image: url("https://example.com/avatar/jane.png"); }`,
		`.card { background: u\72l("https://example.com/avatar/jane.png"); }`,
		`@import "https://example.com/private.css";`,
		`.card { --email: "jane@example.com"; }`,
		`.card { background: image-set("https://example.com/private.png" 1x); }`,
	} {
		if validReplayCSS(value, false) {
			t.Errorf("accepted private CSS: %s", value)
		}
	}
}

func TestReplayPrivateRegionsAndInputs(t *testing.T) {
	for _, body := range []string{
		`{"type":2,"timestamp":1,"data":{"node":{"type":2,"id":1,"tagName":"div","attributes":{"data-fidibeki-mask":"","data-fidibeki-safe-text":""},"childNodes":[{"type":3,"id":2,"textContent":"Jane Doe"}]}}}`,
		`{"type":2,"timestamp":1,"data":{"node":{"type":2,"id":1,"tagName":"input","attributes":{"value":"secret"},"childNodes":[]}}}`,
		`{"type":3,"timestamp":1,"data":{"source":0,"texts":[{"id":2,"value":"Call +351 912 345 678"}]}}`,
	} {
		if validRRWebEvent(json.RawMessage(body), "https://example.com/") {
			t.Errorf("accepted private content: %s", body)
		}
	}
}

// Set this to a Playwright captured-replay.json artifact to verify real browser
// uploads with the same validator used by the batch and merge workers.
func TestReplayBrowserCapture(t *testing.T) {
	path := os.Getenv("FIDIBEKI_REPLAY_FIXTURE")
	if path == "" {
		t.Skip("no browser capture artifact supplied")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var events []json.RawMessage
	if err := json.Unmarshal(raw, &events); err != nil {
		t.Fatal(err)
	}
	for index, event := range events {
		if !validRRWebEvent(event, "http://127.0.0.1:4173/sample") {
			t.Errorf("rejected browser event %d: %s", index, event)
		}
	}
}
