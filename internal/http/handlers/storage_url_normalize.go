package handlers

import (
	"strings"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
)

func resolveBucketBackedURL(
	bucket gcp.BucketService,
	category gcp.BucketCategory,
	storageKey string,
	currentURL string,
) string {
	key := strings.TrimSpace(storageKey)
	if bucket == nil || key == "" {
		return strings.TrimSpace(currentURL)
	}
	resolved := strings.TrimSpace(bucket.GetPublicURL(category, key))
	if resolved == "" {
		return strings.TrimSpace(currentURL)
	}
	return resolved
}

func normalizeUserAvatarURL(bucket gcp.BucketService, u *types.User) {
	if u == nil {
		return
	}
	u.AvatarURL = resolveBucketBackedURL(bucket, gcp.BucketCategoryAvatar, u.AvatarBucketKey, u.AvatarURL)
}

func normalizePathAvatarURLs(bucket gcp.BucketService, p *types.Path) {
	if p == nil {
		return
	}
	p.AvatarURL = resolveBucketBackedURL(bucket, gcp.BucketCategoryAvatar, p.AvatarBucketKey, p.AvatarURL)
	p.AvatarSquareURL = resolveBucketBackedURL(bucket, gcp.BucketCategoryAvatar, p.AvatarSquareBucketKey, p.AvatarSquareURL)
}

func normalizePathNodeAvatarURLs(bucket gcp.BucketService, n *types.PathNode) {
	if n == nil {
		return
	}
	n.AvatarURL = resolveBucketBackedURL(bucket, gcp.BucketCategoryAvatar, n.AvatarBucketKey, n.AvatarURL)
	n.AvatarSquareURL = resolveBucketBackedURL(bucket, gcp.BucketCategoryAvatar, n.AvatarSquareBucketKey, n.AvatarSquareURL)
}

func normalizeMaterialFileURLs(bucket gcp.BucketService, files []*types.MaterialFile) {
	for _, f := range files {
		if f == nil {
			continue
		}
		f.FileURL = resolveBucketBackedURL(bucket, gcp.BucketCategoryMaterial, f.StorageKey, f.FileURL)
	}
}

func normalizeMaterialAssetURLs(bucket gcp.BucketService, assets []*types.MaterialAsset) {
	for _, a := range assets {
		if a == nil {
			continue
		}
		a.URL = resolveBucketBackedURL(bucket, gcp.BucketCategoryMaterial, a.StorageKey, a.URL)
	}
}
