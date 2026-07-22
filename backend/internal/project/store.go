package project

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrNotFound          = errors.New("project resource not found")
	ErrPublicKeyConflict = errors.New("project public key collision")
)

type Store struct{}

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
) (Project, error) {
	project, err := scanProject(query.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (public_key) DO NOTHING
		RETURNING id, tenant_id, name, public_key, allowed_domains, widget_last_seen_at, created_at, updated_at
	`, tenantID, name, publicKey, allowedDomains))
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
		SELECT id, tenant_id, name, public_key, allowed_domains, widget_last_seen_at, created_at, updated_at
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
		SELECT id, tenant_id, name, public_key, allowed_domains, widget_last_seen_at, created_at, updated_at
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
) (Project, error) {
	project, err := scanProject(query.QueryRow(ctx, `
		UPDATE projects SET name = $3, allowed_domains = $4
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, name, public_key, allowed_domains, widget_last_seen_at, created_at, updated_at
	`, tenantID, projectID, name, allowedDomains))
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("update project: %w", err)
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
) (WidgetRule, error) {
	rule, err := scanWidgetRule(query.QueryRow(ctx, `
		INSERT INTO widget_rules (
			project_id, name, priority, enabled, matcher_type, matcher_value,
			trigger_type, trigger_delay_seconds, trigger_scroll_percent,
			can_rate, can_write_text, can_record_audio, can_record_video,
			can_attach_element, can_attach_screenshot, greeting, position, theme,
				accent_color, replay_mode, replay_sampling_rate, replay_max_duration_seconds,
				replay_max_events, replay_mask_all_inputs, replay_mask_text_selector,
				recurrence_mode, recurrence_days
			)
			SELECT id, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
			       $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26,
			       $27, $28
		FROM projects WHERE tenant_id = $1 AND id = $2
		RETURNING `+widgetRuleColumns+`
	`, widgetRuleArguments(tenantID, projectID, values)...))
	if errors.Is(err, pgx.ErrNoRows) {
		return WidgetRule{}, ErrNotFound
	}
	if err != nil {
		return WidgetRule{}, fmt.Errorf("create widget rule: %w", err)
	}
	return rule, nil
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
	return rules, nil
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
	return rule, nil
}

func (Store) UpdateWidgetRule(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	ruleID string,
	values WidgetRuleValues,
) (WidgetRule, error) {
	arguments := append([]any{tenantID, projectID, ruleID}, widgetRuleValueArguments(values)...)
	rule, err := scanWidgetRule(query.QueryRow(ctx, `
		UPDATE widget_rules AS rule SET
			name = $4, priority = $5, enabled = $6, matcher_type = $7, matcher_value = $8,
			trigger_type = $9, trigger_delay_seconds = $10, trigger_scroll_percent = $11,
			can_rate = $12, can_write_text = $13, can_record_audio = $14, can_record_video = $15,
			can_attach_element = $16, can_attach_screenshot = $17, greeting = $18,
			position = $19, theme = $20, accent_color = $21, replay_mode = $22,
				replay_sampling_rate = $23, replay_max_duration_seconds = $24,
				replay_max_events = $25, replay_mask_all_inputs = $26, replay_mask_text_selector = $27,
				recurrence_mode = $28, recurrence_days = $29
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
	return rule, nil
}

func (Store) DeleteWidgetRule(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	ruleID string,
) error {
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

func (Store) LoadPublicConfiguration(
	ctx context.Context,
	query db.Querier,
	publicKey string,
) (PublicConfiguration, error) {
	var configuration PublicConfiguration
	var plan string
	var status string
	var trialEndsAt pgtype.Timestamptz
	var deletionRequestedAt pgtype.Timestamptz
	err := query.QueryRow(ctx, `
		SELECT tenant.id, tenant.slug, project.id, project.allowed_domains,
		       tenant.plan, tenant.status, tenant.trial_ends_at, tenant.deletion_requested_at
		FROM projects AS project
		JOIN tenants AS tenant ON tenant.id = project.tenant_id
		WHERE project.public_key = $1
	`, publicKey).Scan(
		&configuration.TenantID, &configuration.TenantSlug,
		&configuration.ProjectID, &configuration.AllowedDomains, &plan, &status, &trialEndsAt,
		&deletionRequestedAt,
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

	rows, err := query.Query(ctx, `
		SELECT `+prefixedWidgetRuleColumns("rule")+`
		FROM widget_rules AS rule
		WHERE rule.project_id = $1 AND rule.enabled = true
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
	return configuration, nil
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
		replay_mask_all_inputs, replay_mask_text_selector, recurrence_mode, recurrence_days,
		created_at, updated_at`

func prefixedWidgetRuleColumns(alias string) string {
	columns := []string{
		"id", "project_id", "name", "priority", "enabled", "matcher_type", "matcher_value",
		"trigger_type", "trigger_delay_seconds", "trigger_scroll_percent", "can_rate", "can_write_text",
		"can_record_audio", "can_record_video", "can_attach_element", "can_attach_screenshot", "greeting",
		"position", "theme", "accent_color", "replay_mode", "replay_sampling_rate",
		"replay_max_duration_seconds", "replay_max_events", "replay_mask_all_inputs",
		"replay_mask_text_selector", "recurrence_mode", "recurrence_days", "created_at", "updated_at",
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

func widgetRuleArguments(tenantID string, projectID string, values WidgetRuleValues) []any {
	return append([]any{tenantID, projectID}, widgetRuleValueArguments(values)...)
}

func widgetRuleValueArguments(values WidgetRuleValues) []any {
	return []any{
		values.Name, values.Priority, values.Enabled, values.MatcherType, values.MatcherValue,
		values.TriggerType, values.TriggerDelaySeconds, values.TriggerScrollPercent,
		values.CanRate, values.CanWriteText, values.CanRecordAudio, values.CanRecordVideo,
		values.CanAttachElement, values.CanAttachScreenshot, values.Greeting, values.Position,
		values.Theme, values.AccentColor, values.ReplayMode, values.ReplaySamplingRate,
		values.ReplayMaxDurationSeconds, values.ReplayMaxEvents, values.ReplayMaskAllInputs,
		values.ReplayMaskTextSelector, values.RecurrenceMode, values.RecurrenceDays,
	}
}

type rowScanner interface {
	Scan(...any) error
}

func scanProject(row rowScanner) (Project, error) {
	var project Project
	var widgetLastSeenAt pgtype.Timestamptz
	err := row.Scan(
		&project.ID, &project.TenantID, &project.Name, &project.PublicKey,
		&project.AllowedDomains, &widgetLastSeenAt, &project.CreatedAt, &project.UpdatedAt,
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
		&rule.ReplayMaskTextSelector, &rule.RecurrenceMode, &rule.RecurrenceDays,
		&rule.CreatedAt, &rule.UpdatedAt,
	)
	return rule, err
}
