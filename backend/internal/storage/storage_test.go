package storage_test

import (
	"context"
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
