package tenantmember

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	"github.com/fidibeki/app.fidibeki/backend/internal/twofactor"
)

// TwoFactorStore is the persistence surface the two-factor service needs. It is
// separate from MemberStore so the enrolment flow cannot reach the session and
// membership mutations it has no business performing.
type TwoFactorStore interface {
	TwoFactorStatus(context.Context, db.Querier, string, string) (TwoFactorStatus, error)
	StartTwoFactorEnrollment(context.Context, db.Querier, string, string, []byte) error
	ConfirmTwoFactorEnrollment(context.Context, db.Querier, string, string, int64, []string) error
	RecordTwoFactorStep(context.Context, db.Querier, string, string, int64) error
	ConsumeRecoveryCode(context.Context, db.Querier, string, string, string) error
	RegenerateRecoveryCodes(context.Context, db.Querier, string, string, []string) error
	RemoveTwoFactorEnrollment(context.Context, db.Querier, string, string) error
	RequireRecentAuthentication(context.Context, db.Querier, string, string) error
	PendingChallengeSession(context.Context, db.Querier, string, string) (Member, error)
	SatisfyTwoFactorChallenge(context.Context, db.Querier, string, string, string, string) error
	RecordTwoFactorFailure(context.Context, db.Querier, string, string, string, string) error
	TwoFactorPolicy(context.Context, db.Querier, string) (TwoFactorPolicy, error)
	SetTwoFactorPolicy(context.Context, db.Querier, string, string, string) (TwoFactorPolicy, error)
	TwoFactorCompliance(context.Context, db.Querier, string) ([]TwoFactorCompliance, error)
	ResetMemberTwoFactor(context.Context, db.Querier, string, string, string) error
	twoFactorEnrollment(context.Context, db.Querier, string, string, bool) ([]byte, *int64, error)
}

// TwoFactorService owns everything that touches a plaintext secret or code.
// Handlers never see either: they pass the submitted value in and receive only
// a decision.
type TwoFactorService struct {
	Store  TwoFactorStore
	Cipher *twofactor.Cipher
	Now    func() time.Time
}

func (s TwoFactorService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Status reports the member's own enrolment.
func (s TwoFactorService) Status(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
) (TwoFactorStatus, error) {
	return s.Store.TwoFactorStatus(ctx, query, tenantID, memberID)
}

// StartEnrollment generates a secret, stores it encrypted, and returns the
// provisioning material. This is the only moment the secret leaves the server.
func (s TwoFactorService) StartEnrollment(
	ctx context.Context,
	query db.Querier,
	resolved tenant.Tenant,
	memberID string,
	account string,
) (TwoFactorEnrollment, error) {
	secret, err := twofactor.GenerateSecret()
	if err != nil {
		return TwoFactorEnrollment{}, err
	}
	ciphertext, err := s.Cipher.Encrypt([]byte(secret), resolved.ID, memberID)
	if err != nil {
		return TwoFactorEnrollment{}, err
	}
	if err := s.Store.StartTwoFactorEnrollment(ctx, query, resolved.ID, memberID, ciphertext); err != nil {
		return TwoFactorEnrollment{}, err
	}
	issuer := resolved.Name
	if issuer == "" {
		issuer = resolved.Slug
	}
	uri := twofactor.ProvisioningURI(issuer, account, secret)
	svg, err := twofactor.QRCodeSVG(uri)
	if err != nil {
		return TwoFactorEnrollment{}, err
	}
	return TwoFactorEnrollment{Secret: secret, ProvisioningURI: uri, QRCodeSVG: svg}, nil
}

// ConfirmEnrollment activates a pending enrolment once the member proves the
// authenticator is configured, and returns the recovery codes exactly once.
func (s TwoFactorService) ConfirmEnrollment(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	code string,
) ([]string, error) {
	step, err := s.verifyStoredCode(ctx, query, tenantID, memberID, code, false)
	if err != nil {
		return nil, err
	}
	codes, err := twofactor.GenerateRecoveryCodes()
	if err != nil {
		return nil, err
	}
	if err := s.Store.ConfirmTwoFactorEnrollment(
		ctx, query, tenantID, memberID, step, hashRecoveryCodes(codes),
	); err != nil {
		return nil, err
	}
	return codes, nil
}

// Challenge answers the post-authentication challenge with either a TOTP code
// or a recovery code, and promotes the session when it succeeds.
func (s TwoFactorService) Challenge(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	refreshHash string,
	code string,
) (Member, error) {
	member, err := s.Store.PendingChallengeSession(ctx, query, tenantID, refreshHash)
	if err != nil {
		return Member{}, err
	}
	method, err := s.verifyPossession(ctx, query, tenantID, member.ID, code)
	if err != nil {
		if errors.Is(err, ErrTwoFactorCodeInvalid) {
			if recordErr := s.Store.RecordTwoFactorFailure(
				ctx, query, tenantID, member.ID, "two_factor.challenge_failed", "code_invalid",
			); recordErr != nil {
				return Member{}, recordErr
			}
		}
		return Member{}, err
	}
	if err := s.Store.SatisfyTwoFactorChallenge(
		ctx, query, tenantID, refreshHash, member.ID, method,
	); err != nil {
		return Member{}, err
	}
	return member, nil
}

// RegenerateRecoveryCodes replaces the member's codes after a fresh sign-in and
// a valid proof of possession.
func (s TwoFactorService) RegenerateRecoveryCodes(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	code string,
) ([]string, error) {
	if err := s.Store.RequireRecentAuthentication(ctx, query, tenantID, memberID); err != nil {
		return nil, err
	}
	if _, err := s.verifyPossession(ctx, query, tenantID, memberID, code); err != nil {
		return nil, err
	}
	codes, err := twofactor.GenerateRecoveryCodes()
	if err != nil {
		return nil, err
	}
	if err := s.Store.RegenerateRecoveryCodes(
		ctx, query, tenantID, memberID, hashRecoveryCodes(codes),
	); err != nil {
		return nil, err
	}
	return codes, nil
}

// Remove deletes the member's own enrolment after a fresh sign-in and a valid
// proof of possession. The store refuses while the policy requires two-factor.
func (s TwoFactorService) Remove(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	code string,
) error {
	if err := s.Store.RequireRecentAuthentication(ctx, query, tenantID, memberID); err != nil {
		return err
	}
	if _, err := s.verifyPossession(ctx, query, tenantID, memberID, code); err != nil {
		return err
	}
	return s.Store.RemoveTwoFactorEnrollment(ctx, query, tenantID, memberID)
}

// verifyPossession accepts either a TOTP code or an unused recovery code and
// reports which one matched.
func (s TwoFactorService) verifyPossession(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	code string,
) (string, error) {
	if _, err := twofactor.NormalizeCode(code); err == nil {
		if _, err := s.verifyStoredCode(ctx, query, tenantID, memberID, code, true); err != nil {
			return "", err
		}
		return "totp", nil
	}
	normalized, err := twofactor.NormalizeRecoveryCode(code)
	if err != nil {
		return "", ErrTwoFactorCodeInvalid
	}
	// Loading the enrolment first keeps a recovery code useless for a member
	// who is not enrolled at all.
	if _, _, err := s.Store.twoFactorEnrollment(ctx, query, tenantID, memberID, true); err != nil {
		return "", err
	}
	if err := s.Store.ConsumeRecoveryCode(
		ctx, query, tenantID, memberID, twofactor.HashRecoveryCode(normalized),
	); err != nil {
		return "", err
	}
	return "recovery_code", nil
}

// verifyStoredCode checks a TOTP code against the stored secret and pins the
// accepted time step so it cannot be replayed.
func (s TwoFactorService) verifyStoredCode(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	code string,
	confirmed bool,
) (int64, error) {
	ciphertext, lastUsedStep, err := s.Store.twoFactorEnrollment(ctx, query, tenantID, memberID, confirmed)
	if err != nil {
		return 0, err
	}
	secret, err := decryptSecret(s.Cipher, ciphertext, tenantID, memberID)
	if err != nil {
		return 0, fmt.Errorf("load two-factor secret: %w", err)
	}
	step, err := twofactor.Verify(secret, code, s.now(), lastUsedStep)
	if err != nil {
		return 0, ErrTwoFactorCodeInvalid
	}
	if confirmed {
		if err := s.Store.RecordTwoFactorStep(ctx, query, tenantID, memberID, step); err != nil {
			return 0, err
		}
	}
	return step, nil
}

func hashRecoveryCodes(codes []string) []string {
	hashes := make([]string, len(codes))
	for index, code := range codes {
		hashes[index] = twofactor.HashRecoveryCode(code)
	}
	return hashes
}
