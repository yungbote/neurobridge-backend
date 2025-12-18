package gcp

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type BucketCategory string

const (
	BucketCategoryAvatar   BucketCategory = "avatar"
	BucketCategoryMaterial BucketCategory = "material"
)

type bucketConfig struct {
	name      string
	cdnDomain string
}

type BucketService interface {
	UploadFile(ctx context.Context, tx *gorm.DB, category BucketCategory, key string, file io.Reader) error
	DeleteFile(ctx context.Context, tx *gorm.DB, category BucketCategory, key string) error
	ReplaceFile(ctx context.Context, tx *gorm.DB, category BucketCategory, key string, newFile io.Reader) error
	DownloadFile(ctx context.Context, category BucketCategory, key string) (io.ReadCloser, error)
	GetPublicURL(category BucketCategory, key string) string
}

type bucketService struct {
	log            *logger.Logger
	storageClient  *storage.Client
	avatarBucket   bucketConfig
	materialBucket bucketConfig
}

func NewBucketService(log *logger.Logger) (BucketService, error) {
	serviceLog := log.With("service", "BucketService")

	avatarBucketName := os.Getenv("AVATAR_GCS_BUCKET_NAME")
	materialBucketName := os.Getenv("MATERIAL_GCS_BUCKET_NAME")
	if avatarBucketName == "" {
		return nil, fmt.Errorf("missing env var AVATAR_GCS_BUCKET_NAME")
	}
	if materialBucketName == "" {
		return nil, fmt.Errorf("missing env var MATERIAL_GCS_BUCKET_NAME")
	}

	avatarCDN := os.Getenv("AVATAR_CDN_DOMAIN")
	materialCDN := os.Getenv("MATERIAL_CDN_DOMAIN")

	saPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS_JSON")
	if saPath == "" {
		serviceLog.Warn("The storage client may rely on other ADC or fail because GOOGLE_APPLICATION_CREDENTIALS_JSON env var missing...")
	}

	ctx := context.Background()
	var stClient *storage.Client
	var err error
	if saPath != "" {
		stClient, err = storage.NewClient(ctx, option.WithCredentialsFile(saPath), option.WithScopes(storage.ScopeReadWrite))
	} else {
		stClient, err = storage.NewClient(ctx, option.WithScopes(storage.ScopeReadWrite))
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create storage client: %w", err)
	}

	return &bucketService{
		log:           serviceLog,
		storageClient: stClient,
		avatarBucket: bucketConfig{
			name:      avatarBucketName,
			cdnDomain: avatarCDN,
		},
		materialBucket: bucketConfig{
			name:      materialBucketName,
			cdnDomain: materialCDN,
		},
	}, nil
}

func (bs *bucketService) getBucketConfig(category BucketCategory) (bucketConfig, error) {
	switch category {
	case BucketCategoryAvatar:
		return bs.avatarBucket, nil
	case BucketCategoryMaterial:
		return bs.materialBucket, nil
	default:
		return bucketConfig{}, fmt.Errorf("unknown bucket category: %s", category)
	}
}

func (bs *bucketService) UploadFile(ctx context.Context, tx *gorm.DB, category BucketCategory, key string, file io.Reader) error {
	cfg, err := bs.getBucketConfig(category)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	w := bs.storageClient.Bucket(cfg.name).Object(key).NewWriter(ctx)
	if _, err := io.Copy(w, file); err != nil {
		_ = w.Close()
		return fmt.Errorf("failed to write data to GCS: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to close GCS writer: %w", err)
	}
	return nil
}

func (bs *bucketService) DeleteFile(ctx context.Context, tx *gorm.DB, category BucketCategory, key string) error {
	cfg, err := bs.getBucketConfig(category)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	o := bs.storageClient.Bucket(cfg.name).Object(key)
	if err := o.Delete(ctx); err != nil {
		return fmt.Errorf("failed to delete GCS object %q in bucket %q: %w", key, cfg.name, err)
	}
	return nil
}

func (bs *bucketService) ReplaceFile(ctx context.Context, tx *gorm.DB, category BucketCategory, key string, newFile io.Reader) error {
	if err := bs.DeleteFile(ctx, tx, category, key); err != nil {
		return fmt.Errorf("failed deleting old file: %w", err)
	}
	if err := bs.UploadFile(ctx, tx, category, key, newFile); err != nil {
		return fmt.Errorf("failed uploading new file: %w", err)
	}
	return nil
}

func (bs *bucketService) GetPublicURL(category BucketCategory, key string) string {
	cfg, err := bs.getBucketConfig(category)
	if err != nil {
		return key
	}
	if cfg.cdnDomain != "" {
		return fmt.Sprintf("https://%s/%s", cfg.cdnDomain, key)
	}
	return fmt.Sprintf("https://storage.googleapis.com/%s/%s", cfg.name, key)
}

// IMPORTANT FIX:
// Do NOT `defer cancel()` before returning the reader.
// If you do, the context is canceled immediately and callers read 0 bytes.
// We attach the cancel to the reader's Close().
type readCloserWithCancel struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *readCloserWithCancel) Close() error {
	err := r.ReadCloser.Close()
	if r.cancel != nil {
		r.cancel()
	}
	return err
}

func (bs *bucketService) DownloadFile(ctx context.Context, category BucketCategory, key string) (io.ReadCloser, error) {
	cfg, err := bs.getBucketConfig(category)
	if err != nil {
		return nil, err
	}

	// Create a context that stays alive for the life of the reader.
	// Cancel only after the reader is closed.
	ctx2, cancel := context.WithTimeout(ctx, 2*time.Minute)

	r, err := bs.storageClient.Bucket(cfg.name).Object(key).NewReader(ctx2)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to open GCS reader: %w", err)
	}

	return &readCloserWithCancel{ReadCloser: r, cancel: cancel}, nil
}
