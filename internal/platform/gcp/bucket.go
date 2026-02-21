package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
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
	OpenRangeReader(ctx context.Context, category BucketCategory, key string, offset, length int64) (io.ReadCloser, error)
	GetObjectAttrs(ctx context.Context, category BucketCategory, key string) (*ObjectAttrs, error)
	CopyObject(ctx context.Context, category BucketCategory, srcKey, dstKey string) error
	ListKeys(ctx context.Context, category BucketCategory, prefix string) ([]string, error)
	DeletePrefix(ctx context.Context, category BucketCategory, prefix string) error
	GetPublicURL(category BucketCategory, key string) string
}

type ObjectAttrs struct {
	Size        int64
	ContentType string
	Updated     time.Time
	ETag        string
}

type bucketService struct {
	log            *logger.Logger
	storageClient  *storage.Client
	storageMode    ObjectStorageMode
	emulatorHost   string
	avatarBucket   bucketConfig
	materialBucket bucketConfig
	publicBaseURL  string
}

func NewBucketService(log *logger.Logger) (BucketService, error) {
	storageCfg, err := ResolveObjectStorageConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("resolve object storage config: %w", err)
	}
	return NewBucketServiceWithConfig(log, storageCfg)
}

func NewBucketServiceWithConfig(log *logger.Logger, storageCfg ObjectStorageConfig) (BucketService, error) {
	if err := ValidateObjectStorageConfig(storageCfg); err != nil {
		return nil, fmt.Errorf("validate object storage config: %w", err)
	}
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
	publicBaseURL, publicBaseSource, err := resolveObjectStoragePublicBaseURL(storageCfg)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	stClient, err := newStorageClientForMode(ctx, storageCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage client: %w", err)
	}

	modeSource := storageCfg.ModeSource()
	serviceLog.Info(
		"Object storage initialized",
		"mode", storageCfg.Mode,
		"mode_source", modeSource,
		"emulator_host", storageCfg.EmulatorHost,
		"public_base_source", publicBaseSource,
		"public_base_url", publicBaseURL,
		"avatar_bucket", avatarBucketName,
		"material_bucket", materialBucketName,
	)

	return &bucketService{
		log:           serviceLog,
		storageClient: stClient,
		storageMode:   storageCfg.Mode,
		emulatorHost:  strings.TrimRight(strings.TrimSpace(storageCfg.EmulatorHost), "/"),
		avatarBucket: bucketConfig{
			name:      avatarBucketName,
			cdnDomain: avatarCDN,
		},
		materialBucket: bucketConfig{
			name:      materialBucketName,
			cdnDomain: materialCDN,
		},
		publicBaseURL: publicBaseURL,
	}, nil
}

func newStorageClientForMode(ctx context.Context, storageCfg ObjectStorageConfig) (*storage.Client, error) {
	switch storageCfg.Mode {
	case ObjectStorageModeGCS:
		opts := ClientOptionsFromEnv()
		opts = append(opts, option.WithScopes(storage.ScopeReadWrite))
		return storage.NewClient(ctx, opts...)
	case ObjectStorageModeGCSEmulator:
		endpoint := strings.TrimRight(strings.TrimSpace(storageCfg.EmulatorHost), "/")
		_ = os.Setenv("STORAGE_EMULATOR_HOST", endpoint)
		opts := []option.ClientOption{
			option.WithoutAuthentication(),
		}
		return storage.NewClient(ctx, opts...)
	default:
		return nil, &ObjectStorageConfigError{
			Code: ObjectStorageConfigErrorInvalidMode,
			Mode: string(storageCfg.Mode),
		}
	}
}

func resolveObjectStoragePublicBaseURL(storageCfg ObjectStorageConfig) (baseURL string, source string, err error) {
	raw := strings.TrimSpace(os.Getenv("OBJECT_STORAGE_PUBLIC_BASE_URL"))
	if raw != "" {
		parsed, parseErr := url.Parse(raw)
		if parseErr != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
			return "", "", fmt.Errorf(
				"invalid OBJECT_STORAGE_PUBLIC_BASE_URL=%q; expected absolute URL like http://localhost:4443",
				raw,
			)
		}
		return strings.TrimRight(raw, "/"), "object_storage_public_base_url", nil
	}

	if storageCfg.IsEmulatorMode() {
		return strings.TrimRight(strings.TrimSpace(storageCfg.EmulatorHost), "/"), "storage_emulator_host", nil
	}

	return "", "gcs_default", nil
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
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	if cfg.cdnDomain != "" {
		return fmt.Sprintf("https://%s/%s", cfg.cdnDomain, key)
	}
	if bs.storageMode == ObjectStorageModeGCSEmulator {
		if u := bs.publicEmulatorObjectMediaURL(cfg.name, key); u != "" {
			return u
		}
	}
	if bs.publicBaseURL != "" {
		return fmt.Sprintf("%s/%s/%s", bs.publicBaseURL, cfg.name, key)
	}
	return fmt.Sprintf("https://storage.googleapis.com/%s/%s", cfg.name, key)
}

func (bs *bucketService) publicEmulatorObjectMediaURL(bucket, key string) string {
	base := strings.TrimRight(strings.TrimSpace(bs.publicBaseURL), "/")
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(bs.emulatorHost), "/")
	}
	if base == "" {
		return ""
	}
	return fmt.Sprintf(
		"%s/storage/v1/b/%s/o/%s?alt=media",
		base,
		url.PathEscape(bucket),
		url.PathEscape(key),
	)
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

func (bs *bucketService) isEmulatorMode() bool {
	return bs != nil && IsEmulatorObjectStorageMode(bs.storageMode) && strings.TrimSpace(bs.emulatorHost) != ""
}

func (bs *bucketService) emulatorObjectMediaURL(bucket, key string) string {
	return fmt.Sprintf(
		"%s/storage/v1/b/%s/o/%s?alt=media",
		strings.TrimRight(strings.TrimSpace(bs.emulatorHost), "/"),
		url.PathEscape(bucket),
		url.PathEscape(key),
	)
}

func (bs *bucketService) emulatorObjectMetaURL(bucket, key string) string {
	return fmt.Sprintf(
		"%s/storage/v1/b/%s/o/%s",
		strings.TrimRight(strings.TrimSpace(bs.emulatorHost), "/"),
		url.PathEscape(bucket),
		url.PathEscape(key),
	)
}

func (bs *bucketService) DownloadFile(ctx context.Context, category BucketCategory, key string) (io.ReadCloser, error) {
	cfg, err := bs.getBucketConfig(category)
	if err != nil {
		return nil, err
	}
	if bs.isEmulatorMode() {
		ctx2, cancel := context.WithTimeout(ctx, 2*time.Minute)
		req, err := http.NewRequestWithContext(ctx2, http.MethodGet, bs.emulatorObjectMediaURL(cfg.name, key), nil)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed creating emulator download request: %w", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed emulator download request: %w", err)
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			cancel()
			return nil, fmt.Errorf("emulator download failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return &readCloserWithCancel{ReadCloser: resp.Body, cancel: cancel}, nil
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

func (bs *bucketService) OpenRangeReader(ctx context.Context, category BucketCategory, key string, offset, length int64) (io.ReadCloser, error) {
	cfg, err := bs.getBucketConfig(category)
	if err != nil {
		return nil, err
	}
	if bs.isEmulatorMode() {
		ctx2, cancel := context.WithTimeout(ctx, 2*time.Minute)
		req, err := http.NewRequestWithContext(ctx2, http.MethodGet, bs.emulatorObjectMediaURL(cfg.name, key), nil)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed creating emulator range request: %w", err)
		}
		if offset > 0 || length != 0 {
			var rangeHeader string
			if length > 0 {
				end := offset + length - 1
				rangeHeader = fmt.Sprintf("bytes=%d-%d", offset, end)
			} else {
				rangeHeader = fmt.Sprintf("bytes=%d-", offset)
			}
			req.Header.Set("Range", rangeHeader)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed emulator range request: %w", err)
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			cancel()
			return nil, fmt.Errorf("emulator range read failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return &readCloserWithCancel{ReadCloser: resp.Body, cancel: cancel}, nil
	}
	ctx2, cancel := context.WithTimeout(ctx, 2*time.Minute)
	r, err := bs.storageClient.Bucket(cfg.name).Object(key).NewRangeReader(ctx2, offset, length)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to open GCS range reader: %w", err)
	}
	return &readCloserWithCancel{ReadCloser: r, cancel: cancel}, nil
}

func (bs *bucketService) GetObjectAttrs(ctx context.Context, category BucketCategory, key string) (*ObjectAttrs, error) {
	cfg, err := bs.getBucketConfig(category)
	if err != nil {
		return nil, err
	}
	if bs.isEmulatorMode() {
		ctx2, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx2, http.MethodGet, bs.emulatorObjectMetaURL(cfg.name, key), nil)
		if err != nil {
			return nil, fmt.Errorf("failed creating emulator attrs request: %w", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed emulator attrs request: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			return nil, fmt.Errorf("emulator attrs failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var payload struct {
			Size        string `json:"size"`
			ContentType string `json:"contentType"`
			Updated     string `json:"updated"`
			ETag        string `json:"etag"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return nil, fmt.Errorf("decode emulator attrs: %w", err)
		}
		size, _ := strconv.ParseInt(strings.TrimSpace(payload.Size), 10, 64)
		updated := time.Time{}
		if ts := strings.TrimSpace(payload.Updated); ts != "" {
			if parsed, parseErr := time.Parse(time.RFC3339, ts); parseErr == nil {
				updated = parsed
			}
		}
		return &ObjectAttrs{
			Size:        size,
			ContentType: payload.ContentType,
			Updated:     updated,
			ETag:        payload.ETag,
		}, nil
	}
	ctx2, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	attrs, err := bs.storageClient.Bucket(cfg.name).Object(key).Attrs(ctx2)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GCS object attrs: %w", err)
	}
	return &ObjectAttrs{
		Size:        attrs.Size,
		ContentType: attrs.ContentType,
		Updated:     attrs.Updated,
		ETag:        attrs.Etag,
	}, nil
}
