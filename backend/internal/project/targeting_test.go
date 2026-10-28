package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
)

type targetingContract struct {
	SchemaVersion  int `json:"schema_version"`
	CanonicalPages []struct {
		Name      string `json:"name"`
		Input     string `json:"input"`
		Valid     bool   `json:"valid"`
		Canonical string `json:"canonical"`
		Pathname  string `json:"pathname"`
	} `json:"canonical_pages"`
	ConditionMatches []struct {
		Name      string                         `json:"name"`
		URL       string                         `json:"url"`
		Condition WidgetRuleTargetConditionInput `json:"condition"`
		Matches   bool                           `json:"matches"`
	} `json:"condition_matches"`
	RuleDecisions []struct {
		Name              string                           `json:"name"`
		URL               string                           `json:"url"`
		IncludeConditions []WidgetRuleTargetConditionInput `json:"include_conditions"`
		ExcludeConditions []WidgetRuleTargetConditionInput `json:"exclude_conditions"`
		Eligible          bool                             `json:"eligible"`
	} `json:"rule_decisions"`
	InvalidRules []struct {
		Name              string                           `json:"name"`
		IncludeConditions []WidgetRuleTargetConditionInput `json:"include_conditions"`
		ExcludeConditions []WidgetRuleTargetConditionInput `json:"exclude_conditions"`
		Field             string                           `json:"field"`
	} `json:"invalid_rules"`
}

func TestGoRegexAndMatchingUseSharedTargetingContract(t *testing.T) {
	t.Parallel()
	contract := loadTargetingContract(t)
	for _, testCase := range contract.ConditionMatches {
		t.Run(testCase.Name, func(t *testing.T) {
			t.Parallel()
			if got := contractConditionMatches(t, testCase.Condition, testCase.URL); got != testCase.Matches {
				t.Errorf("condition matches = %t, want %t", got, testCase.Matches)
			}
		})
	}
	for _, testCase := range contract.RuleDecisions {
		t.Run(testCase.Name, func(t *testing.T) {
			t.Parallel()
			included := false
			for _, condition := range testCase.IncludeConditions {
				included = included || contractConditionMatches(t, condition, testCase.URL)
			}
			excluded := false
			for _, condition := range testCase.ExcludeConditions {
				excluded = excluded || contractConditionMatches(t, condition, testCase.URL)
			}
			if got := included && !excluded; got != testCase.Eligible {
				t.Errorf("rule eligible = %t, want %t", got, testCase.Eligible)
			}
		})
	}
}

func TestCanonicalizePageUsesSharedTargetingContract(t *testing.T) {
	t.Parallel()
	contract := loadTargetingContract(t)
	for _, testCase := range contract.CanonicalPages {
		t.Run(testCase.Name, func(t *testing.T) {
			t.Parallel()
			page, err := CanonicalizePage(testCase.Input)
			if !testCase.Valid {
				if err == nil {
					t.Fatalf("CanonicalizePage(%q) = %#v, want error", testCase.Input, page)
				}
				return
			}
			if err != nil {
				t.Fatalf("CanonicalizePage(%q) error = %v", testCase.Input, err)
			}
			if page.Target != testCase.Canonical || page.Pathname != testCase.Pathname {
				t.Errorf("CanonicalizePage(%q) = %#v, want %q/%q", testCase.Input, page, testCase.Canonical, testCase.Pathname)
			}
		})
	}
}

func TestValidateWidgetRuleUsesSharedInvalidTargetingContract(t *testing.T) {
	t.Parallel()
	contract := loadTargetingContract(t)
	for _, testCase := range contract.InvalidRules {
		t.Run(testCase.Name, func(t *testing.T) {
			t.Parallel()
			_, fields := ValidateWidgetRule(WidgetRuleInput{
				Name: "Targeting contract", TriggerType: TriggerExitIntent,
				IncludeConditions: testCase.IncludeConditions,
				ExcludeConditions: testCase.ExcludeConditions,
			}, tenant.Tenant{Plan: tenant.PlanStarter})
			if fields[testCase.Field] == "" {
				t.Errorf("fields[%q] missing from %#v", testCase.Field, fields)
			}
		})
	}
}

func TestValidateWidgetRuleNormalizesTargetConditionsAndLegacyProjection(t *testing.T) {
	t.Parallel()
	exact := " HTTPS://Example.COM:443/chart///?asset=BAI#price "
	prefix := "https://example.com/account/"
	values, fields := ValidateWidgetRule(WidgetRuleInput{
		Name: "Charts", TriggerType: TriggerExitIntent,
		IncludeConditions: []WidgetRuleTargetConditionInput{
			{MatcherType: MatcherURLExact, MatcherValue: &exact},
			{MatcherType: MatcherURLPrefix, MatcherValue: &prefix},
		},
		ExcludeConditions: []WidgetRuleTargetConditionInput{
			{MatcherType: MatcherURLPrefix, MatcherValue: &prefix},
		},
	}, tenant.Tenant{Plan: tenant.PlanStarter})
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if values.MatcherType != MatcherURLExact || values.MatcherValue == nil ||
		*values.MatcherValue != "https://example.com/chart" {
		t.Errorf("legacy projection = %q/%v", values.MatcherType, values.MatcherValue)
	}
	if got := values.IncludeConditions[1].MatcherValue; got == nil || *got != "https://example.com/account" {
		t.Errorf("normalized prefix = %v", got)
	}
}

func TestValidateWidgetRuleEnforcesTargetConditionLimits(t *testing.T) {
	t.Parallel()
	value := "https://example.com/docs"
	conditions := make([]WidgetRuleTargetConditionInput, 21)
	for index := range conditions {
		conditions[index] = WidgetRuleTargetConditionInput{MatcherType: MatcherURLExact, MatcherValue: &value}
	}
	_, includeFields := ValidateWidgetRule(WidgetRuleInput{
		Name: "Too many includes", TriggerType: TriggerExitIntent,
		IncludeConditions: conditions, ExcludeConditions: []WidgetRuleTargetConditionInput{},
	}, tenant.Tenant{Plan: tenant.PlanStarter})
	if includeFields["include_conditions"] == "" {
		t.Errorf("include fields = %#v", includeFields)
	}
	_, excludeFields := ValidateWidgetRule(WidgetRuleInput{
		Name: "Too many excludes", TriggerType: TriggerExitIntent,
		IncludeConditions: []WidgetRuleTargetConditionInput{{MatcherType: MatcherURLExact, MatcherValue: &value}},
		ExcludeConditions: conditions,
	}, tenant.Tenant{Plan: tenant.PlanStarter})
	if excludeFields["exclude_conditions"] == "" {
		t.Errorf("exclude fields = %#v", excludeFields)
	}
}

func contractConditionMatches(t *testing.T, condition WidgetRuleTargetConditionInput, rawURL string) bool {
	t.Helper()
	page, err := CanonicalizePage(rawURL)
	if err != nil {
		t.Fatalf("canonicalize contract URL: %v", err)
	}
	switch condition.MatcherType {
	case MatcherAllPages:
		return true
	case MatcherURLExact, MatcherURLPrefix:
		if condition.MatcherValue == nil {
			return false
		}
		matcher, err := CanonicalizePage(*condition.MatcherValue)
		if err != nil {
			t.Fatalf("canonicalize contract matcher: %v", err)
		}
		if condition.MatcherType == MatcherURLExact {
			return matcher.Target == page.Target
		}
		return strings.HasPrefix(page.Target, matcher.Target)
	case MatcherURLRegex:
		if condition.MatcherValue == nil {
			return false
		}
		matcher, err := regexp.Compile(*condition.MatcherValue)
		if err != nil {
			t.Fatalf("compile contract matcher: %v", err)
		}
		return matcher.MatchString(page.Target)
	default:
		t.Fatalf("unknown contract matcher %q", condition.MatcherType)
		return false
	}
}

func loadTargetingContract(t *testing.T) targetingContract {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve targeting contract test path")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../contracts/widget-targeting.json"))
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read targeting contract: %v", err)
	}
	var contract targetingContract
	if err := json.Unmarshal(contents, &contract); err != nil {
		t.Fatalf("decode targeting contract: %v", err)
	}
	if contract.SchemaVersion != 1 {
		t.Fatalf("targeting contract schema version = %d, want 1", contract.SchemaVersion)
	}
	return contract
}
