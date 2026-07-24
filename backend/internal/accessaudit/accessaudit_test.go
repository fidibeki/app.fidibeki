package accessaudit

import (
	"context"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
)

func TestActorFromClaims(t *testing.T) {
	t.Run("human session", func(t *testing.T) {
		claims := auth.Claims{}
		claims.Subject = "member-1"
		actor := ActorFromClaims(claims)
		if actor.Type != ActorHuman || actor.MemberID != "member-1" || actor.APIKeyID != "" {
			t.Fatalf("unexpected actor: %+v", actor)
		}
	})

	t.Run("api key credential", func(t *testing.T) {
		claims := auth.Claims{IsAPIKey: true}
		claims.Subject = "key-1"
		actor := ActorFromClaims(claims)
		if actor.Type != ActorAPIKey || actor.APIKeyID != "key-1" || actor.MemberID != "" {
			t.Fatalf("unexpected actor: %+v", actor)
		}
	})
}

func TestPlatformAdminActor(t *testing.T) {
	actor := PlatformAdminActor("admin-1")
	if actor.Type != ActorPlatformAdmin || actor.PlatformAdminID != "admin-1" {
		t.Fatalf("unexpected actor: %+v", actor)
	}
	if actor.MemberID != "" || actor.APIKeyID != "" {
		t.Fatalf("expected no other identity set: %+v", actor)
	}
}

func TestRecordRequiresIdentity(t *testing.T) {
	tests := []struct {
		name  string
		input RecordInput
	}{
		{"missing tenant", RecordInput{Kind: "k.a", Outcome: OutcomeSuccess, Actor: Actor{Type: ActorHuman}}},
		{"missing kind", RecordInput{TenantID: "t", Outcome: OutcomeSuccess, Actor: Actor{Type: ActorHuman}}},
		{"missing outcome", RecordInput{TenantID: "t", Kind: "k.a", Actor: Actor{Type: ActorHuman}}},
		{"missing actor type", RecordInput{TenantID: "t", Kind: "k.a", Outcome: OutcomeSuccess}},
		{"human without member", RecordInput{TenantID: "t", Kind: "k.a", Outcome: OutcomeSuccess, Actor: Actor{Type: ActorHuman}}},
		{"API key with member", RecordInput{TenantID: "t", Kind: "k.a", Outcome: OutcomeSuccess, Actor: Actor{Type: ActorAPIKey, APIKeyID: "key", MemberID: "member"}}},
		{"unknown actor", RecordInput{TenantID: "t", Kind: "k.a", Outcome: OutcomeSuccess, Actor: Actor{Type: "unknown"}}},
		{"unknown outcome", RecordInput{TenantID: "t", Kind: "k.a", Outcome: "unknown", Actor: Actor{Type: ActorSystem}}},
		{"subsecond deduplication", RecordInput{TenantID: "t", Kind: "k.a", Outcome: OutcomeSuccess, Actor: Actor{Type: ActorSystem}, DedupWindow: time.Millisecond}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := Record(context.Background(), nil, test.input); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
