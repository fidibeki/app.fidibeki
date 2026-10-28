package testimonial

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/storage"
)

func TestMediaCleanupRemovesAbandonedTerminalAndOrphanedSources(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	abandoned := MediaSourceCleanup{
		TenantID: "tenant", MediaID: "abandoned", StorageKey: "testimonials/tenant/abandoned/source",
		DeleteRow: true,
	}
	terminal := MediaSourceCleanup{
		TenantID: "tenant", MediaID: "ready", StorageKey: "testimonials/tenant/ready/source",
	}
	orphanKey := "testimonials/tenant/orphan/source"
	activeKey := "testimonials/tenant/active/source"
	store := &mediaCleanupStoreStub{
		abandoned: []MediaSourceCleanup{abandoned},
		terminal:  []MediaSourceCleanup{terminal},
		active:    map[string]bool{activeKey: true},
	}
	objects := &mediaCleanupStorageStub{objects: []storage.StoredObject{
		{Key: orphanKey, LastModified: now.Add(-videoUploadTTL - 10*time.Minute)},
		{Key: activeKey, LastModified: now.Add(-videoUploadTTL - 10*time.Minute)},
		{Key: "testimonials/tenant/recent/source", LastModified: now.Add(-time.Minute)},
		{Key: "testimonials/tenant/ready/video.mp4", LastModified: now.Add(-24 * time.Hour)},
	}}
	maintainer := MediaCleanupMaintainer{
		Store: store, Storage: objects, Now: func() time.Time { return now },
	}

	if err := maintainer.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	for _, key := range []string{abandoned.StorageKey, terminal.StorageKey, orphanKey} {
		if !slices.Contains(objects.removed, key) {
			t.Errorf("removed keys = %#v, missing %q", objects.removed, key)
		}
	}
	if slices.Contains(objects.removed, activeKey) ||
		slices.Contains(objects.removed, "testimonials/tenant/recent/source") ||
		slices.Contains(objects.removed, "testimonials/tenant/ready/video.mp4") {
		t.Fatalf("cleanup removed a live/recent/rendition object: %#v", objects.removed)
	}
	if !slices.Equal(store.completed, []MediaSourceCleanup{abandoned, terminal}) {
		t.Fatalf("completed cleanup = %#v", store.completed)
	}
	if store.abandonedBefore != now.Add(-videoUploadTTL-videoOrphanGrace) {
		t.Fatalf("abandoned cutoff = %v", store.abandonedBefore)
	}
}

func TestMediaCleanupRetriesDatabaseCompletionAfterStorageFailure(t *testing.T) {
	t.Parallel()

	item := MediaSourceCleanup{
		TenantID: "tenant", MediaID: "abandoned", StorageKey: "testimonials/tenant/abandoned/source",
		DeleteRow: true,
	}
	store := &mediaCleanupStoreStub{abandoned: []MediaSourceCleanup{item}}
	objects := &mediaCleanupStorageStub{removeErr: errors.New("storage unavailable")}
	maintainer := MediaCleanupMaintainer{Store: store, Storage: objects}

	if err := maintainer.ReconcileOnce(context.Background()); err == nil {
		t.Fatal("ReconcileOnce() error = nil")
	}
	if len(store.completed) != 0 {
		t.Fatalf("storage failure completed cleanup = %#v", store.completed)
	}
}

type mediaCleanupStoreStub struct {
	abandoned       []MediaSourceCleanup
	terminal        []MediaSourceCleanup
	completed       []MediaSourceCleanup
	active          map[string]bool
	abandonedBefore time.Time
}

func (s *mediaCleanupStoreStub) ClaimAbandonedVideoSources(
	_ context.Context,
	_ time.Time,
	before time.Time,
	_ int,
) ([]MediaSourceCleanup, error) {
	s.abandonedBefore = before
	return s.abandoned, nil
}

func (s *mediaCleanupStoreStub) ListTerminalVideoSources(
	context.Context,
	int,
) ([]MediaSourceCleanup, error) {
	return s.terminal, nil
}

func (s *mediaCleanupStoreStub) CompleteVideoSourceCleanup(
	_ context.Context,
	item MediaSourceCleanup,
	_ time.Time,
) error {
	s.completed = append(s.completed, item)
	return nil
}

func (s *mediaCleanupStoreStub) VideoSourceKeyActive(
	_ context.Context,
	key string,
) (bool, error) {
	return s.active[key], nil
}

type mediaCleanupStorageStub struct {
	objects   []storage.StoredObject
	removed   []string
	removeErr error
}

func (s *mediaCleanupStorageStub) ListObjects(
	context.Context,
	string,
	string,
	int,
) ([]storage.StoredObject, error) {
	return s.objects, nil
}

func (s *mediaCleanupStorageStub) RemoveObject(_ context.Context, key string) error {
	s.removed = append(s.removed, key)
	return s.removeErr
}
