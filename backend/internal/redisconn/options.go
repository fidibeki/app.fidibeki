package redisconn

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"strings"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

func ClientOptions(redisURL string, caCertificates ...string) (*redis.Options, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	options.TLSConfig, err = tlsConfig(options.TLSConfig, first(caCertificates))
	if err != nil {
		return nil, err
	}
	return options, nil
}

func AsynqOptions(redisURL string, caCertificates ...string) (asynq.RedisConnOpt, error) {
	options, err := ClientOptions(redisURL, caCertificates...)
	if err != nil {
		return nil, err
	}
	return asynq.RedisClientOpt{
		Network:      options.Network,
		Addr:         options.Addr,
		Username:     options.Username,
		Password:     options.Password,
		DB:           options.DB,
		DialTimeout:  options.DialTimeout,
		ReadTimeout:  options.ReadTimeout,
		WriteTimeout: options.WriteTimeout,
		PoolSize:     options.PoolSize,
		TLSConfig:    options.TLSConfig,
	}, nil
}

func tlsConfig(current *tls.Config, caCertificate string) (*tls.Config, error) {
	if current == nil {
		if caCertificate != "" {
			return nil, errors.New("custom Redis CA certificate requires TLS")
		}
		return nil, nil
	}
	config := current.Clone()
	if config.MinVersion < tls.VersionTLS12 {
		config.MinVersion = tls.VersionTLS12
	}
	if caCertificate == "" {
		return config, nil
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(caCertificate)) {
		return nil, errors.New("redis CA certificate must contain a valid PEM certificate")
	}
	config.RootCAs = roots
	return config, nil
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}
