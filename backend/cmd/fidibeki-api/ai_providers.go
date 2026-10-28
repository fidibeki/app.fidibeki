package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/fidibeki/app.fidibeki/backend/internal/ai"
	"github.com/fidibeki/app.fidibeki/backend/internal/aiprovider"
	"github.com/fidibeki/app.fidibeki/backend/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newAIProviderFactory(settings config.Config, database *pgxpool.Pool, logger *slog.Logger) (*ai.ProviderFactory, error) {
	keys := map[string]string{}
	active := settings.AIActiveEncryptionKey
	if settings.AIEncryptionKeys != "" {
		if err := json.Unmarshal([]byte(settings.AIEncryptionKeys), &keys); err != nil {
			return nil, errors.New("AI_ENCRYPTION_KEYS must be a JSON map of key IDs to base64 encryption keys")
		}
	} else if settings.IntegrationEncryptionKey != "" {
		active = "integration-v1"
		keys[active] = settings.IntegrationEncryptionKey
	}
	var ring *aiprovider.Keyring
	if len(keys) > 0 {
		var err error
		ring, err = aiprovider.NewKeyring(active, keys)
		if err != nil {
			return nil, err
		}
	}
	providers := map[string]bool{}
	tenants := map[string]bool{}
	for _, p := range strings.Split(settings.AIProviders, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := aiprovider.Lookup(p); !ok {
			return nil, errors.New("AI_CUSTOMER_PROVIDERS contains an unsupported provider")
		}
		providers[p] = true
	}
	for _, t := range strings.Split(settings.AITenants, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if t != "*" {
			if _, err := uuid.Parse(t); err != nil {
				return nil, errors.New("AI_CUSTOMER_TENANTS must contain workspace UUIDs or *")
			}
		}
		tenants[t] = true
	}
	if len(providers) > 0 && ring == nil {
		return nil, errors.New("customer AI requires credential encryption keys")
	}
	key := settings.GeminiAPIKey
	if settings.AIManagedMode == "disabled" {
		key = ""
	}
	store := ai.Store{Database: database, Model: settings.GeminiModel, ProjectDailyLimit: settings.AIProjectDailyLimit, Logger: logger}
	return &ai.ProviderFactory{Connections: aiprovider.Store{Database: database, Keys: ring}, Ledger: store, ManagedKey: key, ManagedModel: settings.GeminiModel, Logger: logger, Allowed: func(tenant, provider string) bool { return providers[provider] && (tenants[tenant] || tenants["*"]) }, AWS: ai.AWSCaller{AccessKey: settings.AIAWSAccessKey, SecretKey: settings.AIAWSSecretKey, SessionToken: settings.AIAWSSessionToken, ARN: settings.AIAWSCallerARN}}, nil
}
