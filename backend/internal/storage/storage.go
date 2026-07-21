package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Endpoint         string
	PublicEndpoint   string
	Region           string
	Bucket           string
	AccessKey        string
	SecretKey        string
	VirtualHostStyle bool
}

type Storage struct {
	client        *minio.Client
	presignClient *minio.Client
	bucket        string
}

func Open(config Config) (*Storage, error) {
	endpoint, err := parseEndpoint(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse S3 endpoint: %w", err)
	}
	client, err := newClient(endpoint, config)
	if err != nil {
		return nil, err
	}
	publicEndpoint := endpoint
	if config.PublicEndpoint != "" {
		publicEndpoint, err = parseEndpoint(config.PublicEndpoint)
		if err != nil {
			return nil, fmt.Errorf("parse public S3 endpoint: %w", err)
		}
	}
	presignClient, err := newClient(publicEndpoint, config)
	if err != nil {
		return nil, err
	}
	return &Storage{client: client, presignClient: presignClient, bucket: config.Bucket}, nil
}

func newClient(endpoint *url.URL, config Config) (*minio.Client, error) {
	bucketLookup := minio.BucketLookupAuto
	if config.VirtualHostStyle {
		bucketLookup = minio.BucketLookupDNS
	}
	client, err := minio.New(endpoint.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
		Secure:       endpoint.Scheme == "https",
		Region:       config.Region,
		BucketLookup: bucketLookup,
	})
	if err != nil {
		return nil, fmt.Errorf("create S3 client for %s: %w", endpoint.Host, err)
	}
	return client, nil
}

func parseEndpoint(raw string) (*url.URL, error) {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, fmt.Errorf("invalid endpoint %q", raw)
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return nil, fmt.Errorf("endpoint scheme must be http or https")
	}
	if strings.Trim(endpoint.Path, "/") != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, fmt.Errorf("endpoint must not contain a path, query, or fragment")
	}
	return endpoint, nil
}

type ObjectInfo struct {
	Size        int64
	ContentType string
}

func (s *Storage) PresignPut(ctx context.Context, key string, ttl time.Duration) (string, error) {
	presigned, err := s.presignClient.PresignedPutObject(ctx, s.bucket, key, ttl)
	if err != nil {
		return "", fmt.Errorf("presign S3 upload: %w", err)
	}
	return presigned.String(), nil
}

func (s *Storage) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("stat S3 object: %w", err)
	}
	return ObjectInfo{Size: info.Size, ContentType: info.ContentType}, nil
}

func (s *Storage) OpenObject(ctx context.Context, key string) (io.ReadCloser, error) {
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("open S3 object: %w", err)
	}
	if _, err := object.Stat(); err != nil {
		_ = object.Close()
		return nil, fmt.Errorf("stat opened S3 object: %w", err)
	}
	return object, nil
}

func (s *Storage) PutObject(
	ctx context.Context,
	key string,
	reader io.Reader,
	size int64,
	contentType string,
) error {
	if _, err := s.client.PutObject(ctx, s.bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	}); err != nil {
		return fmt.Errorf("put S3 object: %w", err)
	}
	return nil
}

func (s *Storage) RemoveObject(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove S3 object: %w", err)
	}
	return nil
}

func (s *Storage) RemovePrefix(ctx context.Context, prefix string) error {
	for object := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix: prefix, Recursive: true,
	}) {
		if object.Err != nil {
			return fmt.Errorf("list S3 prefix %q: %w", prefix, object.Err)
		}
		if err := s.RemoveObject(ctx, object.Key); err != nil {
			return err
		}
	}
	return nil
}

func (s *Storage) Check(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check S3 bucket: %w", err)
	}
	if !exists {
		return fmt.Errorf("S3 bucket %q does not exist", s.bucket)
	}
	return nil
}

func (s *Storage) EnsureBucket(ctx context.Context, region string) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check S3 bucket before create: %w", err)
	}
	if exists {
		return nil
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: region}); err != nil {
		return fmt.Errorf("create S3 bucket: %w", err)
	}
	return nil
}

func (s *Storage) Client() *minio.Client {
	return s.client
}

func (s *Storage) Bucket() string {
	return s.bucket
}
