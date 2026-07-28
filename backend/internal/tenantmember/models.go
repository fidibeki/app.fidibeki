package tenantmember

import (
	"errors"
	"time"
)

var (
	ErrMemberNotFound               = errors.New("tenant member not found")
	ErrMemberExists                 = errors.New("tenant member already exists")
	ErrMemberDomainNotVerified      = errors.New("email domain is not verified for this workspace")
	ErrMemberDomainAlreadyVerified  = errors.New("member domain is already verified")
	ErrDomainVerificationInvalid    = errors.New("domain verification is invalid or expired")
	ErrMagicLinkInvalid             = errors.New("magic link is invalid or expired")
	ErrRefreshInvalid               = errors.New("refresh token is invalid or expired")
	ErrRefreshSSORequired           = errors.New("local refresh is blocked by required SSO")
	ErrRefreshExpired               = errors.New("refresh token is expired")
	ErrRefreshReuse                 = errors.New("revoked refresh token was reused")
	ErrOwnerProtection              = errors.New("workspace owner is protected")
	ErrOwnershipConfirmation        = errors.New("workspace ownership confirmation is invalid")
	ErrRecentAuthenticationRequired = errors.New("recent authentication is required")
	ErrOwnershipTargetInvalid       = errors.New("workspace ownership target is invalid")
	ErrLastAdministrator            = errors.New("last recoverable administrator is protected")
	ErrMemberAlreadyRemoved         = errors.New("tenant member is already removed")
	ErrMemberStatusInvalid          = errors.New("tenant member status is invalid")
	ErrMemberActorInvalid           = errors.New("active tenant member actor is required")
	ErrLocalLoginDisabled           = errors.New("local login is disabled by the organization's single sign-on policy")
	ErrTwoFactorNotEnrolled         = errors.New("tenant member is not enrolled in two-factor authentication")
	ErrTwoFactorAlreadyEnrolled     = errors.New("tenant member is already enrolled in two-factor authentication")
	ErrTwoFactorCodeInvalid         = errors.New("two-factor code is invalid")
	ErrTwoFactorChallengeNotPending = errors.New("session has no pending two-factor challenge")
	ErrTwoFactorPolicyInvalid       = errors.New("two-factor policy is invalid")
	// ErrTwoFactorPolicyBlocked guards the two lock-out paths: requiring the
	// policy while the acting administrator is not itself recoverable, and
	// removing an enrolment the workspace policy still demands.
	ErrTwoFactorPolicyBlocked = errors.New("two-factor policy blocks this change")
	// ErrTwoFactorManagedByIdP is returned when required single sign-on has
	// delegated multi-factor assurance to the identity provider.
	ErrTwoFactorManagedByIdP = errors.New("multi-factor authentication is managed by the identity provider")
)

// Two-factor session states. A session is created "pending" when the member is
// enrolled and must still answer the challenge, becomes "satisfied" once it
// does, and stays "not_required" when Fidibeki-managed two-factor does not
// apply to it — including every SAML-authenticated session.
const (
	TwoFactorStateNotRequired = "not_required"
	TwoFactorStatePending     = "pending"
	TwoFactorStateSatisfied   = "satisfied"
)

const (
	AuthenticationMethodLocal = "local"
	AuthenticationMethodSAML  = "saml"
)

const (
	TwoFactorPolicyOptional = "optional"
	TwoFactorPolicyRequired = "required"
)

// Two-factor requirement statuses returned to a client that has authenticated
// but cannot yet be given an access token.
const (
	TwoFactorRequirementNone       = ""
	TwoFactorRequirementChallenge  = "challenge_required"
	TwoFactorRequirementEnrollment = "enrollment_required"
)

// SessionSecurity describes how a session proved identity and what it still
// owes before it can carry workspace access.
type SessionSecurity struct {
	AuthenticationMethod string
	TwoFactorState       string
	PolicyRequired       bool
	Enrolled             bool
	// SAMLIdPEntityID and SAMLSubjectID identify the asserting identity
	// provider and its subject. They are set only for a SAML session and are
	// persisted for audit; neither is ever serialized to a client.
	SAMLIdPEntityID string
	SAMLSubjectID   string
	// breakGlass is true only on the initial local session issued to the owner
	// while Required SSO is active. It reaches the HTTP boundary so the
	// successful recovery login can be audited in the issuing transaction.
	breakGlass bool
}

// IsSAML reports whether an identity provider authenticated this session.
// Such a session delegates multi-factor assurance to that provider and never
// receives a Fidibeki-managed challenge.
func (s SessionSecurity) IsSAML() bool {
	return s.AuthenticationMethod == AuthenticationMethodSAML
}

func (s SessionSecurity) IsBreakGlass() bool {
	return s.breakGlass
}

// NormalizedForPersistence fills in the local default and drops identity
// provider fields that would violate the sessions table's consistency check.
func (s SessionSecurity) NormalizedForPersistence() SessionSecurity {
	if !s.IsSAML() {
		s.AuthenticationMethod = AuthenticationMethodLocal
		s.SAMLIdPEntityID, s.SAMLSubjectID = "", ""
	}
	return s
}

// Requirement maps a session's state onto what the client must do next. A SAML
// session never has a Fidibeki-managed requirement because its authentication
// assurance is delegated to the identity provider.
func (s SessionSecurity) Requirement() string {
	if s.AuthenticationMethod == AuthenticationMethodSAML {
		return TwoFactorRequirementNone
	}
	if s.TwoFactorState == TwoFactorStatePending {
		return TwoFactorRequirementChallenge
	}
	if s.PolicyRequired && !s.Enrolled {
		return TwoFactorRequirementEnrollment
	}
	return TwoFactorRequirementNone
}

// TwoFactorStatus is a member's own view of their enrolment. It never carries
// the shared secret or any recovery code.
type TwoFactorStatus struct {
	Policy                 string     `json:"policy"`
	Enrolled               bool       `json:"enrolled"`
	ConfirmedAt            *time.Time `json:"confirmed_at,omitempty"`
	RecoveryCodesRemaining int        `json:"recovery_codes_remaining"`
	CanRemove              bool       `json:"can_remove"`
	AuthenticationMethod   string     `json:"authentication_method"`
}

// TwoFactorEnrollment is returned once, when enrolment starts. The secret and
// provisioning payload exist only in this response.
type TwoFactorEnrollment struct {
	Secret          string `json:"secret"`
	ProvisioningURI string `json:"provisioning_uri"`
	QRCodeSVG       string `json:"qr_code_svg"`
}

// TwoFactorPolicy is the organization-level setting. ManagedByIdP reports that
// required single sign-on has taken multi-factor assurance out of Fidibeki's
// hands, so the control is inert until local authentication is restored.
type TwoFactorPolicy struct {
	Policy       string     `json:"policy"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
	UpdatedBy    *string    `json:"updated_by,omitempty"`
	ManagedByIdP bool       `json:"managed_by_idp"`
}

// TwoFactorCompliance is one row of the administrator's compliance view. It
// deliberately carries no secret, no recovery code, and no code counter.
type TwoFactorCompliance struct {
	MemberID    string     `json:"member_id"`
	Email       string     `json:"email"`
	Name        *string    `json:"name,omitempty"`
	Enrolled    bool       `json:"enrolled"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
}

// IsOwner reports whether this member is the workspace owner. Under Required
// SSO the owner is the only member who keeps local login, as the audited
// break-glass path.
func (m Member) IsOwner() bool { return m.Role == "owner" }

type MemberStatus string

const (
	MemberStatusActive      MemberStatus = "active"
	MemberStatusDeactivated MemberStatus = "deactivated"
	MemberStatusRemoved     MemberStatus = "removed"
)

type Member struct {
	ID                   string       `json:"id"`
	TenantID             string       `json:"tenant_id"`
	Email                string       `json:"email"`
	Name                 *string      `json:"name,omitempty"`
	AvatarVersion        *string      `json:"avatar_version,omitempty"`
	Locale               *string      `json:"locale"`
	Timezone             *string      `json:"timezone"`
	Permissions          []string     `json:"permissions"`
	Status               MemberStatus `json:"status"`
	Role                 string       `json:"role"`
	Verified             bool         `json:"verified"`
	AuthorizationVersion int          `json:"-"`
}

type MemberProfilePatch struct {
	SetName     bool
	Name        *string
	SetLocale   bool
	Locale      *string
	SetTimezone bool
	Timezone    *string
}

type MemberDomain struct {
	ID                    string     `json:"id"`
	TenantID              string     `json:"tenant_id"`
	Domain                string     `json:"domain"`
	VerificationEmail     string     `json:"verification_email"`
	VerificationExpiresAt time.Time  `json:"verification_expires_at"`
	VerifiedAt            *time.Time `json:"verified_at,omitempty"`
	Status                string     `json:"status"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type SessionTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Member       Member
	// Requirement is empty for a session that may carry workspace access, and
	// otherwise names the two-factor step the member still owes.
	Requirement string
	Security    SessionSecurity
}
