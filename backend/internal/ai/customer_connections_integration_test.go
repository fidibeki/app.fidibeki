package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/aiprovider"
	"github.com/fidibeki/app.fidibeki/backend/internal/ratelimit"
	"github.com/hibiken/asynq"
)

func TestCustomerConnectionsPinWorkAndPreserveCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("PostgreSQL integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := aiTestDatabase(t, ctx)
	seedAIFeedback(t, ctx, pool)
	keys, err := aiprovider.NewKeyring("v1", map[string]string{"v1": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))})
	if err != nil {
		t.Fatal(err)
	}
	cs := aiprovider.Store{Database: pool, Keys: keys}
	store := Store{Database: pool, Model: "managed-model"}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	input := aiprovider.Input{Name: "Customer Google", Provider: aiprovider.Google, Credential: "customer-secret-v1", Enabled: true, IsDefault: true}
	c, err := cs.Create(ctx, tx, testTenantID, testMemberID, input)
	if err != nil {
		t.Fatal(err)
	}
	model := aiprovider.Model{ID: "customer-model", Inputs: []string{"feedback_text", "audio", "screenshot"}, Validated: true}
	selection := aiprovider.Selection{BillingMode: aiprovider.Customer, ConnectionID: c.ID, ConfigVersion: c.ConfigVersion, Provider: c.Provider, Model: model.ID}
	if err = aiprovider.ValidateSelection(ctx, tx, testTenantID, selection, []string{"feedback_text"}); !errors.Is(err, aiprovider.ErrUnvalidated) {
		t.Fatalf("unvalidated model accepted: %v", err)
	}
	if err = cs.RecordValidation(ctx, tx, c, testMemberID, model, nil); err != nil {
		t.Fatal(err)
	}
	if err = aiprovider.ValidateSelection(ctx, tx, testStarterTenantID, selection, []string{"feedback_text"}); !errors.Is(err, aiprovider.ErrUnvalidated) {
		t.Fatalf("cross-tenant selection accepted: %v", err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// Exercise the application's forced RLS role, not just explicit predicates.
	isolated, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = isolated.Exec(ctx, `SET LOCAL ROLE fidibeki_app`); err != nil {
		t.Fatal(err)
	}
	if _, err = isolated.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, testStarterTenantID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"ai_connections", "ai_connection_credentials", "ai_connection_models"} {
		var count int
		if err = isolated.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("RLS %s count=%d err=%v", table, count, err)
		}
	}
	_ = isolated.Rollback(ctx)
	var contract map[string]any
	var raw []byte
	if err = pool.QueryRow(ctx, `SELECT ai_processing FROM widget_rules WHERE id=$1`, testWidgetRuleID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	contract["billing_mode"] = aiprovider.Customer
	contract["connection_id"] = c.ID
	contract["config_version"] = c.ConfigVersion
	contract["model"] = model.ID
	contract["policy_version"] = "2026-10-02"
	contract["input_categories"] = []string{"feedback_text", "audio", "screenshot"}
	raw, _ = json.Marshal(contract)
	if _, err = pool.Exec(ctx, `UPDATE widget_rules SET ai_processing=$2 WHERE id=$1`, testWidgetRuleID, raw); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	tasks, _, err := store.PrepareFeedback(ctx, testFeedbackID, 10, now)
	if err != nil || len(tasks) != 3 {
		t.Fatalf("prepare %v %v", tasks, err)
	}
	var textTask PreparedTask
	for _, task := range tasks {
		if task.Kind == TaskKindText {
			textTask = task
		}
	}
	work, err := store.ClaimText(ctx, testTenantID, testFeedbackID, textTask.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if work.Execution.Model != model.ID || work.Execution.ConnectionID != c.ID {
		t.Fatalf("wrong execution: %#v", work.Execution)
	}
	// Resolve with the validated credential and retain that exact analyzer while
	// rotation races dispatch. Its transport must reject before any network call.
	factory := &ProviderFactory{Connections: cs, Ledger: store, Allowed: func(string, string) bool { return true }}
	resolved, _, _, err := factory.Resolve(ctx, testTenantID, work.Execution, "feedback_text", nil)
	if err != nil {
		t.Fatal(err)
	}
	lease := &countingConnectionLeaser{}
	processor := Processor{Factory: factory, Store: store, Leaser: lease, Limiter: testLimiter{decision: ratelimit.Decision{Allowed: false, RetryAfter: time.Second}}}
	if _, _, _, err := processor.resolve(ctx, testTenantID, work.Execution, "feedback_text", work.RunID, func(context.Context) (bool, error) { return true, nil }); err == nil || lease.released != 1 {
		t.Fatal("rate-limit rejection leaked connection lease", err)
	}
	processor.Limiter = allowLimiter{}
	_, _, release, err := processor.resolve(ctx, testTenantID, work.Execution, "feedback_text", work.RunID, func(context.Context) (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	release()
	if lease.released != 2 {
		t.Fatal("completed dispatch preparation leaked lease")
	}
	// The caller keeps the key for cleanup, independently of later rotation/deletion.
	secret, err := cs.Credential(ctx, pool, c)
	if err != nil {
		t.Fatal(err)
	}
	cleanupID, err := store.TrackGeminiFile(ctx, AttachmentWork{TenantID: testTenantID, AttachmentID: testAudioID, RunID: work.RunID, Execution: work.Execution, CredentialVersion: secret.Version, CredentialEnvelope: secret.Envelope}, "files/customer-fixture", now)
	if err != nil {
		t.Fatal(err)
	}
	update := input
	update.Revision = c.Revision
	update.Credential = "customer-secret-v2"
	update.DefaultModel = "other-model"
	tx, _ = pool.Begin(ctx)
	rotated, err := cs.Update(ctx, tx, testTenantID, testMemberID, c.ID, update)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.ConfigVersion != c.ConfigVersion || rotated.CredentialVersion != 2 {
		t.Fatal("rotation changed destination or missed credential version")
	}
	if err = aiprovider.ValidateSelection(ctx, tx, testTenantID, selection, []string{"feedback_text"}); !errors.Is(err, aiprovider.ErrUnvalidated) {
		t.Fatalf("rotated key remained validated: %v", err)
	}
	if err = cs.RecordValidation(ctx, tx, rotated, testMemberID, model, nil); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = resolved.AnalyzeText(ctx, "This must never reach the provider"); err == nil || !strings.Contains(err.Error(), "credential_changed") || IsPermanent(err) {
		t.Fatalf("rotation did not stop stale dispatch: %v", err)
	}
	allowed, err := store.AuthorizeTextForProvider(ctx, work, now)
	if err != nil || !allowed {
		t.Fatalf("default/rotation rerouted unchanged contract: %v %v", allowed, err)
	}
	// A new consent contract cannot receive a stale result.
	contract["model"] = "other-model"
	raw, _ = json.Marshal(contract)
	if _, err = pool.Exec(ctx, `UPDATE widget_rules SET ai_processing=$2 WHERE id=$1`, testWidgetRuleID, raw); err != nil {
		t.Fatal(err)
	}
	allowed, err = store.AuthorizeTextForProvider(ctx, work, now)
	if err != nil || allowed {
		t.Fatalf("stale work authorized: %v %v", allowed, err)
	}
	tx, _ = pool.Begin(ctx)
	status, err := cs.Delete(ctx, tx, testTenantID, testMemberID, c.ID)
	if err != nil || status != "cleanup_pending" {
		t.Fatalf("delete %q %v", status, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM ai_connection_credentials WHERE connection_id=$1`, c.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("inference credentials retained")
	}
	// Source deletion must leave a schedulable cleanup tombstone.
	if _, err = pool.Exec(ctx, `DELETE FROM attachments WHERE id=$1`, testAudioID); err != nil {
		t.Fatal(err)
	}
	pending, err := store.PrepareGeminiFileDeletions(ctx, 10, now)
	if err != nil || len(pending) != 1 {
		t.Fatalf("cleanup lost: %v %v", pending, err)
	}
	cleanup, err := store.ClaimGeminiFile(ctx, testTenantID, cleanupID, pending[0].Attempt)
	if err != nil {
		t.Fatal(err)
	}
	oldKey, err := keys.Decrypt(cleanup.CredentialEnvelope, aiprovider.CredentialAAD(testTenantID, c.ID, cleanup.CredentialVersion))
	if err != nil || oldKey != "customer-secret-v1" {
		t.Fatal("cleanup lost original key")
	}
	if err = store.CompleteGeminiFileDeletion(ctx, cleanup, now); err != nil {
		t.Fatal(err)
	}
	var envelope []byte
	if err = pool.QueryRow(ctx, `SELECT credential_envelope FROM ai_gemini_files WHERE id=$1`, cleanupID).Scan(&envelope); err != nil || len(envelope) != 0 {
		t.Fatal("cleanup credential not erased")
	}
	// A deletion racing the upload can still record its tombstone with a vanished run.
	_, err = store.TrackGeminiFile(ctx, AttachmentWork{TenantID: testTenantID, AttachmentID: testAudioID, RunID: "00000000-0000-4000-8000-000000000999", Execution: work.Execution, CredentialVersion: secret.Version, CredentialEnvelope: secret.Envelope}, "files/late-upload", now)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCustomerMigrationCanRollBackBeforeEnablement(t *testing.T) {
	if testing.Short() {
		t.Skip("PostgreSQL migration integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := aiTestDatabase(t, ctx)
	for _, direction := range []string{"down", "up"} {
		content, err := os.ReadFile(filepath.Join(aiMigrationsPath(t), "000143_add_customer_ai_connections."+direction+".sql"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, string(content)); err != nil {
			t.Fatalf("migration %s: %v", direction, err)
		}
	}
}

type countingConnectionLeaser struct{ released int }

func (l *countingConnectionLeaser) Acquire(context.Context, string, int) (func(), error) {
	return func() { l.released++ }, nil
}

func TestCleanupWithoutDecryptionKeyPreservesRecoveryEvidence(t *testing.T) {
	if testing.Short() {
		t.Skip("PostgreSQL integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := aiTestDatabase(t, ctx)
	seedAIFeedback(t, ctx, pool)
	store := Store{Database: pool}
	now := time.Now().UTC()
	id, err := store.TrackGeminiFile(ctx, AttachmentWork{TenantID: testTenantID, AttachmentID: testAudioID, CredentialVersion: 1, CredentialEnvelope: []byte(`{"v":1,"key":"retired","data":"YQ=="}`)}, "files/recovery", now)
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := store.PrepareGeminiFileDeletions(ctx, 10, now)
	if err != nil || len(tasks) != 1 {
		t.Fatal("cleanup not scheduled", err)
	}
	payload, err := json.Marshal(taskPayload{TenantID: testTenantID, ResourceID: id, Attempt: tasks[0].Attempt})
	if err != nil {
		t.Fatal(err)
	}
	f := &ProviderFactory{Ledger: store}
	task := asynq.NewTask(TaskGeminiFileDelete, payload)
	if err = f.FileDeleteHandler()(ctx, task); !errors.Is(err, asynq.SkipRetry) {
		t.Fatal("missing key did not stop dispatch", err)
	}
	var failed bool
	var envelope []byte
	if err = pool.QueryRow(ctx, `SELECT failed_at IS NOT NULL AND deleted_at IS NULL,credential_envelope FROM ai_gemini_files WHERE id=$1`, id).Scan(&failed, &envelope); err != nil || !failed || len(envelope) == 0 {
		t.Fatal("lost cleanup recovery evidence", err)
	}
	if err = f.FileDeleteHandler()(ctx, task); err != nil {
		t.Fatal("completed attempt was replayed", err)
	}
	if err = f.FileDeleteHandler()(ctx, asynq.NewTask(TaskGeminiFileDelete, []byte("{"))); !errors.Is(err, asynq.SkipRetry) {
		t.Fatal("invalid cleanup payload retried", err)
	}
}
