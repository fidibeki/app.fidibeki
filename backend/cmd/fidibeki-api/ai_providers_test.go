package main

import (
	"encoding/base64"
	"github.com/fidibeki/app.fidibeki/backend/internal/config"
	"strings"
	"testing"
)

func TestAIProviderRolloutFlagsAndManagedMode(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	settings := config.Config{GeminiAPIKey: "managed", GeminiModel: "fixture", IntegrationEncryptionKey: key}
	f, err := newAIProviderFactory(settings, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.Allowed("workspace", "openai") || f.ManagedKey != "managed" {
		t.Fatal("customer mode must default off and preserve managed")
	}
	settings.AIProviders = "openai,amazon_bedrock"
	settings.AITenants = "10000000-0000-4000-8000-000000000001"
	settings.AIManagedMode = "disabled"
	f, err = newAIProviderFactory(settings, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.ManagedKey != "" || !f.Allowed(settings.AITenants, "openai") || f.Allowed("20000000-0000-4000-8000-000000000001", "openai") || f.Allowed(settings.AITenants, "anthropic") {
		t.Fatal("rollout scopes ignored")
	}
	settings.IntegrationEncryptionKey = ""
	if _, err = newAIProviderFactory(settings, nil, nil); err == nil {
		t.Fatal("customer mode accepted without encryption")
	}
	settings.IntegrationEncryptionKey = key
	settings.AIProviders = "invented"
	if _, err = newAIProviderFactory(settings, nil, nil); err == nil {
		t.Fatal("unknown rollout provider accepted")
	}
}
