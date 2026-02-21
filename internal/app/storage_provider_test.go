package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func TestClassifyStorageProviderBootstrapErrorInvalidMode(t *testing.T) {
	storageCfg := gcp.ObjectStorageConfig{
		Mode: gcp.ObjectStorageMode("bad-mode"),
	}
	srcErr := &gcp.ObjectStorageConfigError{
		Code: gcp.ObjectStorageConfigErrorInvalidMode,
		Mode: "bad-mode",
	}

	err := classifyStorageProviderBootstrapError(storageCfg, srcErr)

	var got *StorageProviderBootstrapError
	if !errors.As(err, &got) {
		t.Fatalf("expected StorageProviderBootstrapError, got=%T", err)
	}
	if got.Code != StorageProviderBootstrapErrorInvalidMode {
		t.Fatalf("code: want=%q got=%q", StorageProviderBootstrapErrorInvalidMode, got.Code)
	}
}

func TestClassifyStorageProviderBootstrapErrorMissingEmulatorHost(t *testing.T) {
	storageCfg := gcp.ObjectStorageConfig{
		Mode: gcp.ObjectStorageModeGCSEmulator,
	}
	srcErr := &gcp.ObjectStorageConfigError{
		Code: gcp.ObjectStorageConfigErrorMissingEmulatorHost,
		Mode: string(gcp.ObjectStorageModeGCSEmulator),
	}

	err := classifyStorageProviderBootstrapError(storageCfg, srcErr)

	var got *StorageProviderBootstrapError
	if !errors.As(err, &got) {
		t.Fatalf("expected StorageProviderBootstrapError, got=%T", err)
	}
	if got.Code != StorageProviderBootstrapErrorMissingEmulatorHost {
		t.Fatalf("code: want=%q got=%q", StorageProviderBootstrapErrorMissingEmulatorHost, got.Code)
	}
}

func TestClassifyStorageProviderBootstrapErrorInvalidEmulatorHost(t *testing.T) {
	storageCfg := gcp.ObjectStorageConfig{
		Mode:         gcp.ObjectStorageModeGCSEmulator,
		EmulatorHost: "fake-gcs:4443",
	}
	srcErr := &gcp.ObjectStorageConfigError{
		Code:         gcp.ObjectStorageConfigErrorInvalidEmulatorHost,
		Mode:         string(gcp.ObjectStorageModeGCSEmulator),
		EmulatorHost: "fake-gcs:4443",
	}

	err := classifyStorageProviderBootstrapError(storageCfg, srcErr)

	var got *StorageProviderBootstrapError
	if !errors.As(err, &got) {
		t.Fatalf("expected StorageProviderBootstrapError, got=%T", err)
	}
	if got.Code != StorageProviderBootstrapErrorInvalidEmulatorHost {
		t.Fatalf("code: want=%q got=%q", StorageProviderBootstrapErrorInvalidEmulatorHost, got.Code)
	}
}

func TestClassifyStorageProviderBootstrapErrorConnectFailed(t *testing.T) {
	storageCfg := gcp.ObjectStorageConfig{
		Mode: gcp.ObjectStorageModeGCS,
	}
	srcErr := errors.New("dial tcp: connection refused")

	err := classifyStorageProviderBootstrapError(storageCfg, srcErr)

	var got *StorageProviderBootstrapError
	if !errors.As(err, &got) {
		t.Fatalf("expected StorageProviderBootstrapError, got=%T", err)
	}
	if got.Code != StorageProviderBootstrapErrorConnectFailed {
		t.Fatalf("code: want=%q got=%q", StorageProviderBootstrapErrorConnectFailed, got.Code)
	}
}

func TestResolveBucketServiceInvalidMode(t *testing.T) {
	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	defer log.Sync()

	_, err = resolveBucketService(log, Config{
		ObjectStorageMode: "invalid",
	})
	if err == nil {
		t.Fatalf("resolveBucketService: expected error, got nil")
	}

	var got *StorageProviderBootstrapError
	if !errors.As(err, &got) {
		t.Fatalf("expected StorageProviderBootstrapError, got=%T", err)
	}
	if got.Code != StorageProviderBootstrapErrorInvalidMode {
		t.Fatalf("code: want=%q got=%q", StorageProviderBootstrapErrorInvalidMode, got.Code)
	}
}

func TestResolveBucketServiceGCSMode(t *testing.T) {
	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	defer log.Sync()

	orig := newBucketServiceWithConfig
	t.Cleanup(func() {
		newBucketServiceWithConfig = orig
	})

	var captured gcp.ObjectStorageConfig
	expected := &testBucketService{}
	newBucketServiceWithConfig = func(_ *logger.Logger, cfg gcp.ObjectStorageConfig) (gcp.BucketService, error) {
		captured = cfg
		return expected, nil
	}

	got, err := resolveBucketService(log, Config{
		ObjectStorageMode: string(gcp.ObjectStorageModeGCS),
	})
	if err != nil {
		t.Fatalf("resolveBucketService: %v", err)
	}
	if got != expected {
		t.Fatalf("bucket: expected stub bucket instance")
	}
	if captured.Mode != gcp.ObjectStorageModeGCS {
		t.Fatalf("mode: want=%q got=%q", gcp.ObjectStorageModeGCS, captured.Mode)
	}
}

func TestResolveBucketServiceGCSEmulatorMode(t *testing.T) {
	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	defer log.Sync()

	orig := newBucketServiceWithConfig
	t.Cleanup(func() {
		newBucketServiceWithConfig = orig
	})

	var captured gcp.ObjectStorageConfig
	expected := &testBucketService{}
	newBucketServiceWithConfig = func(_ *logger.Logger, cfg gcp.ObjectStorageConfig) (gcp.BucketService, error) {
		captured = cfg
		return expected, nil
	}

	got, err := resolveBucketService(log, Config{
		ObjectStorageMode:   string(gcp.ObjectStorageModeGCSEmulator),
		StorageEmulatorHost: "http://fake-gcs:4443",
	})
	if err != nil {
		t.Fatalf("resolveBucketService: %v", err)
	}
	if got != expected {
		t.Fatalf("bucket: expected stub bucket instance")
	}
	if captured.Mode != gcp.ObjectStorageModeGCSEmulator {
		t.Fatalf("mode: want=%q got=%q", gcp.ObjectStorageModeGCSEmulator, captured.Mode)
	}
	if captured.EmulatorHost != "http://fake-gcs:4443" {
		t.Fatalf("emulator host: want=%q got=%q", "http://fake-gcs:4443", captured.EmulatorHost)
	}
}

func TestResolveBucketServiceMissingEmulatorHost(t *testing.T) {
	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	defer log.Sync()

	orig := newBucketServiceWithConfig
	t.Cleanup(func() {
		newBucketServiceWithConfig = orig
	})

	newBucketServiceWithConfig = gcp.NewBucketServiceWithConfig

	_, err = resolveBucketService(log, Config{
		ObjectStorageMode: string(gcp.ObjectStorageModeGCSEmulator),
	})
	if err == nil {
		t.Fatalf("resolveBucketService: expected error, got nil")
	}

	var got *StorageProviderBootstrapError
	if !errors.As(err, &got) {
		t.Fatalf("expected StorageProviderBootstrapError, got=%T", err)
	}
	if got.Code != StorageProviderBootstrapErrorMissingEmulatorHost {
		t.Fatalf("code: want=%q got=%q", StorageProviderBootstrapErrorMissingEmulatorHost, got.Code)
	}
}

func TestResolveBucketServiceInvalidEmulatorHost(t *testing.T) {
	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	defer log.Sync()

	orig := newBucketServiceWithConfig
	t.Cleanup(func() {
		newBucketServiceWithConfig = orig
	})

	newBucketServiceWithConfig = gcp.NewBucketServiceWithConfig

	_, err = resolveBucketService(log, Config{
		ObjectStorageMode:   string(gcp.ObjectStorageModeGCSEmulator),
		StorageEmulatorHost: "not-a-url",
	})
	if err == nil {
		t.Fatalf("resolveBucketService: expected error, got nil")
	}

	var got *StorageProviderBootstrapError
	if !errors.As(err, &got) {
		t.Fatalf("expected StorageProviderBootstrapError, got=%T", err)
	}
	if got.Code != StorageProviderBootstrapErrorInvalidEmulatorHost {
		t.Fatalf("code: want=%q got=%q", StorageProviderBootstrapErrorInvalidEmulatorHost, got.Code)
	}
}

type testBucketService struct{}

func (t *testBucketService) UploadFile(dbc dbctx.Context, category gcp.BucketCategory, key string, file io.Reader) error {
	return nil
}

func (t *testBucketService) DeleteFile(dbc dbctx.Context, category gcp.BucketCategory, key string) error {
	return nil
}

func (t *testBucketService) ReplaceFile(dbc dbctx.Context, category gcp.BucketCategory, key string, newFile io.Reader) error {
	return nil
}

func (t *testBucketService) DownloadFile(ctx context.Context, category gcp.BucketCategory, key string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (t *testBucketService) OpenRangeReader(ctx context.Context, category gcp.BucketCategory, key string, offset, length int64) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (t *testBucketService) GetObjectAttrs(ctx context.Context, category gcp.BucketCategory, key string) (*gcp.ObjectAttrs, error) {
	return &gcp.ObjectAttrs{}, nil
}

func (t *testBucketService) CopyObject(ctx context.Context, category gcp.BucketCategory, srcKey, dstKey string) error {
	return nil
}

func (t *testBucketService) ListKeys(ctx context.Context, category gcp.BucketCategory, prefix string) ([]string, error) {
	return nil, nil
}

func (t *testBucketService) DeletePrefix(ctx context.Context, category gcp.BucketCategory, prefix string) error {
	return nil
}

func (t *testBucketService) GetPublicURL(category gcp.BucketCategory, key string) string {
	return ""
}
