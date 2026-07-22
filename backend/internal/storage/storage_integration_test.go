package storage_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/storage"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestStorageCreatesAndChecksBucketAgainstMinIO(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MinIO integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, err := testcontainers.Run(ctx, "minio/minio:RELEASE.2025-09-07T16-13-09Z",
		testcontainers.WithExposedPorts("9000/tcp"),
		testcontainers.WithEnv(map[string]string{
			"MINIO_ROOT_USER":     "fidibeki-test",
			"MINIO_ROOT_PASSWORD": "fidibeki-test-secret",
		}),
		testcontainers.WithCmd("server", "/data"),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/minio/health/live").WithPort("9000/tcp").WithStartupTimeout(time.Minute),
		),
	)
	if err != nil {
		t.Fatalf("start MinIO container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate MinIO container: %v", err)
		}
	})
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("resolve MinIO host: %v", err)
	}
	port, err := container.MappedPort(ctx, "9000/tcp")
	if err != nil {
		t.Fatalf("resolve MinIO port: %v", err)
	}

	client, err := storage.Open(storage.Config{
		Endpoint: fmt.Sprintf("http://%s:%s", host, port.Port()),
		Region:   "us-east-1", Bucket: "fidibeki-test",
		AccessKey: "fidibeki-test", SecretKey: "fidibeki-test-secret",
	})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	if err := client.EnsureBucket(ctx, "us-east-1"); err != nil {
		t.Fatalf("ensure bucket: %v", err)
	}
	if err := client.Check(ctx); err != nil {
		t.Fatalf("storage readiness: %v", err)
	}
	presigned, err := client.PresignPut(ctx, "tenants/test/attachment.webm", 10*time.Minute)
	if err != nil {
		t.Fatalf("presign upload: %v", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, presigned, strings.NewReader("audio-data"))
	if err != nil {
		t.Fatalf("create upload request: %v", err)
	}
	request.Header.Set("Content-Type", "audio/webm")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("upload through presigned URL: %v", err)
	}
	t.Cleanup(func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close upload response: %v", err)
		}
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("upload status = %d", response.StatusCode)
	}
	info, err := client.Stat(ctx, "tenants/test/attachment.webm")
	if err != nil {
		t.Fatalf("stat uploaded object: %v", err)
	}
	if info.Size != int64(len("audio-data")) || info.ContentType != "audio/webm" {
		t.Errorf("object info = %#v", info)
	}
	reader, err := client.OpenObject(ctx, "tenants/test/attachment.webm")
	if err != nil {
		t.Fatalf("open uploaded object: %v", err)
	}
	contents, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil || closeErr != nil || string(contents) != "audio-data" {
		t.Errorf("read uploaded object = %q, read error = %v, close error = %v", contents, err, closeErr)
	}
	for key, body := range map[string]string{
		"tenants/test/cleanup/first.txt":  "first",
		"tenants/test/cleanup/second.txt": "second",
	} {
		if err := client.PutObject(ctx, key, strings.NewReader(body), int64(len(body)), "text/plain"); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}
	if err := client.RemoveObject(ctx, "tenants/test/attachment.webm"); err != nil {
		t.Fatalf("remove uploaded object: %v", err)
	}
	if _, err := client.Stat(ctx, "tenants/test/attachment.webm"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("removed upload stat error = %v, want ErrObjectNotFound", err)
	}
	if err := client.RemovePrefix(ctx, "tenants/test/cleanup/"); err != nil {
		t.Fatalf("remove prefix: %v", err)
	}
	for _, key := range []string{
		"tenants/test/cleanup/first.txt", "tenants/test/cleanup/second.txt",
	} {
		if _, err := client.Stat(ctx, key); err == nil {
			t.Errorf("prefix object %s still exists", key)
		}
	}
	if err := client.RemovePrefix(ctx, "tenants/test/missing/"); err != nil {
		t.Fatalf("remove empty prefix: %v", err)
	}
}
