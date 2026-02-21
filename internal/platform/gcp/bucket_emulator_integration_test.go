package gcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func TestBucketServiceEmulatorCRUDLifecycle(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("NB_RUN_GCS_EMULATOR_INTEGRATION")), "true") {
		t.Skip("set NB_RUN_GCS_EMULATOR_INTEGRATION=true to run emulator integration tests")
	}

	emulatorHost := strings.TrimSpace(os.Getenv("NB_GCS_EMULATOR_HOST"))
	if emulatorHost == "" {
		emulatorHost = strings.TrimSpace(os.Getenv("STORAGE_EMULATOR_HOST"))
	}
	if emulatorHost == "" {
		emulatorHost = "http://127.0.0.1:4443"
	}
	emulatorHost = strings.TrimRight(emulatorHost, "/")

	if !isEmulatorReachable(t, emulatorHost) {
		t.Skipf("storage emulator not reachable at %s", emulatorHost)
	}

	suffix := time.Now().UnixNano()
	avatarBucket := fmt.Sprintf("nb-it-avatar-%d", suffix)
	materialBucket := fmt.Sprintf("nb-it-material-%d", suffix)
	createBucketIfMissing(t, emulatorHost, avatarBucket)
	createBucketIfMissing(t, emulatorHost, materialBucket)

	t.Setenv("AVATAR_GCS_BUCKET_NAME", avatarBucket)
	t.Setenv("MATERIAL_GCS_BUCKET_NAME", materialBucket)
	t.Setenv("AVATAR_CDN_DOMAIN", "")
	t.Setenv("MATERIAL_CDN_DOMAIN", "")
	t.Setenv("STORAGE_EMULATOR_HOST", emulatorHost)
	t.Setenv("OBJECT_STORAGE_PUBLIC_BASE_URL", emulatorHost)

	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	defer log.Sync()

	bucket, err := NewBucketServiceWithConfig(log, ObjectStorageConfig{
		Mode:         ObjectStorageModeGCSEmulator,
		EmulatorHost: emulatorHost,
	})
	if err != nil {
		t.Fatalf("NewBucketServiceWithConfig: %v", err)
	}

	ctx := context.Background()
	prefix := fmt.Sprintf("it/%d", suffix)
	keyA := prefix + "/a.txt"
	keyB := prefix + "/b.txt"

	if err := bucket.UploadFile(dbctx.Context{Ctx: ctx}, BucketCategoryMaterial, keyA, strings.NewReader("alpha")); err != nil {
		t.Fatalf("UploadFile(%s): %v", keyA, err)
	}
	if err := bucket.UploadFile(dbctx.Context{Ctx: ctx}, BucketCategoryMaterial, keyB, strings.NewReader("beta")); err != nil {
		t.Fatalf("UploadFile(%s): %v", keyB, err)
	}

	waitForKeys(t, bucket, ctx, prefix, keyA, keyB)

	body, err := downloadWithRetry(ctx, bucket, BucketCategoryMaterial, keyA, 5*time.Second)
	if err != nil {
		t.Fatalf("downloadWithRetry(%s): %v", keyA, err)
	}
	if string(body) != "alpha" {
		t.Fatalf("download body: want=%q got=%q", "alpha", string(body))
	}

	keys, err := bucket.ListKeys(ctx, BucketCategoryMaterial, prefix)
	if err != nil {
		t.Fatalf("ListKeys(%s): %v", prefix, err)
	}
	if !slices.Contains(keys, keyA) || !slices.Contains(keys, keyB) {
		t.Fatalf("ListKeys missing uploaded keys: got=%v", keys)
	}

	if err := bucket.DeleteFile(dbctx.Context{Ctx: ctx}, BucketCategoryMaterial, keyA); err != nil {
		t.Fatalf("DeleteFile(%s): %v", keyA, err)
	}
	keys, err = bucket.ListKeys(ctx, BucketCategoryMaterial, prefix)
	if err != nil {
		t.Fatalf("ListKeys after delete: %v", err)
	}
	if slices.Contains(keys, keyA) {
		t.Fatalf("expected %s to be deleted; keys=%v", keyA, keys)
	}
	if !slices.Contains(keys, keyB) {
		t.Fatalf("expected %s to remain; keys=%v", keyB, keys)
	}

	if err := bucket.DeletePrefix(ctx, BucketCategoryMaterial, prefix); err != nil {
		t.Fatalf("DeletePrefix(%s): %v", prefix, err)
	}
	keys, err = bucket.ListKeys(ctx, BucketCategoryMaterial, prefix)
	if err != nil {
		t.Fatalf("ListKeys after delete-prefix: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected empty prefix after DeletePrefix; keys=%v", keys)
	}
}

func isEmulatorReachable(t *testing.T, emulatorHost string) bool {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(emulatorHost + "/storage/v1/b?project=local-dev")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 500
}

func createBucketIfMissing(t *testing.T, emulatorHost string, bucket string) {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"name": bucket})
	if err != nil {
		t.Fatalf("json.Marshal(bucket): %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(
		http.MethodPost,
		emulatorHost+"/storage/v1/b?project=local-dev",
		bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("http.NewRequest(create bucket): %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("create bucket %q: %v", bucket, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusConflict {
		return
	}
	b, _ := io.ReadAll(resp.Body)
	t.Fatalf("create bucket %q failed: status=%d body=%s", bucket, resp.StatusCode, strings.TrimSpace(string(b)))
}

func waitForKeys(
	t *testing.T,
	bucket BucketService,
	ctx context.Context,
	prefix string,
	keys ...string,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last []string
	for {
		got, err := bucket.ListKeys(ctx, BucketCategoryMaterial, prefix)
		if err == nil {
			last = got
			ok := true
			for _, k := range keys {
				if !slices.Contains(got, k) {
					ok = false
					break
				}
			}
			if ok {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for keys %v under prefix %q; last=%v", keys, prefix, last)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func downloadWithRetry(
	ctx context.Context,
	bucket BucketService,
	category BucketCategory,
	key string,
	timeout time.Duration,
) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		rc, err := bucket.DownloadFile(ctx, category, key)
		if err == nil {
			body, readErr := io.ReadAll(rc)
			_ = rc.Close()
			if readErr == nil {
				return body, nil
			}
			lastErr = readErr
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			return nil, lastErr
		}
		time.Sleep(100 * time.Millisecond)
	}
}
