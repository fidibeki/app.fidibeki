package privacy

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemberCaseRectificationIsAuditedAndIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 24, 16, 0, 0, 0, time.UTC)
	tenantID, actorMemberID := seedPrivacyWorkspace(
		t, ctx, database, "member-rectification", "Rectification feedback",
	)
	var subjectMemberID string
	if err := database.QueryRow(ctx, `
		INSERT INTO tenant_members (
			tenant_id, email, name, locale, timezone, permissions, status
		) VALUES (
			$1, 'subject@member-rectification.test', 'Old name', 'en',
			'Europe/Lisbon', ARRAY['feedback:read'], 'active'
		)
		RETURNING id
	`, tenantID).Scan(&subjectMemberID); err != nil {
		t.Fatalf("create rectification subject: %v", err)
	}

	store := Store{Database: database}
	privacyCase, _, _, err := store.CreateCase(
		ctx, database, tenantID, actorMemberID, "member-rectification-case",
		CreateCaseInput{
			SubjectType:     SubjectMember,
			RightType:       RightRectification,
			ControllerRole:  RoleController,
			SubjectMemberID: subjectMemberID,
			OwnerMemberID:   actorMemberID,
		},
		now,
	)
	if err != nil {
		t.Fatalf("create member rectification case: %v", err)
	}

	input := RecordCaseRectificationInput{
		Changes: map[string]*string{
			"name":     pointerTo("  New name  "),
			"locale":   pointerTo("pt_pt"),
			"timezone": nil,
		},
		Reason: "Correct the verified member profile fields.",
	}
	rectification, created, err := store.ApplyCaseRectification(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"member-rectification-apply", input, now.Add(time.Minute),
	)
	if err != nil || !created {
		t.Fatalf("ApplyCaseRectification() = %#v created=%t error=%v",
			rectification, created, err)
	}
	if rectification.SubjectMemberID != subjectMemberID ||
		rectification.AppliedByMemberID != actorMemberID ||
		valueAt(rectification.Changes, "name") != "New name" ||
		valueAt(rectification.Changes, "locale") != "pt-PT" ||
		rectification.Changes["timezone"] != nil ||
		valueAt(rectification.PreviousValues, "name") != "Old name" ||
		valueAt(rectification.PreviousValues, "timezone") != "Europe/Lisbon" {
		t.Fatalf("rectification = %#v", rectification)
	}

	var name, locale string
	var timezone *string
	if err := database.QueryRow(ctx, `
		SELECT name, locale, timezone
		FROM tenant_members
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, subjectMemberID).Scan(&name, &locale, &timezone); err != nil {
		t.Fatalf("read rectified member: %v", err)
	}
	if name != "New name" || locale != "pt-PT" || timezone != nil {
		t.Fatalf("rectified profile = name=%q locale=%q timezone=%v", name, locale, timezone)
	}

	replayed, created, err := store.ApplyCaseRectification(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"member-rectification-apply", input, now.Add(2*time.Minute),
	)
	if err != nil || created || replayed.ID != rectification.ID {
		t.Fatalf("idempotent rectification = %#v created=%t error=%v", replayed, created, err)
	}
	if _, _, err := store.ApplyCaseRectification(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"member-rectification-apply",
		RecordCaseRectificationInput{
			Changes: map[string]*string{"name": pointerTo("Different")},
			Reason:  input.Reason,
		},
		now.Add(3*time.Minute),
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting rectification error = %v", err)
	}

	loaded, err := store.GetCase(ctx, database, tenantID, privacyCase.ID)
	if err != nil || len(loaded.Rectifications) != 1 ||
		loaded.Rectifications[0].ID != rectification.ID {
		t.Fatalf("rectification case history = %#v error=%v", loaded.Rectifications, err)
	}
	var eventCount, auditCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM privacy_case_events
		WHERE tenant_id = $1 AND privacy_case_id = $2
		  AND event_type = 'privacy_case.rectification_applied'
	`, tenantID, privacyCase.ID).Scan(&eventCount); err != nil {
		t.Fatalf("count rectification events: %v", err)
	}
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'privacy.member_rectification_applied'
		  AND actor_member_id = $2 AND subject_member_id = $3
	`, tenantID, actorMemberID, subjectMemberID).Scan(&auditCount); err != nil {
		t.Fatalf("count rectification audits: %v", err)
	}
	if eventCount != 1 || auditCount != 1 {
		t.Fatalf("rectification evidence event=%d audit=%d, want 1/1", eventCount, auditCount)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO privacy_case_rectifications (
			tenant_id, privacy_case_id, subject_member_id, changes,
			previous_values, reason, applied_by_member_id, applied_at,
			idempotency_key, request_fingerprint
		) VALUES (
			$1, $2, $3, '{"email":"changed@example.test"}',
			'{"email":"old@example.test"}', 'Bypass the supported field allowlist.',
			$4, $5, 'unsupported-direct-field', decode(repeat('00', 32), 'hex')
		)
	`, tenantID, privacyCase.ID, subjectMemberID, actorMemberID, now); err == nil {
		t.Fatal("rectification evidence accepted an unsupported direct field")
	}
	if _, err := database.Exec(ctx, `
		UPDATE privacy_case_rectifications SET reason = 'rewritten'
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, rectification.ID); err == nil {
		t.Fatal("rectification evidence was mutable")
	}
}

func TestMemberCaseRectificationRejectsInvalidScope(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 24, 17, 0, 0, 0, time.UTC)
	tenantID, actorMemberID := seedPrivacyWorkspace(
		t, ctx, database, "rectification-scope", "Scope feedback",
	)
	otherTenantID, otherActorMemberID := seedPrivacyWorkspace(
		t, ctx, database, "rectification-other", "Other feedback",
	)
	store := Store{Database: database}
	privacyCase, _, _, err := store.CreateCase(
		ctx, database, tenantID, actorMemberID, "member-rectification-scope",
		CreateCaseInput{
			SubjectType:     SubjectMember,
			RightType:       RightRectification,
			ControllerRole:  RoleController,
			SubjectMemberID: actorMemberID,
			OwnerMemberID:   actorMemberID,
		},
		now,
	)
	if err != nil {
		t.Fatalf("create member rectification case: %v", err)
	}

	tests := []struct {
		name      string
		tenantID  string
		caseID    string
		actorID   string
		key       string
		input     RecordCaseRectificationInput
		wantError error
	}{
		{
			name: "unsupported field", tenantID: tenantID, caseID: privacyCase.ID,
			actorID: actorMemberID, key: "invalid-field",
			input: RecordCaseRectificationInput{
				Changes: map[string]*string{"email": pointerTo("changed@example.test")},
				Reason:  "Email changes require a separately verified identity flow.",
			},
			wantError: ErrInvalidRequest,
		},
		{
			name: "invalid locale", tenantID: tenantID, caseID: privacyCase.ID,
			actorID: actorMemberID, key: "invalid-locale",
			input: RecordCaseRectificationInput{
				Changes: map[string]*string{"locale": pointerTo("fr")},
				Reason:  "Attempt an unsupported locale.",
			},
			wantError: ErrInvalidRequest,
		},
		{
			name: "no change", tenantID: tenantID, caseID: privacyCase.ID,
			actorID: actorMemberID, key: "no-change",
			input: RecordCaseRectificationInput{
				Changes: map[string]*string{"name": nil},
				Reason:  "No effective change.",
			},
			wantError: ErrInvalidRequest,
		},
		{
			name: "cross tenant", tenantID: otherTenantID, caseID: privacyCase.ID,
			actorID: otherActorMemberID, key: "cross-tenant",
			input: RecordCaseRectificationInput{
				Changes: map[string]*string{"name": pointerTo("Leaked")},
				Reason:  "Must not cross tenant boundaries.",
			},
			wantError: ErrNotFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := store.ApplyCaseRectification(
				ctx, database, test.tenantID, test.caseID, test.actorID,
				test.key, test.input, now.Add(time.Minute),
			); !errors.Is(err, test.wantError) {
				t.Fatalf("ApplyCaseRectification() error = %v, want %v", err, test.wantError)
			}
		})
	}
}

func pointerTo(value string) *string {
	return &value
}

func valueAt(values map[string]*string, key string) string {
	if values[key] == nil {
		return ""
	}
	return *values[key]
}
