package redisconn_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/redisconn"
	"github.com/hibiken/asynq"
)

func TestSecureOptionsUseOnlyTheConfiguredCA(t *testing.T) {
	t.Parallel()
	caCertificate := testCACertificate(t)

	clientOptions, err := redisconn.ClientOptions(
		"rediss://redis.fidibeki.test:6380/3", caCertificate,
	)
	if err != nil {
		t.Fatalf("ClientOptions() error = %v", err)
	}
	assertTLSConfig(t, clientOptions.TLSConfig, caCertificate)

	asynqOptions, err := redisconn.AsynqOptions(
		"rediss://redis.fidibeki.test:6380/3", caCertificate,
	)
	if err != nil {
		t.Fatalf("AsynqOptions() error = %v", err)
	}
	redisOptions, ok := asynqOptions.(asynq.RedisClientOpt)
	if !ok {
		t.Fatalf("AsynqOptions() type = %T", asynqOptions)
	}
	assertTLSConfig(t, redisOptions.TLSConfig, caCertificate)
}

func TestOptionsPropagateNamedRedisCredentials(t *testing.T) {
	t.Parallel()
	const redisURL = "rediss://fidibeki_app:redis-secret@redis.fidibeki.test:6380/3"

	clientOptions, err := redisconn.ClientOptions(redisURL)
	if err != nil {
		t.Fatalf("ClientOptions() error = %v", err)
	}
	if clientOptions.Username != "fidibeki_app" || clientOptions.Password != "redis-secret" {
		t.Errorf("go-redis credentials = %q/%q", clientOptions.Username, clientOptions.Password)
	}

	asynqOptions, err := redisconn.AsynqOptions(redisURL)
	if err != nil {
		t.Fatalf("AsynqOptions() error = %v", err)
	}
	redisOptions, ok := asynqOptions.(asynq.RedisClientOpt)
	if !ok {
		t.Fatalf("AsynqOptions() type = %T", asynqOptions)
	}
	if redisOptions.Username != "fidibeki_app" || redisOptions.Password != "redis-secret" {
		t.Errorf("Asynq credentials = %q/%q", redisOptions.Username, redisOptions.Password)
	}
}

func TestCustomCARequiresTLSAndValidPEM(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		redisURL string
		ca       string
	}{
		{name: "plaintext", redisURL: "redis://redis.fidibeki.test:6379/0", ca: testCACertificate(t)},
		{name: "invalid PEM", redisURL: "rediss://redis.fidibeki.test:6380/0", ca: "not a certificate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := redisconn.ClientOptions(test.redisURL, test.ca); err == nil {
				t.Error("ClientOptions() error = nil")
			}
			if _, err := redisconn.AsynqOptions(test.redisURL, test.ca); err == nil {
				t.Error("AsynqOptions() error = nil")
			}
		})
	}
}

func assertTLSConfig(t *testing.T, config *tls.Config, caCertificate string) {
	t.Helper()
	if config == nil || config.RootCAs == nil {
		t.Fatal("TLS config or configured roots are nil")
	}
	if config.ServerName != "redis.fidibeki.test" {
		t.Errorf("TLS server name = %q", config.ServerName)
	}
	if config.MinVersion < tls.VersionTLS12 {
		t.Errorf("TLS minimum version = %d, want TLS 1.2 or newer", config.MinVersion)
	}
	if config.InsecureSkipVerify {
		t.Error("TLS certificate verification is disabled")
	}
	if _, err := parseCertificate(t, caCertificate).Verify(x509.VerifyOptions{
		Roots:     config.RootCAs,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		t.Errorf("configured CA is not trusted: %v", err)
	}
	if _, err := parseCertificate(t, testCACertificate(t)).Verify(x509.VerifyOptions{
		Roots:     config.RootCAs,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err == nil {
		t.Error("unconfigured CA is trusted")
	}
}

func parseCertificate(t *testing.T, certificatePEM string) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode([]byte(certificatePEM))
	if block == nil {
		t.Fatal("decode certificate PEM")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return certificate
}

func testCACertificate(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Fidibeki Redis Test CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
