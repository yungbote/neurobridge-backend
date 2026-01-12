package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
)

type thumbCandidate struct {
	storageKey string
	url        string
	mimeType   string
	page       *int
}

func metaInt(meta map[string]any, key string) (int, bool) {
	if meta == nil || strings.TrimSpace(key) == "" {
		return 0, false
	}
	switch v := meta[key].(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float32:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

func pickThumbnailCandidate(kind string, mf *types.MaterialFile, assets []AssetRef) *thumbCandidate {
	k := strings.ToLower(strings.TrimSpace(kind))

	// Prefer rendered first page for docs.
	if k == "pdf" || k == "docx" || k == "pptx" {
		var best *AssetRef
		bestPage := 0
		for i := range assets {
			a := assets[i]
			if strings.ToLower(strings.TrimSpace(a.Kind)) != "pdf_page" {
				continue
			}
			page, ok := metaInt(a.Metadata, "page")
			if ok && page == 1 {
				p := 1
				return &thumbCandidate{
					storageKey: a.Key,
					url:        a.URL,
					mimeType:   "image/png",
					page:       &p,
				}
			}

			if best == nil {
				best = &assets[i]
				if ok {
					bestPage = page
				}
				continue
			}

			// Prefer the smallest known page number.
			if ok && page > 0 && (bestPage == 0 || page < bestPage) {
				best = &assets[i]
				bestPage = page
			}
		}
		if best != nil {
			var p *int
			if bestPage > 0 {
				pp := bestPage
				p = &pp
			}
			return &thumbCandidate{
				storageKey: best.Key,
				url:        best.URL,
				mimeType:   "image/png",
				page:       p,
			}
		}
	}

	// Prefer first extracted frame for videos.
	if k == "video" {
		var best *AssetRef
		bestIdx := 0
		for i := range assets {
			a := assets[i]
			if strings.ToLower(strings.TrimSpace(a.Kind)) != "frame" {
				continue
			}
			idx, ok := metaInt(a.Metadata, "frame_index")
			if ok && idx == 1 {
				return &thumbCandidate{
					storageKey: a.Key,
					url:        a.URL,
					mimeType:   "image/jpeg",
				}
			}
			if best == nil {
				best = &assets[i]
				if ok {
					bestIdx = idx
				}
				continue
			}
			// Prefer the smallest known frame index.
			if ok && idx > 0 && (bestIdx == 0 || idx < bestIdx) {
				best = &assets[i]
				bestIdx = idx
			}
		}
		if best != nil {
			return &thumbCandidate{
				storageKey: best.Key,
				url:        best.URL,
				mimeType:   "image/jpeg",
			}
		}
	}

	// For images, we can use the original upload as the thumbnail (served via /material-assets with metadata mime).
	if k == "image" {
		if mf == nil || mf.ID == uuid.Nil || strings.TrimSpace(mf.StorageKey) == "" {
			return nil
		}
		mt := strings.TrimSpace(mf.MimeType)
		if mt == "" {
			mt = "application/octet-stream"
		}
		return &thumbCandidate{
			storageKey: mf.StorageKey,
			url:        "",
			mimeType:   mt,
		}
	}

	return nil
}

func thumbnailKeyForMaterial(mf *types.MaterialFile) string {
	base := strings.TrimRight(strings.TrimSpace(mf.StorageKey), "/")
	if base == "" {
		return ""
	}
	return base + "/derived/thumbnail.png"
}

func (s *service) EnsureThumbnail(dbc dbctx.Context, mf *types.MaterialFile) error {
	if s == nil || mf == nil || mf.ID == uuid.Nil {
		return nil
	}
	if s.ex == nil || s.ex.Bucket == nil {
		return nil
	}

	kind := inferThumbKind(mf)
	assets := s.discoverThumbCandidates(dbc.Ctx, mf, kind)
	_, err := s.ensureThumbnailAsset(dbc, mf, kind, assets)
	return err
}

func inferThumbKind(mf *types.MaterialFile) string {
	if mf == nil {
		return "unknown"
	}

	if v := strings.ToLower(strings.TrimSpace(mf.AIType)); v != "" {
		switch v {
		case "pdf", "docx", "pptx", "image", "video", "audio":
			return v
		}
	}

	name := strings.ToLower(strings.TrimSpace(mf.OriginalName))
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(name)))
	mime := strings.ToLower(strings.TrimSpace(mf.MimeType))

	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	case mime == "application/pdf" || ext == ".pdf":
		return "pdf"
	case ext == ".docx" || ext == ".doc" || strings.Contains(mime, "wordprocessingml"):
		return "docx"
	case ext == ".pptx" || ext == ".ppt" || strings.Contains(mime, "presentationml"):
		return "pptx"
	default:
		return "unknown"
	}
}

func (s *service) discoverThumbCandidates(ctx context.Context, mf *types.MaterialFile, kind string) []AssetRef {
	if s == nil || s.ex == nil || s.ex.Bucket == nil || mf == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	base := strings.TrimRight(strings.TrimSpace(mf.StorageKey), "/")
	if base == "" {
		return nil
	}

	k := strings.ToLower(strings.TrimSpace(kind))

	switch k {
	case "pdf", "docx", "pptx":
		prefix := base + "/derived/pages/"
		keys, err := s.ex.Bucket.ListKeys(ctx, gcp.BucketCategoryMaterial, prefix)
		if err != nil || len(keys) == 0 {
			return nil
		}
		sort.Strings(keys)
		out := make([]AssetRef, 0, len(keys))
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key == "" || !strings.HasSuffix(strings.ToLower(key), ".png") {
				continue
			}
			page := parseSuffixInt(key, "page_", ".png")
			meta := map[string]any{"format": "png"}
			if page > 0 {
				meta["page"] = page
			}
			out = append(out, AssetRef{
				Kind:     "pdf_page",
				Key:      key,
				URL:      s.ex.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, key),
				Metadata: meta,
			})
		}
		return out

	case "video":
		prefix := base + "/derived/frames/"
		keys, err := s.ex.Bucket.ListKeys(ctx, gcp.BucketCategoryMaterial, prefix)
		if err != nil || len(keys) == 0 {
			return nil
		}
		sort.Strings(keys)
		out := make([]AssetRef, 0, len(keys))
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			lk := strings.ToLower(key)
			if !strings.HasSuffix(lk, ".jpg") && !strings.HasSuffix(lk, ".jpeg") {
				continue
			}
			frame := parseSuffixInt(key, "frame_", filepath.Ext(key))
			meta := map[string]any{}
			if frame > 0 {
				meta["frame_index"] = frame
			}
			out = append(out, AssetRef{
				Kind:     "frame",
				Key:      key,
				URL:      s.ex.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, key),
				Metadata: meta,
			})
		}
		return out

	default:
		return nil
	}
}

func parseSuffixInt(key string, prefix string, suffix string) int {
	key = strings.TrimSpace(key)
	prefix = strings.TrimSpace(prefix)
	suffix = strings.TrimSpace(suffix)
	if key == "" || prefix == "" || suffix == "" {
		return 0
	}
	base := filepath.Base(key)
	if !strings.HasPrefix(base, prefix) || !strings.HasSuffix(base, suffix) {
		return 0
	}
	s := strings.TrimSuffix(strings.TrimPrefix(base, prefix), suffix)
	s = strings.TrimLeft(s, "0")
	if s == "" {
		s = "0"
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func (s *service) ensureThumbnailAsset(dbc dbctx.Context, mf *types.MaterialFile, kind string, assets []AssetRef) (*types.MaterialAsset, error) {
	if s == nil || s.ex == nil || s.ex.Bucket == nil || mf == nil || mf.ID == uuid.Nil {
		return nil, nil
	}
	if s.materialAssets == nil {
		return nil, nil
	}

	now := time.Now().UTC()
	tx := dbc.Tx
	if tx == nil {
		tx = s.ex.DB
	}
	if tx == nil {
		return nil, nil
	}

	// Validate existing linkage if present; repair if broken.
	if mf.ThumbnailAssetID != nil && *mf.ThumbnailAssetID != uuid.Nil {
		existing, err := s.materialAssets.GetByID(dbc, *mf.ThumbnailAssetID)
		if err == nil && existing != nil && existing.ID != uuid.Nil &&
			existing.MaterialFileID == mf.ID &&
			strings.EqualFold(strings.TrimSpace(existing.Kind), "thumbnail") &&
			strings.TrimSpace(existing.StorageKey) != "" {
			return existing, nil
		}
		// Clear invalid pointer so we can re-link.
		_ = tx.WithContext(dbc.Ctx).Model(&types.MaterialFile{}).
			Where("id = ?", mf.ID).
			Updates(map[string]any{
				"thumbnail_asset_id": nil,
				"updated_at":         now,
			}).Error
		mf.ThumbnailAssetID = nil
	}

	// Reuse an existing thumbnail asset row if one exists (avoid duplicates).
	existingRows, err := s.materialAssets.GetByMaterialFileIDs(dbc, []uuid.UUID{mf.ID})
	if err == nil && len(existingRows) > 0 {
		for _, r := range existingRows {
			if r == nil || r.ID == uuid.Nil {
				continue
			}
			if r.MaterialFileID != mf.ID {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(r.Kind), "thumbnail") {
				continue
			}
			if strings.TrimSpace(r.StorageKey) == "" {
				continue
			}
			_ = tx.WithContext(dbc.Ctx).Model(&types.MaterialFile{}).
				Where("id = ?", mf.ID).
				Updates(map[string]any{
					"thumbnail_asset_id": r.ID,
					"updated_at":         now,
				}).Error
			mf.ThumbnailAssetID = &r.ID
			return r, nil
		}
	}

	candidate := pickThumbnailCandidate(kind, mf, assets)
	var storageKey string
	var url string
	var mimeType string
	var page *int

	if candidate != nil {
		storageKey = candidate.storageKey
		url = candidate.url
		mimeType = candidate.mimeType
		page = candidate.page
	} else {
		// Always generate a fallback thumbnail image so the UI can reliably render something.
		key := thumbnailKeyForMaterial(mf)
		if key == "" {
			return nil, nil
		}
		pngBytes, err := generateFallbackThumbnailPNG(fmt.Sprintf("%s:%s", mf.ID.String(), kind))
		if err != nil {
			return nil, err
		}
		if err := s.ex.Bucket.UploadFile(dbctx.Context{Ctx: dbc.Ctx}, gcp.BucketCategoryMaterial, key, bytes.NewReader(pngBytes)); err != nil {
			return nil, err
		}
		storageKey = key
		url = s.ex.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, key)
		mimeType = "image/png"
		page = nil
	}

	if strings.TrimSpace(storageKey) == "" {
		return nil, nil
	}
	if strings.TrimSpace(url) == "" {
		url = s.ex.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, storageKey)
	}

	meta := map[string]any{
		"mime":        mimeType,
		"source_kind": strings.ToLower(strings.TrimSpace(kind)),
	}
	metaJSON, _ := json.Marshal(meta)

	row := &types.MaterialAsset{
		ID:             uuid.New(),
		MaterialFileID: mf.ID,
		Kind:           "thumbnail",
		StorageKey:     storageKey,
		URL:            url,
		Page:           page,
		Metadata:       datatypes.JSON(metaJSON),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	created, err := s.materialAssets.Create(dbc, []*types.MaterialAsset{row})
	if err != nil {
		return nil, err
	}
	if len(created) == 0 || created[0] == nil || created[0].ID == uuid.Nil {
		return nil, fmt.Errorf("thumbnail asset create returned empty")
	}
	thumb := created[0]

	if err := tx.WithContext(dbc.Ctx).Model(&types.MaterialFile{}).
		Where("id = ?", mf.ID).
		Updates(map[string]any{
			"thumbnail_asset_id": thumb.ID,
			"updated_at":         now,
		}).Error; err != nil {
		return nil, err
	}

	mf.ThumbnailAssetID = &thumb.ID
	return thumb, nil
}

func generateFallbackThumbnailPNG(seed string) ([]byte, error) {
	const (
		w = 640
		h = 360
	)
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	c1, c2 := gradientColors(seed)
	for y := 0; y < h; y++ {
		t := float64(y) / float64(h-1)
		r := uint8(math.Round(float64(c1.R)*(1-t) + float64(c2.R)*t))
		g := uint8(math.Round(float64(c1.G)*(1-t) + float64(c2.G)*t))
		b := uint8(math.Round(float64(c1.B)*(1-t) + float64(c2.B)*t))
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}

	// Subtle top highlight band.
	for y := 0; y < 8; y++ {
		alpha := uint8(18 - y*2)
		for x := 0; x < w; x++ {
			dst := img.RGBAAt(x, y)
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((uint16(dst.R)*(255-uint16(alpha)) + uint16(255)*uint16(alpha)) / 255),
				G: uint8((uint16(dst.G)*(255-uint16(alpha)) + uint16(255)*uint16(alpha)) / 255),
				B: uint8((uint16(dst.B)*(255-uint16(alpha)) + uint16(255)*uint16(alpha)) / 255),
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gradientColors(seed string) (color.RGBA, color.RGBA) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	sum := h.Sum32()

	// Two related hues for a premium-ish gradient.
	r1 := uint8(32 + (sum & 0x7F))
	g1 := uint8(24 + ((sum >> 7) & 0x7F))
	b1 := uint8(48 + ((sum >> 14) & 0x7F))

	r2 := uint8(24 + ((sum >> 5) & 0x7F))
	g2 := uint8(48 + ((sum >> 12) & 0x7F))
	b2 := uint8(32 + ((sum >> 19) & 0x7F))

	return color.RGBA{R: r1, G: g1, B: b1, A: 255}, color.RGBA{R: r2, G: g2, B: b2, A: 255}
}
