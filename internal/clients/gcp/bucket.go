package gcp

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
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
	UploadFile(dbc dbctx.Context, category BucketCategory, key string, file io.Reader) error
	DeleteFile(dbc dbctx.Context, category BucketCategory, key string) error
	ReplaceFile(dbc dbctx.Context, category BucketCategory, key string, newFile io.Reader) error
	DownloadFile(ctx context.Context, category BucketCategory, key string) (io.ReadCloser, error)
	CopyObject(ctx context.Context, category BucketCategory, srcKey, dstKey string) error
	ListKeys(ctx context.Context, category BucketCategory, prefix string) ([]string, error)
	DeletePrefix(ctx context.Context, category BucketCategory, prefix string) error
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

	ctx := context.Background()
	opts := ClientOptionsFromEnv()
	opts = append(opts, option.WithScopes(storage.ScopeReadWrite))
	stClient, err := storage.NewClient(ctx, opts...)
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

func (bs *bucketService) UploadFile(dbc dbctx.Context, category BucketCategory, key string, file io.Reader) error {
	cfg, err := bs.getBucketConfig(category)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(dbc.Ctx, 2*time.Minute)
	defer cancel()

	w := bs.storageClient.Bucket(cfg.name).Object(key).NewWriter(ctx)
	if ct := contentTypeForKey(key); ct != "" {
		w.ContentType = ct
	}
	if _, err := io.Copy(w, file); err != nil {
		_ = w.Close()
		return fmt.Errorf("failed to write data to GCS: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to close GCS writer: %w", err)
	}
	return nil
}

func contentTypeForKey(key string) string {
	s := strings.ToLower(strings.TrimSpace(key))
	if s == "" {
		return ""
	}
	// Strip query string (defensive; keys typically won't have this).
	if i := strings.Index(s, "?"); i >= 0 {
		s = s[:i]
	}
	switch {
	case strings.HasSuffix(s, ".png"):
		return "image/png"
	case strings.HasSuffix(s, ".jpg"), strings.HasSuffix(s, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(s, ".webp"):
		return "image/webp"
	case strings.HasSuffix(s, ".gif"):
		return "image/gif"
	case strings.HasSuffix(s, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(s, ".mp4"), strings.HasSuffix(s, ".m4v"):
		return "video/mp4"
	case strings.HasSuffix(s, ".webm"):
		return "video/webm"
	case strings.HasSuffix(s, ".mov"):
		return "video/quicktime"
	case strings.HasSuffix(s, ".pdf"):
		return "application/pdf"
	case strings.HasSuffix(s, ".json"):
		return "application/json"
	default:
		return ""
	}
}

func (bs *bucketService) DeleteFile(dbc dbctx.Context, category BucketCategory, key string) error {
	cfg, err := bs.getBucketConfig(category)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(dbc.Ctx, 30*time.Second)
	defer cancel()
	o := bs.storageClient.Bucket(cfg.name).Object(key)
	if err := o.Delete(ctx); err != nil {
		return fmt.Errorf("failed to delete GCS object %q in bucket %q: %w", key, cfg.name, err)
	}
	return nil
}

func (bs *bucketService) ReplaceFile(dbc dbctx.Context, category BucketCategory, key string, newFile io.Reader) error {
	if err := bs.DeleteFile(dbc, category, key); err != nil {
		return fmt.Errorf("failed deleting old file: %w", err)
	}
	if err := bs.UploadFile(dbc, category, key, newFile); err != nil {
		return fmt.Errorf("failed uploading new file: %w", err)
	}
	return nil
}

func (bs *bucketService) CopyObject(ctx context.Context, category BucketCategory, srcKey, dstKey string) error {
	cfg, err := bs.getBucketConfig(category)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	src := bs.storageClient.Bucket(cfg.name).Object(srcKey)
	dst := bs.storageClient.Bucket(cfg.name).Object(dstKey)
	_, err = dst.CopierFrom(src).Run(ctx)
	if err != nil {
		return fmt.Errorf("copy %s->%s: %w", srcKey, dstKey, err)
	}
	return nil
}

func (bs *bucketService) ListKeys(ctx context.Context, category BucketCategory, prefix string) ([]string, error) {
	cfg, err := bs.getBucketConfig(category)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	it := bs.storageClient.Bucket(cfg.name).Objects(ctx, &storage.Query{Prefix: prefix})
	out := []string{}
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		out = append(out, attrs.Name)
	}
	return out, nil
}

func (bs *bucketService) DeletePrefix(ctx context.Context, category BucketCategory, prefix string) error {
	keys, err := bs.ListKeys(ctx, category, prefix)
	if err != nil {
		return err
	}
	for _, k := range keys {
		_ = bs.DeleteFile(dbctx.Context{Ctx: ctx}, category, k)
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
