package breachregister

import (
	"strings"
	"testing"
	"time"
)

func TestContainmentControlCatalogIsCompleteAndUnique(t *testing.T) {
	t.Parallel()

	if len(containmentControls) != 9 {
		t.Fatalf("containment control count = %d, want 9", len(containmentControls))
	}
	seen := make(map[ContainmentArea]struct{}, len(containmentControls))
	for _, control := range containmentControls {
		if _, duplicate := seen[control.Area]; duplicate {
			t.Fatalf("duplicate containment area %q", control.Area)
		}
		seen[control.Area] = struct{}{}
		if strings.TrimSpace(control.Title) == "" || len(control.Title) > 300 {
			t.Errorf("invalid title for %q: %q", control.Area, control.Title)
		}
	}
}

func TestNormalizeCreatePostIncidentActionInput(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 24, 10, 0, 0, 0, time.UTC)
	reference := "issue:FID-42"
	valid := CreatePostIncidentActionInput{
		Title:                "  Complete the credential rotation review  ",
		OwnerPlatformAdminID: "11111111-1111-4111-8111-111111111111",
		DueAt:                now.Add(24 * time.Hour), CorrectiveReference: &reference,
	}
	normalized, err := normalizeCreatePostIncidentActionInput(valid)
	if err != nil {
		t.Fatalf("normalizeCreatePostIncidentActionInput() error = %v", err)
	}
	if normalized.Title != "Complete the credential rotation review" ||
		normalized.DueAt.Location() != time.UTC {
		t.Fatalf("normalized action = %#v", normalized)
	}

	tests := map[string]func(CreatePostIncidentActionInput) CreatePostIncidentActionInput{
		"empty title": func(input CreatePostIncidentActionInput) CreatePostIncidentActionInput {
			input.Title = " "
			return input
		},
		"title too long": func(input CreatePostIncidentActionInput) CreatePostIncidentActionInput {
			input.Title = strings.Repeat("a", 301)
			return input
		},
		"title control character": func(input CreatePostIncidentActionInput) CreatePostIncidentActionInput {
			input.Title = "Complete review\x00"
			return input
		},
		"invalid owner": func(input CreatePostIncidentActionInput) CreatePostIncidentActionInput {
			input.OwnerPlatformAdminID = "not-a-uuid"
			return input
		},
		"zero due date": func(input CreatePostIncidentActionInput) CreatePostIncidentActionInput {
			input.DueAt = time.Time{}
			return input
		},
		"empty corrective reference": func(input CreatePostIncidentActionInput) CreatePostIncidentActionInput {
			empty := " "
			input.CorrectiveReference = &empty
			return input
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeCreatePostIncidentActionInput(mutate(valid)); err != ErrInvalidRequest {
				t.Errorf("normalizeCreatePostIncidentActionInput() error = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestNormalizeAppendActionRevisionInputEnforcesStateEvidence(t *testing.T) {
	t.Parallel()

	ownerID := "11111111-1111-4111-8111-111111111111"
	outcome := "Credential rotation completed and verified."
	valid := AppendActionRevisionInput{
		ExpectedRevision: 1, Status: ActionCompleted, OwnerPlatformAdminID: &ownerID,
		OutcomeSummary: &outcome, EvidenceReferences: []string{"ticket:INC-42"},
	}
	if _, err := normalizeAppendActionRevisionInput(valid); err != nil {
		t.Fatalf("normalizeAppendActionRevisionInput() error = %v", err)
	}

	tests := map[string]AppendActionRevisionInput{
		"zero revision":      {Status: ActionPending},
		"unknown status":     {ExpectedRevision: 1, Status: "unknown"},
		"in progress owner":  {ExpectedRevision: 1, Status: ActionInProgress},
		"completed owner":    {ExpectedRevision: 1, Status: ActionCompleted, OutcomeSummary: &outcome, EvidenceReferences: []string{"ref"}},
		"completed outcome":  {ExpectedRevision: 1, Status: ActionCompleted, OwnerPlatformAdminID: &ownerID, EvidenceReferences: []string{"ref"}},
		"completed evidence": {ExpectedRevision: 1, Status: ActionCompleted, OwnerPlatformAdminID: &ownerID, OutcomeSummary: &outcome},
		"not applicable owner": {
			ExpectedRevision: 1, Status: ActionNotApplicable, OutcomeSummary: &outcome,
		},
		"not applicable reason": {
			ExpectedRevision: 1, Status: ActionNotApplicable, OwnerPlatformAdminID: &ownerID,
		},
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeAppendActionRevisionInput(input); err != ErrInvalidRequest {
				t.Errorf("normalizeAppendActionRevisionInput() error = %v, want ErrInvalidRequest", err)
			}
		})
	}

	aggregateTooLarge := valid
	aggregateTooLarge.EvidenceReferences = make([]string, 14)
	for index := range aggregateTooLarge.EvidenceReferences {
		aggregateTooLarge.EvidenceReferences[index] = strings.Repeat("e", 300)
	}
	if _, err := normalizeAppendActionRevisionInput(aggregateTooLarge); err != ErrInvalidRequest {
		t.Errorf("aggregate evidence error = %v, want ErrInvalidRequest", err)
	}
}

func TestValidActionTransitionRequiresTerminalReopen(t *testing.T) {
	t.Parallel()

	for _, terminal := range []ActionStatus{ActionCompleted, ActionNotApplicable} {
		if !validActionTransition(terminal, ActionInProgress) {
			t.Errorf("%q -> in_progress rejected", terminal)
		}
		for _, invalid := range []ActionStatus{ActionPending, ActionCompleted, ActionNotApplicable} {
			if validActionTransition(terminal, invalid) {
				t.Errorf("%q -> %q accepted", terminal, invalid)
			}
		}
	}
	if !validActionTransition(ActionPending, ActionCompleted) {
		t.Error("pending -> completed rejected")
	}
}
