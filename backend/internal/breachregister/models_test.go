package breachregister

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func validCreateIncidentInput() CreateIncidentInput {
	return CreateIncidentInput{
		AwarenessAt: time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC), DetectionSource: DetectionInternalMonitoring,
		ControllerRole: RoleProcessor, AffectedEnvironments: []string{"staging"},
		AffectedSystemCategories: []string{"database"}, AffectedDataCategories: []string{"contact_info"},
		AffectedSubjectCategories: []string{"tenant_members"}, ConfidentialityImpact: true,
		ContainmentSummary: "Rotated the affected credential.",
		AffectedTenantIDs:  []string{"11111111-1111-1111-1111-111111111111"},
	}
}

func TestNormalizeCreateIncidentInputAcceptsValidInput(t *testing.T) {
	t.Parallel()
	normalized, err := normalizeCreateIncidentInput(validCreateIncidentInput())
	if err != nil {
		t.Fatalf("normalizeCreateIncidentInput() error = %v", err)
	}
	if normalized.ContainmentSummary != "Rotated the affected credential." {
		t.Errorf("containment summary = %q", normalized.ContainmentSummary)
	}
}

func TestNormalizeCreateIncidentInputRejectsInvalidFields(t *testing.T) {
	t.Parallel()

	tests := map[string]func(CreateIncidentInput) CreateIncidentInput{
		"unknown detection source": func(input CreateIncidentInput) CreateIncidentInput {
			input.DetectionSource = "not_real"
			return input
		},
		"unknown controller role": func(input CreateIncidentInput) CreateIncidentInput {
			input.ControllerRole = "vendor"
			return input
		},
		"zero awareness time": func(input CreateIncidentInput) CreateIncidentInput {
			input.AwarenessAt = time.Time{}
			return input
		},
		"no impact dimension": func(input CreateIncidentInput) CreateIncidentInput {
			input.ConfidentialityImpact = false
			input.IntegrityImpact = false
			input.AvailabilityImpact = false
			return input
		},
		"empty affected environments": func(input CreateIncidentInput) CreateIncidentInput {
			input.AffectedEnvironments = nil
			return input
		},
		"unknown affected environment": func(input CreateIncidentInput) CreateIncidentInput {
			input.AffectedEnvironments = []string{"moon_base"}
			return input
		},
		"unknown system category": func(input CreateIncidentInput) CreateIncidentInput {
			input.AffectedSystemCategories = []string{"time_machine"}
			return input
		},
		"unknown data category": func(input CreateIncidentInput) CreateIncidentInput {
			input.AffectedDataCategories = []string{"telepathy"}
			return input
		},
		"unknown subject category": func(input CreateIncidentInput) CreateIncidentInput {
			input.AffectedSubjectCategories = []string{"aliens"}
			return input
		},
		"empty containment summary": func(input CreateIncidentInput) CreateIncidentInput {
			input.ContainmentSummary = "   "
			return input
		},
		"containment summary too long": func(input CreateIncidentInput) CreateIncidentInput {
			input.ContainmentSummary = strings.Repeat("a", 4001)
			return input
		},
		"no affected tenants": func(input CreateIncidentInput) CreateIncidentInput {
			input.AffectedTenantIDs = nil
			return input
		},
		"malformed tenant id": func(input CreateIncidentInput) CreateIncidentInput {
			input.AffectedTenantIDs = []string{"not-a-uuid"}
			return input
		},
		"too many affected tenants": func(input CreateIncidentInput) CreateIncidentInput {
			ids := make([]string, maxAffectedTenants+1)
			for index := range ids {
				ids[index] = "11111111-1111-1111-1111-111111111111"
			}
			// distinct IDs so bound is exceeded rather than deduplicated away
			for index := range ids {
				ids[index] = hexTenantID(index)
			}
			input.AffectedTenantIDs = ids
			return input
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeCreateIncidentInput(mutate(validCreateIncidentInput())); err != ErrInvalidRequest {
				t.Errorf("normalizeCreateIncidentInput() error = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func hexTenantID(seed int) string {
	digits := []rune("0123456789abcdef")
	suffix := make([]rune, 12)
	value := seed
	for index := len(suffix) - 1; index >= 0; index-- {
		suffix[index] = digits[value%16]
		value /= 16
	}
	return "11111111-1111-1111-1111-" + string(suffix)
}

func TestNormalizeCreateIncidentInputDeduplicatesTenants(t *testing.T) {
	t.Parallel()
	input := validCreateIncidentInput()
	input.AffectedTenantIDs = []string{
		"11111111-1111-1111-1111-111111111111", "11111111-1111-1111-1111-111111111111",
	}
	normalized, err := normalizeCreateIncidentInput(input)
	if err != nil {
		t.Fatalf("normalizeCreateIncidentInput() error = %v", err)
	}
	if len(normalized.AffectedTenantIDs) != 1 {
		t.Errorf("affected tenant ids = %#v, want one deduplicated entry", normalized.AffectedTenantIDs)
	}
}

func TestNormalizeAppendChronologyEntryInputValidatesBounds(t *testing.T) {
	t.Parallel()

	valid := AppendChronologyEntryInput{
		EntryKind: EntryContained, Summary: "Rotated the credential.",
		EvidenceReferences: []string{"ticket:INC-1"},
	}
	if _, err := normalizeAppendChronologyEntryInput(valid); err != nil {
		t.Fatalf("normalizeAppendChronologyEntryInput() error = %v", err)
	}

	tests := map[string]AppendChronologyEntryInput{
		"unknown entry kind": {EntryKind: "not_real", Summary: "Contained."},
		"empty summary":      {EntryKind: EntryContained, Summary: "   "},
		"summary too long":   {EntryKind: EntryContained, Summary: strings.Repeat("a", 4001)},
		"empty evidence reference": {
			EntryKind: EntryContained, Summary: "Contained.", EvidenceReferences: []string{"  "},
		},
		"evidence reference too long": {
			EntryKind: EntryContained, Summary: "Contained.",
			EvidenceReferences: []string{strings.Repeat("a", 301)},
		},
		"evidence reference with control character": {
			EntryKind: EntryContained, Summary: "Contained.",
			EvidenceReferences: []string{"ticket:INC-1\x00"},
		},
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeAppendChronologyEntryInput(input); err != ErrInvalidRequest {
				t.Errorf("normalizeAppendChronologyEntryInput() error = %v, want ErrInvalidRequest", err)
			}
		})
	}

	tooManyReferences := AppendChronologyEntryInput{EntryKind: EntryContained, Summary: "Contained."}
	for range maxEvidenceReferences + 1 {
		tooManyReferences.EvidenceReferences = append(tooManyReferences.EvidenceReferences, "ref")
	}
	if _, err := normalizeAppendChronologyEntryInput(tooManyReferences); err != ErrInvalidRequest {
		t.Errorf("normalizeAppendChronologyEntryInput() error = %v, want ErrInvalidRequest", err)
	}
}

func TestIncidentJSONNeverExposesTenantContent(t *testing.T) {
	t.Parallel()
	// Incident must only ever carry a bare tenant ID reference, never a
	// nested tenant object with name/slug/billing content. Marshaling and
	// inspecting the actual JSON keys (rather than the Go struct) catches a
	// future edit that embeds tenant business data here.
	incident := Incident{AffectedTenantIDs: []string{"11111111-1111-1111-1111-111111111111"}}
	encoded, err := json.Marshal(incident)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	tenantIDs, ok := decoded["affected_tenant_ids"].([]any)
	if !ok || len(tenantIDs) != 1 {
		t.Fatalf("affected_tenant_ids = %#v", decoded["affected_tenant_ids"])
	}
	if _, isString := tenantIDs[0].(string); !isString {
		t.Fatalf("affected tenant entry = %#v, want a bare string reference", tenantIDs[0])
	}
	for forbidden := range map[string]struct{}{
		"tenant_name": {}, "name": {}, "slug": {}, "billing_email": {}, "tenants": {},
	} {
		if _, present := decoded[forbidden]; present {
			t.Errorf("incident JSON unexpectedly carries %q", forbidden)
		}
	}
}
