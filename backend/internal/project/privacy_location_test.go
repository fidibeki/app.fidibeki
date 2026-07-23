package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type privacyLocationContract struct {
	SchemaVersion int `json:"schema_version"`
	Projections   []struct {
		Name   string     `json:"name"`
		URL    string     `json:"url"`
		Rule   WidgetRule `json:"rule"`
		Valid  bool       `json:"valid"`
		Target string     `json:"target"`
		Source string     `json:"source"`
	} `json:"projections"`
}

func TestProjectPrivacySafeLocationUsesSharedContract(t *testing.T) {
	t.Parallel()
	contract := loadPrivacyLocationContract(t)
	for _, testCase := range contract.Projections {
		t.Run(testCase.Name, func(t *testing.T) {
			t.Parallel()
			location, err := ProjectPrivacySafeLocation(testCase.URL, testCase.Rule)
			if !testCase.Valid {
				if err == nil {
					t.Fatalf("ProjectPrivacySafeLocation(%q) = %#v, want error", testCase.URL, location)
				}
				return
			}
			if err != nil {
				t.Fatalf("ProjectPrivacySafeLocation(%q) error = %v", testCase.URL, err)
			}
			if location.Target != testCase.Target || string(location.Source) != testCase.Source {
				t.Errorf("location = %#v, want %q/%q", location, testCase.Target, testCase.Source)
			}
		})
	}
}

func loadPrivacyLocationContract(t *testing.T) privacyLocationContract {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve privacy location contract path")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../contracts/privacy-location.json"))
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read privacy location contract: %v", err)
	}
	var contract privacyLocationContract
	if err := json.Unmarshal(contents, &contract); err != nil {
		t.Fatalf("decode privacy location contract: %v", err)
	}
	if contract.SchemaVersion != 1 {
		t.Fatalf("privacy location schema version = %d, want 1", contract.SchemaVersion)
	}
	return contract
}
