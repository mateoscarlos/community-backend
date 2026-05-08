package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/community-app/community-backend/internal/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/rs/zerolog"
)

// Storage handles object storage operations (MinIO locally, S3 in dev/prod).
type Storage struct {
	client *minio.Client
	bucket string
	log    zerolog.Logger
}

// New connects to the configured storage backend and ensures the bucket exists.
func New(cfg *config.Config, log zerolog.Logger) (*Storage, error) {
	var (
		endpoint  string
		accessKey string
		secretKey string
		bucket    string
		region    string
		useSSL    bool
	)

	switch cfg.StorageDriver {
	case "s3":
		endpoint = cfg.S3Endpoint
		accessKey = cfg.AWSAccessKeyID
		secretKey = cfg.AWSSecretKey
		bucket = cfg.S3Bucket
		region = cfg.AWSRegion
		useSSL = true
	default: // minio
		endpoint = cfg.MinioEndpoint
		accessKey = cfg.MinioAccessKey
		secretKey = cfg.MinioSecretKey
		bucket = cfg.MinioBucket
		useSSL = false
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("init storage client: %w", err)
	}

	ctx := context.Background()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket %q: %w", bucket, err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create bucket %q: %w", bucket, err)
		}
		log.Info().Str("bucket", bucket).Msg("storage bucket created")
	}

	log.Info().Str("driver", cfg.StorageDriver).Str("bucket", bucket).Msg("storage connected")
	return &Storage{client: client, bucket: bucket, log: log}, nil
}

// Ping checks that the storage backend is reachable.
func (s *Storage) Ping(ctx context.Context) error {
	_, err := s.client.BucketExists(ctx, s.bucket)
	return err
}

// PresignedGetURL returns a time-limited URL to download an object.
func (s *Storage) PresignedGetURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := s.client.PresignedGetObject(ctx, s.bucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("presign get %q: %w", key, err)
	}
	return u.String(), nil
}

// PresignedPutURL returns a time-limited URL for a client to upload an object directly.
func (s *Storage) PresignedPutURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := s.client.PresignedPutObject(ctx, s.bucket, key, expiry)
	if err != nil {
		return "", fmt.Errorf("presign put %q: %w", key, err)
	}
	return u.String(), nil
}

// GetObject downloads an object's bytes server-side.
func (s *Storage) GetObject(ctx context.Context, key string) ([]byte, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %q: %w", key, err)
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", key, err)
	}
	return data, nil
}

// PutObject uploads bytes to storage server-side.
func (s *Storage) PutObject(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := s.client.PutObject(
		ctx, s.bucket, key,
		bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType},
	)
	if err != nil {
		return fmt.Errorf("put %q: %w", key, err)
	}
	return nil
}

// DeleteObject removes a single object. Idempotent: a missing object is not
// treated as an error so callers can safely re-run cleanups.
func (s *Storage) DeleteObject(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete %q: %w", key, err)
	}
	return nil
}

// DeleteObjects removes many objects in a single round-trip per batch (1000
// per request — the S3/R2 limit). Returns the keys that were successfully
// removed and the first error encountered, if any. Removes are idempotent on
// the storage side; missing keys do not generate errors.
func (s *Storage) DeleteObjects(ctx context.Context, keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	const maxBatch = 1000
	removed := make([]string, 0, len(keys))
	var firstErr error

	for start := 0; start < len(keys); start += maxBatch {
		end := start + maxBatch
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[start:end]

		objCh := make(chan minio.ObjectInfo, len(batch))
		go func() {
			defer close(objCh)
			for _, k := range batch {
				objCh <- minio.ObjectInfo{Key: k}
			}
		}()

		errCh := s.client.RemoveObjects(ctx, s.bucket, objCh, minio.RemoveObjectsOptions{})
		failed := make(map[string]struct{}, 4)
		for e := range errCh {
			if firstErr == nil && e.Err != nil {
				firstErr = fmt.Errorf("delete %q: %w", e.ObjectName, e.Err)
			}
			if e.Err != nil {
				failed[e.ObjectName] = struct{}{}
			}
		}
		for _, k := range batch {
			if _, bad := failed[k]; !bad {
				removed = append(removed, k)
			}
		}
	}
	return removed, firstErr
}