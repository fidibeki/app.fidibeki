package billing

import (
	"context"
	"errors"
	"testing"
)

func TestSeatReconcilerConvergesProviderQuantity(t *testing.T) {
	store := &seatSyncStoreStub{candidates: []SeatSyncCandidate{{
		TenantID:          "tenant-1",
		SubscriptionID:    "sub-1",
		StateKey:          "membership-v1",
		ConfirmedQuantity: 1,
		DesiredQuantity:   3,
	}}}
	provider := &providerStub{subscription: Subscription{
		ID: "sub-1", ItemID: "si-1", Quantity: 1,
	}}
	reconciler := SeatReconciler{Store: store, Provider: provider}

	if err := reconciler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	if provider.updatedItemID != "si-1" || provider.updatedQuantity != 3 ||
		provider.updateKey != "seat-sync:tenant-1:membership-v1:3" {
		t.Fatalf(
			"provider update = item %q quantity %d key %q",
			provider.updatedItemID,
			provider.updatedQuantity,
			provider.updateKey,
		)
	}
	if store.succeededQuantity != 3 || store.succeededSubscription != "sub-1" {
		t.Fatalf("recorded success = %#v", store)
	}
	if store.failureMessage != "" {
		t.Fatalf("failure message = %q", store.failureMessage)
	}
}

func TestSeatReconcilerAcceptsAlreadyConvergedRemoteState(t *testing.T) {
	store := &seatSyncStoreStub{candidates: []SeatSyncCandidate{{
		TenantID:          "tenant-1",
		SubscriptionID:    "sub-1",
		StateKey:          "membership-v1",
		ConfirmedQuantity: 1,
		DesiredQuantity:   3,
	}}}
	provider := &providerStub{subscription: Subscription{
		ID: "sub-1", ItemID: "si-1", Quantity: 3,
	}}

	if err := (SeatReconciler{Store: store, Provider: provider}).
		ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	if provider.updatedQuantity != 0 {
		t.Fatalf("provider quantity update = %d, want none", provider.updatedQuantity)
	}
	if store.succeededQuantity != 3 {
		t.Fatalf("recorded quantity = %d, want 3", store.succeededQuantity)
	}
}

func TestSeatReconcilerPersistsSafeFailureAndContinues(t *testing.T) {
	want := errors.New("Stripe unavailable")
	store := &seatSyncStoreStub{candidates: []SeatSyncCandidate{{
		TenantID: "tenant-1", SubscriptionID: "sub-1",
		StateKey: "membership-v1", DesiredQuantity: 2,
	}}}
	provider := &providerStub{err: want}

	err := (SeatReconciler{Store: store, Provider: provider}).
		ReconcileOnce(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("ReconcileOnce() error = %v, want %v", err, want)
	}
	if store.failureMessage != seatSyncFailureMessage {
		t.Fatalf("failure message = %q", store.failureMessage)
	}
	if store.succeededQuantity != 0 {
		t.Fatalf("recorded success = %d", store.succeededQuantity)
	}
}

func TestSeatReconcilerUsesAStateSpecificIdempotencyKey(t *testing.T) {
	t.Parallel()
	keys := make([]string, 0, 2)
	for _, stateKey := range []string{"membership-v1", "membership-v2"} {
		store := &seatSyncStoreStub{candidates: []SeatSyncCandidate{{
			TenantID: "tenant-1", SubscriptionID: "sub-1",
			StateKey: stateKey, DesiredQuantity: 3,
		}}}
		provider := &providerStub{subscription: Subscription{
			ID: "sub-1", ItemID: "si-1", Quantity: 1,
		}}
		if err := (SeatReconciler{Store: store, Provider: provider}).
			ReconcileOnce(context.Background()); err != nil {
			t.Fatalf("ReconcileOnce(%s) error = %v", stateKey, err)
		}
		keys = append(keys, provider.updateKey)
	}
	if keys[0] == keys[1] {
		t.Fatalf("idempotency keys = %q and %q, want distinct operations", keys[0], keys[1])
	}
}

func TestSeatReconcilerRequiresCollaborators(t *testing.T) {
	if err := (SeatReconciler{}).ReconcileOnce(context.Background()); err == nil {
		t.Fatal("ReconcileOnce() error = nil")
	}
}

type seatSyncStoreStub struct {
	candidates            []SeatSyncCandidate
	listErr               error
	successErr            error
	failureErr            error
	succeededSubscription string
	succeededQuantity     int
	failureMessage        string
}

func (store *seatSyncStoreStub) ListSeatSyncCandidates(
	context.Context,
	int,
) ([]SeatSyncCandidate, error) {
	return store.candidates, store.listErr
}

func (store *seatSyncStoreStub) RecordSeatSyncSuccess(
	_ context.Context,
	_ string,
	subscriptionID string,
	quantity int,
) error {
	store.succeededSubscription = subscriptionID
	store.succeededQuantity = quantity
	return store.successErr
}

func (store *seatSyncStoreStub) RecordSeatSyncFailure(
	_ context.Context,
	_ string,
	_ string,
	message string,
) error {
	store.failureMessage = message
	return store.failureErr
}
