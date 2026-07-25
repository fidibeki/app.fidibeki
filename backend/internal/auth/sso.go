package auth

// Vocabulary shared by the organization SSO policy (internal/samlsso) and the
// local magic-link path (internal/tenantmember). It lives here because both
// packages need it and samlsso already depends on tenantmember, so the
// constants cannot live in either of them without creating an import cycle.

// SSOMode mirrors the tenant_sso_mode enum in migration 000111.
type SSOMode string

const (
	// SSOModeDisabled is the default for every organization: SAML is not
	// configured and the existing Fidibeki authentication applies unchanged.
	SSOModeDisabled SSOMode = "disabled"
	// SSOModeOptional lets members choose either SAML or local authentication.
	SSOModeOptional SSOMode = "optional"
	// SSOModeRequired routes normal member access through the identity
	// provider. Only the workspace owner keeps the audited break-glass path.
	SSOModeRequired SSOMode = "required"
)

func (m SSOMode) Valid() bool {
	switch m {
	case SSOModeDisabled, SSOModeOptional, SSOModeRequired:
		return true
	default:
		return false
	}
}

// AuthenticationMethod records which credential path produced a session. It
// mirrors the tenant_member_sessions.authentication_method check constraint.
const (
	AuthenticationMethodLocal = "local"
	AuthenticationMethodSAML  = "saml"
)

// SessionIdentity is the authentication provenance carried from the credential
// that proved identity through to the session it produces. SAML sessions record
// the asserting identity provider and its subject identifier; local sessions
// carry neither. Assurance decisions (including any Fidibeki-managed second
// factor) read Method rather than inspecting the credential again.
type SessionIdentity struct {
	Method      string `json:"method"`
	IdPEntityID string `json:"idp_entity_id,omitempty"`
	// SubjectID is the IdP's opaque subject identifier. It is persisted for
	// audit and never serialized to clients.
	SubjectID string `json:"-"`
}

// LocalSessionIdentity is the provenance of a magic-link session.
func LocalSessionIdentity() SessionIdentity {
	return SessionIdentity{Method: AuthenticationMethodLocal}
}

// SAMLSessionIdentity is the provenance of a session created from a validated
// SAML assertion.
func SAMLSessionIdentity(idpEntityID, subjectID string) SessionIdentity {
	return SessionIdentity{
		Method: AuthenticationMethodSAML, IdPEntityID: idpEntityID, SubjectID: subjectID,
	}
}

// IsSAML reports whether the session was authenticated by an identity provider.
// SAML-authenticated sessions delegate multi-factor assurance to that provider
// and never receive a Fidibeki-managed challenge.
func (i SessionIdentity) IsSAML() bool {
	return i.Method == AuthenticationMethodSAML
}

// Normalized fills in the local default and drops SAML fields that would
// violate the database's identity-consistency constraint.
func (i SessionIdentity) Normalized() SessionIdentity {
	if i.Method != AuthenticationMethodSAML {
		return SessionIdentity{Method: AuthenticationMethodLocal}
	}
	return i
}
