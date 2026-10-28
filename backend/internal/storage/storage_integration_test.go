package storage_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/storage"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestStorageCreatesAndChecksBucketAgainstMinIO(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MinIO integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	container, err := testcontainers.Run(ctx, "",
		testcontainers.WithDockerfile(testcontainers.FromDockerfile{
			Context: "testdata/minio",
		}),
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
	limited, err := client.PresignPost(
		ctx, "tenants/test/limited.webm", "video/webm", 4, 10*time.Minute,
	)
	if err != nil {
		t.Fatalf("presign limited POST upload: %v", err)
	}
	limitedResponse := postPresignedObject(t, ctx, limited, "clip.webm", "four", "")
	if limitedResponse.StatusCode < 200 || limitedResponse.StatusCode >= 300 {
		t.Fatalf("limited POST upload status = %d", limitedResponse.StatusCode)
	}
	_ = limitedResponse.Body.Close()
	oversized, err := client.PresignPost(
		ctx, "tenants/test/oversized.webm", "video/webm", 4, 10*time.Minute,
	)
	if err != nil {
		t.Fatalf("presign oversized POST upload: %v", err)
	}
	oversizedResponse := postPresignedObject(t, ctx, oversized, "clip.webm", "fives", "")
	if oversizedResponse.StatusCode >= 200 && oversizedResponse.StatusCode < 300 {
		t.Fatalf("oversized POST upload status = %d, want rejection", oversizedResponse.StatusCode)
	}
	_ = oversizedResponse.Body.Close()
	if _, err := client.Stat(ctx, "tenants/test/oversized.webm"); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("oversized object stat error = %v, want ErrObjectNotFound", err)
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
	listed, err := client.ListObjects(ctx, "tenants/test/cleanup/", "", 10)
	if err != nil {
		t.Fatalf("list cleanup objects: %v", err)
	}
	if len(listed) != 2 || listed[0].Key == "" || listed[0].LastModified.IsZero() {
		t.Fatalf("listed objects = %#v", listed)
	}
	if err := client.RemoveObject(ctx, "tenants/test/attachment.webm"); err != nil {
		t.Fatalf("remove uploaded object: %v", err)
	}
	if err := client.RemoveObject(ctx, "tenants/test/limited.webm"); err != nil {
		t.Fatalf("remove limited upload: %v", err)
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

func postPresignedObject(
	t *testing.T,
	ctx context.Context,
	upload storage.PresignedPost,
	filename string,
	body string,
	origin string,
) *http.Response {
	t.Helper()
	var encoded bytes.Buffer
	writer := multipart.NewWriter(&encoded)
	for name, value := range upload.Fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write POST field %s: %v", name, err)
		}
	}
	file, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create POST file field: %v", err)
	}
	if _, err := io.WriteString(file, body); err != nil {
		t.Fatalf("write POST object: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close POST upload: %v", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, upload.URL, &encoded)
	if err != nil {
		t.Fatalf("create POST upload request: %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("perform POST upload: %v", err)
	}
	return response
}
