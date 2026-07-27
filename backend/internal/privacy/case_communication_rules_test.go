package privacy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestCaseCommunicationInputAndStateRules(t *testing.T) {
	t.Parallel()

	normalized, err := normalizeCaseCommunicationInput(CreateCaseCommunicationInput{
		CommunicationKind: CaseCommunicationProgress,
		SubjectLine:       "  Request update  ",
		MessageBody:       "  We are reviewing your request.  ",
	})
	if err != nil || normalized.SubjectLine != "Request update" ||
		normalized.MessageBody != "We are reviewing your request." {
		t.Fatalf("normalized input = %#v error=%v", normalized, err)
	}

	for _, input := range []CreateCaseCommunicationInput{
		{CommunicationKind: "unknown", SubjectLine: "Update", MessageBody: "Body"},
		{CommunicationKind: CaseCommunicationProgress, MessageBody: "Body"},
		{CommunicationKind: CaseCommunicationProgress, SubjectLine: "Update"},
	} {
		if _, err := normalizeCaseCommunicationInput(input); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("normalize invalid input %#v error = %v", input, err)
		}
	}

	verifiedAt := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		privacyCase PrivacyCase
		kind        CaseCommunicationKind
		wantErr     error
	}{
		{
			name: "unverified progress",
			privacyCase: PrivacyCase{
				Status: CaseReceived,
			},
			kind:    CaseCommunicationProgress,
			wantErr: ErrCaseCommunicationUnavailable,
		},
		{
			name: "active progress",
			privacyCase: PrivacyCase{
				Status: CaseInProgress, VerifiedAt: &verifiedAt,
			},
			kind: CaseCommunicationProgress,
		},
		{
			name: "terminal progress",
			privacyCase: PrivacyCase{
				Status: CaseCompleted, VerifiedAt: &verifiedAt,
			},
			kind:    CaseCommunicationProgress,
			wantErr: ErrCaseCommunicationUnavailable,
		},
		{
			name: "expected clarification",
			privacyCase: PrivacyCase{
				Status: CaseAwaitingClarification, VerifiedAt: &verifiedAt,
			},
			kind: CaseCommunicationClarification,
		},
		{
			name: "unexpected clarification",
			privacyCase: PrivacyCase{
				Status: CaseInProgress, VerifiedAt: &verifiedAt,
			},
			kind:    CaseCommunicationClarification,
			wantErr: ErrCaseCommunicationUnavailable,
		},
		{
			name: "terminal decision",
			privacyCase: PrivacyCase{
				Status: CaseRefused, VerifiedAt: &verifiedAt,
			},
			kind: CaseCommunicationDecision,
		},
		{
			name: "early decision",
			privacyCase: PrivacyCase{
				Status: CaseInProgress, VerifiedAt: &verifiedAt,
			},
			kind:    CaseCommunicationDecision,
			wantErr: ErrCaseCommunicationUnavailable,
		},
		{
			name: "unknown communication",
			privacyCase: PrivacyCase{
				Status: CaseInProgress, VerifiedAt: &verifiedAt,
			},
			kind:    "unknown",
			wantErr: ErrInvalidRequest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateCaseCommunicationState(test.privacyCase, test.kind)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("state validation error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestCaseCommunicationRecipientsEvidenceAndMessages(t *testing.T) {
	t.Parallel()

	emailAddress := "  Reporter@Example.com "
	recipient, err := caseCommunicationRecipient(
		context.Background(), privacyErrorQuerier{}, "tenant",
		PrivacyCase{SubjectType: SubjectReporter, SubjectEmail: &emailAddress},
	)
	if err != nil || recipient != "reporter@example.com" {
		t.Fatalf("reporter recipient = %q error=%v", recipient, err)
	}
	for _, privacyCase := range []PrivacyCase{
		{SubjectType: SubjectReporter},
		{SubjectType: SubjectReporter, SubjectEmail: stringPointer("invalid")},
		{SubjectType: SubjectMember},
	} {
		if _, err := caseCommunicationRecipient(
			context.Background(), privacyErrorQuerier{}, "tenant", privacyCase,
		); !errors.Is(err, ErrCaseCommunicationUnavailable) {
			t.Fatalf("unavailable recipient %#v error = %v", privacyCase, err)
		}
	}
	memberID := "member"
	if _, err := caseCommunicationRecipient(
		context.Background(), privacyErrorQuerier{err: pgx.ErrNoRows}, "tenant",
		PrivacyCase{SubjectType: SubjectMember, SubjectMemberID: &memberID},
	); !errors.Is(err, ErrCaseCommunicationUnavailable) {
		t.Fatalf("missing member recipient error = %v", err)
	}
	databaseError := errors.New("database unavailable")
	if _, err := caseCommunicationRecipient(
		context.Background(), privacyErrorQuerier{err: databaseError}, "tenant",
		PrivacyCase{SubjectType: SubjectMember, SubjectMemberID: &memberID},
	); !errors.Is(err, databaseError) {
		t.Fatalf("member recipient database error = %v", err)
	}

	evidence := summarizeErasureReview([]ReviewItem{
		{Status: ReviewPending},
		{Status: ReviewRedacted},
		{Status: ReviewDeleted},
		{Status: ReviewNotPersonalData},
		{Status: ReviewRetainedException},
		{Status: "future_status"},
	})
	if evidence.Pending != 1 || evidence.Redacted != 1 || evidence.Deleted != 1 ||
		evidence.NotPersonalData != 1 || evidence.RetainedException != 1 ||
		len(evidence.Items) != 6 {
		t.Fatalf("erasure review evidence = %#v", evidence)
	}

	for _, test := range []struct {
		kind  CaseCommunicationKind
		label string
	}{
		{kind: CaseCommunicationProgress, label: "Progress update"},
		{kind: CaseCommunicationClarification, label: "Information request"},
		{kind: CaseCommunicationDecision, label: "Decision notice"},
	} {
		message, err := caseCommunicationMessage(
			CaseCommunicationNotification{
				TenantName: "Workspace", CasePublicID: "PRIV-42",
				Communication: CaseCommunication{
					ID: "communication", CommunicationKind: test.kind,
					RecipientEmail: "reporter@example.com",
					SubjectLine:    "Request update", MessageBody: "Current status.",
				},
			},
			CaseCommunicationHandler{
				From:          "Fidibeki <privacy@fidibeki.com>",
				PublicBaseURL: "https://api.fidibeki.com",
			},
		)
		if err != nil || !strings.Contains(message.Text, test.label) ||
			!strings.Contains(message.Text, "PRIV-42") {
			t.Fatalf("%s message = %#v error=%v", test.kind, message, err)
		}
	}
	if _, err := caseCommunicationMessage(
		CaseCommunicationNotification{},
		CaseCommunicationHandler{PublicBaseURL: "://invalid"},
	); err == nil {
		t.Fatal("invalid public base URL message error = nil")
	}

	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.FixedZone("WEST", 3600))
	if got := (CaseCommunicationHandler{Now: func() time.Time { return now }}).now(); !got.Equal(now.UTC()) {
		t.Fatalf("configured now = %s", got)
	}
	if got := (CaseCommunicationHandler{}).now(); got.Location() != time.UTC {
		t.Fatalf("default now location = %v", got.Location())
	}
}

func TestPrivacyCaseParsingAndResourceRestrictionRouting(t *testing.T) {
	t.Parallel()

	if rightType, err := ParseRightType(" access "); err != nil || rightType != RightAccess {
		t.Fatalf("parsed right type = %q error=%v", rightType, err)
	}
	if _, err := ParseRightType("unknown"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid right type error = %v", err)
	}
	if role, err := ParseControllerRole(" controller "); err != nil || role != RoleController {
		t.Fatalf("parsed controller role = %q error=%v", role, err)
	}
	if _, err := ParseControllerRole("unknown"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid controller role error = %v", err)
	}

	for _, resourceType := range []string{"feedback", "issue", "unknown"} {
		err := EnsureResourceProcessingAllowed(
			context.Background(), nil, "tenant", resourceType, "resource", PurposeIntegration,
		)
		if !errors.Is(err, ErrProcessingRestricted) {
			t.Fatalf("%s resource restriction error = %v", resourceType, err)
		}
	}
}

func stringPointer(value string) *string {
	return &value
}
