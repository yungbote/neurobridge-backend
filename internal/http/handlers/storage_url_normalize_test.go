package handlers

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
)

type testBucketService struct{}

func (t *testBucketService) UploadFile(dbctx.Context, gcp.BucketCategory, string, io.Reader) error {
	return nil
}
func (t *testBucketService) DeleteFile(dbctx.Context, gcp.BucketCategory, string) error { return nil }
func (t *testBucketService) ReplaceFile(dbctx.Context, gcp.BucketCategory, string, io.Reader) error {
	return nil
}
func (t *testBucketService) DownloadFile(context.Context, gcp.BucketCategory, string) (io.ReadCloser, error) {
	return nil, nil
}
func (t *testBucketService) OpenRangeReader(context.Context, gcp.BucketCategory, string, int64, int64) (io.ReadCloser, error) {
	return nil, nil
}
func (t *testBucketService) GetObjectAttrs(context.Context, gcp.BucketCategory, string) (*gcp.ObjectAttrs, error) {
	return nil, nil
}
func (t *testBucketService) CopyObject(context.Context, gcp.BucketCategory, string, string) error {
	return nil
}
func (t *testBucketService) ListKeys(context.Context, gcp.BucketCategory, string) ([]string, error) {
	return nil, nil
}
func (t *testBucketService) DeletePrefix(context.Context, gcp.BucketCategory, string) error {
	return nil
}
func (t *testBucketService) GetPublicURL(category gcp.BucketCategory, key string) string {
	return fmt.Sprintf("resolved://%s/%s", category, key)
}

func TestResolveBucketBackedURL(t *testing.T) {
	b := &testBucketService{}

	if got := resolveBucketBackedURL(b, gcp.BucketCategoryAvatar, "user_avatar/u/1.png", "http://old/url.png"); got != "resolved://avatar/user_avatar/u/1.png" {
		t.Fatalf("resolveBucketBackedURL (bucket): got=%q", got)
	}

	if got := resolveBucketBackedURL(nil, gcp.BucketCategoryAvatar, "user_avatar/u/1.png", " http://old/url.png "); got != "http://old/url.png" {
		t.Fatalf("resolveBucketBackedURL (no bucket): got=%q", got)
	}

	if got := resolveBucketBackedURL(b, gcp.BucketCategoryAvatar, "", " http://old/url.png "); got != "http://old/url.png" {
		t.Fatalf("resolveBucketBackedURL (no key): got=%q", got)
	}
}

func TestNormalizeAvatarAndMaterialURLs(t *testing.T) {
	b := &testBucketService{}

	u := &types.User{
		AvatarBucketKey: "user_avatar/u/1.png",
		AvatarURL:       "http://localhost:4443/neurobridge-avatar/user_avatar/u/1.png",
	}
	normalizeUserAvatarURL(b, u)
	if u.AvatarURL != "resolved://avatar/user_avatar/u/1.png" {
		t.Fatalf("normalizeUserAvatarURL: got=%q", u.AvatarURL)
	}

	p := &types.Path{
		AvatarBucketKey:       "path_avatars/p/1.png",
		AvatarURL:             "legacy",
		AvatarSquareBucketKey: "path_avatars/p/1_square.png",
		AvatarSquareURL:       "legacy",
	}
	normalizePathAvatarURLs(b, p)
	if p.AvatarURL != "resolved://avatar/path_avatars/p/1.png" {
		t.Fatalf("normalizePathAvatarURLs avatar: got=%q", p.AvatarURL)
	}
	if p.AvatarSquareURL != "resolved://avatar/path_avatars/p/1_square.png" {
		t.Fatalf("normalizePathAvatarURLs square: got=%q", p.AvatarSquareURL)
	}

	n := &types.PathNode{
		AvatarBucketKey:       "unit_avatars/p/n/1.png",
		AvatarURL:             "legacy",
		AvatarSquareBucketKey: "unit_avatars/p/n/1_square.png",
		AvatarSquareURL:       "legacy",
	}
	normalizePathNodeAvatarURLs(b, n)
	if n.AvatarURL != "resolved://avatar/unit_avatars/p/n/1.png" {
		t.Fatalf("normalizePathNodeAvatarURLs avatar: got=%q", n.AvatarURL)
	}
	if n.AvatarSquareURL != "resolved://avatar/unit_avatars/p/n/1_square.png" {
		t.Fatalf("normalizePathNodeAvatarURLs square: got=%q", n.AvatarSquareURL)
	}

	files := []*types.MaterialFile{
		{StorageKey: "materials/ms/f1", FileURL: "legacy"},
		{StorageKey: "", FileURL: "keep"},
	}
	normalizeMaterialFileURLs(b, files)
	if files[0].FileURL != "resolved://material/materials/ms/f1" {
		t.Fatalf("normalizeMaterialFileURLs[0]: got=%q", files[0].FileURL)
	}
	if files[1].FileURL != "keep" {
		t.Fatalf("normalizeMaterialFileURLs[1]: got=%q", files[1].FileURL)
	}

	assets := []*types.MaterialAsset{
		{
			ID:             uuid.New(),
			MaterialFileID: uuid.New(),
			Kind:           "frame",
			StorageKey:     "materials/ms/a1",
			URL:            "legacy",
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
	}
	normalizeMaterialAssetURLs(b, assets)
	if assets[0].URL != "resolved://material/materials/ms/a1" {
		t.Fatalf("normalizeMaterialAssetURLs[0]: got=%q", assets[0].URL)
	}
}
