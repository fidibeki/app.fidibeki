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
