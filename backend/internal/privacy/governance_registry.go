package privacy

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const governanceRegistryVersion = 1

// governanceReviewStalenessWindow bounds how long a recorded review may be treated as current.
const governanceReviewStalenessWindow = 180 * 24 * time.Hour

const (
	maxGovernancePersonalDataCategories  = 32
	maxGovernancePersonalDataCategoryLen = 200
)

const (
	governanceUnknown    = "unknown"
	governanceUnassigned = "unassigned"
	governancePending    = "pending"
)

type GovernanceContractStatus string

const (
	GovernanceContractUnknown  GovernanceContractStatus = "unknown"
	GovernanceContractPending  GovernanceContractStatus = "pending"
	GovernanceContractExecuted GovernanceContractStatus = "executed"
)

type GovernanceApprovalStatus string

const (
	GovernanceApprovalPending  GovernanceApprovalStatus = "pending"
	GovernanceApprovalApproved GovernanceApprovalStatus = "approved"
)

// GovernanceBoundaryKind is a closed classification of the relationship a boundary has to
// Fidibeki. Not every evidenced boundary is a subprocessor: infrastructure hosting, a genuine
// third-party service provider, a customer's own configured destination, and a purely internal
// runtime fallback are legally and factually distinct relationships, so the registry records which
// one applies instead of describing every row as a subprocessor.
type GovernanceBoundaryKind string

const (
	// GovernanceBoundaryInfrastructureProvider hosts or runs application infrastructure Fidibeki
	// operates (database, queue, object storage).
	GovernanceBoundaryInfrastructureProvider GovernanceBoundaryKind = "infrastructure_provider"
	// GovernanceBoundaryServiceProvider is an external service used for a specific product
	// function. Its precise controller/processor/subprocessor status remains a legal-review field.
	GovernanceBoundaryServiceProvider GovernanceBoundaryKind = "service_provider"
	// GovernanceBoundaryCustomerConfiguredDestination is a recipient a customer configures
	// themselves (their own Slack/Teams/webhook); Fidibeki delivers to it but does not choose it.
	GovernanceBoundaryCustomerConfiguredDestination GovernanceBoundaryKind = "customer_configured_destination"
	// GovernanceBoundaryInternalRuntime never leaves the host Fidibeki operates; it is not a
	// third party at all.
	GovernanceBoundaryInternalRuntime GovernanceBoundaryKind = "internal_runtime_boundary"
)

// GovernanceEnvironmentEnablement describes how a provider actually activates in a running
// instance. It never asserts that a provider is enabled anywhere; it only records the mechanism
// evidenced by code, which is why it is tracked separately from the provider's code presence
// (every registered provider is, by definition, present in code).
type GovernanceEnvironmentEnablement string

const (
	GovernanceEnvironmentAlwaysRequired        GovernanceEnvironmentEnablement = "always_required_by_config"
	GovernanceEnvironmentStagingProduction     GovernanceEnvironmentEnablement = "required_in_staging_and_production"
	GovernanceEnvironmentProductionOnly        GovernanceEnvironmentEnablement = "required_in_production_only"
	GovernanceEnvironmentConditionalOnDriver   GovernanceEnvironmentEnablement = "conditional_on_driver_selection"
	GovernanceEnvironmentOptionalNeverRequired GovernanceEnvironmentEnablement = "optional_never_required"
	GovernanceEnvironmentPerTenantConfig       GovernanceEnvironmentEnablement = "per_tenant_product_configuration"
)

// ProviderGovernanceRecord is one code-evidenced provider/infrastructure/destination boundary.
// BoundaryKind records what kind of relationship it actually is (see GovernanceBoundaryKind); not
// every entry is a subprocessor. DisplayName is the boundary's service name; Purpose and
// PersonalDataCategories record, per #63's requirement, why the boundary is reached and what
// personal-data categories cross it. Every governance field defaults to an explicit
// unknown/pending/unassigned sentinel; only a later, founder/legal-reviewed slice may replace a
// sentinel with a real, evidenced value.
type ProviderGovernanceRecord struct {
	ProviderID             string
	DisplayName            string
	AdapterCategory        string
	BoundaryKind           GovernanceBoundaryKind
	Purpose                string
	PersonalDataCategories []string
	CodeEvidence           string
	EnablementSignal       string
	EnvironmentEnablement  GovernanceEnvironmentEnablement
	LegalEntity            string
	ProcessingLocations    []string
	TransferMechanism      string
	ContractStatus         GovernanceContractStatus
	RetentionReviewRef     string
	Owner                  string
	ReviewedAt             *time.Time
	Approval               GovernanceApprovalStatus
	ApprovedBy             string
	ApprovedAt             *time.Time
}

// GovernanceRoleMatrixEntry is one row of the controller/processor role matrix. It is derived
// from the retention registry rather than hand-maintained, so it cannot disagree with it.
type GovernanceRoleMatrixEntry struct {
	Category string
	Label    string
	Purpose  string
	Role     RetentionRole
}

type GovernanceSummary struct {
	Approved         int
	PendingApproval  int
	ContractExecuted int
	ContractPending  int
	ContractUnknown  int
}

type ProviderGovernanceRegistry struct {
	Version    int
	Providers  []ProviderGovernanceRecord
	RoleMatrix []GovernanceRoleMatrixEntry
	Summary    GovernanceSummary
}

// LoadProviderGovernance validates and assembles the provider governance registry and the
// controller/processor role matrix. now is required explicitly (never time.Now() internally) so
// staleness can be tested deterministically.
//
// Limitation: cross-checking this registry's provider set against the retention registry's
// evidenced RetentionProvider resources detects drift BETWEEN the two registries (a boundary added
// to one but not the other). It cannot, on its own, discover a wholly new external adapter that was
// never added to either registry — that still depends on a human recording the new boundary when
// the code introducing it is written. This is a consistency check between two hand-maintained
// registries, not a static-analysis scan of the codebase for outbound network calls.
func LoadProviderGovernance(now time.Time) (ProviderGovernanceRegistry, error) {
	retentionDefinitions := retentionRegistry()
	if err := validateRetentionRegistry(retentionDefinitions); err != nil {
		return ProviderGovernanceRegistry{}, fmt.Errorf("retention registry is invalid: %w", err)
	}
	providers := providerGovernanceRegistry()
	roleMatrix := governanceRoleMatrix(retentionDefinitions)
	if err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now); err != nil {
		return ProviderGovernanceRegistry{}, err
	}
	return ProviderGovernanceRegistry{
		Version:    governanceRegistryVersion,
		Providers:  providers,
		RoleMatrix: roleMatrix,
		Summary:    summarizeProviderGovernance(providers),
	}, nil
}

func providerGovernanceRegistry() []ProviderGovernanceRecord {
	return []ProviderGovernanceRecord{
		pendingProvider(
			"postgresql_runtime", "PostgreSQL runtime database", "hosting_database",
			GovernanceBoundaryInfrastructureProvider,
			"Persist tenant-scoped application data for all product operations.",
			[]string{
				"workspace, member, and reporter identity and contact data",
				"authentication, session, security, and technical identifiers",
				"feedback, issue, comment, and resolution communication content",
				"attachment and session-replay manifests and derived AI analysis",
				"pseudonymous widget analytics and consent evidence",
				"integration configuration, delivery state, and audit records",
				"billing customer and subscription records",
				"privacy-rights and breach-response records",
			},
			"backend/internal/config/config.go (DatabaseURLApp, DatabaseURLAdmin, DatabaseURLDirect)",
			"DATABASE_URL_APP, DATABASE_URL_ADMIN, DATABASE_URL_DIRECT required in every environment",
			GovernanceEnvironmentAlwaysRequired, "external_infrastructure",
		),
		pendingProvider(
			"redis_asynq", "Redis / Asynq queue runtime", "cache_queue",
			GovernanceBoundaryInfrastructureProvider,
			"Queue and rate-limit background work.",
			[]string{
				"pseudonymous and internal record identifiers in queued jobs and rate-limit keys",
				"task-specific delivery and notification content, including recipient contact data where applicable",
				"job metadata and failure diagnostics",
			},
			"backend/internal/config/config.go (RedisURL)",
			"REDIS_URL required in every environment",
			GovernanceEnvironmentAlwaysRequired, "external_infrastructure",
		),
		pendingProvider(
			"s3_compatible_object_storage", "S3-compatible object storage", "object_storage",
			GovernanceBoundaryInfrastructureProvider,
			"Store attachment, session-replay, and export objects.",
			[]string{
				"attachment media (screenshot, audio, video)",
				"session-replay objects",
				"workspace logos and member avatars",
				"temporary export archives",
			},
			"backend/internal/config/config.go (S3Endpoint, S3Bucket, S3AccessKey, S3SecretKey)",
			"S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY, S3_SECRET_KEY required in every environment",
			GovernanceEnvironmentAlwaysRequired, "external_infrastructure",
		),
		pendingProvider(
			"resend_transactional_email", "Resend transactional email", "email",
			GovernanceBoundaryServiceProvider,
			"Deliver transactional and lifecycle email to workspace members and reporters.",
			[]string{"recipient email address", "transactional message content"},
			"backend/internal/config/config.go (ResendAPIKey)",
			"EMAIL_DRIVER=resend selects this provider and then requires RESEND_API_KEY",
			GovernanceEnvironmentConditionalOnDriver, "external_communications",
		),
		pendingProvider(
			"email_capture_filesystem", "Local filesystem email capture", "email",
			GovernanceBoundaryInternalRuntime,
			"Capture outgoing email to local disk instead of delivering through an external "+
				"provider (default/local driver).",
			[]string{"recipient email address", "transactional message content"},
			"backend/internal/config/config.go (EmailCaptureDir)",
			"EMAIL_DRIVER=capture (default) writes messages under EMAIL_CAPTURE_DIR instead of a provider",
			GovernanceEnvironmentConditionalOnDriver, "external_communications",
		),
		pendingProvider(
			"google_gemini_api", "Google Gemini Developer API", "ai",
			GovernanceBoundaryServiceProvider,
			"Analyze explicitly authorized feedback attachments or text for AI-assisted triage.",
			[]string{"explicitly authorized feedback attachment media or text"},
			"backend/internal/config/config.go (GeminiAPIKey); "+
				"docs/superpowers/specs/2026-07-24-ai-privacy-governance-design.md",
			"GEMINI_API_KEY required only when APP_ENV=production; "+
				"additionally gated per widget rule ai_processing.enabled",
			GovernanceEnvironmentProductionOnly, "ai_provider_files",
		),
		pendingProvider(
			"stripe_billing_api", "Stripe billing API", "billing",
			GovernanceBoundaryServiceProvider,
			"Process subscription billing and payment state.",
			[]string{"billing contact email", "external customer and subscription identifiers"},
			"backend/internal/config/config.go (StripeSecretKey, StripeWebhookSecret, StripePrice*)",
			"STRIPE_SECRET_KEY, STRIPE_WEBHOOK_SECRET, STRIPE_PRICE_STARTER/GROWTH/SCALE "+
				"required only in staging and production",
			GovernanceEnvironmentStagingProduction, "external_infrastructure",
		),
		pendingProvider(
			"sentry_error_telemetry", "Sentry error telemetry", "telemetry",
			GovernanceBoundaryServiceProvider,
			"Capture error and exception telemetry for operational monitoring.",
			[]string{
				"error and exception telemetry (message, stack trace); " +
					"default PII capture disabled (backend/internal/observability/sentry.go, SendDefaultPII: false)",
			},
			"backend/internal/config/config.go (SentryDSN); backend/cmd/fidibeki-api/main.go (sentry.Init)",
			"SENTRY_DSN is read and passed to sentry.Init unconditionally, but is never required; "+
				"an empty value is the SDK's documented no-op state",
			GovernanceEnvironmentOptionalNeverRequired, "external_infrastructure",
		),
		pendingProvider(
			"slack_incoming_webhook", "Slack incoming webhook", "chat_webhook",
			GovernanceBoundaryCustomerConfiguredDestination,
			"Deliver minimized operational notifications to a customer-configured Slack destination.",
			[]string{"minimized operational notification fields per versioned per-destination allowlist"},
			"backend/internal/integration/models.go (KindSlack)",
			"a tenant-created integrations row with kind=slack, not a global environment variable",
			GovernanceEnvironmentPerTenantConfig, "external_communications",
		),
		pendingProvider(
			"microsoft_teams_incoming_webhook", "Microsoft Teams incoming webhook", "chat_webhook",
			GovernanceBoundaryCustomerConfiguredDestination,
			"Deliver minimized operational notifications to a customer-configured Teams destination.",
			[]string{"minimized operational notification fields per versioned per-destination allowlist"},
			"backend/internal/integration/models.go (KindTeams)",
			"a tenant-created integrations row with kind=teams, not a global environment variable",
			GovernanceEnvironmentPerTenantConfig, "external_communications",
		),
		pendingProvider(
			"customer_generic_webhook", "Customer-configured generic webhook", "generic_webhook",
			GovernanceBoundaryCustomerConfiguredDestination,
			"Deliver minimized operational notifications to a customer-configured generic "+
				"webhook destination.",
			[]string{"minimized operational notification fields per versioned per-destination allowlist"},
			"backend/internal/integration/models.go (KindWebhook)",
			"a tenant-created integrations row with kind=webhook, not a global environment variable",
			GovernanceEnvironmentPerTenantConfig, "external_communications",
		),
		pendingProvider(
			"cloudflare_turnstile", "Cloudflare Turnstile signup verification", "security_verification",
			GovernanceBoundaryServiceProvider,
			"Verify a signup request is not automated abuse.",
			[]string{"signup challenge token", "submitting request IP address"},
			"backend/internal/billing/turnstile.go (TurnstileVerifier.Verify posts token+remoteip to "+
				"challenges.cloudflare.com/turnstile/v0/siteverify); "+
				"backend/internal/billing/service.go (Verifier.Verify call site); "+
				"web/components/TurnstileWidget.client.vue (loads challenges.cloudflare.com/turnstile/v0/api.js)",
			"TURNSTILE_SECRET_KEY, TURNSTILE_SITE_KEY required only in staging and production "+
				"(backend/internal/config/config.go)",
			GovernanceEnvironmentStagingProduction, "security_verification",
		),
	}
}

// pendingProvider constructs a provider governance record with every governance field set to its
// explicit unknown/pending/unassigned default. This makes it structurally impossible to add a new
// provider entry that accidentally omits a default or invents a value not evidenced by the repo.
func pendingProvider(
	id, displayName, adapterCategory string,
	boundaryKind GovernanceBoundaryKind,
	purpose string,
	personalDataCategories []string,
	codeEvidence, enablementSignal string,
	environmentEnablement GovernanceEnvironmentEnablement,
	retentionReviewRef string,
) ProviderGovernanceRecord {
	return ProviderGovernanceRecord{
		ProviderID:             id,
		DisplayName:            displayName,
		AdapterCategory:        adapterCategory,
		BoundaryKind:           boundaryKind,
		Purpose:                purpose,
		PersonalDataCategories: personalDataCategories,
		CodeEvidence:           codeEvidence,
		EnablementSignal:       enablementSignal,
		EnvironmentEnablement:  environmentEnablement,
		LegalEntity:            governanceUnknown,
		ProcessingLocations:    []string{governanceUnknown},
		TransferMechanism:      governanceUnknown,
		ContractStatus:         GovernanceContractUnknown,
		RetentionReviewRef:     retentionReviewRef,
		Owner:                  governanceUnassigned,
		Approval:               GovernanceApprovalPending,
	}
}

func governanceRoleMatrix(definitions []retentionDefinition) []GovernanceRoleMatrixEntry {
	matrix := make([]GovernanceRoleMatrixEntry, 0, len(definitions))
	for _, definition := range definitions {
		matrix = append(matrix, GovernanceRoleMatrixEntry{
			Category: definition.Category,
			Label:    definition.Label,
			Purpose:  definition.Purpose,
			Role:     definition.Role,
		})
	}
	return matrix
}

func validateProviderGovernance(
	providers []ProviderGovernanceRecord,
	retentionDefinitions []retentionDefinition,
	roleMatrix []GovernanceRoleMatrixEntry,
	now time.Time,
) error {
	if now.IsZero() {
		return errors.New("governance validation time must not be zero")
	}
	if len(providers) == 0 {
		return errors.New("provider governance registry is empty")
	}

	retentionProviderNames := make(map[string]struct{})
	retentionCategories := make(map[string]struct{}, len(retentionDefinitions))
	for _, definition := range retentionDefinitions {
		retentionCategories[definition.Category] = struct{}{}
		for _, resource := range definition.Resources {
			if resource.Kind == RetentionProvider {
				retentionProviderNames[resource.Name] = struct{}{}
			}
		}
	}

	seen := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		if err := validateProviderGovernanceFields(provider); err != nil {
			return err
		}
		if _, exists := seen[provider.ProviderID]; exists {
			return fmt.Errorf("duplicate provider governance entry %q", provider.ProviderID)
		}
		seen[provider.ProviderID] = struct{}{}

		if _, ok := retentionProviderNames[provider.ProviderID]; !ok {
			return fmt.Errorf(
				"provider governance entry %q is not an evidenced retention-registry provider",
				provider.ProviderID,
			)
		}
		if _, ok := retentionCategories[provider.RetentionReviewRef]; !ok {
			return fmt.Errorf(
				"provider governance entry %q references unknown retention category %q",
				provider.ProviderID, provider.RetentionReviewRef,
			)
		}

		if err := validateGovernanceTimestamps(provider, now); err != nil {
			return err
		}
		if provider.Approval == GovernanceApprovalApproved && !providerGovernanceComplete(provider) {
			return fmt.Errorf(
				"provider governance entry %q is approved without complete legal entity, "+
					"processing locations, transfer mechanism, executed contract, owner, review, "+
					"and approval evidence",
				provider.ProviderID,
			)
		}
	}

	for name := range retentionProviderNames {
		if _, ok := seen[name]; !ok {
			return fmt.Errorf(
				"retention-evidenced provider %q is missing from the provider governance registry", name,
			)
		}
	}

	if err := validateGovernanceRoleMatrix(roleMatrix, retentionCategories); err != nil {
		return err
	}

	return nil
}

// validateGovernanceTimestamps rejects a zero or future ReviewedAt/ApprovedAt, an ApprovedAt
// before its ReviewedAt, and (for ReviewedAt) a review older than the staleness window. It applies
// to every provider whose timestamps are set, not only approved ones, so a pending entry cannot
// carry a nonsensical review/approval date either.
func validateGovernanceTimestamps(provider ProviderGovernanceRecord, now time.Time) error {
	if provider.ReviewedAt != nil {
		if provider.ReviewedAt.IsZero() || provider.ReviewedAt.After(now) {
			return fmt.Errorf(
				"provider governance entry %q has an invalid reviewed-at timestamp %s",
				provider.ProviderID, provider.ReviewedAt.Format(time.RFC3339),
			)
		}
		if now.Sub(*provider.ReviewedAt) > governanceReviewStalenessWindow {
			return fmt.Errorf(
				"provider governance entry %q has a stale review dated %s",
				provider.ProviderID, provider.ReviewedAt.Format("2006-01-02"),
			)
		}
	}
	if provider.ApprovedAt != nil {
		if provider.ReviewedAt == nil {
			return fmt.Errorf(
				"provider governance entry %q has an approved-at timestamp without a reviewed-at timestamp",
				provider.ProviderID,
			)
		}
		if provider.ApprovedAt.IsZero() || provider.ApprovedAt.After(now) {
			return fmt.Errorf(
				"provider governance entry %q has an invalid approved-at timestamp %s",
				provider.ProviderID, provider.ApprovedAt.Format(time.RFC3339),
			)
		}
		if provider.ReviewedAt != nil && provider.ApprovedAt.Before(*provider.ReviewedAt) {
			return fmt.Errorf(
				"provider governance entry %q has an approved-at timestamp before its reviewed-at timestamp",
				provider.ProviderID,
			)
		}
	}
	return nil
}

func validateProviderGovernanceFields(provider ProviderGovernanceRecord) error {
	if strings.TrimSpace(provider.ProviderID) == "" ||
		strings.TrimSpace(provider.DisplayName) == "" ||
		strings.TrimSpace(provider.AdapterCategory) == "" ||
		strings.TrimSpace(provider.Purpose) == "" ||
		strings.TrimSpace(string(provider.BoundaryKind)) == "" ||
		strings.TrimSpace(provider.CodeEvidence) == "" ||
		strings.TrimSpace(provider.EnablementSignal) == "" ||
		strings.TrimSpace(string(provider.EnvironmentEnablement)) == "" ||
		strings.TrimSpace(provider.LegalEntity) == "" ||
		len(provider.ProcessingLocations) == 0 ||
		strings.TrimSpace(provider.TransferMechanism) == "" ||
		strings.TrimSpace(string(provider.ContractStatus)) == "" ||
		strings.TrimSpace(provider.RetentionReviewRef) == "" ||
		strings.TrimSpace(provider.Owner) == "" ||
		strings.TrimSpace(string(provider.Approval)) == "" {
		return fmt.Errorf("provider governance entry %q is incomplete", provider.ProviderID)
	}
	for _, location := range provider.ProcessingLocations {
		if strings.TrimSpace(location) == "" {
			return fmt.Errorf(
				"provider governance entry %q has a blank processing location", provider.ProviderID,
			)
		}
	}
	if err := validateGovernancePersonalDataCategories(provider); err != nil {
		return err
	}
	switch provider.BoundaryKind {
	case GovernanceBoundaryInfrastructureProvider, GovernanceBoundaryServiceProvider,
		GovernanceBoundaryCustomerConfiguredDestination, GovernanceBoundaryInternalRuntime:
	default:
		return fmt.Errorf(
			"provider governance entry %q has unknown boundary kind %q",
			provider.ProviderID, provider.BoundaryKind,
		)
	}
	switch provider.ContractStatus {
	case GovernanceContractUnknown, GovernanceContractPending, GovernanceContractExecuted:
	default:
		return fmt.Errorf(
			"provider governance entry %q has unknown contract status %q",
			provider.ProviderID, provider.ContractStatus,
		)
	}
	switch provider.Approval {
	case GovernanceApprovalPending, GovernanceApprovalApproved:
	default:
		return fmt.Errorf(
			"provider governance entry %q has unknown approval status %q",
			provider.ProviderID, provider.Approval,
		)
	}
	if provider.Approval == GovernanceApprovalPending &&
		(strings.TrimSpace(provider.ApprovedBy) != "" || provider.ApprovedAt != nil) {
		return fmt.Errorf(
			"provider governance entry %q is pending but carries approval evidence",
			provider.ProviderID,
		)
	}
	switch provider.EnvironmentEnablement {
	case GovernanceEnvironmentAlwaysRequired, GovernanceEnvironmentStagingProduction,
		GovernanceEnvironmentProductionOnly, GovernanceEnvironmentConditionalOnDriver,
		GovernanceEnvironmentOptionalNeverRequired, GovernanceEnvironmentPerTenantConfig:
	default:
		return fmt.Errorf(
			"provider governance entry %q has unknown environment enablement %q",
			provider.ProviderID, provider.EnvironmentEnablement,
		)
	}
	return nil
}

// validateGovernancePersonalDataCategories requires at least one bounded, non-blank personal-data
// category per boundary and rejects duplicates within the same entry.
func validateGovernancePersonalDataCategories(provider ProviderGovernanceRecord) error {
	if len(provider.PersonalDataCategories) == 0 {
		return fmt.Errorf(
			"provider governance entry %q has no personal-data categories", provider.ProviderID,
		)
	}
	if len(provider.PersonalDataCategories) > maxGovernancePersonalDataCategories {
		return fmt.Errorf(
			"provider governance entry %q has too many personal-data categories", provider.ProviderID,
		)
	}
	seen := make(map[string]struct{}, len(provider.PersonalDataCategories))
	for _, category := range provider.PersonalDataCategories {
		trimmed := strings.TrimSpace(category)
		if trimmed == "" {
			return fmt.Errorf(
				"provider governance entry %q has a blank personal-data category", provider.ProviderID,
			)
		}
		if len(trimmed) > maxGovernancePersonalDataCategoryLen {
			return fmt.Errorf(
				"provider governance entry %q has an oversized personal-data category",
				provider.ProviderID,
			)
		}
		normalized := strings.ToLower(trimmed)
		if _, exists := seen[normalized]; exists {
			return fmt.Errorf(
				"provider governance entry %q has a duplicate personal-data category %q",
				provider.ProviderID, trimmed,
			)
		}
		seen[normalized] = struct{}{}
	}
	return nil
}

// governancePlaceholder reports whether value, once trimmed and case-folded, is one of the
// sentinel placeholder strings ("", "unknown", "unassigned", "pending"). It is used to detect an
// approved entry that carries a placeholder disguised by whitespace or casing (for example
// " unknown " or "Unassigned"), which an exact-match comparison would miss.
func governancePlaceholder(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", governanceUnknown, governanceUnassigned, governancePending:
		return true
	default:
		return false
	}
}

func providerGovernanceComplete(provider ProviderGovernanceRecord) bool {
	if governancePlaceholder(provider.LegalEntity) ||
		governancePlaceholder(provider.Owner) ||
		governancePlaceholder(provider.TransferMechanism) ||
		governancePlaceholder(provider.ApprovedBy) ||
		provider.ContractStatus != GovernanceContractExecuted ||
		provider.ReviewedAt == nil || provider.ApprovedAt == nil {
		return false
	}
	return !slices.ContainsFunc(provider.ProcessingLocations, governancePlaceholder)
}

func validateGovernanceRoleMatrix(
	roleMatrix []GovernanceRoleMatrixEntry,
	retentionCategories map[string]struct{},
) error {
	seen := make(map[string]struct{}, len(roleMatrix))
	for _, entry := range roleMatrix {
		if _, ok := retentionCategories[entry.Category]; !ok {
			return fmt.Errorf("role matrix references unknown retention category %q", entry.Category)
		}
		if _, exists := seen[entry.Category]; exists {
			return fmt.Errorf("role matrix has duplicate category %q", entry.Category)
		}
		seen[entry.Category] = struct{}{}
		switch entry.Role {
		case RetentionRoleController, RetentionRoleProcessor, RetentionRoleControllerProcessor:
		default:
			return fmt.Errorf("role matrix category %q has unknown role %q", entry.Category, entry.Role)
		}
	}
	for category := range retentionCategories {
		if _, ok := seen[category]; !ok {
			return fmt.Errorf("personal-data category %q is missing from the role matrix", category)
		}
	}
	return nil
}

func summarizeProviderGovernance(providers []ProviderGovernanceRecord) GovernanceSummary {
	summary := GovernanceSummary{}
	for _, provider := range providers {
		switch provider.Approval {
		case GovernanceApprovalApproved:
			summary.Approved++
		case GovernanceApprovalPending:
			summary.PendingApproval++
		}
		switch provider.ContractStatus {
		case GovernanceContractExecuted:
			summary.ContractExecuted++
		case GovernanceContractPending:
			summary.ContractPending++
		case GovernanceContractUnknown:
			summary.ContractUnknown++
		}
	}
	return summary
}
