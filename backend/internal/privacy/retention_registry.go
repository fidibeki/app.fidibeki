package privacy

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

const retentionRegistryVersion = 1

type retentionDurationKind string

const (
	retentionDurationFixed       retentionDurationKind = "fixed"
	retentionDurationPlanStorage retentionDurationKind = "plan_storage"
	retentionDurationConfigured  retentionDurationKind = "configured"
	retentionDurationDecision    retentionDurationKind = "decision_required"
	retentionDurationExternal    retentionDurationKind = "external_control"
)

type retentionResource struct {
	Kind RetentionResourceKind
	Name string
}

type retentionDefinition struct {
	Category       string
	Label          string
	Purpose        string
	Role           RetentionRole
	Status         RetentionLifecycleStatus
	DurationKind   retentionDurationKind
	Duration       string
	DurationDays   *int
	Source         string
	Trigger        string
	Action         string
	Cadence        string
	HoldBehavior   RetentionHoldBehavior
	Implementation string
	Resources      []retentionResource
}

func retentionRegistry() []retentionDefinition {
	return []retentionDefinition{
		{
			Category: "workspace_configuration", Label: "Workspace and collection configuration",
			Purpose: "Operate tenant identity, projects, access, targeting, and configuration.",
			Role:    "controller_and_processor", Status: RetentionDecisionRequired,
			DurationKind: retentionDurationDecision, Source: "Policy decision required",
			Trigger: "Workspace or configuration lifecycle.",
			Action:  "Approved deletion or anonymization policy is not yet implemented.",
			Cadence: "Not scheduled", HoldBehavior: RetentionHoldDecisionRequired,
			Resources: tables(
				"api_keys", "member_notification_preferences", "projects", "tags",
				"tenant_deletions", "tenant_member_domains", "tenant_members", "tenants",
				"widget_rule_target_conditions", "widget_rules",
			),
		},
		{
			Category: "workflow_metadata", Label: "Feedback and issue workflow content",
			Purpose: "Capture, triage, resolve, and communicate product feedback.",
			Role:    "processor", Status: RetentionDecisionRequired,
			DurationKind: retentionDurationDecision, Source: "Policy decision required",
			Trigger: "Feedback and issue workflow lifecycle.",
			Action:  "Approved deletion or anonymization policy is not yet implemented.",
			Cadence: "Not scheduled", HoldBehavior: RetentionHoldCaseScoped,
			Resources: tables(
				"feedback_tags", "feedbacks", "issue_activities", "issue_comment_mentions",
				"issue_comments", "issue_counters", "issue_tags", "issue_updates", "issues",
				"member_notifications", "reporter_clarification_requests",
			),
		},
		{
			Category: "auth_magic_links", Label: "Login and invitation magic links",
			Purpose: "Authenticate members and accept invitations.",
			Role:    "controller", Status: RetentionEnforced,
			DurationKind: retentionDurationConfigured,
			Duration:     "Configured link expiry, deleted within 1 hour of expiring",
			Source:       "Platform default", Trigger: "Authoritative expires_at timestamp.",
			Action:  "Hard-delete credential row and retain a non-content audit event.",
			Cadence: "Hourly", HoldBehavior: RetentionHoldNotApplicable,
			Implementation: "tenantmember.Maintainer, platformadmin.Maintainer, samlsso.Maintainer",
			Resources: tables(
				"platform_admin_magic_links", "retention_maintenance_status", "tenant_magic_links",
				"tenant_saml_assertions", "tenant_saml_auth_requests",
			),
		},
		{
			Category: "authentication_security", Label: "Authentication and security records",
			Purpose: "Authenticate operators, investigate access, and protect accounts.",
			Role:    "controller", Status: RetentionDecisionRequired,
			DurationKind: retentionDurationDecision, Source: "Policy decision required",
			Trigger: "Session, platform-access, or recovery lifecycle.",
			Action:  "Security-record period and IP/user-agent minimization require approval.",
			Cadence: "Not scheduled", HoldBehavior: RetentionHoldDecisionRequired,
			Resources: tables(
				"platform_admin_sessions", "platform_admins", "platform_security_events",
				"reporter_access_tokens", "tenant_member_recovery_codes", "tenant_member_sessions",
				"tenant_member_totp", "tenant_saml_certificates", "tenant_saml_configurations",
			),
		},
		{
			Category: "attachments", Label: "Attachments",
			Purpose: "Preserve reporter-provided evidence for the feedback workflow.",
			Role:    "processor", Status: RetentionConfigurable,
			DurationKind: retentionDurationPlanStorage, Source: "Plan storage limit",
			Trigger: "Attachment completion time.", Action: "Delete object before clearing its key.",
			Cadence: "Daily", HoldBehavior: RetentionHoldCaseScoped,
			Implementation: "privacy.RetentionMaintainer",
			Resources: combine(
				tables("attachments"),
				resource(RetentionObjectPrefix, "tenants/{tenant}/feedbacks/{feedback}/attachments/"),
				resource(RetentionDatabaseScope, "retention_maintenance_status:attachments"),
			),
		},
		{
			Category: "identity_assets", Label: "Workspace logos and member avatars",
			Purpose: "Display workspace and member identity.",
			Role:    "controller", Status: RetentionDecisionRequired,
			DurationKind: retentionDurationDecision, Source: "Policy decision required",
			Trigger: "Asset replacement, member lifecycle, or workspace lifecycle.",
			Action:  "Superseded assets are cleaned; active-asset maximum period is not approved.",
			Cadence: "Retry queue", HoldBehavior: RetentionHoldDecisionRequired,
			Resources: combine(
				tables("identity_asset_deletions"),
				resource(RetentionObjectPrefix, "tenants/{tenant}/identity/logos/"),
				resource(RetentionObjectPrefix, "tenants/{tenant}/identity/members/"),
				resource(RetentionDatabaseScope, "retention_maintenance_status:identity_asset_deletions"),
			),
		},
		{
			Category: "session_replays", Label: "Session replay objects",
			Purpose: "Reproduce the reporter session when separately authorized.",
			Role:    "processor", Status: RetentionConfigurable,
			DurationKind: retentionDurationPlanStorage, Source: "Plan storage limit",
			Trigger: "Replay session end time.", Action: "Delete batches and merge before clearing keys.",
			Cadence: "Daily", HoldBehavior: RetentionHoldDecisionRequired,
			Implementation: "privacy.RetentionMaintainer",
			Resources: combine(
				tables("session_batches", "sessions"),
				resource(RetentionObjectPrefix, "tenants/{tenant}/sessions/{session}/batches/"),
				resource(RetentionObjectPrefix, "tenants/{tenant}/sessions/{session}/replay.json"),
				resource(RetentionDatabaseScope, "retention_maintenance_status:replay_sessions"),
			),
		},
		{
			Category: "widget_events", Label: "Raw widget funnel events",
			Purpose: "Measure consented widget funnel performance.",
			Role:    "processor", Status: RetentionEnforced,
			DurationKind: retentionDurationFixed, Duration: "90 days",
			DurationDays: days(90), Source: "Platform default",
			Trigger: "Event occurrence time.", Action: "Hard-delete expired raw events.",
			Cadence: "Hourly", HoldBehavior: RetentionHoldNotApplicable,
			Implementation: "widgetanalytics.Maintainer",
			Resources: combine(
				tables("widget_funnel_events"),
				resource(RetentionDatabaseScope, "retention_maintenance_status:widget_funnel_events"),
			),
		},
		{
			Category: "widget_daily", Label: "Daily widget funnel aggregates",
			Purpose: "Report long-term aggregate widget performance.",
			Role:    "processor", Status: RetentionEnforced,
			DurationKind: retentionDurationFixed, Duration: "730 days",
			DurationDays: days(730), Source: "Platform default",
			Trigger: "Aggregate calendar date.", Action: "Hard-delete expired daily aggregates.",
			Cadence: "Hourly", HoldBehavior: RetentionHoldNotApplicable,
			Implementation: "widgetanalytics.Maintainer",
			Resources: combine(
				tables("widget_funnel_daily", "widget_funnel_maintenance"),
				resource(RetentionDatabaseScope, "retention_maintenance_status:widget_funnel_daily"),
			),
		},
		{
			Category: "widget_analytics_browser", Label: "Widget analytics browser state",
			Purpose: "Maintain consented analytics identity and retry accepted events.",
			Role:    "processor", Status: RetentionDecisionRequired,
			DurationKind: retentionDurationDecision, Source: "Policy decision required",
			Trigger: "Analytics consent, visitor creation, or a retryable event.",
			Action:  "The retry outbox is bounded, but visitor identity has no approved maximum period.",
			Cadence: "Partial expiry only", HoldBehavior: RetentionHoldNotApplicable,
			Resources: resources(
				retentionResource{Kind: RetentionBrowserKey, Name: "fidibeki:visitor:v1"},
				retentionResource{Kind: RetentionBrowserKey, Name: "fidibeki:analytics-outbox:v2:*"},
			),
		},
		{
			Category: "widget_recurrence", Label: "Widget recurrence state",
			Purpose: "Avoid repetitive prompts and apply customer recurrence settings.",
			Role:    "processor", Status: RetentionConfigurable,
			DurationKind: retentionDurationConfigured,
			Duration:     "Session, cooldown, or never, according to the rule",
			Source:       "Widget rule", Trigger: "Submit, dismiss, snooze, or recurrence expiry.",
			Action:  "Browser replaces or expires page-scoped state.",
			Cadence: "Request-time", HoldBehavior: RetentionHoldNotApplicable,
			Implementation: "widget/src/suppression.ts",
			Resources: resources(
				retentionResource{Kind: RetentionBrowserKey, Name: "fidibeki:session:v1"},
				retentionResource{Kind: RetentionBrowserKey, Name: "fidibeki:state:v1:*"},
				retentionResource{Kind: RetentionBrowserKey, Name: "fidibeki:state:v2:*"},
			),
		},
		{
			Category: "widget_targeting", Label: "Widget targeting visit state",
			Purpose: "Evaluate configured page-visit targeting without transmitting the signal.",
			Role:    "processor", Status: RetentionDecisionRequired,
			DurationKind: retentionDurationDecision, Source: "Policy decision required",
			Trigger: "Same-origin page visits.", Action: "Browser-profile lifetime is not yet bounded.",
			Cadence: "Not scheduled", HoldBehavior: RetentionHoldNotApplicable,
			Resources: resource(RetentionBrowserKey, "fidibeki:policy-visit:v1:*"),
		},
		{
			Category: "consent_evidence", Label: "Purpose-specific consent evidence",
			Purpose: "Prove grant, denial, withdrawal, expiry, and notice version by purpose.",
			Role:    "controller_and_processor", Status: RetentionDecisionRequired,
			DurationKind: retentionDurationDecision, Source: "Policy decision required",
			Trigger: "Consent grant, denial, withdrawal, or expiry.",
			Action:  "Identity-minimized post-withdrawal period requires approval.",
			Cadence: "Not scheduled", HoldBehavior: RetentionHoldDecisionRequired,
			Resources: combine(
				tables("replay_consents", "widget_analytics_consents"),
				resource(RetentionBrowserKey, "fidibeki:replay-consent:v1:*"),
				resource(RetentionBrowserKey, "fidibeki:analytics-consent:v1:*"),
			),
		},
		{
			Category: "ai_provider_files", Label: "Temporary AI provider files",
			Purpose: "Analyze explicitly authorized media without retaining provider files.",
			Role:    "processor", Status: RetentionEnforced,
			DurationKind: retentionDurationConfigured,
			Duration:     "Immediate deletion target; provider fallback expiry is 48 hours",
			Source:       "Platform and provider boundary", Trigger: "Provider analysis completion or failure.",
			Action:  "Delete provider file through the retry ledger.",
			Cadence: "Continuous retry", HoldBehavior: RetentionHoldNotApplicable,
			Implementation: "ai.TaskGeminiFileDelete",
			Resources: combine(
				tables("ai_gemini_files"),
				resource(RetentionProvider, "google_gemini_api"),
			),
		},
		{
			Category: "ai_processing", Label: "AI processing and derived evidence",
			Purpose: "Govern authorized AI work, budgets, results, and safe evidence.",
			Role:    "processor", Status: RetentionDecisionRequired,
			DurationKind: retentionDurationDecision, Source: "Policy decision required",
			Trigger: "Source workflow, processing run, and provider deletion lifecycle.",
			Action:  "Source/derived-data maximum period requires approval.",
			Cadence: "Not scheduled", HoldBehavior: RetentionHoldCaseScoped,
			Resources: combine(
				tables(
					"ai_daily_usage", "ai_processing_runs", "ai_project_daily_usage",
					"ai_work_reservations",
				),
				resource(RetentionDatabaseScope, "retention_maintenance_status:ai_budget_ledgers"),
			),
		},
		{
			Category: "integration_delivery", Label: "Integration configuration and delivery evidence",
			Purpose: "Deliver minimized workflow events and preserve disclosure evidence.",
			Role:    "processor", Status: RetentionDecisionRequired,
			DurationKind: retentionDurationDecision, Source: "Policy decision required",
			Trigger: "Integration and domain-event delivery lifecycle.",
			Action:  "Credential, outbox, attempt, and disclosure-ledger periods require approval.",
			Cadence: "Not scheduled", HoldBehavior: RetentionHoldDecisionRequired,
			Resources: tables(
				"delivery_logs", "integrations", "issue_external_links",
				"issue_tracker_connections", "member_digest_deliveries", "outbox_events",
			),
		},
		{
			Category: "testimonials", Label: "Published testimonials and collection evidence",
			Purpose: "Publish author-consented quotes, and evidence that consent was given and withdrawn.",
			// Controller and processor: the customer decides what to publish, but
			// Fidibeki serves the bytes from its own domain and holds the consent
			// record, so neither role alone describes it.
			Role: "controller_and_processor", Status: RetentionDecisionRequired,
			DurationKind: retentionDurationDecision, Source: "Policy decision required",
			Trigger: "Collection request, consent grant, publication, and consent withdrawal.",
			// A revoked testimonial keeps a tombstone: identifier, revocation time,
			// notice version and a hash of the consent evidence. That is enough to
			// evidence that consent existed and was withdrawn, with none of the
			// personal data retained. How long the tombstone itself lives is the
			// decision still outstanding.
			Action:  "Purge author fields and media on withdrawal; tombstone period requires approval.",
			Cadence: "Not scheduled", HoldBehavior: RetentionHoldDecisionRequired,
			Resources: tables(
				"testimonial_candidates", "testimonial_forms", "testimonial_media",
				"testimonial_media_renditions", "testimonial_requests", "testimonial_tags",
				"testimonial_wall_daily_views", "testimonial_wall_items",
				"testimonial_wall_snapshots", "testimonial_walls", "testimonials",
			),
		},
		{
			Category: "privacy_rights", Label: "Privacy cases and rights-operation evidence",
			Purpose: "Verify, execute, review, and evidence privacy-rights operations.",
			Role:    "controller_and_processor", Status: RetentionDecisionRequired,
			DurationKind: retentionDurationDecision, Source: "Policy decision required",
			Trigger: "Privacy request/case receipt, verification, execution, and closure.",
			Action:  "Case and decision-evidence maximum period requires approval.",
			Cadence: "Partial expiry only", HoldBehavior: RetentionHoldCaseScoped,
			Resources: tables(
				"privacy_case_communication_attempts", "privacy_case_communications",
				"privacy_case_events", "privacy_case_feedback_rectifications",
				"privacy_case_legal_holds", "privacy_case_portability_assessments",
				"privacy_case_rectifications", "privacy_cases", "privacy_integration_actions",
				"privacy_jobs", "privacy_processing_restrictions",
				"privacy_recipient_action_attempts", "privacy_recipient_actions",
				"reporter_privacy_request_scope", "reporter_privacy_requests",
				"reporter_privacy_review_items",
			),
		},
		{
			Category: "export_archives", Label: "Export archives",
			Purpose: "Provide temporary authenticated workspace and subject exports.",
			Role:    "controller_and_processor", Status: RetentionEnforced,
			DurationKind: retentionDurationFixed, Duration: "7 days",
			DurationDays: days(7), Source: "Platform default",
			Trigger: "Archive completion time.", Action: "Delete object before expiring archive access.",
			Cadence: "Daily", HoldBehavior: RetentionHoldNotApplicable,
			Implementation: "privacy.RetentionMaintainer",
			Resources: combine(
				resource(RetentionObjectPrefix, "tenants/{tenant}/exports/"),
				resource(RetentionDatabaseScope, "retention_maintenance_status:privacy_export_archives"),
			),
		},
		{
			Category: "unverified_requests", Label: "Unverified reporter requests",
			Purpose: "Allow a reporter to prove control before a privacy operation.",
			Role:    "controller_and_processor", Status: RetentionEnforced,
			DurationKind: retentionDurationFixed, Duration: "48 hours",
			DurationDays: days(2), Source: "Platform default",
			Trigger: "Reporter request creation time.", Action: "Expire unverified request access.",
			Cadence: "Daily", HoldBehavior: RetentionHoldNotApplicable,
			Implementation: "privacy.RetentionMaintainer",
			Resources: combine(
				resource(RetentionDatabaseScope, "reporter_privacy_requests:unverified"),
				resource(RetentionDatabaseScope,
					"retention_maintenance_status:reporter_privacy_requests:unverified"),
			),
		},
		{
			Category: "verified_requests", Label: "Verified reporter request access",
			Purpose: "Provide bounded access to a verified privacy-request result.",
			Role:    "controller_and_processor", Status: RetentionEnforced,
			DurationKind: retentionDurationFixed, Duration: "30 days",
			DurationDays: days(30), Source: "Platform default",
			Trigger: "Reporter verification time.", Action: "Expire reporter result access.",
			Cadence: "Daily", HoldBehavior: RetentionHoldNotApplicable,
			Implementation: "privacy.RetentionMaintainer",
			Resources: combine(
				resource(RetentionDatabaseScope, "reporter_privacy_requests:verified_access"),
				resource(RetentionDatabaseScope,
					"retention_maintenance_status:reporter_privacy_requests:verified_access"),
			),
		},
		{
			Category: "access_security_audit", Label: "Sensitive-access and security audit evidence",
			Purpose: "Investigate access and demonstrate accountable use of sensitive resources.",
			Role:    "controller", Status: RetentionDecisionRequired,
			DurationKind: retentionDurationDecision, Source: "Policy decision required",
			Trigger: "Sensitive access or security event.",
			Action:  "Security-evidence maximum period and holds require approval.",
			Cadence: "Not scheduled", HoldBehavior: RetentionHoldDecisionRequired,
			Resources: tables("tenant_access_events"),
		},
		{
			Category: "billing_records", Label: "Billing and subscription evidence",
			Purpose: "Operate subscriptions and preserve accounting evidence.",
			Role:    "controller", Status: RetentionDecisionRequired,
			DurationKind: retentionDurationDecision, Source: "Policy decision required",
			Trigger: "Billing provider event and account lifecycle.",
			Action:  "Accounting/legal retention requires approval.",
			Cadence: "Not scheduled", HoldBehavior: RetentionHoldDecisionRequired,
			Resources: tables("stripe_events"),
		},
		{
			Category: "breach_register", Label: "Personal-data breach register",
			Purpose: "Preserve breach facts, assessments, chronology, and measures.",
			Role:    "controller_and_processor", Status: RetentionDecisionRequired,
			DurationKind: retentionDurationDecision, Source: "Policy decision required",
			Trigger: "Server-authoritative breach awareness time.",
			Action:  "Protected register period and legal holds require approval.",
			Cadence: "Not scheduled", HoldBehavior: RetentionHoldDecisionRequired,
			Resources: tables(
				"breach_action_events", "breach_assessments", "breach_chronology_entries",
				"breach_controller_notification_attempts",
				"breach_controller_notification_outbox", "breach_controller_notifications",
				"breach_incident_tenants", "breach_incidents",
				"breach_provider_incident_intakes", "breach_provider_incident_tenants",
			),
		},
		{
			Category: "abuse_diagnostics", Label: "Minimized abuse diagnostics",
			Purpose: "Investigate bounded abuse and rate-limit signals without content.",
			Role:    "controller", Status: RetentionEnforced,
			DurationKind: retentionDurationFixed, Duration: "90 days",
			DurationDays: days(90), Source: "Platform default",
			Trigger: "Hourly diagnostic bucket.", Action: "Hard-delete expired buckets.",
			Cadence: "Hourly", HoldBehavior: RetentionHoldNotApplicable,
			Implementation: "abuse.Maintainer",
			Resources: combine(
				tables("abuse_diagnostics_hourly"),
				resource(RetentionDatabaseScope, "retention_maintenance_status:abuse_diagnostics"),
			),
		},
		{
			Category: "external_communications", Label: "External communication provider boundaries",
			Purpose: "Deliver transactional email and configured operational notifications.",
			Role:    "controller_and_processor", Status: RetentionExternalControl,
			DurationKind: retentionDurationExternal, Source: "External configuration",
			Trigger: "Provider-accepted transactional message or operational notification.",
			Action:  "Provider retention and deletion configuration must be verified.",
			Cadence: "Provider controlled", HoldBehavior: RetentionHoldDecisionRequired,
			Resources: resources(
				retentionResource{Kind: RetentionProvider, Name: "resend_transactional_email"},
				retentionResource{Kind: RetentionProvider, Name: "email_capture_filesystem"},
				retentionResource{Kind: RetentionProvider, Name: "slack_incoming_webhook"},
				retentionResource{Kind: RetentionProvider, Name: "slack_oauth_install"},
				retentionResource{Kind: RetentionProvider, Name: "microsoft_teams_incoming_webhook"},
				retentionResource{Kind: RetentionProvider, Name: "customer_generic_webhook"},
				retentionResource{Kind: RetentionProvider, Name: "jira_cloud_issue_tracker"},
				retentionResource{Kind: RetentionProvider, Name: "linear_issue_tracker"},
			),
		},
		{
			Category: "external_identity", Label: "Customer identity provider boundary",
			Purpose: "Authenticate workspace members through the organization's own identity provider.",
			Role:    "controller", Status: RetentionExternalControl,
			DurationKind: retentionDurationExternal, Source: "External configuration",
			Trigger: "Member sign-in through the configured identity provider.",
			Action: "Assertions are validated in memory and never stored; only a hashed assertion " +
				"ID and a non-content audit event remain, and the identity provider controls its " +
				"own authentication records.",
			Cadence: "Provider controlled", HoldBehavior: RetentionHoldNotApplicable,
			Resources: resources(
				retentionResource{Kind: RetentionProvider, Name: "customer_saml_identity_provider"},
			),
		},
		{
			Category: "external_infrastructure", Label: "Infrastructure, queue, billing, and telemetry boundaries",
			Purpose: "Store application data, execute queues, bill workspaces, and observe failures.",
			Role:    "controller_and_processor", Status: RetentionExternalControl,
			DurationKind: retentionDurationExternal, Source: "External configuration",
			Trigger: "Environment/provider operation.",
			Action:  "Regions, logs, backups, queue archives, telemetry, and provider deletion require verification.",
			Cadence: "Provider controlled", HoldBehavior: RetentionHoldDecisionRequired,
			Resources: resources(
				retentionResource{Kind: RetentionQueue, Name: "domain-pool"},
				retentionResource{Kind: RetentionQueue, Name: "email-pool"},
				retentionResource{Kind: RetentionQueue, Name: "attachment-verification-pool"},
				retentionResource{Kind: RetentionQueue, Name: "replay-pool"},
				retentionResource{Kind: RetentionQueue, Name: "privacy-pool"},
				retentionResource{Kind: RetentionQueue, Name: "ai-pool"},
				retentionResource{Kind: RetentionProvider, Name: "postgresql_runtime"},
				retentionResource{Kind: RetentionProvider, Name: "redis_asynq"},
				retentionResource{Kind: RetentionProvider, Name: "s3_compatible_object_storage"},
				retentionResource{Kind: RetentionProvider, Name: "sentry_error_telemetry"},
				retentionResource{Kind: RetentionProvider, Name: "stripe_billing_api"},
			),
		},
		{
			Category: "security_verification", Label: "Signup bot/abuse verification boundary",
			Purpose: "Verify a signup request is not automated abuse before creating a workspace.",
			Role:    "controller", Status: RetentionExternalControl,
			DurationKind: retentionDurationExternal, Source: "External configuration",
			Trigger: "Signup verification request.",
			Action:  "Provider retention and deletion configuration must be verified.",
			Cadence: "Provider controlled", HoldBehavior: RetentionHoldDecisionRequired,
			Resources: resource(RetentionProvider, "cloudflare_turnstile"),
		},
	}
}

func buildEffectiveRetention(
	plan tenant.Plan,
	definitions []retentionDefinition,
) (EffectiveRetention, error) {
	planDefinition, ok := config.PlanFor(plan)
	if !ok {
		return EffectiveRetention{}, errors.New("unknown plan")
	}
	if err := validateRetentionRegistry(definitions); err != nil {
		return EffectiveRetention{}, err
	}
	policies := make([]RetentionPolicy, 0, len(definitions))
	summary := RetentionSummary{}
	for _, definition := range definitions {
		duration := definition.Duration
		durationDays := definition.DurationDays
		if definition.DurationKind == retentionDurationPlanStorage {
			storageDays := planDefinition.Limits.StorageDays
			duration = "Custom"
			durationDays = nil
			if storageDays >= 0 {
				duration = fmt.Sprintf("%d days", storageDays)
				durationDays = days(storageDays)
			}
		}
		if definition.DurationKind == retentionDurationDecision {
			duration = "Decision required"
			durationDays = nil
		}
		if definition.DurationKind == retentionDurationExternal {
			duration = "External configuration"
			durationDays = nil
		}
		switch definition.Status {
		case RetentionEnforced:
			summary.Enforced++
		case RetentionConfigurable:
			summary.Configurable++
		case RetentionExternalControl:
			summary.ExternalControl++
		case RetentionDecisionRequired:
			summary.DecisionRequired++
		}
		policies = append(policies, RetentionPolicy{
			Category: definition.Category, Label: definition.Label,
			Duration: duration, DurationDays: durationDays, Source: definition.Source,
			Purpose: definition.Purpose, Role: definition.Role, Status: definition.Status,
			Trigger: definition.Trigger, Action: definition.Action, Cadence: definition.Cadence,
			HoldBehavior: definition.HoldBehavior, Implementation: definition.Implementation,
			ResourceCounts: countRetentionResources(definition.Resources),
		})
	}
	return EffectiveRetention{
		Plan: plan, RegistryVersion: retentionRegistryVersion,
		Summary: summary, Policies: policies,
	}, nil
}

func validateRetentionRegistry(definitions []retentionDefinition) error {
	if len(definitions) == 0 {
		return errors.New("retention registry is empty")
	}
	categories := make(map[string]struct{}, len(definitions))
	resources := make(map[string]string)
	for _, definition := range definitions {
		if strings.TrimSpace(definition.Category) == "" ||
			strings.TrimSpace(definition.Label) == "" ||
			strings.TrimSpace(definition.Purpose) == "" ||
			strings.TrimSpace(string(definition.Role)) == "" ||
			strings.TrimSpace(definition.Source) == "" ||
			strings.TrimSpace(definition.Trigger) == "" ||
			strings.TrimSpace(definition.Action) == "" ||
			strings.TrimSpace(definition.Cadence) == "" ||
			definition.HoldBehavior == "" ||
			len(definition.Resources) == 0 {
			return fmt.Errorf("retention category %q is incomplete", definition.Category)
		}
		if _, exists := categories[definition.Category]; exists {
			return fmt.Errorf("duplicate retention category %q", definition.Category)
		}
		categories[definition.Category] = struct{}{}
		switch definition.Role {
		case RetentionRoleController, RetentionRoleProcessor, RetentionRoleControllerProcessor:
		default:
			return fmt.Errorf(
				"retention category %q has unknown role %q",
				definition.Category, definition.Role,
			)
		}
		switch definition.HoldBehavior {
		case RetentionHoldCaseScoped, RetentionHoldSupported, RetentionHoldNotApplicable,
			RetentionHoldDecisionRequired:
		default:
			return fmt.Errorf(
				"retention category %q has unknown hold behavior %q",
				definition.Category, definition.HoldBehavior,
			)
		}
		switch definition.DurationKind {
		case retentionDurationFixed:
			if strings.TrimSpace(definition.Duration) == "" ||
				definition.DurationDays == nil ||
				*definition.DurationDays <= 0 {
				return fmt.Errorf(
					"retention category %q has an incomplete fixed duration",
					definition.Category,
				)
			}
		case retentionDurationConfigured:
			if strings.TrimSpace(definition.Duration) == "" {
				return fmt.Errorf(
					"retention category %q has an incomplete configured duration",
					definition.Category,
				)
			}
		case retentionDurationPlanStorage, retentionDurationDecision, retentionDurationExternal:
		default:
			return fmt.Errorf(
				"retention category %q has unknown duration kind %q",
				definition.Category, definition.DurationKind,
			)
		}
		switch definition.Status {
		case RetentionEnforced, RetentionConfigurable:
			if definition.DurationKind == retentionDurationDecision ||
				definition.DurationKind == retentionDurationExternal ||
				strings.TrimSpace(definition.Implementation) == "" {
				return fmt.Errorf(
					"retention category %q makes an unsupported enforcement claim",
					definition.Category,
				)
			}
		case RetentionDecisionRequired:
			if definition.DurationKind != retentionDurationDecision ||
				definition.Implementation != "" {
				return fmt.Errorf("retention category %q has an invalid decision state", definition.Category)
			}
		case RetentionExternalControl:
			if definition.DurationKind != retentionDurationExternal ||
				definition.Implementation != "" {
				return fmt.Errorf("retention category %q has an invalid external state", definition.Category)
			}
		default:
			return fmt.Errorf(
				"retention category %q has unknown status %q",
				definition.Category, definition.Status,
			)
		}
		for _, resource := range definition.Resources {
			if resource.Kind == "" || strings.TrimSpace(resource.Name) == "" {
				return fmt.Errorf("retention category %q has an invalid resource", definition.Category)
			}
			switch resource.Kind {
			case RetentionPostgresTable, RetentionDatabaseScope, RetentionObjectPrefix,
				RetentionQueue, RetentionBrowserKey, RetentionProvider:
			default:
				return fmt.Errorf(
					"retention category %q has unknown resource kind %q",
					definition.Category, resource.Kind,
				)
			}
			key := string(resource.Kind) + ":" + resource.Name
			if owner, exists := resources[key]; exists {
				return fmt.Errorf(
					"retention resource %q belongs to both %q and %q",
					key, owner, definition.Category,
				)
			}
			resources[key] = definition.Category
		}
	}
	return nil
}

func countRetentionResources(resources []retentionResource) []RetentionResourceCount {
	counts := make(map[RetentionResourceKind]int)
	for _, resource := range resources {
		counts[resource.Kind]++
	}
	kinds := make([]string, 0, len(counts))
	for kind := range counts {
		kinds = append(kinds, string(kind))
	}
	sort.Strings(kinds)
	result := make([]RetentionResourceCount, 0, len(kinds))
	for _, kind := range kinds {
		resourceKind := RetentionResourceKind(kind)
		result = append(result, RetentionResourceCount{
			Kind: resourceKind, Count: counts[resourceKind],
		})
	}
	return result
}

func tables(names ...string) []retentionResource {
	resources := make([]retentionResource, len(names))
	for index, name := range names {
		resources[index] = retentionResource{Kind: RetentionPostgresTable, Name: name}
	}
	return resources
}

func resource(kind RetentionResourceKind, name string) []retentionResource {
	return []retentionResource{{Kind: kind, Name: name}}
}

func resources(values ...retentionResource) []retentionResource {
	return values
}

func combine(parts ...[]retentionResource) []retentionResource {
	var result []retentionResource
	for _, part := range parts {
		result = append(result, part...)
	}
	return result
}

func days(value int) *int {
	return &value
}
