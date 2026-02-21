package gcp

import (
	"strings"
	"testing"
)

func TestResolveObjectStoragePublicBaseURLGCSDefault(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_PUBLIC_BASE_URL", "")

	baseURL, source, err := resolveObjectStoragePublicBaseURL(ObjectStorageConfig{
		Mode: ObjectStorageModeGCS,
	})
	if err != nil {
		t.Fatalf("resolveObjectStoragePublicBaseURL: %v", err)
	}
	if baseURL != "" {
		t.Fatalf("baseURL: want empty got=%q", baseURL)
	}
	if source != "gcs_default" {
		t.Fatalf("source: want=%q got=%q", "gcs_default", source)
	}
}

func TestResolveObjectStoragePublicBaseURLEmulatorFallback(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_PUBLIC_BASE_URL", "")

	baseURL, source, err := resolveObjectStoragePublicBaseURL(ObjectStorageConfig{
		Mode:         ObjectStorageModeGCSEmulator,
		EmulatorHost: "http://fake-gcs:4443",
	})
	if err != nil {
		t.Fatalf("resolveObjectStoragePublicBaseURL: %v", err)
	}
	if baseURL != "http://fake-gcs:4443" {
		t.Fatalf("baseURL: want=%q got=%q", "http://fake-gcs:4443", baseURL)
	}
	if source != "storage_emulator_host" {
		t.Fatalf("source: want=%q got=%q", "storage_emulator_host", source)
	}
}

func TestResolveObjectStoragePublicBaseURLEnvOverride(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_PUBLIC_BASE_URL", "http://localhost:4443/")

	baseURL, source, err := resolveObjectStoragePublicBaseURL(ObjectStorageConfig{
		Mode:         ObjectStorageModeGCSEmulator,
		EmulatorHost: "http://fake-gcs:4443",
	})
	if err != nil {
		t.Fatalf("resolveObjectStoragePublicBaseURL: %v", err)
	}
	if baseURL != "http://localhost:4443" {
		t.Fatalf("baseURL: want=%q got=%q", "http://localhost:4443", baseURL)
	}
	if source != "object_storage_public_base_url" {
		t.Fatalf("source: want=%q got=%q", "object_storage_public_base_url", source)
	}
}

func TestResolveObjectStoragePublicBaseURLInvalidEnv(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_PUBLIC_BASE_URL", "localhost:4443")

	_, _, err := resolveObjectStoragePublicBaseURL(ObjectStorageConfig{
		Mode:         ObjectStorageModeGCSEmulator,
		EmulatorHost: "http://fake-gcs:4443",
	})
	if err == nil {
		t.Fatalf("resolveObjectStoragePublicBaseURL: expected error, got nil")
	}
}

func TestGetPublicURLGCSDefault(t *testing.T) {
	bs := &bucketService{
		avatarBucket: bucketConfig{name: "avatar-bucket"},
	}

	got := bs.GetPublicURL(BucketCategoryAvatar, "avatars/user.png")
	want := "https://storage.googleapis.com/avatar-bucket/avatars/user.png"
	if got != want {
		t.Fatalf("GetPublicURL: want=%q got=%q", want, got)
	}
}

func TestGetPublicURLUsesCDNDomain(t *testing.T) {
	bs := &bucketService{
		materialBucket: bucketConfig{
			name:      "material-bucket",
			cdnDomain: "cdn.example.com",
		},
	}

	got := bs.GetPublicURL(BucketCategoryMaterial, "materials/file.pdf")
	want := "https://cdn.example.com/materials/file.pdf"
	if got != want {
		t.Fatalf("GetPublicURL: want=%q got=%q", want, got)
	}
}

func TestGetPublicURLUsesPublicBaseURL(t *testing.T) {
	bs := &bucketService{
		publicBaseURL: "http://localhost:4443",
		materialBucket: bucketConfig{
			name: "material-bucket",
		},
	}

	got := bs.GetPublicURL(BucketCategoryMaterial, "/materials/file.pdf")
	want := "http://localhost:4443/material-bucket/materials/file.pdf"
	if got != want {
		t.Fatalf("GetPublicURL: want=%q got=%q", want, got)
	}
}

func TestGetPublicURLUsesEmulatorMediaEndpoint(t *testing.T) {
	bs := &bucketService{
		storageMode:   ObjectStorageModeGCSEmulator,
		publicBaseURL: "http://localhost:4443",
		avatarBucket: bucketConfig{
			name: "avatar-bucket",
		},
	}

	got := bs.GetPublicURL(BucketCategoryAvatar, "user_avatar/abc/123.png")
	want := "http://localhost:4443/storage/v1/b/avatar-bucket/o/user_avatar%2Fabc%2F123.png?alt=media"
	if got != want {
		t.Fatalf("GetPublicURL: want=%q got=%q", want, got)
	}
}

func TestGetPublicURLUsesEmulatorHostWhenPublicBaseMissing(t *testing.T) {
	bs := &bucketService{
		storageMode:  ObjectStorageModeGCSEmulator,
		emulatorHost: "http://fake-gcs:4443",
		avatarBucket: bucketConfig{
			name: "avatar-bucket",
		},
	}

	got := bs.GetPublicURL(BucketCategoryAvatar, "/user_avatar/abc/123.png")
	want := "http://fake-gcs:4443/storage/v1/b/avatar-bucket/o/user_avatar%2Fabc%2F123.png?alt=media"
	if got != want {
		t.Fatalf("GetPublicURL: want=%q got=%q", want, got)
	}
}

func TestEmulatorPublicURLSmokeRenderableAssets(t *testing.T) {
	bs := &bucketService{
		storageMode:   ObjectStorageModeGCSEmulator,
		publicBaseURL: "http://localhost:4443",
		avatarBucket: bucketConfig{
			name: "avatar-bucket",
		},
		materialBucket: bucketConfig{
			name: "material-bucket",
		},
	}

	cases := []struct {
		name       string
		category   BucketCategory
		key        string
		wantBucket string
		wantCT     string
	}{
		{
			name:       "avatar png",
			category:   BucketCategoryAvatar,
			key:        "user_avatar/u/1.png",
			wantBucket: "avatar-bucket",
			wantCT:     "image/png",
		},
		{
			name:       "material png",
			category:   BucketCategoryMaterial,
			key:        "materials/ms/cover.png",
			wantBucket: "material-bucket",
			wantCT:     "image/png",
		},
		{
			name:       "material video mp4",
			category:   BucketCategoryMaterial,
			key:        "materials/ms/clip.mp4",
			wantBucket: "material-bucket",
			wantCT:     "video/mp4",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			publicURL := bs.GetPublicURL(tc.category, tc.key)
			if !strings.HasPrefix(publicURL, "http://localhost:4443/storage/v1/b/"+tc.wantBucket+"/o/") {
				t.Fatalf("publicURL prefix mismatch for %s: %s", tc.name, publicURL)
			}
			if !strings.Contains(publicURL, "alt=media") {
				t.Fatalf("publicURL should include alt=media for renderable object endpoint: %s", publicURL)
			}
			if !strings.Contains(publicURL, strings.ReplaceAll(tc.key, "/", "%2F")) {
				t.Fatalf("publicURL should escape object key path: %s", publicURL)
			}
			if got := contentTypeForKey(tc.key); got != tc.wantCT {
				t.Fatalf("contentTypeForKey(%q): want=%q got=%q", tc.key, tc.wantCT, got)
			}
		})
	}
}
