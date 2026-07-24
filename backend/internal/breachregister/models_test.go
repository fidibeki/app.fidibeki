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

func validCreateAssessmentInput() CreateAssessmentInput {
	return CreateAssessmentInput{
		AuthorityNotificationDecision:     AuthorityNotificationAssessmentPending,
		AuthorityNotificationReasoning:    "Awaiting confirmation of exfiltration scope.",
		HighRiskToIndividuals:             RiskAssessmentPending,
		HighRiskReasoning:                 "Risk to data subjects not yet determined.",
		DataSubjectCommunicationDecision:  CommunicationAssessmentPending,
		DataSubjectCommunicationReasoning: "Depends on the risk assessment above.",
	}
}

func TestNormalizeCreateAssessmentInputAcceptsValidInput(t *testing.T) {
	t.Parallel()
	if _, err := normalizeCreateAssessmentInput(validCreateAssessmentInput()); err != nil {
		t.Fatalf("normalizeCreateAssessmentInput() error = %v", err)
	}
}

func TestNormalizeCreateAssessmentInputRejectsInvalidFields(t *testing.T) {
	t.Parallel()

	subjectsMin, subjectsMax := int64(10), int64(20)

	tests := map[string]func(CreateAssessmentInput) CreateAssessmentInput{
		"unknown authority decision": func(input CreateAssessmentInput) CreateAssessmentInput {
			input.AuthorityNotificationDecision = "not_real"
			return input
		},
		"unknown risk decision": func(input CreateAssessmentInput) CreateAssessmentInput {
			input.HighRiskToIndividuals = "not_real"
			return input
		},
		"unknown communication decision": func(input CreateAssessmentInput) CreateAssessmentInput {
			input.DataSubjectCommunicationDecision = "not_real"
			return input
		},
		"empty authority reasoning": func(input CreateAssessmentInput) CreateAssessmentInput {
			input.AuthorityNotificationReasoning = "   "
			return input
		},
		"authority reasoning too long": func(input CreateAssessmentInput) CreateAssessmentInput {
			input.AuthorityNotificationReasoning = strings.Repeat("a", 4001)
			return input
		},
		"empty high risk reasoning": func(input CreateAssessmentInput) CreateAssessmentInput {
			input.HighRiskReasoning = "   "
			return input
		},
		"empty communication reasoning": func(input CreateAssessmentInput) CreateAssessmentInput {
			input.DataSubjectCommunicationReasoning = "   "
			return input
		},
		"subject estimate min without max": func(input CreateAssessmentInput) CreateAssessmentInput {
			input.EstimatedAffectedSubjectsMin = &subjectsMin
			return input
		},
		"subject estimate max without min": func(input CreateAssessmentInput) CreateAssessmentInput {
			input.EstimatedAffectedSubjectsMax = &subjectsMax
			return input
		},
		"subject estimate min greater than max": func(input CreateAssessmentInput) CreateAssessmentInput {
			input.EstimatedAffectedSubjectsMin = &subjectsMax
			input.EstimatedAffectedSubjectsMax = &subjectsMin
			return input
		},
		"negative subject estimate": func(input CreateAssessmentInput) CreateAssessmentInput {
			negative := int64(-1)
			input.EstimatedAffectedSubjectsMin = &negative
			input.EstimatedAffectedSubjectsMax = &subjectsMax
			return input
		},
		"subject estimate exceeds bound": func(input CreateAssessmentInput) CreateAssessmentInput {
			tooLarge := int64(maxEstimatedCount + 1)
			input.EstimatedAffectedSubjectsMin = &subjectsMin
			input.EstimatedAffectedSubjectsMax = &tooLarge
			return input
		},
		"record estimate min without max": func(input CreateAssessmentInput) CreateAssessmentInput {
			input.EstimatedAffectedRecordsMin = &subjectsMin
			return input
		},
		"empty likely consequences": func(input CreateAssessmentInput) CreateAssessmentInput {
			empty := "   "
			input.LikelyConsequences = &empty
			return input
		},
		"likely consequences too long": func(input CreateAssessmentInput) CreateAssessmentInput {
			tooLong := strings.Repeat("a", 4001)
			input.LikelyConsequences = &tooLong
			return input
		},
		"empty recovery measures": func(input CreateAssessmentInput) CreateAssessmentInput {
			empty := "   "
			input.RecoveryMeasures = &empty
			return input
		},
		"too many evidence references": func(input CreateAssessmentInput) CreateAssessmentInput {
			for range maxEvidenceReferences + 1 {
				input.EvidenceReferences = append(input.EvidenceReferences, "ref")
			}
			return input
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeCreateAssessmentInput(mutate(validCreateAssessmentInput())); err != ErrInvalidRequest {
				t.Errorf("normalizeCreateAssessmentInput() error = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestNormalizeCreateAssessmentInputAcceptsBoundedEstimatesAndNotes(t *testing.T) {
	t.Parallel()

	subjectsMin, subjectsMax := int64(0), int64(1000)
	recordsMin, recordsMax := int64(0), int64(2000)
	consequences := "Possible unauthorized read access to contact details."
	recovery := "Forced re-authentication and rotated the leaked credential."

	input := validCreateAssessmentInput()
	input.EstimatedAffectedSubjectsMin = &subjectsMin
	input.EstimatedAffectedSubjectsMax = &subjectsMax
	input.EstimatedAffectedRecordsMin = &recordsMin
	input.EstimatedAffectedRecordsMax = &recordsMax
	input.LikelyConsequences = &consequences
	input.RecoveryMeasures = &recovery
	input.EvidenceReferences = []string{"ticket:INC-42"}

	normalized, err := normalizeCreateAssessmentInput(input)
	if err != nil {
		t.Fatalf("normalizeCreateAssessmentInput() error = %v", err)
	}
	if *normalized.EstimatedAffectedSubjectsMax != subjectsMax || *normalized.EstimatedAffectedRecordsMax != recordsMax {
		t.Errorf("normalized estimates = %#v", normalized)
	}
	if *normalized.LikelyConsequences != consequences || *normalized.RecoveryMeasures != recovery {
		t.Errorf("normalized notes = %#v", normalized)
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
