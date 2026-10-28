package aiprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	Database *pgxpool.Pool
	Keys     *Keyring
}

const connectionJSON = `to_jsonb(c) || jsonb_build_object('models',COALESCE((
 SELECT jsonb_agg(jsonb_build_object('id',m.model_id,'name',m.model_id,'input_categories',m.input_categories,'validated',true) ORDER BY m.model_id)
 FROM ai_connection_models m WHERE m.tenant_id=c.tenant_id AND m.connection_id=c.id
 AND m.config_version=c.config_version AND m.credential_version=c.credential_version
),'[]'::jsonb))`

func scanConnection(row pgx.Row) (Connection, error) {
	var data []byte
	var c Connection
	if err := row.Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c, ErrNotFound
		}
		return c, err
	}
	err := json.Unmarshal(data, &c)
	return c, err
}
func (s Store) Get(ctx context.Context, q db.Querier, tenant, id string) (Connection, error) {
	c, err := scanConnection(q.QueryRow(ctx, `SELECT `+connectionJSON+` FROM ai_connections c WHERE c.tenant_id=$1 AND c.id=$2`, tenant, id))
	c.TenantID = tenant
	return c, err
}
func (s Store) List(ctx context.Context, q db.Querier, tenant string) ([]Connection, error) {
	rows, err := q.Query(ctx, `SELECT `+connectionJSON+` FROM ai_connections c WHERE c.tenant_id=$1 AND NOT c.deleted ORDER BY c.is_default DESC,c.name,c.id`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Connection{}
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		c.TenantID = tenant
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s Store) Create(ctx context.Context, q db.Querier, tenant, actor string, input Input) (Connection, error) {
	input, err := ValidateInput(input)
	if err != nil {
		return Connection{}, err
	}
	if input.AuthMode != "role" && input.Credential == "" {
		return Connection{}, fmt.Errorf("%w: credential is required", ErrInvalid)
	}
	id := uuid.NewString()
	externalID := ""
	if input.AuthMode == "role" {
		externalID = uuid.NewString()
		input.Credential = ""
	}
	encrypted, err := s.Keys.Encrypt(input.Credential, CredentialAAD(tenant, id, 1))
	if err != nil {
		return Connection{}, err
	}
	// Serialize default updates per workspace as well as connection creation.
	if _, err = q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,138))`, tenant); err != nil {
		return Connection{}, err
	}
	if input.IsDefault {
		if _, err = q.Exec(ctx, `UPDATE ai_connections SET is_default=false WHERE tenant_id=$1 AND is_default`, tenant); err != nil {
			return Connection{}, err
		}
	}
	_, err = q.Exec(ctx, `INSERT INTO ai_connections(id,tenant_id,name,provider,base_url,region,auth_mode,role_arn,external_id,enabled,rpm,concurrency,default_model,is_default,credential_expires_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, id, tenant, input.Name, input.Provider, input.BaseURL, input.Region, input.AuthMode, input.RoleARN, externalID, input.Enabled, input.RPM, input.Concurrency, input.DefaultModel, input.IsDefault, input.CredentialExpiresAt)
	if err != nil {
		return Connection{}, err
	}
	if _, err = q.Exec(ctx, `INSERT INTO ai_connection_credentials(tenant_id,connection_id,version,envelope) VALUES($1,$2,1,$3)`, tenant, id, encrypted); err != nil {
		return Connection{}, err
	}
	if err = audit(ctx, q, tenant, actor, id, "created", 1); err != nil {
		return Connection{}, err
	}
	return s.Get(ctx, q, tenant, id)
}

func (s Store) Update(ctx context.Context, q db.Querier, tenant, actor, id string, input Input) (Connection, error) {
	input, err := ValidateInput(input)
	if err != nil {
		return Connection{}, err
	}
	if _, err = q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,138))`, tenant); err != nil {
		return Connection{}, err
	}
	var locked string
	if err = q.QueryRow(ctx, `SELECT id FROM ai_connections WHERE tenant_id=$1 AND id=$2 AND NOT deleted FOR UPDATE`, tenant, id).Scan(&locked); errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, ErrNotFound
	} else if err != nil {
		return Connection{}, err
	}
	old, err := s.Get(ctx, q, tenant, id)
	if err != nil {
		return Connection{}, err
	}
	if old.Revision != input.Revision {
		return Connection{}, ErrConflict
	}
	destinationChanged := old.Provider != input.Provider || old.BaseURL != input.BaseURL || old.Region != input.Region || old.RoleARN != input.RoleARN || old.AuthMode != input.AuthMode
	if old.Provider != input.Provider {
		return Connection{}, fmt.Errorf("%w: create a new connection to change provider", ErrInvalid)
	}
	if destinationChanged && input.AuthMode != "role" && input.Credential == "" {
		return Connection{}, fmt.Errorf("%w: re-enter credentials when changing destination", ErrInvalid)
	}
	version, credentialVersion := old.ConfigVersion, old.CredentialVersion
	health := old.Health
	event := "updated"
	if destinationChanged {
		version++
		health = "unvalidated"
		event = "destination_changed"
	}
	if input.Credential != "" || old.AuthMode != input.AuthMode {
		credentialVersion++
		health = "unvalidated"
		if !destinationChanged {
			event = "credential_rotated"
		}
		value := input.Credential
		if input.AuthMode == "role" {
			value = ""
		}
		encrypted, err := s.Keys.Encrypt(value, CredentialAAD(tenant, id, credentialVersion))
		if err != nil {
			return Connection{}, err
		}
		if _, err = q.Exec(ctx, `INSERT INTO ai_connection_credentials(tenant_id,connection_id,version,envelope) VALUES($1,$2,$3,$4)`, tenant, id, credentialVersion, encrypted); err != nil {
			return Connection{}, err
		}
	}
	if !input.Enabled {
		event = "disabled"
	}
	externalID := old.ExternalID
	if input.AuthMode == "role" && externalID == "" {
		externalID = uuid.NewString()
	}
	if input.IsDefault {
		if _, err = q.Exec(ctx, `UPDATE ai_connections SET is_default=false WHERE tenant_id=$1 AND id<>$2 AND is_default`, tenant, id); err != nil {
			return Connection{}, err
		}
	}
	_, err = q.Exec(ctx, `UPDATE ai_connections SET name=$3,base_url=$4,region=$5,auth_mode=$6,role_arn=$7,external_id=$8,enabled=$9,rpm=$10,concurrency=$11,default_model=$12,is_default=$13,credential_expires_at=$14,config_version=$15,credential_version=$16,revision=revision+1,health=$17,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, id, input.Name, input.BaseURL, input.Region, input.AuthMode, input.RoleARN, externalID, input.Enabled, input.RPM, input.Concurrency, input.DefaultModel, input.IsDefault, input.CredentialExpiresAt, version, credentialVersion, health)
	if err != nil {
		return Connection{}, err
	}
	if err = audit(ctx, q, tenant, actor, id, event, old.Revision+1); err != nil {
		return Connection{}, err
	}
	// The ledger has its own encrypted cleanup snapshot; retired inference keys
	// need not remain in the connection table.
	if _, err = q.Exec(ctx, `DELETE FROM ai_connection_credentials WHERE tenant_id=$1 AND connection_id=$2 AND version<>$3`, tenant, id, credentialVersion); err != nil {
		return Connection{}, err
	}
	return s.Get(ctx, q, tenant, id)
}

func (s Store) Delete(ctx context.Context, q db.Querier, tenant, actor, id string) (string, error) {
	tag, err := q.Exec(ctx, `UPDATE ai_connections SET enabled=false,deleted=true,is_default=false,revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND NOT deleted`, tenant, id)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	if _, err = q.Exec(ctx, `DELETE FROM ai_connection_credentials WHERE tenant_id=$1 AND connection_id=$2`, tenant, id); err != nil {
		return "", err
	}
	if err = audit(ctx, q, tenant, actor, id, "deleted", 0); err != nil {
		return "", err
	}
	var pending bool
	err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_gemini_files WHERE tenant_id=$1 AND connection_id=$2 AND deleted_at IS NULL)`, tenant, id).Scan(&pending)
	if pending {
		return "cleanup_pending", err
	}
	return "disabled", err
}

func (s Store) Credential(ctx context.Context, q db.Querier, c Connection) (Secret, error) {
	if c.Deleted || !c.Enabled {
		return Secret{}, ErrDisabled
	}
	if c.CredentialExpiresAt != nil && !c.CredentialExpiresAt.After(time.Now()) {
		return Secret{}, &Failure{Code: "credential_expired"}
	}
	var encrypted []byte
	err := q.QueryRow(ctx, `SELECT envelope FROM ai_connection_credentials WHERE tenant_id=$1 AND connection_id=$2 AND version=$3`, c.TenantID, c.ID, c.CredentialVersion).Scan(&encrypted)
	if err != nil {
		return Secret{}, ErrNotFound
	}
	value, err := s.Keys.Decrypt(encrypted, CredentialAAD(c.TenantID, c.ID, c.CredentialVersion))
	return Secret{Value: value, Version: c.CredentialVersion, Envelope: encrypted}, err
}

func (s Store) RecordValidation(ctx context.Context, q db.Querier, c Connection, actor string, model Model, failure error) error {
	health := "ready"
	if failure != nil {
		health = "validation_failed"
		var classified *Failure
		if errors.As(failure, &classified) {
			health = classified.Code
		}
	}
	tag, err := q.Exec(ctx, `UPDATE ai_connections SET health=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND revision=$3 AND enabled AND NOT deleted`, c.TenantID, c.ID, c.Revision, health)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	if failure == nil {
		_, err = q.Exec(ctx, `INSERT INTO ai_connection_models(tenant_id,connection_id,model_id,config_version,credential_version,input_categories) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,connection_id,model_id,config_version,credential_version) DO UPDATE SET input_categories=EXCLUDED.input_categories,validated_at=now()`, c.TenantID, c.ID, model.ID, c.ConfigVersion, c.CredentialVersion, model.Inputs)
		if err != nil {
			return err
		}
	}
	if failure != nil {
		if _, err = q.Exec(ctx, `DELETE FROM ai_connection_models WHERE tenant_id=$1 AND connection_id=$2 AND model_id=$3 AND config_version=$4 AND credential_version=$5`, c.TenantID, c.ID, model.ID, c.ConfigVersion, c.CredentialVersion); err != nil {
			return err
		}
	}
	return audit(ctx, q, c.TenantID, actor, c.ID, "validated", c.Revision)
}

func ValidateSelection(ctx context.Context, q db.Querier, tenant string, selection Selection, inputs []string) error {
	if selection.BillingMode == "" || selection.BillingMode == Managed {
		if selection.ConnectionID != "" || selection.Provider != Google {
			return ErrInvalid
		}
		return nil
	}
	if selection.BillingMode != Customer || !ValidModelID(selection.Model, false) {
		return ErrInvalid
	}
	if _, err := uuid.Parse(selection.ConnectionID); err != nil {
		return ErrInvalid
	}
	var valid bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_connections c JOIN ai_connection_models m ON m.tenant_id=c.tenant_id AND m.connection_id=c.id AND m.config_version=c.config_version AND m.credential_version=c.credential_version
 WHERE c.tenant_id=$1 AND c.id=$2 AND c.provider=$3 AND c.config_version=$4 AND c.enabled AND NOT c.deleted AND (c.credential_expires_at IS NULL OR c.credential_expires_at>now()) AND m.model_id=$5 AND m.input_categories @> $6::text[])`, tenant, selection.ConnectionID, selection.Provider, selection.ConfigVersion, selection.Model, inputs).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrUnvalidated
	}
	return nil
}

func audit(ctx context.Context, q db.Querier, tenant, actor, id, event string, revision int64) error {
	data, _ := json.Marshal(map[string]any{"connection_id": id, "revision": revision})
	var member any
	if _, err := uuid.Parse(actor); err == nil {
		member = actor
	}
	_, err := q.Exec(ctx, `INSERT INTO tenant_access_events(tenant_id,kind,actor_member_id,data) VALUES($1,$2,$3,$4::jsonb)`, tenant, "ai_connection."+event, member, string(data))
	return err
}
