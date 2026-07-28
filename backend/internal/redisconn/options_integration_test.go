package redisconn_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/redisconn"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestNamedACLUsersWorkForCacheAndQueuesOverPrivateCATLS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Redis TLS integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	caCertificate, serverCertificate, serverKey := testRedisTLSCertificates(t)
	const acl = `user default off
user fidibeki_app on >app-secret ~* &* +@all -@admin -@dangerous
user fidibeki_worker on >worker-secret ~* &* +@all -@admin -@dangerous
`

	container, err := testcontainers.Run(ctx, "redis:8.2.1",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithFiles(
			testcontainers.ContainerFile{
				Reader: strings.NewReader(caCertificate), ContainerFilePath: "/tls/ca.crt", FileMode: 0o444,
			},
			testcontainers.ContainerFile{
				Reader: strings.NewReader(serverCertificate), ContainerFilePath: "/tls/server.crt", FileMode: 0o444,
			},
			testcontainers.ContainerFile{
				Reader: strings.NewReader(serverKey), ContainerFilePath: "/tls/server.key", FileMode: 0o444,
			},
			testcontainers.ContainerFile{
				Reader: strings.NewReader(acl), ContainerFilePath: "/tls/users.acl", FileMode: 0o444,
			},
		),
		testcontainers.WithCmd(
			"redis-server",
			"--port", "0",
			"--tls-port", "6379",
			"--tls-cert-file", "/tls/server.crt",
			"--tls-key-file", "/tls/server.key",
			"--tls-ca-cert-file", "/tls/ca.crt",
			"--tls-auth-clients", "no",
			"--aclfile", "/tls/users.acl",
		),
		testcontainers.WithWaitStrategy(
			wait.ForLog("Ready to accept connections").WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start TLS Redis: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("resolve Redis host: %v", err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatalf("resolve Redis port: %v", err)
	}
	appRedisURL := fmt.Sprintf("rediss://fidibeki_app:app-secret@%s:%s/0", host, port.Port())
	workerRedisURL := fmt.Sprintf(
		"rediss://fidibeki_worker:worker-secret@%s:%s/0", host, port.Port(),
	)

	clientOptions, err := redisconn.ClientOptions(appRedisURL, caCertificate)
	if err != nil {
		t.Fatalf("ClientOptions() error = %v", err)
	}
	cacheClient := redis.NewClient(clientOptions)
	t.Cleanup(func() { _ = cacheClient.Close() })
	if err := cacheClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("TLS Redis PING error = %v", err)
	}
	if err := cacheClient.Do(ctx, "CONFIG", "GET", "*").Err(); err == nil ||
		!strings.Contains(err.Error(), "NOPERM") {
		t.Fatalf("administrative command error = %v, want NOPERM", err)
	}

	queueOptions, err := redisconn.AsynqOptions(appRedisURL, caCertificate)
	if err != nil {
		t.Fatalf("AsynqOptions() error = %v", err)
	}
	queueClient := asynq.NewClient(queueOptions)
	t.Cleanup(func() { _ = queueClient.Close() })

	workerOptions, err := redisconn.AsynqOptions(workerRedisURL, caCertificate)
	if err != nil {
		t.Fatalf("worker AsynqOptions() error = %v", err)
	}
	processed := make(chan struct{}, 1)
	mux := asynq.NewServeMux()
	mux.HandleFunc("redisconn:tls-test", func(context.Context, *asynq.Task) error {
		processed <- struct{}{}
		return nil
	})
	server := asynq.NewServer(workerOptions, asynq.Config{
		Concurrency: 1,
		Queues:      map[string]int{"tls-test": 1},
	})
	if err := server.Start(mux); err != nil {
		t.Fatalf("start ACL worker: %v", err)
	}
	t.Cleanup(server.Shutdown)

	if _, err := queueClient.EnqueueContext(
		ctx, asynq.NewTask("redisconn:tls-test", nil), asynq.Queue("tls-test"),
	); err != nil {
		t.Fatalf("TLS Asynq enqueue error = %v", err)
	}
	select {
	case <-processed:
	case <-ctx.Done():
		t.Fatalf("ACL worker did not process task: %v", ctx.Err())
	}

	invalidOptions, err := redisconn.ClientOptions(
		fmt.Sprintf("rediss://fidibeki_app:wrong-secret@%s:%s/0", host, port.Port()),
		caCertificate,
	)
	if err != nil {
		t.Fatalf("invalid credential options: %v", err)
	}
	invalidClient := redis.NewClient(invalidOptions)
	t.Cleanup(func() { _ = invalidClient.Close() })
	if err := invalidClient.Ping(ctx).Err(); err == nil {
		t.Fatal("Redis accepted an invalid ACL password")
	}
}

func testRedisTLSCertificates(t *testing.T) (string, string, string) {
	t.Helper()
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Fidibeki Redis Integration CA"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(
		rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey,
	)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(
		rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey,
	)
	if err != nil {
		t.Fatalf("create server certificate: %v", err)
	}
	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		t.Fatalf("marshal server key: %v", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})),
		string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: serverKeyDER}))
}
