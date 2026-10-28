package ai

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/fidibeki/app.fidibeki/backend/internal/aiprovider"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

type Execution struct {
	aiprovider.Selection
	Contract json.RawMessage `json:"contract"`
}

func (s Store) snapshot(contract json.RawMessage) (Execution, error) {
	var selection aiprovider.Selection
	if err := json.Unmarshal(contract, &selection); err != nil {
		return Execution{}, err
	}
	if selection.BillingMode == "" {
		selection.BillingMode = aiprovider.Managed
	}
	if selection.BillingMode == aiprovider.Managed {
		if selection.ConnectionID != "" || selection.Provider != aiprovider.Google {
			return Execution{}, aiprovider.ErrInvalid
		}
		// The configured system model is captured at scheduling, not at dispatch.
		selection.Model = s.model()
	}
	return Execution{Selection: selection, Contract: contract}, nil
}

func (s Store) pinPrepared(ctx context.Context, q db.Querier, tasks []PreparedTask, contract json.RawMessage) error {
	execution, err := s.snapshot(contract)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(execution)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		query := `UPDATE feedbacks SET ai_execution=$3::jsonb WHERE tenant_id=$1 AND id=$2`
		if task.Kind == TaskKindAttachment {
			query = `UPDATE attachments SET ai_execution=$3::jsonb WHERE tenant_id=$1 AND id=$2`
		}
		if _, err := q.Exec(ctx, query, task.TenantID, task.ResourceID, string(encoded)); err != nil {
			return err
		}
	}
	return nil
}

func (s Store) readExecution(ctx context.Context, q db.Querier, tenant, id string, attachment bool, contract json.RawMessage) (Execution, error) {
	query := `SELECT ai_execution FROM feedbacks WHERE tenant_id=$1 AND id=$2`
	if attachment {
		query = `SELECT ai_execution FROM attachments WHERE tenant_id=$1 AND id=$2`
	}
	var encoded []byte
	if err := q.QueryRow(ctx, query, tenant, id).Scan(&encoded); err != nil {
		return Execution{}, err
	}
	if len(encoded) == 0 {
		// Only legacy managed work can lack a snapshot. A customer destination
		// must never be inferred from the rule's current defaults.
		execution, err := s.snapshot(contract)
		if err != nil {
			return execution, err
		}
		if execution.BillingMode != aiprovider.Managed {
			return Execution{}, ErrNoWork
		}
		return execution, nil
	}
	var execution Execution
	if err := json.Unmarshal(encoded, &execution); err != nil {
		return execution, err
	}
	return execution, nil
}

func executionEligible(ctx context.Context, q db.Querier, tenant, rule, category string, e Execution) (bool, error) {
	if len(e.Contract) == 0 {
		return false, nil
	}
	var current []byte
	err := q.QueryRow(ctx, `SELECT rule.ai_processing FROM widget_rules rule JOIN projects p ON p.id=rule.project_id WHERE p.tenant_id=$1 AND rule.id=$2 FOR SHARE OF rule`, tenant, rule).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var left, right map[string]any
	if json.Unmarshal(current, &left) != nil || json.Unmarshal(e.Contract, &right) != nil || !reflect.DeepEqual(left, right) {
		return false, nil
	}
	if left["enabled"] != true {
		return false, nil
	}
	var config struct {
		Inputs []string `json:"input_categories"`
	}
	if json.Unmarshal(current, &config) != nil || !aiprovider.Supports(config.Inputs, category) {
		return false, nil
	}
	if e.BillingMode == aiprovider.Customer {
		var id string
		// Hold this lock through persistence, so disable/configuration changes
		// and committing an AI result have a defined transaction order.
		err = q.QueryRow(ctx, `SELECT id FROM ai_connections WHERE tenant_id=$1 AND id=$2 FOR SHARE`, tenant, e.ConnectionID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	if err := aiprovider.ValidateSelection(ctx, q, tenant, e.Selection, []string{category}); err != nil {
		if errors.Is(err, aiprovider.ErrUnvalidated) || errors.Is(err, aiprovider.ErrInvalid) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
