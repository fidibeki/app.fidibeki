package accessaudit

import (
	"errors"
	"strings"
)

// PermissionPathSupportAccess is recorded as the permission_path for
// platform-admin/support access events. It intentionally differs from
// auth.PermissionPlatformTenantSupportAccess (the permission that actually
// gates the route): tenant_access_events.permission_path is constrained to a
// single "resource:action" pair, matching the tenant-facing feedback:read /
// session:read convention rather than the three-segment platform:* namespace.
const PermissionPathSupportAccess = "platform_admin:support_access"

const (
	SupportReasonHeader        = "X-Fidibeki-Support-Reason"
	SupportCaseReferenceHeader = "X-Fidibeki-Support-Case"
)

// ErrSupportRequestInvalid indicates a platform-admin/support access request
// did not carry a sufficiently explicit reason and case reference. Sensitive
// tenant data must never be reachable without both.
var ErrSupportRequestInvalid = errors.New("support reason and case reference are required")

const (
	minSupportReasonLength        = 10
	maxSupportReasonLength        = 500
	minSupportCaseReferenceLength = 3
	maxSupportCaseReferenceLength = 100
)

// SupportRequest carries the mandatory justification a platform admin must
// supply before accessing a tenant's sensitive data. Both fields are small,
// non-sensitive metadata (an internal reason and case/ticket reference), never
// evidence content, and are safe to persist in an access-audit event's data.
type SupportRequest struct {
	Reason        string
	CaseReference string
}

// ValidateSupportRequest trims and bounds-checks a support request. It is the
// shared authorization primitive every platform-admin/support access path
// must call before granting access to sensitive tenant data, so the
// reason/case requirement is enforced consistently across resource types.
func ValidateSupportRequest(request SupportRequest) (SupportRequest, error) {
	reason := strings.TrimSpace(request.Reason)
	caseReference := strings.TrimSpace(request.CaseReference)
	if len(reason) < minSupportReasonLength || len(reason) > maxSupportReasonLength ||
		len(caseReference) < minSupportCaseReferenceLength ||
		len(caseReference) > maxSupportCaseReferenceLength ||
		containsSupportControlCharacter(reason) ||
		containsSupportControlCharacter(caseReference) {
		return SupportRequest{}, ErrSupportRequestInvalid
	}
	return SupportRequest{Reason: reason, CaseReference: caseReference}, nil
}

func containsSupportControlCharacter(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}
