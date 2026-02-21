package extractor

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func TestExtractorModeGCS(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_MODE", "gcs")
	t.Setenv("STORAGE_EMULATOR_HOST", "")

	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	defer log.Sync()

	ex := New(nil, log, nil, nil, nil, nil, nil, nil, nil, &fakeVideoAI{}, nil)
	if ex.IsObjectStorageEmulatorMode() {
		t.Fatalf("IsObjectStorageEmulatorMode: want=false got=true")
	}
	if !ex.ShouldAttemptVideoAIGCS() {
		t.Fatalf("ShouldAttemptVideoAIGCS: want=true got=false")
	}
}

func TestExtractorModeGCSEmulatorDefaultVideoPolicy(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_MODE", "gcs_emulator")
	t.Setenv("STORAGE_EMULATOR_HOST", "http://fake-gcs:4443")

	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	defer log.Sync()

	ex := New(nil, log, nil, nil, nil, nil, nil, nil, nil, &fakeVideoAI{}, nil)
	if !ex.IsObjectStorageEmulatorMode() {
		t.Fatalf("IsObjectStorageEmulatorMode: want=true got=false")
	}
	if ex.ObjectStorageMode != gcp.ObjectStorageModeGCSEmulator {
		t.Fatalf("ObjectStorageMode: want=%q got=%q", gcp.ObjectStorageModeGCSEmulator, ex.ObjectStorageMode)
	}
	if !ex.ShouldAttemptVideoAIGCS() {
		t.Fatalf("ShouldAttemptVideoAIGCS: want=true got=false")
	}
}

func TestExtractorModeGCSEmulatorVideoPolicyIgnoresEnv(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_MODE", "gcs_emulator")
	t.Setenv("STORAGE_EMULATOR_HOST", "http://fake-gcs:4443")

	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	defer log.Sync()

	ex := New(nil, log, nil, nil, nil, nil, nil, nil, nil, &fakeVideoAI{}, nil)
	if !ex.IsObjectStorageEmulatorMode() {
		t.Fatalf("IsObjectStorageEmulatorMode: want=true got=false")
	}
	if !ex.ShouldAttemptVideoAIGCS() {
		t.Fatalf("ShouldAttemptVideoAIGCS: want=true got=false")
	}
}

func TestTryDocAIUsesBytesFallbackInEmulatorMode(t *testing.T) {
	ex := &Extractor{
		DocAI: &fakeDocument{
			processBytesFunc: func(req gcp.DocAIProcessBytesRequest) (*gcp.DocAIResult, error) {
				if len(req.Data) == 0 {
					t.Fatalf("ProcessBytes data: want non-empty")
				}
				return &gcp.DocAIResult{Provider: "fake"}, nil
			},
			processGCSFunc: func(req gcp.DocAIProcessGCSRequest) (*gcp.DocAIResult, error) {
				t.Fatalf("ProcessGCSOnline should not be called in emulator mode")
				return nil, nil
			},
		},
		Bucket: &fakeBucketService{
			downloadFileFunc: func(_ context.Context, category gcp.BucketCategory, key string) (io.ReadCloser, error) {
				if category != gcp.BucketCategoryMaterial {
					t.Fatalf("download category: want material got=%q", category)
				}
				if key != "materials/abc.pdf" {
					t.Fatalf("download key: want=%q got=%q", "materials/abc.pdf", key)
				}
				return io.NopCloser(strings.NewReader("pdf-bytes")), nil
			},
		},
		ObjectStorageMode: gcp.ObjectStorageModeGCSEmulator,
		DocAIProjectID:    "project",
		DocAILocation:     "us",
		DocAIProcessorID:  "processor",
		DocAIProcessorVer: "pretrained",
	}

	_, err := ex.TryDocAI(context.Background(), "application/pdf", "materials/abc.pdf")
	if err != nil {
		t.Fatalf("TryDocAI: %v", err)
	}
}

func TestTryDocAIUsesGCSOnlineInGCSMode(t *testing.T) {
	ex := &Extractor{
		DocAI: &fakeDocument{
			processBytesFunc: func(req gcp.DocAIProcessBytesRequest) (*gcp.DocAIResult, error) {
				t.Fatalf("ProcessBytes should not be called in gcs mode")
				return nil, nil
			},
			processGCSFunc: func(req gcp.DocAIProcessGCSRequest) (*gcp.DocAIResult, error) {
				if req.GCSURI != "gs://materials-bucket/materials/abc.pdf" {
					t.Fatalf("GCSURI: want=%q got=%q", "gs://materials-bucket/materials/abc.pdf", req.GCSURI)
				}
				return &gcp.DocAIResult{Provider: "fake"}, nil
			},
		},
		ObjectStorageMode:  gcp.ObjectStorageModeGCS,
		MaterialBucketName: "materials-bucket",
		DocAIProjectID:     "project",
		DocAILocation:      "us",
		DocAIProcessorID:   "processor",
		DocAIProcessorVer:  "pretrained",
	}

	_, err := ex.TryDocAI(context.Background(), "application/pdf", "materials/abc.pdf")
	if err != nil {
		t.Fatalf("TryDocAI: %v", err)
	}
}

type fakeDocument struct {
	processBytesFunc func(req gcp.DocAIProcessBytesRequest) (*gcp.DocAIResult, error)
	processGCSFunc   func(req gcp.DocAIProcessGCSRequest) (*gcp.DocAIResult, error)
}

func (f *fakeDocument) ProcessBytes(_ context.Context, req gcp.DocAIProcessBytesRequest) (*gcp.DocAIResult, error) {
	if f.processBytesFunc != nil {
		return f.processBytesFunc(req)
	}
	return &gcp.DocAIResult{}, nil
}

func (f *fakeDocument) ProcessGCSOnline(_ context.Context, req gcp.DocAIProcessGCSRequest) (*gcp.DocAIResult, error) {
	if f.processGCSFunc != nil {
		return f.processGCSFunc(req)
	}
	return &gcp.DocAIResult{}, nil
}

func (f *fakeDocument) BatchProcessGCS(_ context.Context, _ gcp.DocAIBatchRequest) (*gcp.DocAIBatchResult, error) {
	return &gcp.DocAIBatchResult{}, nil
}

func (f *fakeDocument) Close() error { return nil }

type fakeBucketService struct {
	downloadFileFunc func(ctx context.Context, category gcp.BucketCategory, key string) (io.ReadCloser, error)
}

func (f *fakeBucketService) UploadFile(dbc dbctx.Context, category gcp.BucketCategory, key string, file io.Reader) error {
	return nil
}

func (f *fakeBucketService) DeleteFile(dbc dbctx.Context, category gcp.BucketCategory, key string) error {
	return nil
}

func (f *fakeBucketService) ReplaceFile(dbc dbctx.Context, category gcp.BucketCategory, key string, newFile io.Reader) error {
	return nil
}

func (f *fakeBucketService) DownloadFile(ctx context.Context, category gcp.BucketCategory, key string) (io.ReadCloser, error) {
	if f.downloadFileFunc != nil {
		return f.downloadFileFunc(ctx, category, key)
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *fakeBucketService) OpenRangeReader(ctx context.Context, category gcp.BucketCategory, key string, offset, length int64) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *fakeBucketService) GetObjectAttrs(ctx context.Context, category gcp.BucketCategory, key string) (*gcp.ObjectAttrs, error) {
	return &gcp.ObjectAttrs{}, nil
}

func (f *fakeBucketService) CopyObject(ctx context.Context, category gcp.BucketCategory, srcKey, dstKey string) error {
	return nil
}

func (f *fakeBucketService) ListKeys(ctx context.Context, category gcp.BucketCategory, prefix string) ([]string, error) {
	return nil, nil
}

func (f *fakeBucketService) DeletePrefix(ctx context.Context, category gcp.BucketCategory, prefix string) error {
	return nil
}

func (f *fakeBucketService) GetPublicURL(category gcp.BucketCategory, key string) string {
	return ""
}

type fakeVideoAI struct{}

func (f *fakeVideoAI) AnnotateVideoGCS(ctx context.Context, gcsURI string, cfg gcp.VideoAIConfig) (*gcp.VideoAIResult, error) {
	return &gcp.VideoAIResult{}, nil
}

func (f *fakeVideoAI) Close() error { return nil }
