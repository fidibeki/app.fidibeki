package storage_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/storage"
)

func TestOpenValidatesAndConfiguresStorage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		endpoint string
		wantErr  bool
	}{
		{name: "HTTP endpoint", endpoint: "http://localhost:9000"},
		{name: "HTTPS endpoint", endpoint: "https://s3.example.test"},
		{name: "missing scheme", endpoint: "localhost:9000", wantErr: true},
		{name: "unsupported scheme", endpoint: "ftp://localhost:9000", wantErr: true},
		{name: "endpoint path", endpoint: "https://s3.example.test/path", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := storage.Open(storage.Config{
				Endpoint: tt.endpoint, Region: "us-east-1", Bucket: "fidibeki",
				AccessKey: "key", SecretKey: "secret",
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Open() error = %v, wantErr %t", err, tt.wantErr)
			}
			if err == nil && (got.Client() == nil || got.Bucket() != "fidibeki") {
				t.Errorf("storage = %#v", got)
			}
		})
	}
}

func TestPresignUsesPublicEndpoint(t *testing.T) {
	t.Parallel()
	client, err := storage.Open(storage.Config{
		Endpoint: "http://minio.internal:9000", PublicEndpoint: "https://uploads.example.test",
		Region: "us-east-1", Bucket: "fidibeki", AccessKey: "key", SecretKey: "secret",
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	url, err := client.PresignPut(context.Background(), "tenant/object.webm", 10*time.Minute)
	if err != nil {
		t.Fatalf("PresignPut() error = %v", err)
	}
	if !strings.HasPrefix(url, "https://uploads.example.test/fidibeki/tenant/object.webm?") {
		t.Errorf("presigned URL = %q", url)
	}
}

func TestPresignUsesVirtualHostedBucketEndpoint(t *testing.T) {
	t.Parallel()
	client, err := storage.Open(storage.Config{
		Endpoint: "https://storage.railway.app", Region: "auto", Bucket: "fidibeki-staging-abc123",
		AccessKey: "key", SecretKey: "secret", VirtualHostStyle: true,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	presignedURL, err := client.PresignPut(
		context.Background(), "tenant/object.webm", 10*time.Minute,
	)
	if err != nil {
		t.Fatalf("PresignPut() error = %v", err)
	}
	if !strings.HasPrefix(
		presignedURL,
		"https://fidibeki-staging-abc123.storage.railway.app/tenant/object.webm?",
	) {
		t.Errorf("presigned URL = %q", presignedURL)
	}
}

func TestStorageOperationsWrapProviderFailures(t *testing.T) {
	t.Parallel()

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "provider rejected request", http.StatusBadRequest)
	}))
	t.Cleanup(provider.Close)
	client, err := storage.Open(storage.Config{
		Endpoint: provider.URL, Region: "us-east-1", Bucket: "fidibeki",
		AccessKey: "key", SecretKey: "secret",
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	tests := []struct {
		name      string
		operation func() error
		wantError string
	}{
		{name: "put", operation: func() error {
			return client.PutObject(context.Background(), "object", strings.NewReader("body"), 4, "text/plain")
		}, wantError: "put S3 object"},
		{name: "remove", operation: func() error {
			return client.RemoveObject(context.Background(), "object")
		}, wantError: "remove S3 object"},
		{name: "remove prefix", operation: func() error {
			return client.RemovePrefix(context.Background(), "prefix/")
		}, wantError: "list S3 prefix"},
		{name: "check", operation: func() error {
			return client.Check(context.Background())
		}, wantError: "check S3 bucket"},
		{name: "ensure bucket", operation: func() error {
			return client.EnsureBucket(context.Background(), "us-east-1")
		}, wantError: "check S3 bucket before create"},
		{name: "open", operation: func() error {
			_, err := client.OpenObject(context.Background(), "object")
			return err
		}, wantError: "stat opened S3 object"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.operation(); err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("operation error = %v, want %q", err, test.wantError)
			}
		})
	}
}
