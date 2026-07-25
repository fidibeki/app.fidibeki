package privacy

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const ropaRegistryVersion = 1

const (
	maxROPAClassifications  = 8
	maxROPASecurityControls = 12
	maxROPAControlLen       = 220
)

// ROPADataSubjectClass is a closed classification of whose personal data a processing activity
// touches. It restates the data-subject groups already described in docs/privacy-data-map.md's
// data-category table rather than introducing a new legal claim.
type ROPADataSubjectClass string

const (
	ROPADataSubjectTenantMembers   ROPADataSubjectClass = "tenant_members"
	ROPADataSubjectReporters       ROPADataSubjectClass = "reporters_end_users"
	ROPADataSubjectWebsiteVisitors ROPADataSubjectClass = "website_visitors"
	ROPADataSubjectPlatformAdmins  ROPADataSubjectClass = "platform_admins"
	ROPADataSubjectBillingContacts ROPADataSubjectClass = "billing_contacts"
	// ROPADataSubjectThirdParties covers a person named or shown within another data subject's
	// content (free-text feedback, attachments, AI inputs, privacy-rights records, breach
	// records) rather than a subject who directly interacts with Fidibeki.
	ROPADataSubjectThirdParties ROPADataSubjectClass = "third_parties"
)

// ROPADataCategoryClass is a closed classification of what kind of personal data a processing
// activity touches, at the same technical altitude as RetentionResourceKind.
type ROPADataCategoryClass string

const (
	ROPADataCategoryIdentityContact        ROPADataCategoryClass = "identity_contact"
	ROPADataCategoryAuthenticationSecurity ROPADataCategoryClass = "authentication_security"
	ROPADataCategoryContentCommunications  ROPADataCategoryClass = "content_communications"
	ROPADataCategoryTechnicalUsage         ROPADataCategoryClass = "technical_usage"
	ROPADataCategoryMediaEvidence          ROPADataCategoryClass = "media_evidence"
	ROPADataCategoryFinancialBilling       ROPADataCategoryClass = "financial_billing"
	ROPADataCategoryConsentEvidence        ROPADataCategoryClass = "consent_evidence"
)

// LawfulBasisStatus records whether a GDPR Article 6 basis has been identified for an activity.
// It defaults to, and in this slice always is, pending: no lawful basis is invented here.
type LawfulBasisStatus string

const (
	LawfulBasisPending    LawfulBasisStatus = "pending"
	LawfulBasisIdentified LawfulBasisStatus = "identified"
)

// LawfulBasisGround is the closed Article 6(1) enum. It is only meaningful once LawfulBasisStatus
// is LawfulBasisIdentified.
type LawfulBasisGround string

const (
	LawfulBasisGroundConsent            LawfulBasisGround = "consent"
	LawfulBasisGroundContract           LawfulBasisGround = "contract"
	LawfulBasisGroundLegalObligation    LawfulBasisGround = "legal_obligation"
	LawfulBasisGroundVitalInterests     LawfulBasisGround = "vital_interests"
	LawfulBasisGroundPublicTask         LawfulBasisGround = "public_task"
	LawfulBasisGroundLegitimateInterest LawfulBasisGround = "legitimate_interests"
)

// DPIAOutcome records a DPIA screening decision. It defaults to, and in this slice always is,
// pending: no required/not_required conclusion is invented here.
type DPIAOutcome string

const (
	DPIAOutcomePending     DPIAOutcome = "pending"
	DPIAOutcomeRequired    DPIAOutcome = "required"
	DPIAOutcomeNotRequired DPIAOutcome = "not_required"
)

// DPIARiskIndicator is the closed set of EDPB WP248 nine high-risk criteria. Tagging that a
// criterion is technically present is an engineering observation already documented elsewhere in
// this repository (see docs/ai-provider-assessment.md); it is not itself a legal conclusion about
// whether a DPIA is required.
type DPIARiskIndicator string

const (
	DPIARiskEvaluationOrScoring          DPIARiskIndicator = "evaluation_or_scoring"
	DPIARiskAutomatedDecisionLegalEffect DPIARiskIndicator = "automated_decision_with_legal_or_similarly_significant_effect"
	DPIARiskSystematicMonitoring         DPIARiskIndicator = "systematic_monitoring"
	DPIARiskSensitiveOrHighlyPersonal    DPIARiskIndicator = "sensitive_or_highly_personal_data"
	DPIARiskLargeScaleProcessing         DPIARiskIndicator = "large_scale_processing"
	DPIARiskMatchingOrCombiningDatasets  DPIARiskIndicator = "matching_or_combining_datasets"
	DPIARiskVulnerableDataSubjects       DPIARiskIndicator = "vulnerable_data_subjects"
	DPIARiskInnovativeOrNewTechnology    DPIARiskIndicator = "innovative_or_new_technology"
	// DPIARiskPreventsRightsOrServiceUse is the ninth EDPB WP248 criterion: the processing, in
	// itself, prevents a data subject from exercising a right or using a service or contract. It
	// is not a transfer criterion; international transfer is not one of the nine WP248 criteria.
	DPIARiskPreventsRightsOrServiceUse DPIARiskIndicator = "prevents_exercise_of_a_right_or_service_or_contract_use"
)

// DPIAScreening is the per-activity DPIA screening snapshot. Every field defaults to its explicit
// pending/unassigned sentinel; only a later founder/legal-reviewed slice may replace a sentinel
// with a real evidenced value.
type DPIAScreening struct {
	RiskIndicators []DPIARiskIndicator
	Outcome        DPIAOutcome
	Reason         string
	Owner          string
	ReviewedAt     *time.Time
	ApprovedBy     string
	ApprovedAt     *time.Time
}

// ProcessingActivityRecord is one Article 30 record-of-processing-activity row. It groups one or
// more retention registry categories under a single business-level activity; RetentionCategories
// must partition the retention registry exactly, and Role/RecipientProviders are validated
// against, or derived from, the retention and provider governance registries rather than
// hand-duplicated.
type ProcessingActivityRecord struct {
	ActivityID          string
	Version             int
	Label               string
	Purpose             string
	Role                RetentionRole
	DataSubjects        []ROPADataSubjectClass
	DataCategories      []ROPADataCategoryClass
	RetentionCategories []string
	SecurityControls    []string
	RecipientProviders  []string
	LawfulBasis         LawfulBasisStatus
	LawfulBasisGrounds  []LawfulBasisGround
	LawfulBasisReason   string
	Owner               string
	// ReviewedBy is the named reviewer accountable for ReviewedAt. It must be paired
	// consistently with ReviewedAt: neither may be set without the other, and both must be
	// non-placeholder before an identified lawful basis can count toward PublicationReady.
	ReviewedBy       string
	ReviewedAt       *time.Time
	DPIA             DPIAScreening
	PublicationReady bool
}

type ROPASummary struct {
	Activities            int
	LawfulBasisPending    int
	LawfulBasisIdentified int
	DPIAPending           int
	DPIARequired          int
	DPIANotRequired       int
	PublicationReady      int
}

type ProcessingActivityRegistry struct {
	Version    int
	Activities []ProcessingActivityRecord
	Summary    ROPASummary
}

// LoadProcessingActivityRegistry validates and assembles the ROPA/DPIA registry. now is required
// explicitly (never time.Now() internally) so staleness and publication readiness can be tested
// deterministically. It loads the provider governance registry first so a ROPA build can never
// proceed against an already-inconsistent retention/provider pair.
func LoadProcessingActivityRegistry(now time.Time) (ProcessingActivityRegistry, error) {
	governance, err := LoadProviderGovernance(now)
	if err != nil {
		return ProcessingActivityRegistry{}, fmt.Errorf("provider governance registry is invalid: %w", err)
	}
	retentionDefinitions := retentionRegistry()
	activities := processingActivityRegistry()
	hydrateRecipientProviders(activities, retentionDefinitions)
	if err := validateProcessingActivityRegistry(activities, retentionDefinitions, governance.Providers, now); err != nil {
		return ProcessingActivityRegistry{}, err
	}
	for index := range activities {
		activities[index].PublicationReady = processingActivityPublicationReady(activities[index], now)
	}
	return ProcessingActivityRegistry{
		Version:    ropaRegistryVersion,
		Activities: activities,
		Summary:    summarizeProcessingActivities(activities),
	}, nil
}

func processingActivityRegistry() []ProcessingActivityRecord {
	const (
		lawfulBasisReasonController = "Fidibeki is the controller for this activity; the applicable " +
			"GDPR Article 6 basis has not been reviewed or recorded by founder/legal."
		lawfulBasisReasonProcessor = "Fidibeki processes this activity solely on behalf of a " +
			"customer controller; the customer determines the Article 6 lawful basis toward their " +
			"own data subjects, and Fidibeki processes only on the customer's documented " +
			"instructions under the applicable Article 28 processor contract, which the provider " +
			"governance registry still records as contract-status pending/unknown. Article 28 is a " +
			"processing-instruction and safeguards framework, not itself an Article 6 lawful basis."
		lawfulBasisReasonControllerProcessor = "This activity spans both a controller and a " +
			"processor role; the applicable Article 6 basis for the controller portion has not been " +
			"reviewed or recorded by founder/legal, and for the processor portion Fidibeki processes " +
			"only on the customer controller's documented instructions under the applicable " +
			"Article 28 contract rather than under an independent Article 6 basis."
		dpiaReasonNoIndicator = "No EDPB WP248 high-risk criterion is currently evidenced in code or " +
			"docs for this activity; a privacy-owner DPIA screening decision has not been recorded."
	)

	return []ProcessingActivityRecord{
		{
			ActivityID: "workspace_identity_and_access", Version: 1,
			Label:   "Workspace identity and access administration",
			Purpose: "Operate tenant identity, membership, projects, access, targeting, and widget configuration.",
			Role:    RetentionRoleControllerProcessor,
			DataSubjects: []ROPADataSubjectClass{
				ROPADataSubjectTenantMembers, ROPADataSubjectPlatformAdmins,
			},
			DataCategories: []ROPADataCategoryClass{
				ROPADataCategoryIdentityContact, ROPADataCategoryTechnicalUsage,
			},
			RetentionCategories: []string{"workspace_configuration"},
			SecurityControls: []string{
				"Forced tenant PostgreSQL RLS and tenant-scoped foreign keys",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonControllerProcessor,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				Outcome: DPIAOutcomePending, Reason: dpiaReasonNoIndicator, Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "feedback_workflow_management", Version: 1,
			Label:   "Feedback and issue workflow management",
			Purpose: "Capture, triage, resolve, and communicate reporter-submitted product feedback and issues.",
			Role:    RetentionRoleProcessor,
			DataSubjects: []ROPADataSubjectClass{
				ROPADataSubjectReporters, ROPADataSubjectTenantMembers, ROPADataSubjectThirdParties,
			},
			DataCategories: []ROPADataCategoryClass{
				ROPADataCategoryContentCommunications, ROPADataCategoryIdentityContact,
			},
			RetentionCategories: []string{"workflow_metadata"},
			SecurityControls: []string{
				"Forced tenant PostgreSQL RLS",
				"tenant:update required for privacy-relevant workflow mutation",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonProcessor,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				RiskIndicators: []DPIARiskIndicator{DPIARiskSensitiveOrHighlyPersonal},
				Outcome:        DPIAOutcomePending,
				Reason: "docs/privacy-data-map.md's Customer Responsibilities section documents that " +
					"free text, structured answers, comments, and page URLs can contain identifiers and " +
					"may require human review; a privacy-owner DPIA screening decision has not been " +
					"recorded. Engineering cannot determine the legal threshold alone.",
				Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "authentication_and_account_security", Version: 1,
			Label:   "Authentication and account security",
			Purpose: "Authenticate members, platform admins, and reporters, and investigate and protect account/session security.",
			Role:    RetentionRoleController,
			DataSubjects: []ROPADataSubjectClass{
				ROPADataSubjectTenantMembers, ROPADataSubjectPlatformAdmins, ROPADataSubjectReporters,
			},
			DataCategories: []ROPADataCategoryClass{ROPADataCategoryAuthenticationSecurity},
			RetentionCategories: []string{
				"auth_magic_links", "authentication_security", "external_identity",
			},
			SecurityControls: []string{
				"Reporter request tokens stored only as SHA-256 hashes and rate limited",
				"Hourly bounded magic-link expiry maintainers (tenantmember.Maintainer, platformadmin.Maintainer)",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonController,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				Outcome: DPIAOutcomePending, Reason: dpiaReasonNoIndicator, Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "attachment_evidence_handling", Version: 1,
			Label:               "Attachment evidence handling",
			Purpose:             "Preserve reporter-provided attachment evidence for the feedback workflow.",
			Role:                RetentionRoleProcessor,
			DataSubjects:        []ROPADataSubjectClass{ROPADataSubjectReporters, ROPADataSubjectThirdParties},
			DataCategories:      []ROPADataCategoryClass{ROPADataCategoryMediaEvidence},
			RetentionCategories: []string{"attachments"},
			SecurityControls: []string{
				"Private S3-compatible object storage with tenant-prefixed keys",
				"Object deletion precedes manifest mutation",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonProcessor,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				RiskIndicators: []DPIARiskIndicator{DPIARiskSensitiveOrHighlyPersonal},
				Outcome:        DPIAOutcomePending,
				Reason: "Screenshot, audio, and video attachments can visually or audibly reveal " +
					"special-category or third-party content despite reporter instructions; a " +
					"privacy-owner DPIA screening decision has not been recorded. Engineering cannot " +
					"determine the legal threshold alone.",
				Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "identity_media_assets", Version: 1,
			Label:               "Identity media assets",
			Purpose:             "Display workspace and member identity through logos and avatars.",
			Role:                RetentionRoleController,
			DataSubjects:        []ROPADataSubjectClass{ROPADataSubjectTenantMembers},
			DataCategories:      []ROPADataCategoryClass{ROPADataCategoryIdentityContact, ROPADataCategoryMediaEvidence},
			RetentionCategories: []string{"identity_assets"},
			SecurityControls: []string{
				"Private S3-compatible object storage with tenant-prefixed keys",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonController,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				Outcome: DPIAOutcomePending, Reason: dpiaReasonNoIndicator, Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "session_replay_reproduction", Version: 1,
			Label:   "Session replay reproduction",
			Purpose: "Reproduce a reporter's session experience when separately authorized, without exposing authenticated identity.",
			Role:    RetentionRoleProcessor,
			DataSubjects: []ROPADataSubjectClass{
				ROPADataSubjectWebsiteVisitors, ROPADataSubjectReporters,
			},
			DataCategories: []ROPADataCategoryClass{
				ROPADataCategoryTechnicalUsage, ROPADataCategoryMediaEvidence,
			},
			RetentionCategories: []string{"session_replays"},
			SecurityControls: []string{
				"Structural anonymous-event contract validated before promotion and merge " +
					"(docs/superpowers/specs/2026-07-23-replay-identity-threat-model.md)",
				"Consent-gated capture with independent backend enforcement",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonProcessor,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				RiskIndicators: []DPIARiskIndicator{DPIARiskSystematicMonitoring},
				Outcome:        DPIAOutcomePending,
				Reason: "Session replay observes a visitor's interactions across a session " +
					"(docs/superpowers/specs/2026-07-23-replay-identity-threat-model.md); a " +
					"privacy-owner DPIA screening decision has not been recorded. Engineering cannot " +
					"determine the legal threshold alone.",
				Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "widget_funnel_analytics", Version: 1,
			Label:               "Widget funnel analytics",
			Purpose:             "Measure consented widget matched/impression/interaction/submit/dismiss funnel performance.",
			Role:                RetentionRoleProcessor,
			DataSubjects:        []ROPADataSubjectClass{ROPADataSubjectWebsiteVisitors},
			DataCategories:      []ROPADataCategoryClass{ROPADataCategoryTechnicalUsage},
			RetentionCategories: []string{"widget_events", "widget_daily", "widget_analytics_browser"},
			SecurityControls: []string{
				"Consent-gated collection with independent backend enforcement (docs/widget-analytics-consent.md)",
				"Pseudonymous visitor identifier isolated from authenticated identity",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonProcessor,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				RiskIndicators: []DPIARiskIndicator{DPIARiskSystematicMonitoring},
				Outcome:        DPIAOutcomePending,
				Reason: "Funnel analytics uses a stable pseudonymous visitor identifier across " +
					"sessions (docs/widget-analytics-consent.md); a privacy-owner DPIA screening " +
					"decision has not been recorded. Engineering cannot determine the legal threshold " +
					"alone.",
				Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "widget_presentation_behavior", Version: 1,
			Label:               "Widget presentation behavior",
			Purpose:             "Decide whether and when a configured widget presentation becomes eligible to display and avoid repetitive prompts.",
			Role:                RetentionRoleProcessor,
			DataSubjects:        []ROPADataSubjectClass{ROPADataSubjectWebsiteVisitors},
			DataCategories:      []ROPADataCategoryClass{ROPADataCategoryTechnicalUsage},
			RetentionCategories: []string{"widget_recurrence", "widget_targeting"},
			SecurityControls: []string{
				"Browser-local storage only; never transmitted to the backend",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonProcessor,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				Outcome: DPIAOutcomePending, Reason: dpiaReasonNoIndicator, Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "consent_evidence_management", Version: 1,
			Label:               "Consent evidence management",
			Purpose:             "Prove grant, denial, withdrawal, expiry, and notice version for each independent consent purpose.",
			Role:                RetentionRoleControllerProcessor,
			DataSubjects:        []ROPADataSubjectClass{ROPADataSubjectWebsiteVisitors, ROPADataSubjectReporters},
			DataCategories:      []ROPADataCategoryClass{ROPADataCategoryConsentEvidence},
			RetentionCategories: []string{"consent_evidence"},
			SecurityControls: []string{
				"Independent per-purpose consent ledgers, storage keys, and runtime gates",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonControllerProcessor,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				Outcome: DPIAOutcomePending, Reason: dpiaReasonNoIndicator, Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "ai_assisted_triage", Version: 1,
			Label:               "AI-assisted feedback triage",
			Purpose:             "Analyze explicitly authorized feedback text or media for AI-assisted triage suggestions.",
			Role:                RetentionRoleProcessor,
			DataSubjects:        []ROPADataSubjectClass{ROPADataSubjectReporters, ROPADataSubjectThirdParties},
			DataCategories:      []ROPADataCategoryClass{ROPADataCategoryContentCommunications, ROPADataCategoryMediaEvidence},
			RetentionCategories: []string{"ai_provider_files", "ai_processing"},
			SecurityControls: []string{
				"Deterministic pre/post redaction with quarantine of unsafe output (docs/ai-provider-assessment.md)",
				"Immediate provider file deletion with retry ledger",
				"Per-rule opt-in with explicit input-category selection",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonProcessor,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				RiskIndicators: []DPIARiskIndicator{
					DPIARiskEvaluationOrScoring, DPIARiskSensitiveOrHighlyPersonal,
					DPIARiskInnovativeOrNewTechnology, DPIARiskLargeScaleProcessing,
				},
				Outcome: DPIAOutcomePending,
				Reason: "docs/ai-provider-assessment.md already documents these technical risk " +
					"indicators (free-form text/media at scale, potential special-category content " +
					"despite reporter instructions, a third-party generative model, and triage " +
					"suggestions that could affect how a complaint is handled) and explicitly defers " +
					"the required/not-required decision to the privacy owner; engineering cannot " +
					"determine the legal threshold alone.",
				Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "integration_notification_delivery", Version: 1,
			Label:               "Integration notification delivery",
			Purpose:             "Deliver minimized operational notifications to customer-configured Slack, Teams, and webhook destinations, record external issue-tracker configuration and creation attempts, and track follow-up on already-disclosed copies.",
			Role:                RetentionRoleProcessor,
			DataSubjects:        []ROPADataSubjectClass{ROPADataSubjectTenantMembers, ROPADataSubjectReporters},
			DataCategories:      []ROPADataCategoryClass{ROPADataCategoryContentCommunications, ROPADataCategoryTechnicalUsage},
			RetentionCategories: []string{"integration_delivery"},
			SecurityControls: []string{
				"Versioned per-destination field allowlist; delivered payload is never logged",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonProcessor,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				Outcome: DPIAOutcomePending, Reason: dpiaReasonNoIndicator, Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "privacy_rights_operations", Version: 1,
			Label:   "Privacy-rights operations",
			Purpose: "Verify, execute, review, and evidence data-subject and member privacy-rights operations, including temporary export archives and reporter request access.",
			Role:    RetentionRoleControllerProcessor,
			DataSubjects: []ROPADataSubjectClass{
				ROPADataSubjectReporters, ROPADataSubjectTenantMembers, ROPADataSubjectThirdParties,
			},
			DataCategories: []ROPADataCategoryClass{
				ROPADataCategoryIdentityContact, ROPADataCategoryContentCommunications,
			},
			RetentionCategories: []string{
				"privacy_rights", "export_archives", "unverified_requests", "verified_requests",
			},
			SecurityControls: []string{
				"Verified reporter/member access tokens",
				"Manual completion blocked until corresponding correction/erasure evidence exists",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonControllerProcessor,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				Outcome: DPIAOutcomePending, Reason: dpiaReasonNoIndicator, Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "security_and_access_audit", Version: 1,
			Label:   "Security and access audit",
			Purpose: "Investigate sensitive access and demonstrate accountable use of sensitive resources.",
			Role:    RetentionRoleController,
			DataSubjects: []ROPADataSubjectClass{
				ROPADataSubjectTenantMembers, ROPADataSubjectPlatformAdmins,
			},
			DataCategories: []ROPADataCategoryClass{
				ROPADataCategoryAuthenticationSecurity, ROPADataCategoryTechnicalUsage,
			},
			RetentionCategories: []string{"access_security_audit"},
			SecurityControls: []string{
				"Append-only sensitive-access and security event evidence",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonController,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				Outcome: DPIAOutcomePending, Reason: dpiaReasonNoIndicator, Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "billing_and_subscription_management", Version: 1,
			Label:               "Billing and subscription management",
			Purpose:             "Operate subscriptions, entitlement, and accounting evidence.",
			Role:                RetentionRoleController,
			DataSubjects:        []ROPADataSubjectClass{ROPADataSubjectBillingContacts},
			DataCategories:      []ROPADataCategoryClass{ROPADataCategoryFinancialBilling},
			RetentionCategories: []string{"billing_records"},
			SecurityControls: []string{
				"Configured billing provider (Stripe) as an external processor boundary",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonController,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				Outcome: DPIAOutcomePending, Reason: dpiaReasonNoIndicator, Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "breach_response_recordkeeping", Version: 1,
			Label:   "Breach response recordkeeping",
			Purpose: "Preserve restricted personal-data breach facts, assessments, chronology, and response evidence.",
			Role:    RetentionRoleControllerProcessor,
			DataSubjects: []ROPADataSubjectClass{
				ROPADataSubjectTenantMembers, ROPADataSubjectWebsiteVisitors, ROPADataSubjectReporters,
				ROPADataSubjectPlatformAdmins, ROPADataSubjectThirdParties,
			},
			DataCategories:      []ROPADataCategoryClass{ROPADataCategoryIdentityContact, ROPADataCategoryTechnicalUsage},
			RetentionCategories: []string{"breach_register"},
			SecurityControls: []string{
				"Platform-admin-only connection boundary; register is append-only and excluded from the tenant runtime role",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonControllerProcessor,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				Outcome: DPIAOutcomePending, Reason: dpiaReasonNoIndicator, Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "abuse_and_bot_diagnostics", Version: 1,
			Label:               "Abuse and bot diagnostics",
			Purpose:             "Investigate bounded abuse and rate-limit signals without retaining content.",
			Role:                RetentionRoleController,
			DataSubjects:        []ROPADataSubjectClass{ROPADataSubjectWebsiteVisitors},
			DataCategories:      []ROPADataCategoryClass{ROPADataCategoryTechnicalUsage},
			RetentionCategories: []string{"abuse_diagnostics"},
			SecurityControls: []string{
				"Minimized hourly diagnostic buckets containing no content",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonController,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				Outcome: DPIAOutcomePending, Reason: dpiaReasonNoIndicator, Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "external_communications_delivery", Version: 1,
			Label:   "External communications delivery",
			Purpose: "Deliver transactional and lifecycle email, configured operational notifications, and reviewer-confirmed issue copies to an enabled external issue tracker.",
			Role:    RetentionRoleControllerProcessor,
			DataSubjects: []ROPADataSubjectClass{
				ROPADataSubjectTenantMembers, ROPADataSubjectReporters, ROPADataSubjectPlatformAdmins,
				ROPADataSubjectBillingContacts,
			},
			DataCategories:      []ROPADataCategoryClass{ROPADataCategoryIdentityContact, ROPADataCategoryContentCommunications},
			RetentionCategories: []string{"external_communications"},
			SecurityControls: []string{
				"Configured email/webhook provider boundaries with minimized field allowlists",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonControllerProcessor,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				Outcome: DPIAOutcomePending, Reason: dpiaReasonNoIndicator, Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "infrastructure_and_platform_operations", Version: 1,
			Label:   "Infrastructure and platform operations",
			Purpose: "Store application data, execute background queues, bill workspaces, and observe operational failures across all other activities.",
			Role:    RetentionRoleControllerProcessor,
			DataSubjects: []ROPADataSubjectClass{
				ROPADataSubjectTenantMembers, ROPADataSubjectReporters,
				ROPADataSubjectWebsiteVisitors, ROPADataSubjectPlatformAdmins,
				ROPADataSubjectBillingContacts, ROPADataSubjectThirdParties,
			},
			DataCategories: []ROPADataCategoryClass{
				ROPADataCategoryTechnicalUsage, ROPADataCategoryIdentityContact,
				ROPADataCategoryContentCommunications, ROPADataCategoryMediaEvidence,
				ROPADataCategoryFinancialBilling,
			},
			RetentionCategories: []string{"external_infrastructure"},
			SecurityControls: []string{
				"Provider governance registry tracks legal entity, location, transfer, and contract evidence per boundary",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonControllerProcessor,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				RiskIndicators: []DPIARiskIndicator{DPIARiskLargeScaleProcessing},
				Outcome:        DPIAOutcomePending,
				Reason: "This boundary stores or transports the personal data of every other " +
					"activity across every tenant, matching the EDPB large-scale processing factors " +
					"(subject count, data volume, and duration); a privacy-owner DPIA screening " +
					"decision has not been recorded. Engineering cannot determine the legal threshold " +
					"alone.",
				Owner: governanceUnassigned,
			},
		},
		{
			ActivityID: "signup_abuse_verification", Version: 1,
			Label:               "Signup abuse verification",
			Purpose:             "Verify a signup request is not automated abuse before creating a workspace.",
			Role:                RetentionRoleController,
			DataSubjects:        []ROPADataSubjectClass{ROPADataSubjectWebsiteVisitors},
			DataCategories:      []ROPADataCategoryClass{ROPADataCategoryTechnicalUsage},
			RetentionCategories: []string{"security_verification"},
			SecurityControls: []string{
				"Challenge token and submitting IP address verified directly with Cloudflare " +
					"Turnstile; not separately stored by Fidibeki",
			},
			LawfulBasis: LawfulBasisPending, LawfulBasisReason: lawfulBasisReasonController,
			Owner: governanceUnassigned, ReviewedBy: governanceUnassigned,
			DPIA: DPIAScreening{
				Outcome: DPIAOutcomePending, Reason: dpiaReasonNoIndicator, Owner: governanceUnassigned,
			},
		},
	}
}

// hydrateRecipientProviders derives each activity's RecipientProviders from the RetentionProvider
// resources already attached to its member retention categories, so recipient/provider evidence
// can only ever equal what the retention and provider governance registries already assert.
func hydrateRecipientProviders(activities []ProcessingActivityRecord, definitions []retentionDefinition) {
	byCategory := make(map[string]retentionDefinition, len(definitions))
	for _, definition := range definitions {
		byCategory[definition.Category] = definition
	}
	for index := range activities {
		seen := make(map[string]struct{})
		var providers []string
		for _, category := range activities[index].RetentionCategories {
			definition, ok := byCategory[category]
			if !ok {
				continue
			}
			for _, resource := range definition.Resources {
				if resource.Kind != RetentionProvider {
					continue
				}
				if _, exists := seen[resource.Name]; exists {
					continue
				}
				seen[resource.Name] = struct{}{}
				providers = append(providers, resource.Name)
			}
		}
		sort.Strings(providers)
		activities[index].RecipientProviders = providers
	}
}

func validateProcessingActivityRegistry(
	activities []ProcessingActivityRecord,
	retentionDefinitions []retentionDefinition,
	providers []ProviderGovernanceRecord,
	now time.Time,
) error {
	if now.IsZero() {
		return errors.New("ROPA validation time must not be zero")
	}
	if len(activities) == 0 {
		return errors.New("processing activity registry is empty")
	}

	categoryOwner := make(map[string]retentionDefinition, len(retentionDefinitions))
	for _, definition := range retentionDefinitions {
		categoryOwner[definition.Category] = definition
	}
	providerIDs := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		providerIDs[provider.ProviderID] = struct{}{}
	}

	activityIDs := make(map[string]struct{}, len(activities))
	categoryAssignedTo := make(map[string]string, len(retentionDefinitions))
	for _, activity := range activities {
		if err := validateProcessingActivityFields(activity); err != nil {
			return err
		}
		if _, exists := activityIDs[activity.ActivityID]; exists {
			return fmt.Errorf("duplicate processing activity %q", activity.ActivityID)
		}
		activityIDs[activity.ActivityID] = struct{}{}

		var derivedRole RetentionRole
		for _, category := range activity.RetentionCategories {
			definition, ok := categoryOwner[category]
			if !ok {
				return fmt.Errorf(
					"processing activity %q references unknown retention category %q",
					activity.ActivityID, category,
				)
			}
			if owner, exists := categoryAssignedTo[category]; exists {
				return fmt.Errorf(
					"retention category %q is assigned to both %q and %q",
					category, owner, activity.ActivityID,
				)
			}
			categoryAssignedTo[category] = activity.ActivityID

			if derivedRole == "" {
				derivedRole = definition.Role
			} else if derivedRole != definition.Role {
				return fmt.Errorf(
					"processing activity %q groups retention categories with inconsistent roles (%q vs %q)",
					activity.ActivityID, derivedRole, definition.Role,
				)
			}
		}
		if derivedRole != activity.Role {
			return fmt.Errorf(
				"processing activity %q declares role %q but its retention categories evidence role %q",
				activity.ActivityID, activity.Role, derivedRole,
			)
		}

		for _, providerID := range activity.RecipientProviders {
			if _, ok := providerIDs[providerID]; !ok {
				return fmt.Errorf(
					"processing activity %q references unknown provider governance entry %q",
					activity.ActivityID, providerID,
				)
			}
		}

		if err := validateLawfulBasis(activity); err != nil {
			return err
		}
		if err := validateDPIAScreening(activity, now); err != nil {
			return err
		}
		if err := validateActivityReview(activity, now); err != nil {
			return err
		}
	}

	for category := range categoryOwner {
		if _, ok := categoryAssignedTo[category]; !ok {
			return fmt.Errorf(
				"retention category %q is missing from the processing activity registry", category,
			)
		}
	}
	return nil
}

func validateProcessingActivityFields(activity ProcessingActivityRecord) error {
	if strings.TrimSpace(activity.ActivityID) == "" ||
		strings.TrimSpace(activity.Label) == "" ||
		strings.TrimSpace(activity.Purpose) == "" ||
		strings.TrimSpace(activity.Owner) == "" ||
		strings.TrimSpace(activity.ReviewedBy) == "" ||
		strings.TrimSpace(activity.LawfulBasisReason) == "" ||
		activity.Version <= 0 {
		return fmt.Errorf("processing activity %q is incomplete", activity.ActivityID)
	}
	// PublicationReady is computed by LoadProcessingActivityRegistry after validation; a source
	// literal must never assert it directly.
	if activity.PublicationReady {
		return fmt.Errorf(
			"processing activity %q asserts publication readiness in its source literal; "+
				"PublicationReady must only be computed", activity.ActivityID,
		)
	}
	switch activity.Role {
	case RetentionRoleController, RetentionRoleProcessor, RetentionRoleControllerProcessor:
	default:
		return fmt.Errorf("processing activity %q has unknown role %q", activity.ActivityID, activity.Role)
	}
	if len(activity.RetentionCategories) == 0 {
		return fmt.Errorf("processing activity %q references no retention category", activity.ActivityID)
	}
	if err := validateNoDuplicateStrings(
		activity.ActivityID, "retention category", activity.RetentionCategories,
	); err != nil {
		return err
	}
	if err := validateROPADataSubjects(activity); err != nil {
		return err
	}
	if err := validateROPADataCategories(activity); err != nil {
		return err
	}
	return validateROPASecurityControls(activity)
}

func validateROPADataSubjects(activity ProcessingActivityRecord) error {
	if len(activity.DataSubjects) == 0 || len(activity.DataSubjects) > maxROPAClassifications {
		return fmt.Errorf("processing activity %q has an invalid data-subject list", activity.ActivityID)
	}
	seen := make(map[ROPADataSubjectClass]struct{}, len(activity.DataSubjects))
	for _, subject := range activity.DataSubjects {
		switch subject {
		case ROPADataSubjectTenantMembers, ROPADataSubjectReporters, ROPADataSubjectWebsiteVisitors,
			ROPADataSubjectPlatformAdmins, ROPADataSubjectBillingContacts, ROPADataSubjectThirdParties:
		default:
			return fmt.Errorf(
				"processing activity %q has unknown data-subject class %q", activity.ActivityID, subject,
			)
		}
		if _, exists := seen[subject]; exists {
			return fmt.Errorf(
				"processing activity %q has duplicate data-subject class %q", activity.ActivityID, subject,
			)
		}
		seen[subject] = struct{}{}
	}
	return nil
}

func validateROPADataCategories(activity ProcessingActivityRecord) error {
	if len(activity.DataCategories) == 0 || len(activity.DataCategories) > maxROPAClassifications {
		return fmt.Errorf("processing activity %q has an invalid data-category list", activity.ActivityID)
	}
	seen := make(map[ROPADataCategoryClass]struct{}, len(activity.DataCategories))
	for _, category := range activity.DataCategories {
		switch category {
		case ROPADataCategoryIdentityContact, ROPADataCategoryAuthenticationSecurity,
			ROPADataCategoryContentCommunications, ROPADataCategoryTechnicalUsage,
			ROPADataCategoryMediaEvidence, ROPADataCategoryFinancialBilling,
			ROPADataCategoryConsentEvidence:
		default:
			return fmt.Errorf(
				"processing activity %q has unknown data-category class %q", activity.ActivityID, category,
			)
		}
		if _, exists := seen[category]; exists {
			return fmt.Errorf(
				"processing activity %q has duplicate data-category class %q", activity.ActivityID, category,
			)
		}
		seen[category] = struct{}{}
	}
	return nil
}

func validateROPASecurityControls(activity ProcessingActivityRecord) error {
	if len(activity.SecurityControls) == 0 || len(activity.SecurityControls) > maxROPASecurityControls {
		return fmt.Errorf("processing activity %q has an invalid security-control list", activity.ActivityID)
	}
	seen := make(map[string]struct{}, len(activity.SecurityControls))
	for _, control := range activity.SecurityControls {
		trimmed := strings.TrimSpace(control)
		if trimmed == "" {
			return fmt.Errorf("processing activity %q has a blank security control", activity.ActivityID)
		}
		if len(trimmed) > maxROPAControlLen {
			return fmt.Errorf("processing activity %q has an oversized security control", activity.ActivityID)
		}
		normalized := strings.ToLower(trimmed)
		if _, exists := seen[normalized]; exists {
			return fmt.Errorf(
				"processing activity %q has a duplicate security control %q", activity.ActivityID, trimmed,
			)
		}
		seen[normalized] = struct{}{}
	}
	return nil
}

func validateNoDuplicateStrings(activityID, label string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("processing activity %q has a blank %s", activityID, label)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("processing activity %q has a duplicate %s %q", activityID, label, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateLawfulBasis(activity ProcessingActivityRecord) error {
	switch activity.LawfulBasis {
	case LawfulBasisPending:
		if len(activity.LawfulBasisGrounds) != 0 {
			return fmt.Errorf(
				"processing activity %q is lawful-basis pending but carries lawful-basis grounds",
				activity.ActivityID,
			)
		}
	case LawfulBasisIdentified:
		if len(activity.LawfulBasisGrounds) == 0 {
			return fmt.Errorf(
				"processing activity %q identifies a lawful basis without any ground", activity.ActivityID,
			)
		}
		if governancePlaceholder(activity.LawfulBasisReason) {
			return fmt.Errorf(
				"processing activity %q identifies a lawful basis with a placeholder reason",
				activity.ActivityID,
			)
		}
		seen := make(map[LawfulBasisGround]struct{}, len(activity.LawfulBasisGrounds))
		for _, ground := range activity.LawfulBasisGrounds {
			switch ground {
			case LawfulBasisGroundConsent, LawfulBasisGroundContract, LawfulBasisGroundLegalObligation,
				LawfulBasisGroundVitalInterests, LawfulBasisGroundPublicTask,
				LawfulBasisGroundLegitimateInterest:
			default:
				return fmt.Errorf(
					"processing activity %q has unknown lawful-basis ground %q", activity.ActivityID, ground,
				)
			}
			if _, exists := seen[ground]; exists {
				return fmt.Errorf(
					"processing activity %q has duplicate lawful-basis ground %q", activity.ActivityID, ground,
				)
			}
			seen[ground] = struct{}{}
		}
	default:
		return fmt.Errorf(
			"processing activity %q has unknown lawful-basis status %q", activity.ActivityID, activity.LawfulBasis,
		)
	}
	return nil
}

func validateDPIAScreening(activity ProcessingActivityRecord, now time.Time) error {
	dpia := activity.DPIA
	if strings.TrimSpace(dpia.Reason) == "" || strings.TrimSpace(dpia.Owner) == "" {
		return fmt.Errorf("processing activity %q has an incomplete DPIA screening", activity.ActivityID)
	}
	seen := make(map[DPIARiskIndicator]struct{}, len(dpia.RiskIndicators))
	for _, indicator := range dpia.RiskIndicators {
		switch indicator {
		case DPIARiskEvaluationOrScoring, DPIARiskAutomatedDecisionLegalEffect, DPIARiskSystematicMonitoring,
			DPIARiskSensitiveOrHighlyPersonal, DPIARiskLargeScaleProcessing, DPIARiskMatchingOrCombiningDatasets,
			DPIARiskVulnerableDataSubjects, DPIARiskInnovativeOrNewTechnology, DPIARiskPreventsRightsOrServiceUse:
		default:
			return fmt.Errorf(
				"processing activity %q has unknown DPIA risk indicator %q", activity.ActivityID, indicator,
			)
		}
		if _, exists := seen[indicator]; exists {
			return fmt.Errorf(
				"processing activity %q has duplicate DPIA risk indicator %q", activity.ActivityID, indicator,
			)
		}
		seen[indicator] = struct{}{}
	}

	switch dpia.Outcome {
	case DPIAOutcomePending:
		if strings.TrimSpace(dpia.ApprovedBy) != "" || dpia.ApprovedAt != nil {
			return fmt.Errorf(
				"processing activity %q has a pending DPIA outcome but carries approval evidence",
				activity.ActivityID,
			)
		}
	case DPIAOutcomeRequired, DPIAOutcomeNotRequired:
		if governancePlaceholder(dpia.Owner) || governancePlaceholder(dpia.ApprovedBy) ||
			governancePlaceholder(dpia.Reason) || dpia.ReviewedAt == nil || dpia.ApprovedAt == nil {
			return fmt.Errorf(
				"processing activity %q has a %s DPIA outcome without complete owner, reason, "+
					"review, and approval evidence",
				activity.ActivityID, dpia.Outcome,
			)
		}
	default:
		return fmt.Errorf(
			"processing activity %q has unknown DPIA outcome %q", activity.ActivityID, dpia.Outcome,
		)
	}

	if err := validateReviewStamp(
		fmt.Sprintf("processing activity %q DPIA", activity.ActivityID), dpia.ReviewedAt, now,
	); err != nil {
		return err
	}
	if dpia.ApprovedAt != nil {
		if dpia.ReviewedAt == nil {
			return fmt.Errorf(
				"processing activity %q has a DPIA approved-at timestamp without a reviewed-at timestamp",
				activity.ActivityID,
			)
		}
		if dpia.ApprovedAt.IsZero() || dpia.ApprovedAt.After(now) {
			return fmt.Errorf(
				"processing activity %q has an invalid DPIA approved-at timestamp %s",
				activity.ActivityID, dpia.ApprovedAt.Format(time.RFC3339),
			)
		}
		if dpia.ApprovedAt.Before(*dpia.ReviewedAt) {
			return fmt.Errorf(
				"processing activity %q has a DPIA approved-at timestamp before its reviewed-at timestamp",
				activity.ActivityID,
			)
		}
	}
	return nil
}

// validateReviewStamp rejects a zero or future reviewedAt, and one older than the shared
// governance review staleness window. It is reused, not redefined, from the same policy the
// provider governance registry already enforces.
func validateReviewStamp(label string, reviewedAt *time.Time, now time.Time) error {
	if reviewedAt == nil {
		return nil
	}
	if reviewedAt.IsZero() || reviewedAt.After(now) {
		return fmt.Errorf("%s has an invalid reviewed-at timestamp %s", label, reviewedAt.Format(time.RFC3339))
	}
	if now.Sub(*reviewedAt) > governanceReviewStalenessWindow {
		return fmt.Errorf("%s has a stale review dated %s", label, reviewedAt.Format("2006-01-02"))
	}
	return nil
}

// validateActivityReview requires ReviewedBy and ReviewedAt to be paired consistently: a named,
// non-placeholder reviewer without a review date, or a review date without a named reviewer, both
// describe a review that did not actually happen and are rejected.
func validateActivityReview(activity ProcessingActivityRecord, now time.Time) error {
	reviewerNamed := !governancePlaceholder(activity.ReviewedBy)
	if reviewerNamed && activity.ReviewedAt == nil {
		return fmt.Errorf(
			"processing activity %q names a reviewer without a reviewed-at timestamp", activity.ActivityID,
		)
	}
	if !reviewerNamed && activity.ReviewedAt != nil {
		return fmt.Errorf(
			"processing activity %q has a reviewed-at timestamp without a named reviewer", activity.ActivityID,
		)
	}
	return validateReviewStamp(fmt.Sprintf("processing activity %q", activity.ActivityID), activity.ReviewedAt, now)
}

// processingActivityPublicationReady computes readiness rather than trusting a literal field, so
// no activity can claim it by editing a boolean instead of recording real evidence.
//
// A DPIA outcome of "required" is a screening conclusion, not proof that the required DPIA was
// actually carried out. This registry has no DPIA-completion evidence model (no field records that
// a full DPIA document was produced, reviewed, and signed off), so an activity whose DPIA outcome
// is "required" can never be publication ready here, no matter how complete its owner/reviewer/
// approval fields are. Only a fully evidenced "not_required" screening can currently satisfy this
// gate; adding a completion-evidenced path for "required" is out of scope for this slice.
func processingActivityPublicationReady(activity ProcessingActivityRecord, now time.Time) bool {
	if activity.LawfulBasis != LawfulBasisIdentified || len(activity.LawfulBasisGrounds) == 0 ||
		governancePlaceholder(activity.LawfulBasisReason) {
		return false
	}
	if governancePlaceholder(activity.Owner) || governancePlaceholder(activity.ReviewedBy) ||
		activity.ReviewedAt == nil || now.Sub(*activity.ReviewedAt) > governanceReviewStalenessWindow {
		return false
	}
	dpia := activity.DPIA
	if dpia.Outcome != DPIAOutcomeNotRequired {
		return false
	}
	if governancePlaceholder(dpia.Owner) || governancePlaceholder(dpia.ApprovedBy) ||
		governancePlaceholder(dpia.Reason) || dpia.ReviewedAt == nil || dpia.ApprovedAt == nil {
		return false
	}
	if now.Sub(*dpia.ReviewedAt) > governanceReviewStalenessWindow {
		return false
	}
	return len(activity.RetentionCategories) > 0 && len(activity.SecurityControls) > 0 &&
		len(activity.DataSubjects) > 0 && len(activity.DataCategories) > 0
}

func summarizeProcessingActivities(activities []ProcessingActivityRecord) ROPASummary {
	summary := ROPASummary{Activities: len(activities)}
	for _, activity := range activities {
		switch activity.LawfulBasis {
		case LawfulBasisPending:
			summary.LawfulBasisPending++
		case LawfulBasisIdentified:
			summary.LawfulBasisIdentified++
		}
		switch activity.DPIA.Outcome {
		case DPIAOutcomePending:
			summary.DPIAPending++
		case DPIAOutcomeRequired:
			summary.DPIARequired++
		case DPIAOutcomeNotRequired:
			summary.DPIANotRequired++
		}
		if activity.PublicationReady {
			summary.PublicationReady++
		}
	}
	return summary
}
