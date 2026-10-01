package auth

type Permission string

const (
	PermissionChatRead            Permission = "chat:read"
	PermissionChatWrite           Permission = "chat:write"
	PermissionChatDelete          Permission = "chat:delete"
	PermissionProjectCreate       Permission = "project:create"
	PermissionProjectRead         Permission = "project:read"
	PermissionProjectUpdate       Permission = "project:update"
	PermissionProjectDelete       Permission = "project:delete"
	PermissionWidgetRuleCreate    Permission = "widget_rule:create"
	PermissionWidgetRuleRead      Permission = "widget_rule:read"
	PermissionWidgetRuleUpdate    Permission = "widget_rule:update"
	PermissionWidgetRuleDelete    Permission = "widget_rule:delete"
	PermissionFeedbackRead        Permission = "feedback:read"
	PermissionFeedbackUpdate      Permission = "feedback:update"
	PermissionFeedbackDelete      Permission = "feedback:delete"
	PermissionIssueRead           Permission = "issue:read"
	PermissionIssueUpdate         Permission = "issue:update"
	PermissionIssueDelete         Permission = "issue:delete"
	PermissionIssueMerge          Permission = "issue:merge"
	PermissionSessionRead         Permission = "session:read"
	PermissionSessionDelete       Permission = "session:delete"
	PermissionMemberRead          Permission = "member:read"
	PermissionMemberInvite        Permission = "member:invite"
	PermissionMemberUpdate        Permission = "member:update"
	PermissionMemberRemove        Permission = "member:remove"
	PermissionAPIKeyCreate        Permission = "api_key:create"
	PermissionAPIKeyRead          Permission = "api_key:read"
	PermissionAPIKeyDelete        Permission = "api_key:delete"
	PermissionMobileCaptureCreate Permission = "mobile_capture:create"
	PermissionEmailFeedbackSend   Permission = "email_feedback:send"
	PermissionIntegrationCreate   Permission = "integration:create"
	PermissionIntegrationRead     Permission = "integration:read"
	PermissionIntegrationUpdate   Permission = "integration:update"
	PermissionIntegrationDelete   Permission = "integration:delete"
	PermissionTestimonialRead     Permission = "testimonial:read"
	PermissionTestimonialCreate   Permission = "testimonial:create"
	PermissionTestimonialUpdate   Permission = "testimonial:update"
	PermissionTestimonialDelete   Permission = "testimonial:delete"
	// PermissionTestimonialPublish is deliberately separate from
	// PermissionTestimonialUpdate. Staging a wall is an internal edit; publishing
	// it puts a named individual's words, face and voice on the open internet
	// from a Fidibeki domain, which is a different kind of decision and should be
	// grantable to a different set of people.
	PermissionTestimonialPublish Permission = "testimonial:publish"
	PermissionBillingManage      Permission = "billing:manage"
	PermissionBillingRead        Permission = "billing:read"
	PermissionTenantUpdate       Permission = "tenant:update"
	PermissionTenantRead         Permission = "tenant:read"
	PermissionAuditRead          Permission = "audit:read"
)

const (
	PermissionPlatformSupportRead   Permission = "platform:support:read"
	PermissionPlatformSupportWrite  Permission = "platform:support:write"
	PermissionPlatformTenantCreate  Permission = "platform:tenant:create"
	PermissionPlatformTenantRead    Permission = "platform:tenant:read"
	PermissionPlatformTenantUpdate  Permission = "platform:tenant:update"
	PermissionPlatformTenantSuspend Permission = "platform:tenant:suspend"
	PermissionPlatformTenantDelete  Permission = "platform:tenant:delete"
	PermissionPlatformAdminCreate   Permission = "platform:admin:create"
	PermissionPlatformAdminRead     Permission = "platform:admin:read"
	PermissionPlatformAdminUpdate   Permission = "platform:admin:update"
	PermissionPlatformAdminDelete   Permission = "platform:admin:delete"
	PermissionPlatformBreachCreate  Permission = "platform:breach:create"
	PermissionPlatformBreachRead    Permission = "platform:breach:read"
	PermissionPlatformBreachAppend  Permission = "platform:breach:append"
	// PermissionPlatformQueueRun permits an operator to move one failed
	// background task from retry to pending. It is separate from read-only queue
	// visibility because this command has a direct production side effect.
	PermissionPlatformQueueRun Permission = "platform:queue:run"
	// PermissionPlatformTenantSupportAccess gates platform-admin/support access
	// to a tenant's sensitive data (attachments, replay, exports). Distinct
	// from the tenant CRUD permissions above: it never grants tenant metadata
	// management, only read access to sensitive resources, and every use must
	// carry an explicit, validated support reason and case reference (see
	// accessaudit.ValidateSupportRequest).
	PermissionPlatformTenantSupportAccess Permission = "platform:tenant:support_access"
)

var AllPlatformPermissions = []Permission{
	PermissionPlatformSupportRead,
	PermissionPlatformSupportWrite,
	PermissionPlatformTenantCreate,
	PermissionPlatformTenantRead,
	PermissionPlatformTenantUpdate,
	PermissionPlatformTenantSuspend,
	PermissionPlatformTenantDelete,
	PermissionPlatformAdminCreate,
	PermissionPlatformAdminRead,
	PermissionPlatformAdminUpdate,
	PermissionPlatformAdminDelete,
	PermissionPlatformBreachCreate,
	PermissionPlatformBreachRead,
	PermissionPlatformBreachAppend,
	PermissionPlatformQueueRun,
	PermissionPlatformTenantSupportAccess,
}

var AllTenantPermissions = []Permission{
	PermissionChatRead, PermissionChatWrite, PermissionChatDelete,
	PermissionProjectCreate, PermissionProjectRead, PermissionProjectUpdate, PermissionProjectDelete,
	PermissionWidgetRuleCreate, PermissionWidgetRuleRead, PermissionWidgetRuleUpdate, PermissionWidgetRuleDelete,
	PermissionFeedbackRead, PermissionFeedbackUpdate, PermissionFeedbackDelete,
	PermissionIssueRead, PermissionIssueUpdate, PermissionIssueDelete, PermissionIssueMerge,
	PermissionSessionRead, PermissionSessionDelete,
	PermissionMemberRead, PermissionMemberInvite, PermissionMemberUpdate, PermissionMemberRemove,
	PermissionAPIKeyCreate, PermissionAPIKeyRead, PermissionAPIKeyDelete,
	PermissionMobileCaptureCreate, PermissionEmailFeedbackSend,
	PermissionIntegrationCreate, PermissionIntegrationRead, PermissionIntegrationUpdate, PermissionIntegrationDelete,
	PermissionTestimonialRead, PermissionTestimonialCreate, PermissionTestimonialUpdate,
	PermissionTestimonialDelete, PermissionTestimonialPublish,
	PermissionBillingManage, PermissionBillingRead, PermissionTenantUpdate, PermissionTenantRead,
	PermissionAuditRead,
}

func HasPermission(permissions []string, required Permission) bool {
	for _, permission := range permissions {
		if permission == string(required) {
			return true
		}
	}
	return false
}

func PlatformPermissionStrings() []string {
	return permissionStrings(AllPlatformPermissions)
}

func TenantPermissionStrings() []string {
	return permissionStrings(AllTenantPermissions)
}

func ValidTenantPermissions(values []string) bool {
	allowed := make(map[string]struct{}, len(AllTenantPermissions))
	for _, permission := range AllTenantPermissions {
		allowed[string(permission)] = struct{}{}
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := allowed[value]; !exists {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func permissionStrings(source []Permission) []string {
	permissions := make([]string, len(source))
	for index, permission := range source {
		permissions[index] = string(permission)
	}
	return permissions
}
