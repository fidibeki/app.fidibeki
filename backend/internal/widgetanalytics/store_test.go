package widgetanalytics

import (
	"context"
	"strings"
	"testing"
)

func TestStoreRejectsMixedProjectBatchBeforeDatabaseAccess(t *testing.T) {
	t.Parallel()
	events := []EventValues{
		{TenantID: "tenant-one", ProjectID: "project-one"},
		{TenantID: "tenant-one", ProjectID: "project-two"},
	}
	_, err := (Store{}).InsertEvents(context.Background(), nil, events)
	if err == nil || !strings.Contains(err.Error(), "one tenant and project") {
		t.Fatalf("InsertEvents() error = %v", err)
	}
}
