package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/identity"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	"github.com/fidibeki/app.fidibeki/backend/internal/widgetconfig"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrNotFound          = errors.New("project resource not found")
	ErrPublicKeyConflict = errors.New("project public key collision")
)

type Store struct {
	PublicBaseURL string
}

func (Store) CountProjects(ctx context.Context, query db.Querier, tenantID string) (int, error) {
	var count int
	if err := query.QueryRow(ctx, `SELECT count(*) FROM projects WHERE tenant_id = $1`, tenantID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count projects: %w", err)
	}
	return count, nil
}

func (Store) CreateProject(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	name string,
	publicKey string,
	allowedDomains []string,
	defaultLocale string,
) (Project, error) {
	project, err := scanProject(query.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains, default_locale)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (public_key) DO NOTHING
		RETURNING id, tenant_id, name, public_key, allowed_domains, default_locale,
		          reporter_access_enabled, widget_appearance_overrides,
		          widget_last_seen_at, created_at, updated_at
	`, tenantID, name, publicKey, allowedDomains, defaultLocale))
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrPublicKeyConflict
	}
	if err != nil {
		return Project{}, fmt.Errorf("create project: %w", err)
	}
	return project, nil
}

func (Store) ListProjects(ctx context.Context, query db.Querier, tenantID string) ([]Project, error) {
	rows, err := query.Query(ctx, `
		SELECT id, tenant_id, name, public_key, allowed_domains, default_locale,
		       reporter_access_enabled, widget_appearance_overrides,
		       widget_last_seen_at, created_at, updated_at
		FROM projects WHERE tenant_id = $1 ORDER BY created_at, id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	projects := make([]Project, 0)
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}
	return projects, nil
}

func (Store) GetProject(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
) (Project, error) {
	project, err := scanProject(query.QueryRow(ctx, `
		SELECT id, tenant_id, name, public_key, allowed_domains, default_locale,
		       reporter_access_enabled, widget_appearance_overrides,
		       widget_last_seen_at, created_at, updated_at
		FROM projects WHERE tenant_id = $1 AND id = $2
	`, tenantID, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("get project: %w", err)
	}
	return project, nil
}

func (Store) UpdateProject(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	name string,
	allowedDomains []string,
	defaultLocale string,
	setReporterAccess bool,
	reporterAccessEnabled *bool,
	setWidgetAppearance bool,
	widgetAppearance widgetconfig.AppearanceOverrides,
) (Project, error) {
	appearancePayload, err := json.Marshal(widgetAppearance)
	if err != nil {
		return Project{}, fmt.Errorf("encode project widget appearance: %w", err)
	}
	project, err := scanProject(query.QueryRow(ctx, `
		UPDATE projects SET name = $3, allowed_domains = $4, default_locale = $5,
		                    reporter_access_enabled = CASE
		                        WHEN $6 THEN $7::boolean
		                        ELSE reporter_access_enabled
		                    END,
		                    widget_appearance_overrides = CASE
		                        WHEN $8 THEN $9::jsonb
		                        ELSE widget_appearance_overrides
		                    END
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, name, public_key, allowed_domains, default_locale,
		          reporter_access_enabled, widget_appearance_overrides,
		          widget_last_seen_at, created_at, updated_at
	`, tenantID, projectID, name, allowedDomains, defaultLocale,
		setReporterAccess, reporterAccessEnabled, setWidgetAppearance, string(appearancePayload)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("update project: %w", err)
	}
	if setReporterAccess && reporterAccessEnabled != nil && !*reporterAccessEnabled {
		if _, err := query.Exec(ctx, `
			DELETE FROM reporter_access_tokens WHERE tenant_id = $1 AND project_id = $2
		`, tenantID, projectID); err != nil {
			return Project{}, fmt.Errorf("revoke project reporter access tokens: %w", err)
		}
	}
	return project, nil
}

func (Store) DeleteProject(ctx context.Context, query db.Querier, tenantID string, projectID string) error {
	tag, err := query.Exec(ctx, `DELETE FROM projects WHERE tenant_id = $1 AND id = $2`, tenantID, projectID)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (Store) CreateWidgetRule(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	values WidgetRuleValues,
	actorMemberID string,
) (WidgetRule, error) {
	values.AIProcessing = finalizeAIProcessing(values.AIProcessing, actorMemberID, time.Now().UTC())
	arguments, err := widgetRuleArguments(tenantID, projectID, values)
	if err != nil {
		return WidgetRule{}, err
	}
	rule, err := scanWidgetRule(query.QueryRow(ctx, `
		INSERT INTO widget_rules (
			project_id, name, priority, enabled, matcher_type, matcher_value,
			trigger_type, trigger_delay_seconds, trigger_scroll_percent,
			can_rate, can_write_text, can_record_audio, can_record_video,
			can_attach_element, can_attach_screenshot, greeting, position, theme,
			accent_color, replay_mode, replay_sampling_rate, replay_max_duration_seconds,
			replay_max_events, replay_mask_all_inputs, replay_mask_text_selector,
			replay_record_canvas, replay_canvas_fps, replay_consent,
			recurrence_mode, recurrence_days, definition_key, definition_status, presentation, objective,
			response_blocks, localizations, allow_comment, collect_contact, ai_processing,
			widget_appearance_overrides, analytics_consent, launcher_variant
		)
			SELECT id, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
			       $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26,
			       $27, $28, $29::jsonb, $30, $31,
			       COALESCE(NULLIF($32, ''), 'widget-' || substr(replace(gen_random_uuid()::text, '-', ''), 1, 12)),
			       $33, $34, $35, $36::jsonb, $37::jsonb, $38, $39, $40::jsonb,
			       $41::jsonb, $42::jsonb, $43
		FROM projects WHERE tenant_id = $1 AND id = $2
		RETURNING `+widgetRuleColumns+`
	`, arguments...))
	if errors.Is(err, pgx.ErrNoRows) {
		return WidgetRule{}, ErrNotFound
	}
	if err != nil {
		return WidgetRule{}, fmt.Errorf("create widget rule: %w", err)
	}
	if err := replaceWidgetRuleTargetConditions(ctx, query, rule.ID, values); err != nil {
		return WidgetRule{}, err
	}
	if values.ReplayMode != ReplayNone || values.ReplayConsent.Mode != ReplayConsentNone {
		if err := recordReplayConsentConfigurationAudit(
			ctx, query, tenantID, projectID, rule, actorMemberID, nil, nil, 0,
		); err != nil {
			return WidgetRule{}, err
		}
	}
	if rule.AIProcessing.Enabled {
		if err := recordAIProcessingConfigurationAudit(
			ctx, query, tenantID, projectID, rule, actorMemberID, nil,
		); err != nil {
			return WidgetRule{}, err
		}
	}
	if values.AnalyticsConsent.Mode != AnalyticsConsentNone {
		if err := recordAnalyticsConsentConfigurationAudit(
			ctx, query, tenantID, projectID, rule, actorMemberID, nil, 0,
		); err != nil {
			return WidgetRule{}, err
		}
	}
	rules, err := attachWidgetRuleTargetConditions(ctx, query, projectID, []WidgetRule{rule})
	if err != nil {
		return WidgetRule{}, err
	}
	return rules[0], nil
}

func (Store) ListWidgetRules(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
) ([]WidgetRule, error) {
	rows, err := query.Query(ctx, `
		SELECT `+prefixedWidgetRuleColumns("rule")+`
		FROM widget_rules AS rule
		JOIN projects AS project ON project.id = rule.project_id
		WHERE project.tenant_id = $1 AND project.id = $2
		ORDER BY rule.priority DESC, rule.created_at, rule.id
	`, tenantID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list widget rules: %w", err)
	}
	defer rows.Close()
	rules := make([]WidgetRule, 0)
	for rows.Next() {
		rule, err := scanWidgetRule(rows)
		if err != nil {
			return nil, fmt.Errorf("scan widget rule: %w", err)
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate widget rules: %w", err)
	}
	rows.Close()
	return attachWidgetRuleTargetConditions(ctx, query, projectID, rules)
}

func (Store) GetWidgetRule(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	ruleID string,
) (WidgetRule, error) {
	rule, err := scanWidgetRule(query.QueryRow(ctx, `
		SELECT `+prefixedWidgetRuleColumns("rule")+`
		FROM widget_rules AS rule
		JOIN projects AS project ON project.id = rule.project_id
		WHERE project.tenant_id = $1 AND project.id = $2 AND rule.id = $3
	`, tenantID, projectID, ruleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return WidgetRule{}, ErrNotFound
	}
	if err != nil {
		return WidgetRule{}, fmt.Errorf("get widget rule: %w", err)
	}
	rules, err := attachWidgetRuleTargetConditions(ctx, query, projectID, []WidgetRule{rule})
	if err != nil {
		return WidgetRule{}, err
	}
	return rules[0], nil
}

func (Store) UpdateWidgetRule(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	ruleID string,
	values WidgetRuleValues,
	actorMemberID string,
) (WidgetRule, error) {
	var previousReplayMode ReplayMode
	var previousReplayConsent ReplayConsentConfig
	var previousAIProcessing AIProcessingConfig
	var previousAnalyticsConsent AnalyticsConsentConfig
	var previousDefinitionVersion int
	if err := query.QueryRow(ctx, `
		SELECT rule.replay_mode, rule.replay_consent, rule.ai_processing,
		       rule.analytics_consent, rule.definition_version
		FROM widget_rules AS rule
		JOIN projects AS project ON project.id = rule.project_id
		WHERE project.tenant_id = $1 AND project.id = $2 AND rule.id = $3
		FOR UPDATE OF rule
	`, tenantID, projectID, ruleID).Scan(
		&previousReplayMode, &previousReplayConsent, &previousAIProcessing,
		&previousAnalyticsConsent, &previousDefinitionVersion,
	); errors.Is(err, pgx.ErrNoRows) {
		return WidgetRule{}, ErrNotFound
	} else if err != nil {
		return WidgetRule{}, fmt.Errorf("lock widget replay consent configuration: %w", err)
	}
	values.AIProcessing = finalizeAIProcessingUpdate(
		previousAIProcessing, values.AIProcessing, actorMemberID, time.Now().UTC(),
	)
	valueArguments, err := widgetRuleValueArguments(values)
	if err != nil {
		return WidgetRule{}, err
	}
	arguments := append([]any{tenantID, projectID, ruleID}, valueArguments...)
	arguments = append(arguments, values.ExplicitAppearance)
	rule, err := scanWidgetRule(query.QueryRow(ctx, `
		UPDATE widget_rules AS rule SET
			name = $4, priority = $5, enabled = $6, matcher_type = $7, matcher_value = $8,
			trigger_type = $9, trigger_delay_seconds = $10, trigger_scroll_percent = $11,
			can_rate = $12, can_write_text = $13, can_record_audio = $14, can_record_video = $15,
			can_attach_element = $16, can_attach_screenshot = $17, greeting = $18,
			position = $19, theme = $20, accent_color = $21, replay_mode = $22,
				replay_sampling_rate = $23, replay_max_duration_seconds = $24,
				replay_max_events = $25, replay_mask_all_inputs = $26, replay_mask_text_selector = $27,
				replay_record_canvas = $28, replay_canvas_fps = $29,
				replay_consent = $30::jsonb,
				recurrence_mode = $31, recurrence_days = $32,
				definition_key = COALESCE(NULLIF($33, ''), rule.definition_key),
				definition_version = rule.definition_version + 1,
				definition_status = $34, presentation = $35, objective = $36,
				response_blocks = $37::jsonb, localizations = $38::jsonb,
				allow_comment = $39, collect_contact = $40,
				ai_processing = $41::jsonb,
				widget_appearance_overrides = CASE
					WHEN $45 THEN $42::jsonb
					ELSE rule.widget_appearance_overrides || $42::jsonb
				END,
				analytics_consent = $43::jsonb, launcher_variant = $44
		FROM projects AS project
		WHERE rule.id = $3 AND rule.project_id = $2
		  AND project.id = rule.project_id AND project.tenant_id = $1
		RETURNING `+prefixedWidgetRuleColumns("rule")+`
	`, arguments...))
	if errors.Is(err, pgx.ErrNoRows) {
		return WidgetRule{}, ErrNotFound
	}
	if err != nil {
		return WidgetRule{}, fmt.Errorf("update widget rule: %w", err)
	}
	if err := replaceWidgetRuleTargetConditions(ctx, query, rule.ID, values); err != nil {
		return WidgetRule{}, err
	}
	if previousReplayMode != values.ReplayMode ||
		!reflect.DeepEqual(previousReplayConsent, values.ReplayConsent) {
		if err := recordReplayConsentConfigurationAudit(
			ctx, query, tenantID, projectID, rule, actorMemberID,
			&previousReplayMode, &previousReplayConsent, previousDefinitionVersion,
		); err != nil {
			return WidgetRule{}, err
		}
	}
	if !reflect.DeepEqual(previousAIProcessing, rule.AIProcessing) {
		if err := recordAIProcessingConfigurationAudit(
			ctx, query, tenantID, projectID, rule, actorMemberID, &previousAIProcessing,
		); err != nil {
			return WidgetRule{}, err
		}
	}
	if !reflect.DeepEqual(previousAnalyticsConsent, values.AnalyticsConsent) {
		if err := recordAnalyticsConsentConfigurationAudit(
			ctx, query, tenantID, projectID, rule, actorMemberID,
			&previousAnalyticsConsent, previousDefinitionVersion,
		); err != nil {
			return WidgetRule{}, err
		}
	}
	if previousAIProcessing.Enabled && !rule.AIProcessing.Enabled {
		if err := disableWidgetRuleAI(ctx, query, tenantID, projectID, ruleID, time.Now().UTC()); err != nil {
			return WidgetRule{}, err
		}
	}
	rules, err := attachWidgetRuleTargetConditions(ctx, query, projectID, []WidgetRule{rule})
	if err != nil {
		return WidgetRule{}, err
	}
	return rules[0], nil
}

func finalizeAIProcessing(
	configuration AIProcessingConfig,
	actorMemberID string,
	now time.Time,
) AIProcessingConfig {
	if !configuration.Enabled {
		return AIProcessingConfig{}
	}
	configuration.Acknowledged = true
	configuration.AcknowledgedAt = &now
	configuration.AcknowledgedByMemberID = &actorMemberID
	return configuration
}

func finalizeAIProcessingUpdate(
	previous AIProcessingConfig,
	next AIProcessingConfig,
	actorMemberID string,
	now time.Time,
) AIProcessingConfig {
	if !next.Enabled {
		return AIProcessingConfig{}
	}
	if aiProcessingContractEqual(previous, next) && previous.AcknowledgedAt != nil &&
		previous.AcknowledgedByMemberID != nil {
		next.Acknowledged = true
		next.AcknowledgedAt = previous.AcknowledgedAt
		next.AcknowledgedByMemberID = previous.AcknowledgedByMemberID
		return next
	}
	return finalizeAIProcessing(next, actorMemberID, now)
}

func aiProcessingContractEqual(left, right AIProcessingConfig) bool {
	left.AcknowledgedAt, right.AcknowledgedAt = nil, nil
	left.AcknowledgedByMemberID, right.AcknowledgedByMemberID = nil, nil
	return reflect.DeepEqual(left, right)
}

func recordAIProcessingConfigurationAudit(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	rule WidgetRule,
	actorMemberID string,
	previous *AIProcessingConfig,
) error {
	data := map[string]any{
		"project_id":         projectID,
		"widget_rule_id":     rule.ID,
		"definition_version": rule.DefinitionVersion,
		"ai_processing":      rule.AIProcessing,
	}
	if previous != nil {
		data["previous_ai_processing"] = *previous
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode AI processing configuration audit: %w", err)
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_access_events (
			tenant_id, kind, actor_member_id, data
		) VALUES ($1, 'widget_rule.ai_processing_updated', $2, $3::jsonb)
	`, tenantID, actorMemberID, string(encoded)); err != nil {
		return fmt.Errorf("record AI processing configuration audit: %w", err)
	}
	return nil
}

func disableWidgetRuleAI(
	ctx context.Context,
	query db.Querier,
	tenantID, projectID, ruleID string,
	now time.Time,
) error {
	if _, err := query.Exec(ctx, `
		UPDATE feedbacks
		SET text_process_status = 'skipped', ai_job_enqueued_at = NULL,
		    ai_budget_deferred_until = NULL, ai_budget_deferred_reason = NULL
		WHERE tenant_id = $1 AND project_id = $2 AND widget_rule_id = $3
		  AND text_process_status IN ('pending', 'processing')
	`, tenantID, projectID, ruleID); err != nil {
		return fmt.Errorf("disable widget rule text AI: %w", err)
	}
	if _, err := query.Exec(ctx, `
		UPDATE attachments AS attachment
		SET processing_status = 'skipped', error_message = NULL,
		    ai_budget_deferred_until = NULL
		FROM feedbacks AS feedback
		WHERE feedback.tenant_id = $1 AND feedback.project_id = $2
		  AND feedback.widget_rule_id = $3 AND feedback.id = attachment.feedback_id
		  AND attachment.tenant_id = feedback.tenant_id
		  AND attachment.processing_status IN ('pending', 'processing')
	`, tenantID, projectID, ruleID); err != nil {
		return fmt.Errorf("disable widget rule attachment AI: %w", err)
	}
	if _, err := query.Exec(ctx, `
		UPDATE feedbacks AS feedback
		SET ai_processed_at = $4
		WHERE feedback.tenant_id = $1 AND feedback.project_id = $2
		  AND feedback.widget_rule_id = $3
		  AND feedback.text_process_status IN ('completed', 'failed', 'skipped')
		  AND NOT EXISTS (
		      SELECT 1 FROM attachments AS attachment
		      WHERE attachment.feedback_id = feedback.id
		        AND attachment.processing_status IN ('pending', 'processing')
		  )
	`, tenantID, projectID, ruleID, now); err != nil {
		return fmt.Errorf("complete disabled widget rule AI: %w", err)
	}
	if _, err := query.Exec(ctx, `
		UPDATE ai_processing_runs
		SET status = 'discarded', failure_reason = 'policy_disabled', completed_at = $4
		WHERE tenant_id = $1 AND project_id = $2 AND widget_rule_id = $3
		  AND status = 'processing'
	`, tenantID, projectID, ruleID, now); err != nil {
		return fmt.Errorf("discard disabled widget rule AI evidence: %w", err)
	}
	return nil
}

func recordAnalyticsConsentConfigurationAudit(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	rule WidgetRule,
	actorMemberID string,
	previousConsent *AnalyticsConsentConfig,
	previousDefinitionVersion int,
) error {
	data := map[string]any{
		"project_id":         projectID,
		"widget_rule_id":     rule.ID,
		"definition_version": rule.DefinitionVersion,
		"analytics_consent":  rule.AnalyticsConsent,
	}
	if previousConsent != nil {
		data["previous_definition_version"] = previousDefinitionVersion
		data["previous_analytics_consent"] = *previousConsent
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode widget analytics consent configuration audit: %w", err)
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_access_events (
			tenant_id, kind, actor_member_id, data
		) VALUES ($1, 'widget_rule.analytics_consent_updated', $2, $3::jsonb)
	`, tenantID, actorMemberID, string(encoded)); err != nil {
		return fmt.Errorf("record widget analytics consent configuration audit: %w", err)
	}
	return nil
}

func recordReplayConsentConfigurationAudit(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	rule WidgetRule,
	actorMemberID string,
	previousMode *ReplayMode,
	previousConsent *ReplayConsentConfig,
	previousDefinitionVersion int,
) error {
	data := map[string]any{
		"project_id":         projectID,
		"widget_rule_id":     rule.ID,
		"definition_version": rule.DefinitionVersion,
		"replay_mode":        rule.ReplayMode,
		"replay_consent":     rule.ReplayConsent,
	}
	if previousMode != nil && previousConsent != nil {
		data["previous_definition_version"] = previousDefinitionVersion
		data["previous_replay_mode"] = *previousMode
		data["previous_replay_consent"] = *previousConsent
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode replay consent configuration audit: %w", err)
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_access_events (
			tenant_id, kind, actor_member_id, data
		) VALUES ($1, 'widget_rule.replay_consent_updated', $2, $3::jsonb)
	`, tenantID, actorMemberID, string(encoded)); err != nil {
		return fmt.Errorf("record replay consent configuration audit: %w", err)
	}
	return nil
}

func (Store) DeleteWidgetRule(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	ruleID string,
) error {
	if err := disableWidgetRuleAI(
		ctx, query, tenantID, projectID, ruleID, time.Now().UTC(),
	); err != nil {
		return err
	}
	if _, err := query.Exec(ctx, `
		UPDATE feedbacks
		SET widget_rule_id = NULL
		WHERE tenant_id = $1 AND project_id = $2 AND widget_rule_id = $3
	`, tenantID, projectID, ruleID); err != nil {
		return fmt.Errorf("detach feedback from widget rule: %w", err)
	}
	tag, err := query.Exec(ctx, `
		DELETE FROM widget_rules AS rule
		USING projects AS project
		WHERE rule.id = $3 AND rule.project_id = $2
		  AND project.id = rule.project_id AND project.tenant_id = $1
	`, tenantID, projectID, ruleID)
	if err != nil {
		return fmt.Errorf("delete widget rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (store Store) LoadPublicConfiguration(
	ctx context.Context,
	query db.Querier,
	publicKey string,
) (PublicConfiguration, error) {
	var configuration PublicConfiguration
	var plan string
	var status string
	var trialEndsAt pgtype.Timestamptz
	var deletionRequestedAt pgtype.Timestamptz
	var logoVersion *string
	var controllerName string
	var privacyContactEmail *string
	var privacyRightsEmail *string
	var privacyPolicyURL *string
	var privacyProfileVersion int
	err := query.QueryRow(ctx, `
		SELECT tenant.id, tenant.slug, project.id, project.default_locale, project.allowed_domains,
		       tenant.widget_appearance_defaults, project.widget_appearance_overrides,
		       tenant.plan, tenant.status, tenant.trial_ends_at, tenant.deletion_requested_at,
		       tenant.logo_version::text, tenant.name, tenant.privacy_contact_email::text,
		       tenant.privacy_rights_email::text, tenant.privacy_policy_url,
		       tenant.privacy_profile_version, project.chat_enabled, project.name
		FROM projects AS project
		JOIN tenants AS tenant ON tenant.id = project.tenant_id
		WHERE project.public_key = $1
	`, publicKey).Scan(
		&configuration.TenantID, &configuration.TenantSlug,
		&configuration.ProjectID, &configuration.DefaultLocale, &configuration.AllowedDomains,
		&configuration.TenantAppearanceDefaults, &configuration.ProjectAppearanceOverrides,
		&plan, &status, &trialEndsAt,
		&deletionRequestedAt, &logoVersion,
		&controllerName, &privacyContactEmail, &privacyRightsEmail, &privacyPolicyURL,
		&privacyProfileVersion, &configuration.ChatEnabled, &configuration.ProjectName,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicConfiguration{}, ErrNotFound
	}
	if err != nil {
		return PublicConfiguration{}, fmt.Errorf("load public project: %w", err)
	}
	configuration.Plan = tenant.Plan(plan)
	configuration.Status = tenant.Status(status)
	if trialEndsAt.Valid {
		configuration.TrialEndsAt = &trialEndsAt.Time
	}
	if deletionRequestedAt.Valid {
		configuration.DeletionRequestedAt = &deletionRequestedAt.Time
	}
	if privacyContactEmail != nil && privacyRightsEmail != nil && privacyPolicyURL != nil {
		controllerProfile := ControllerProfile{
			Version: privacyProfileVersion, Name: controllerName,
			PrivacyContactEmail: *privacyContactEmail, PrivacyRightsEmail: *privacyRightsEmail,
			PrivacyPolicyURL: *privacyPolicyURL,
		}
		if controllerProfile.Complete() {
			configuration.ControllerProfile = &controllerProfile
		}
	}
	configuration.ControllerProfileVersion = privacyProfileVersion
	var organizationLogo widgetconfig.AppearanceOverrides
	if logoVersion != nil {
		logoURL, err := identity.WorkspaceLogoURL(
			store.PublicBaseURL, configuration.TenantID, *logoVersion, 256,
		)
		if err != nil {
			return PublicConfiguration{}, fmt.Errorf("build workspace logo URL: %w", err)
		}
		organizationLogo.LogoURL = &logoURL
	}

	rows, err := query.Query(ctx, `
		SELECT `+prefixedWidgetRuleColumns("rule")+`
		FROM widget_rules AS rule
		WHERE rule.project_id = $1
		ORDER BY rule.priority DESC, rule.created_at, rule.id
	`, configuration.ProjectID)
	if err != nil {
		return PublicConfiguration{}, fmt.Errorf("list public widget rules: %w", err)
	}
	defer rows.Close()
	configuration.Rules = make([]WidgetRule, 0)
	for rows.Next() {
		rule, err := scanWidgetRule(rows)
		if err != nil {
			return PublicConfiguration{}, fmt.Errorf("scan public widget rule: %w", err)
		}
		configuration.Rules = append(configuration.Rules, rule)
	}
	if err := rows.Err(); err != nil {
		return PublicConfiguration{}, fmt.Errorf("iterate public widget rules: %w", err)
	}
	rows.Close()
	configuration.Rules, err = attachWidgetRuleTargetConditions(
		ctx, query, configuration.ProjectID, configuration.Rules,
	)
	if err != nil {
		return PublicConfiguration{}, err
	}
	for index := range configuration.Rules {
		appearance := widgetconfig.Resolve(
			organizationLogo,
			configuration.TenantAppearanceDefaults,
			configuration.ProjectAppearanceOverrides,
			configuration.Rules[index].AppearanceOverrides,
		)
		configuration.Rules[index].Appearance = appearance
		configuration.Rules[index].Position = WidgetPosition(appearance.Position)
		configuration.Rules[index].Theme = WidgetTheme(appearance.Theme)
		configuration.Rules[index].AccentColor = stringPointer(appearance.AccentColor)
	}
	return configuration, nil
}

func replaceWidgetRuleTargetConditions(
	ctx context.Context,
	query db.Querier,
	ruleID string,
	values WidgetRuleValues,
) error {
	if _, err := query.Exec(ctx,
		`DELETE FROM widget_rule_target_conditions WHERE widget_rule_id = $1`, ruleID,
	); err != nil {
		return fmt.Errorf("replace widget target conditions: %w", err)
	}
	collections := []struct {
		kind       TargetConditionKind
		conditions []WidgetRuleTargetConditionValues
	}{
		{kind: TargetConditionInclude, conditions: values.IncludeConditions},
		{kind: TargetConditionExclude, conditions: values.ExcludeConditions},
	}
	for _, collection := range collections {
		kind, conditions := collection.kind, collection.conditions
		for position, condition := range conditions {
			if _, err := query.Exec(ctx, `
				INSERT INTO widget_rule_target_conditions (
					widget_rule_id, kind, matcher_type, matcher_value, position
				) VALUES ($1, $2, $3, $4, $5)
			`, ruleID, kind, condition.MatcherType, condition.MatcherValue, position); err != nil {
				return fmt.Errorf("insert widget target condition: %w", err)
			}
		}
	}
	return nil
}

func attachWidgetRuleTargetConditions(
	ctx context.Context,
	query db.Querier,
	projectID string,
	rules []WidgetRule,
) ([]WidgetRule, error) {
	byID := make(map[string]*WidgetRule, len(rules))
	for index := range rules {
		rules[index].IncludeConditions = make([]WidgetRuleTargetCondition, 0)
		rules[index].ExcludeConditions = make([]WidgetRuleTargetCondition, 0)
		byID[rules[index].ID] = &rules[index]
	}
	if len(rules) == 0 {
		return rules, nil
	}
	rows, err := query.Query(ctx, `
		SELECT condition.id, condition.widget_rule_id, condition.kind,
		       condition.matcher_type, condition.matcher_value, condition.position
		FROM widget_rule_target_conditions AS condition
		JOIN widget_rules AS rule ON rule.id = condition.widget_rule_id
		WHERE rule.project_id = $1
		ORDER BY condition.widget_rule_id, condition.kind, condition.position
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list widget target conditions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var condition WidgetRuleTargetCondition
		var ruleID string
		var kind TargetConditionKind
		if err := rows.Scan(
			&condition.ID, &ruleID, &kind, &condition.MatcherType,
			&condition.MatcherValue, &condition.Position,
		); err != nil {
			return nil, fmt.Errorf("scan widget target condition: %w", err)
		}
		rule := byID[ruleID]
		if rule == nil {
			continue
		}
		switch kind {
		case TargetConditionInclude:
			rule.IncludeConditions = append(rule.IncludeConditions, condition)
		case TargetConditionExclude:
			rule.ExcludeConditions = append(rule.ExcludeConditions, condition)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate widget target conditions: %w", err)
	}
	return rules, nil
}

func (Store) MarkWidgetSeen(ctx context.Context, query db.Querier, projectID string, seenAt time.Time) error {
	_, err := query.Exec(ctx, `
		UPDATE projects
		SET widget_last_seen_at = $2
		WHERE id = $1
		  AND (widget_last_seen_at IS NULL OR widget_last_seen_at < $2::timestamptz - interval '5 minutes')
	`, projectID, seenAt)
	if err != nil {
		return fmt.Errorf("mark widget seen: %w", err)
	}
	return nil
}

const widgetRuleColumns = `
	id, project_id, name, priority, enabled, matcher_type, matcher_value,
	trigger_type, trigger_delay_seconds, trigger_scroll_percent,
	can_rate, can_write_text, can_record_audio, can_record_video,
	can_attach_element, can_attach_screenshot, greeting, position, theme, accent_color,
		replay_mode, replay_sampling_rate, replay_max_duration_seconds, replay_max_events,
		replay_mask_all_inputs, replay_mask_text_selector, replay_record_canvas, replay_canvas_fps,
		replay_consent,
		recurrence_mode, recurrence_days,
		definition_key, definition_version, definition_status, presentation, objective,
		response_blocks, localizations, allow_comment, collect_contact, ai_processing,
		widget_appearance_overrides, analytics_consent, launcher_variant,
		created_at, updated_at`

func prefixedWidgetRuleColumns(alias string) string {
	columns := []string{
		"id", "project_id", "name", "priority", "enabled", "matcher_type", "matcher_value",
		"trigger_type", "trigger_delay_seconds", "trigger_scroll_percent", "can_rate", "can_write_text",
		"can_record_audio", "can_record_video", "can_attach_element", "can_attach_screenshot", "greeting",
		"position", "theme", "accent_color", "replay_mode", "replay_sampling_rate",
		"replay_max_duration_seconds", "replay_max_events", "replay_mask_all_inputs",
		"replay_mask_text_selector", "replay_record_canvas", "replay_canvas_fps",
		"replay_consent",
		"recurrence_mode", "recurrence_days",
		"definition_key", "definition_version", "definition_status", "presentation", "objective",
		"response_blocks", "localizations", "allow_comment", "collect_contact", "ai_processing",
		"widget_appearance_overrides", "analytics_consent", "launcher_variant",
		"created_at", "updated_at",
	}
	for index := range columns {
		columns[index] = alias + "." + columns[index]
	}
	return joinColumns(columns)
}

func joinColumns(columns []string) string {
	result := ""
	for index, column := range columns {
		if index > 0 {
			result += ", "
		}
		result += column
	}
	return result
}

func widgetRuleArguments(tenantID string, projectID string, values WidgetRuleValues) ([]any, error) {
	valueArguments, err := widgetRuleValueArguments(values)
	if err != nil {
		return nil, err
	}
	return append([]any{tenantID, projectID}, valueArguments...), nil
}

func widgetRuleValueArguments(values WidgetRuleValues) ([]any, error) {
	responseBlocks, err := json.Marshal(values.ResponseBlocks)
	if err != nil {
		return nil, fmt.Errorf("encode widget response blocks: %w", err)
	}
	localizations, err := json.Marshal(values.Localizations)
	if err != nil {
		return nil, fmt.Errorf("encode widget localizations: %w", err)
	}
	appearance, err := json.Marshal(values.AppearanceOverrides)
	if err != nil {
		return nil, fmt.Errorf("encode widget appearance overrides: %w", err)
	}
	replayConsent, err := json.Marshal(values.ReplayConsent)
	if err != nil {
		return nil, fmt.Errorf("encode replay consent: %w", err)
	}
	aiProcessing, err := json.Marshal(values.AIProcessing)
	if err != nil {
		return nil, fmt.Errorf("encode AI processing configuration: %w", err)
	}
	analyticsConsent, err := json.Marshal(values.AnalyticsConsent)
	if err != nil {
		return nil, fmt.Errorf("encode analytics consent: %w", err)
	}
	return []any{
		values.Name, values.Priority, values.Enabled, values.MatcherType, values.MatcherValue,
		values.TriggerType, values.TriggerDelaySeconds, values.TriggerScrollPercent,
		values.CanRate, values.CanWriteText, values.CanRecordAudio, values.CanRecordVideo,
		values.CanAttachElement, values.CanAttachScreenshot, values.Greeting, values.Position,
		values.Theme, values.AccentColor, values.ReplayMode, values.ReplaySamplingRate,
		values.ReplayMaxDurationSeconds, values.ReplayMaxEvents, values.ReplayMaskAllInputs,
		values.ReplayMaskTextSelector, values.ReplayRecordCanvas, values.ReplayCanvasFPS,
		string(replayConsent),
		values.RecurrenceMode, values.RecurrenceDays,
		values.DefinitionKey, values.DefinitionStatus, values.Presentation, values.Objective,
		string(responseBlocks), string(localizations), values.AllowComment, values.CollectContact,
		string(aiProcessing),
		string(appearance),
		string(analyticsConsent),
		values.LauncherVariant,
	}, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanProject(row rowScanner) (Project, error) {
	var project Project
	var widgetLastSeenAt pgtype.Timestamptz
	err := row.Scan(
		&project.ID, &project.TenantID, &project.Name, &project.PublicKey,
		&project.AllowedDomains, &project.DefaultLocale, &project.ReporterAccessEnabled,
		&project.WidgetAppearanceOverrides, &widgetLastSeenAt,
		&project.CreatedAt, &project.UpdatedAt,
	)
	if widgetLastSeenAt.Valid {
		project.WidgetLastSeenAt = &widgetLastSeenAt.Time
	}
	return project, err
}

func scanWidgetRule(row rowScanner) (WidgetRule, error) {
	var rule WidgetRule
	err := row.Scan(
		&rule.ID, &rule.ProjectID, &rule.Name, &rule.Priority, &rule.Enabled,
		&rule.MatcherType, &rule.MatcherValue, &rule.TriggerType, &rule.TriggerDelaySeconds,
		&rule.TriggerScrollPercent, &rule.CanRate, &rule.CanWriteText, &rule.CanRecordAudio,
		&rule.CanRecordVideo, &rule.CanAttachElement, &rule.CanAttachScreenshot, &rule.Greeting,
		&rule.Position, &rule.Theme, &rule.AccentColor, &rule.ReplayMode, &rule.ReplaySamplingRate,
		&rule.ReplayMaxDurationSeconds, &rule.ReplayMaxEvents, &rule.ReplayMaskAllInputs,
		&rule.ReplayMaskTextSelector, &rule.ReplayRecordCanvas, &rule.ReplayCanvasFPS,
		&rule.ReplayConsent,
		&rule.RecurrenceMode, &rule.RecurrenceDays,
		&rule.DefinitionKey, &rule.DefinitionVersion, &rule.DefinitionStatus,
		&rule.Presentation, &rule.Objective, &rule.ResponseBlocks, &rule.Localizations,
		&rule.AllowComment, &rule.CollectContact,
		&rule.AIProcessing,
		&rule.AppearanceOverrides, &rule.AnalyticsConsent, &rule.LauncherVariant,
		&rule.CreatedAt, &rule.UpdatedAt,
	)
	return rule, err
}

func stringPointer(value string) *string {
	return &value
}
