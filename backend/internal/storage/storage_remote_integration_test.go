package storage_test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/storage"
)

func TestRemoteStorageAllowsBrowserPresignedPut(t *testing.T) {
	endpoint := os.Getenv("S3_ENDPOINT")
	bucket := os.Getenv("S3_BUCKET")
	accessKey := os.Getenv("S3_ACCESS_KEY")
	secretKey := os.Getenv("S3_SECRET_KEY")
	origin := os.Getenv("STORAGE_TEST_ORIGIN")
	if endpoint == "" || bucket == "" || accessKey == "" || secretKey == "" || origin == "" {
		t.Skip("remote storage credentials and STORAGE_TEST_ORIGIN are required")
	}

	client, err := storage.Open(storage.Config{
		Endpoint: endpoint, PublicEndpoint: os.Getenv("S3_PUBLIC_ENDPOINT"),
		Region: os.Getenv("S3_REGION"), Bucket: bucket,
		AccessKey: accessKey, SecretKey: secretKey,
		VirtualHostStyle: os.Getenv("S3_VIRTUAL_HOST_STYLE") == "true",
	})
	if err != nil {
		t.Fatalf("open remote storage: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	key := "diagnostics/browser-cors-" + time.Now().UTC().Format("20060102T150405.000000000") + ".txt"
	presigned, err := client.PresignPut(ctx, key, time.Minute)
	if err != nil {
		t.Fatalf("presign remote upload: %v", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodOptions, presigned, nil)
	if err != nil {
		t.Fatalf("create preflight request: %v", err)
	}
	request.Header.Set("Origin", origin)
	request.Header.Set("Access-Control-Request-Method", http.MethodPut)
	request.Header.Set("Access-Control-Request-Headers", "content-type")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("perform preflight request: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("preflight status = %d, want 2xx", response.StatusCode)
	}
	if got := response.Header.Get("Access-Control-Allow-Origin"); got != origin && got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q or *", got, origin)
	}
	if got := response.Header.Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("Access-Control-Allow-Methods is empty")
	}
	if got := response.Header.Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("Access-Control-Allow-Headers is empty")
	}

	upload, err := http.NewRequestWithContext(ctx, http.MethodPut, presigned, strings.NewReader("cors-ok"))
	if err != nil {
		t.Fatalf("create upload request: %v", err)
	}
	upload.Header.Set("Origin", origin)
	upload.Header.Set("Content-Type", "text/plain")
	uploadResponse, err := http.DefaultClient.Do(upload)
	if err != nil {
		t.Fatalf("perform browser-style upload: %v", err)
	}
	defer uploadResponse.Body.Close()
	t.Cleanup(func() {
		if err := client.RemoveObject(context.Background(), key); err != nil {
			t.Errorf("remove diagnostic object: %v", err)
		}
	})

	if uploadResponse.StatusCode < 200 || uploadResponse.StatusCode >= 300 {
		t.Fatalf("upload status = %d, want 2xx", uploadResponse.StatusCode)
	}
	if got := uploadResponse.Header.Get("Access-Control-Allow-Origin"); got != origin && got != "*" {
		t.Errorf("upload Access-Control-Allow-Origin = %q, want %q or *", got, origin)
	}
}
