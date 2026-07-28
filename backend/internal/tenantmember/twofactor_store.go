package tenantmember

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/twofactor"
	"github.com/jackc/pgx/v5"
)

// recentAuthenticationWindow matches the workspace-ownership rule: a
// destructive security change must follow a fresh sign-in, not merely a live
// session.
const recentAuthenticationWindow = 10 * time.Minute

// sessionSecurity reports the workspace policy and the member's enrolment.
func (s Store) sessionSecurity(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
) (SessionSecurity, error) {
	security := SessionSecurity{AuthenticationMethod: AuthenticationMethodLocal}
	err := query.QueryRow(ctx, `
		SELECT tenant.two_factor_policy = 'required',
		       EXISTS (
		           SELECT 1 FROM tenant_member_totp AS enrolment
		           WHERE enrolment.tenant_id = $1 AND enrolment.member_id = $2
		             AND enrolment.confirmed_at IS NOT NULL
		       )
		FROM tenants AS tenant
		WHERE tenant.id = $1
	`, tenantID, memberID).Scan(&security.PolicyRequired, &security.Enrolled)
	if err != nil {
		return SessionSecurity{}, fmt.Errorf("read tenant two-factor policy: %w", err)
	}
	return security, nil
}

// TwoFactorStatus is the member's own view of their enrolment.
func (s Store) TwoFactorStatus(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
) (TwoFactorStatus, error) {
	var status TwoFactorStatus
	var policyRequired bool
	err := query.QueryRow(ctx, `
		SELECT tenant.two_factor_policy,
		       tenant.two_factor_policy = 'required',
		       enrolment.confirmed_at,
		       COALESCE((
		           SELECT count(*) FROM tenant_member_recovery_codes AS code
		           WHERE code.tenant_id = $1 AND code.member_id = $2 AND code.used_at IS NULL
		       ), 0)
		FROM tenants AS tenant
		LEFT JOIN tenant_member_totp AS enrolment
		  ON enrolment.tenant_id = tenant.id AND enrolment.member_id = $2
		WHERE tenant.id = $1
	`, tenantID, memberID).Scan(
		&status.Policy, &policyRequired, &status.ConfirmedAt, &status.RecoveryCodesRemaining,
	)
	if err != nil {
		return TwoFactorStatus{}, fmt.Errorf("read two-factor status: %w", err)
	}
	status.Enrolled = status.ConfirmedAt != nil
	status.CanRemove = status.Enrolled && !policyRequired
	status.AuthenticationMethod = AuthenticationMethodLocal
	if !status.Enrolled {
		status.RecoveryCodesRemaining = 0
	}
	return status, nil
}

// StartTwoFactorEnrollment stores a fresh unconfirmed secret, replacing any
// enrolment that was started but never confirmed. A confirmed enrolment is
// never overwritten: removing it is a separate, code-protected operation.
func (s Store) StartTwoFactorEnrollment(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	ciphertext []byte,
) error {
	if err := lockTenantMember(ctx, query, tenantID, memberID); err != nil {
		return err
	}
	tag, err := query.Exec(ctx, `
		INSERT INTO tenant_member_totp (tenant_id, member_id, secret_ciphertext)
		VALUES ($1, $2, $3)
		ON CONFLICT (tenant_id, member_id) DO UPDATE
		SET secret_ciphertext = EXCLUDED.secret_ciphertext, last_used_step = NULL
		WHERE tenant_member_totp.confirmed_at IS NULL
	`, tenantID, memberID, ciphertext)
	if err != nil {
		return fmt.Errorf("start two-factor enrollment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTwoFactorAlreadyEnrolled
	}
	return nil
}

// twoFactorEnrollment loads the stored enrolment for verification.
func (s Store) twoFactorEnrollment(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	confirmed bool,
) ([]byte, *int64, error) {
	var ciphertext []byte
	var lastUsedStep *int64
	err := query.QueryRow(ctx, `
		SELECT secret_ciphertext, last_used_step
		FROM tenant_member_totp
		WHERE tenant_id = $1 AND member_id = $2
		  AND (confirmed_at IS NOT NULL) = $3
		FOR UPDATE
	`, tenantID, memberID, confirmed).Scan(&ciphertext, &lastUsedStep)
	if errors.Is(err, pgx.ErrNoRows) {
		if confirmed {
			return nil, nil, ErrTwoFactorNotEnrolled
		}
		return nil, nil, ErrTwoFactorAlreadyEnrolled
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load two-factor enrollment: %w", err)
	}
	return ciphertext, lastUsedStep, nil
}

// ConfirmTwoFactorEnrollment activates the enrolment and replaces the member's
// recovery codes with the freshly generated hashes.
func (s Store) ConfirmTwoFactorEnrollment(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	usedStep int64,
	recoveryHashes []string,
) error {
	confirmedAt := s.now()
	tag, err := query.Exec(ctx, `
		UPDATE tenant_member_totp
		SET confirmed_at = $3, last_used_step = $4
		WHERE tenant_id = $1 AND member_id = $2 AND confirmed_at IS NULL
	`, tenantID, memberID, confirmedAt, usedStep)
	if err != nil {
		return fmt.Errorf("confirm two-factor enrollment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTwoFactorAlreadyEnrolled
	}
	if err := s.replaceRecoveryCodes(ctx, query, tenantID, memberID, recoveryHashes); err != nil {
		return err
	}
	return recordTenantAccessEvent(
		ctx, query, tenantID, "two_factor.enrolled", &memberID, &memberID,
		map[string]any{"method": "totp", "recovery_code_count": len(recoveryHashes)},
	)
}

func (s Store) replaceRecoveryCodes(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	hashes []string,
) error {
	if _, err := query.Exec(ctx, `
		DELETE FROM tenant_member_recovery_codes WHERE tenant_id = $1 AND member_id = $2
	`, tenantID, memberID); err != nil {
		return fmt.Errorf("clear two-factor recovery codes: %w", err)
	}
	for _, hash := range hashes {
		if _, err := query.Exec(ctx, `
			INSERT INTO tenant_member_recovery_codes (tenant_id, member_id, code_hash)
			VALUES ($1, $2, $3)
		`, tenantID, memberID, hash); err != nil {
			return fmt.Errorf("store two-factor recovery code: %w", err)
		}
	}
	return nil
}

// RecordTwoFactorStep pins the last accepted TOTP time step so the same code
// cannot be replayed while its window is still open.
func (s Store) RecordTwoFactorStep(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	step int64,
) error {
	if _, err := query.Exec(ctx, `
		UPDATE tenant_member_totp SET last_used_step = $3
		WHERE tenant_id = $1 AND member_id = $2
	`, tenantID, memberID, step); err != nil {
		return fmt.Errorf("record two-factor step: %w", err)
	}
	return nil
}

// ConsumeRecoveryCode marks one unused recovery code as spent. Recovery codes
// are single-use by construction: the update only matches a row whose used_at
// is still null.
func (s Store) ConsumeRecoveryCode(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	codeHash string,
) error {
	tag, err := query.Exec(ctx, `
		UPDATE tenant_member_recovery_codes SET used_at = $4
		WHERE tenant_id = $1 AND member_id = $2 AND code_hash = $3 AND used_at IS NULL
	`, tenantID, memberID, codeHash, s.now())
	if err != nil {
		return fmt.Errorf("consume two-factor recovery code: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTwoFactorCodeInvalid
	}
	var remaining int
	if err := query.QueryRow(ctx, `
		SELECT count(*) FROM tenant_member_recovery_codes
		WHERE tenant_id = $1 AND member_id = $2 AND used_at IS NULL
	`, tenantID, memberID).Scan(&remaining); err != nil {
		return fmt.Errorf("count remaining recovery codes: %w", err)
	}
	// The hash is never recorded: the ledger only needs to say that a recovery
	// code was spent and how many are left.
	return recordTenantAccessEvent(
		ctx, query, tenantID, "two_factor.recovery_code_used", &memberID, &memberID,
		map[string]any{"remaining": remaining},
	)
}

// RegenerateRecoveryCodes replaces the member's recovery codes wholesale.
func (s Store) RegenerateRecoveryCodes(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	hashes []string,
) error {
	if err := s.replaceRecoveryCodes(ctx, query, tenantID, memberID, hashes); err != nil {
		return err
	}
	return recordTenantAccessEvent(
		ctx, query, tenantID, "two_factor.recovery_codes_regenerated", &memberID, &memberID,
		map[string]any{"recovery_code_count": len(hashes)},
	)
}

// RemoveTwoFactorEnrollment deletes a member's own enrolment. It refuses while
// the workspace policy requires two-factor authentication, because a member
// cannot opt out of an organization rule.
func (s Store) RemoveTwoFactorEnrollment(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
) error {
	var policy string
	if err := query.QueryRow(ctx, `
		SELECT two_factor_policy FROM tenants WHERE id = $1 FOR UPDATE
	`, tenantID).Scan(&policy); err != nil {
		return fmt.Errorf("lock tenant two-factor policy: %w", err)
	}
	if policy == TwoFactorPolicyRequired {
		if err := recordTenantAccessEvent(
			ctx, query, tenantID, "two_factor.removal_denied", &memberID, &memberID,
			map[string]any{"reason": "policy_requires_two_factor"},
		); err != nil {
			return err
		}
		return ErrTwoFactorPolicyBlocked
	}
	if err := s.deleteEnrollment(ctx, query, tenantID, memberID); err != nil {
		return err
	}
	return recordTenantAccessEvent(
		ctx, query, tenantID, "two_factor.removed", &memberID, &memberID,
		map[string]any{"actor": "member"},
	)
}

func (s Store) deleteEnrollment(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
) error {
	tag, err := query.Exec(ctx, `
		DELETE FROM tenant_member_totp WHERE tenant_id = $1 AND member_id = $2
	`, tenantID, memberID)
	if err != nil {
		return fmt.Errorf("remove two-factor enrollment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTwoFactorNotEnrolled
	}
	if _, err := query.Exec(ctx, `
		DELETE FROM tenant_member_recovery_codes WHERE tenant_id = $1 AND member_id = $2
	`, tenantID, memberID); err != nil {
		return fmt.Errorf("remove two-factor recovery codes: %w", err)
	}
	return nil
}

// RequireRecentAuthentication rejects a security-sensitive change when the
// member's last sign-in is older than the recent-authentication window.
func (s Store) RequireRecentAuthentication(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
) error {
	var lastLoginAt *time.Time
	err := query.QueryRow(ctx, `
		SELECT last_login_at FROM tenant_members
		WHERE tenant_id = $1 AND id = $2 AND status = 'active'
		FOR UPDATE
	`, tenantID, memberID).Scan(&lastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMemberActorInvalid
	}
	if err != nil {
		return fmt.Errorf("read member authentication recency: %w", err)
	}
	if lastLoginAt == nil || lastLoginAt.Before(s.now().Add(-recentAuthenticationWindow)) {
		return ErrRecentAuthenticationRequired
	}
	return nil
}

// PendingChallengeSession locks the session behind a refresh token and confirms
// it is still waiting for its two-factor challenge.
func (s Store) PendingChallengeSession(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	refreshHash string,
) (Member, error) {
	if err := tenant.LockForAuthentication(ctx, query, tenantID, s.now()); err != nil {
		if errors.Is(err, tenant.ErrUnknownTenant) {
			return Member{}, ErrRefreshInvalid
		}
		return Member{}, err
	}
	memberID, err := lockTenantMemberForSession(ctx, query, tenantID, refreshHash)
	if err != nil {
		return Member{}, err
	}
	var state string
	var method string
	if err := query.QueryRow(ctx, `
		SELECT two_factor_state, authentication_method
		FROM tenant_member_sessions
		WHERE tenant_id = $1 AND refresh_hash = $2 AND revoked_at IS NULL
		FOR UPDATE
	`, tenantID, refreshHash).Scan(&state, &method); errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrRefreshInvalid
	} else if err != nil {
		return Member{}, fmt.Errorf("load pending two-factor session: %w", err)
	}
	if state != TwoFactorStatePending {
		return Member{}, ErrTwoFactorChallengeNotPending
	}
	member, err := scanMemberWithRole(query.QueryRow(ctx, `
		SELECT member.id, member.tenant_id, member.email, member.name, member.avatar_version,
		       member.locale, member.timezone, member.permissions,
		       member.status, member.authorization_version,
		       member.id = tenant.owner_member_id, member.last_login_at IS NOT NULL
		FROM tenant_members AS member
		JOIN tenants AS tenant ON tenant.id = member.tenant_id
		WHERE member.tenant_id = $1 AND member.id = $2 AND member.status = 'active'
	`, tenantID, memberID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrRefreshInvalid
	}
	if err != nil {
		return Member{}, fmt.Errorf("load two-factor challenge member: %w", err)
	}
	return member, nil
}

// SatisfyTwoFactorChallenge promotes the session once the member has proved
// possession of their authenticator or a recovery code.
func (s Store) SatisfyTwoFactorChallenge(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	refreshHash string,
	memberID string,
	method string,
) error {
	verifiedAt := s.now()
	if err := tenant.LockForAuthentication(ctx, query, tenantID, verifiedAt); err != nil {
		if errors.Is(err, tenant.ErrUnknownTenant) {
			return ErrRefreshInvalid
		}
		return err
	}
	tag, err := query.Exec(ctx, `
		UPDATE tenant_member_sessions
		SET two_factor_state = $3, two_factor_verified_at = $4, last_used_at = $4
		WHERE tenant_id = $1 AND refresh_hash = $2 AND two_factor_state = $5
		  AND revoked_at IS NULL
	`, tenantID, refreshHash, TwoFactorStateSatisfied, verifiedAt, TwoFactorStatePending)
	if err != nil {
		return fmt.Errorf("satisfy two-factor challenge: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTwoFactorChallengeNotPending
	}
	return recordTenantAccessEvent(
		ctx, query, tenantID, "two_factor.challenge_succeeded", &memberID, &memberID,
		map[string]any{"method": method},
	)
}

// RecordTwoFactorFailure appends a failed attempt to the audit ledger. The
// submitted value is never recorded, only that an attempt failed and why.
func (s Store) RecordTwoFactorFailure(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	kind string,
	reason string,
) error {
	return recordTenantAccessEvent(
		ctx, query, tenantID, kind, &memberID, &memberID,
		map[string]any{"reason": reason},
	)
}

// TwoFactorPolicy reads the organization policy.
func (s Store) TwoFactorPolicy(
	ctx context.Context,
	query db.Querier,
	tenantID string,
) (TwoFactorPolicy, error) {
	var policy TwoFactorPolicy
	err := query.QueryRow(ctx, `
		SELECT two_factor_policy, two_factor_policy_updated_at, two_factor_policy_updated_by::text
		FROM tenants WHERE id = $1
	`, tenantID).Scan(&policy.Policy, &policy.UpdatedAt, &policy.UpdatedBy)
	if err != nil {
		return TwoFactorPolicy{}, fmt.Errorf("read two-factor policy: %w", err)
	}
	// Under required single sign-on the identity provider owns multi-factor
	// assurance, so the Fidibeki-managed control no longer applies. The stored
	// policy is left untouched: it becomes meaningful again the moment local
	// authentication is restored.
	mode, err := Store{}.SSOMode(ctx, query, tenantID)
	if err != nil {
		return TwoFactorPolicy{}, err
	}
	policy.ManagedByIdP = mode == auth.SSOModeRequired
	return policy, nil
}

// SetTwoFactorPolicy switches the organization policy. Requiring two-factor
// authentication is refused unless the acting administrator is already enrolled
// and still holds unused recovery codes, which is what keeps at least one
// recoverable administrator in the workspace after the switch.
func (s Store) SetTwoFactorPolicy(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	policy string,
) (TwoFactorPolicy, error) {
	if policy != TwoFactorPolicyOptional && policy != TwoFactorPolicyRequired {
		return TwoFactorPolicy{}, ErrTwoFactorPolicyInvalid
	}
	// Required single sign-on delegates multi-factor assurance to the identity
	// provider, so the Fidibeki-managed policy cannot be changed while it is
	// active. Refusing here rather than in the UI keeps the guarantee in the
	// one place every caller passes through.
	mode, err := Store{}.SSOMode(ctx, query, tenantID)
	if err != nil {
		return TwoFactorPolicy{}, err
	}
	if mode == auth.SSOModeRequired {
		if err := recordTenantAccessEvent(
			ctx, query, tenantID, "two_factor.policy_denied", &actorMemberID, nil,
			map[string]any{"reason": "sso_required_idp_manages_mfa", "requested_policy": policy},
		); err != nil {
			return TwoFactorPolicy{}, err
		}
		return TwoFactorPolicy{}, ErrTwoFactorManagedByIdP
	}
	var current string
	if err := query.QueryRow(ctx, `
		SELECT two_factor_policy FROM tenants WHERE id = $1 FOR UPDATE
	`, tenantID).Scan(&current); err != nil {
		return TwoFactorPolicy{}, fmt.Errorf("lock tenant two-factor policy: %w", err)
	}
	deny := func(reason string) (TwoFactorPolicy, error) {
		if err := recordTenantAccessEvent(
			ctx, query, tenantID, "two_factor.policy_denied", &actorMemberID, nil,
			map[string]any{"reason": reason, "requested_policy": policy},
		); err != nil {
			return TwoFactorPolicy{}, err
		}
		return TwoFactorPolicy{}, ErrTwoFactorPolicyBlocked
	}
	if policy == TwoFactorPolicyRequired && current != TwoFactorPolicyRequired {
		var enrolled bool
		var recoveryCodes int
		if err := query.QueryRow(ctx, `
			SELECT EXISTS (
			         SELECT 1 FROM tenant_member_totp
			         WHERE tenant_id = $1 AND member_id = $2 AND confirmed_at IS NOT NULL
			       ),
			       (SELECT count(*) FROM tenant_member_recovery_codes
			        WHERE tenant_id = $1 AND member_id = $2 AND used_at IS NULL)
		`, tenantID, actorMemberID).Scan(&enrolled, &recoveryCodes); err != nil {
			return TwoFactorPolicy{}, fmt.Errorf("check administrator two-factor readiness: %w", err)
		}
		if !enrolled {
			return deny("administrator_is_not_enrolled")
		}
		if recoveryCodes == 0 {
			return deny("administrator_has_no_recovery_codes")
		}
	}
	updatedAt := s.now()
	var updated TwoFactorPolicy
	if err := query.QueryRow(ctx, `
		UPDATE tenants
		SET two_factor_policy = $2,
		    two_factor_policy_updated_at = $3,
		    two_factor_policy_updated_by = $4::uuid
		WHERE id = $1
		RETURNING two_factor_policy, two_factor_policy_updated_at, two_factor_policy_updated_by::text
	`, tenantID, policy, updatedAt, actorMemberID).Scan(
		&updated.Policy, &updated.UpdatedAt, &updated.UpdatedBy,
	); err != nil {
		return TwoFactorPolicy{}, fmt.Errorf("update two-factor policy: %w", err)
	}
	if err := recordTenantAccessEvent(
		ctx, query, tenantID, "two_factor.policy_changed", &actorMemberID, nil,
		map[string]any{"previous_policy": current, "policy": policy},
	); err != nil {
		return TwoFactorPolicy{}, err
	}
	return updated, nil
}

// TwoFactorCompliance lists every active member with their enrolment state.
func (s Store) TwoFactorCompliance(
	ctx context.Context,
	query db.Querier,
	tenantID string,
) ([]TwoFactorCompliance, error) {
	rows, err := query.Query(ctx, `
		SELECT member.id, member.email, member.name,
		       enrolment.confirmed_at IS NOT NULL, enrolment.confirmed_at
		FROM tenant_members AS member
		LEFT JOIN tenant_member_totp AS enrolment
		  ON enrolment.tenant_id = member.tenant_id AND enrolment.member_id = member.id
		  AND enrolment.confirmed_at IS NOT NULL
		WHERE member.tenant_id = $1 AND member.status <> 'removed'
		ORDER BY member.email
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list two-factor compliance: %w", err)
	}
	defer rows.Close()
	entries := make([]TwoFactorCompliance, 0)
	for rows.Next() {
		var entry TwoFactorCompliance
		if err := rows.Scan(
			&entry.MemberID, &entry.Email, &entry.Name, &entry.Enrolled, &entry.ConfirmedAt,
		); err != nil {
			return nil, fmt.Errorf("scan two-factor compliance: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list two-factor compliance: %w", err)
	}
	return entries, nil
}

// ResetMemberTwoFactor is the administrator-assisted recovery path. It clears
// the member's enrolment and revokes their sessions, so the member must sign in
// again and re-enrol; the administrator never sees a secret or recovery code.
func (s Store) ResetMemberTwoFactor(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	targetMemberID string,
) error {
	if actorMemberID == targetMemberID {
		// Self-service removal has its own code-protected path; routing it
		// through the administrative reset would skip that proof.
		return ErrTwoFactorPolicyBlocked
	}
	if err := lockTenantMember(ctx, query, tenantID, targetMemberID); err != nil {
		return err
	}
	var status string
	if err := query.QueryRow(ctx, `
		SELECT status FROM tenant_members WHERE tenant_id = $1 AND id = $2
	`, tenantID, targetMemberID).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return ErrMemberNotFound
	} else if err != nil {
		return fmt.Errorf("load two-factor reset target: %w", err)
	}
	if err := s.deleteEnrollment(ctx, query, tenantID, targetMemberID); err != nil {
		return err
	}
	revokedAt := s.now()
	revoked, err := revokeMemberAccess(ctx, query, tenantID, targetMemberID, revokedAt)
	if err != nil {
		return err
	}
	return recordTenantAccessEvent(
		ctx, query, tenantID, "two_factor.administrative_reset", &actorMemberID, &targetMemberID,
		map[string]any{"revoked_session_count": revoked, "sessions_revoked_at": revokedAt},
	)
}

// DecryptSecret is the store's only view of a plaintext secret; it exists so
// verification can happen without the secret leaving this package.
func decryptSecret(cipher *twofactor.Cipher, ciphertext []byte, tenantID, memberID string) (string, error) {
	plaintext, err := cipher.Decrypt(ciphertext, tenantID, memberID)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
