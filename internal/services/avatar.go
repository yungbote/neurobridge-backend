package services

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math/rand"
	"os"
	"strings"
	"time"

	_ "image/jpeg"
	_ "image/png"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"gorm.io/gorm"
)

type AvatarService interface {
	CreateAndUploadUserAvatar(dbc dbctx.Context, user *types.User) error
	CreateAndUploadUserAvatarFromImage(dbc dbctx.Context, user *types.User, raw []byte) error
	GenerateUserAvatar(dbc dbctx.Context, user *types.User) (bytes.Buffer, error)

	CreateAndUploadPathAvatar(dbc dbctx.Context, path *types.Path, prompt string) error
	CreateAndUploadPathNodeAvatar(dbc dbctx.Context, node *types.PathNode, prompt string) error
}

type avatarService struct {
	db            *gorm.DB
	log           *logger.Logger
	userRepo      repos.UserRepo
	bucketService gcp.BucketService
	ai            openai.Client

	bgColors   []color.NRGBA
	colorByHex map[string]color.NRGBA
	colorHexes []string

	fontFace font.Face
}

func NewAvatarService(db *gorm.DB, log *logger.Logger, userRepo repos.UserRepo, bucketService gcp.BucketService, ai openai.Client) (AvatarService, error) {
	serviceLog := log.With("service", "AvatarService")

	rand.Seed(time.Now().UnixNano())

	if ai == nil {
		return nil, fmt.Errorf("OpenAI client required")
	}

	colorsJSONPath := os.Getenv("AVATAR_COLORS_JSON_PATH")
	if strings.TrimSpace(colorsJSONPath) == "" {
		return nil, fmt.Errorf("Env var AVATAR_COLORS_JSON_PATH is empty")
	}
	serviceLog.Info("Loading avatar colors...", "path", colorsJSONPath)

	bgColors, err := loadColorsFromFile(colorsJSONPath)
	if err != nil {
		return nil, fmt.Errorf("could not load avatar colors: %w", err)
	}
	if len(bgColors) == 0 {
		return nil, fmt.Errorf("avatar colors list is empty")
	}

	colorByHex := make(map[string]color.NRGBA, len(bgColors))
	colorHexes := make([]string, 0, len(bgColors))
	for _, c := range bgColors {
		h := strings.ToUpper(nrgbaToHex(c))
		colorByHex[h] = c
		colorHexes = append(colorHexes, h)
	}

	fontPath := os.Getenv("AVATAR_FONT")
	if strings.TrimSpace(fontPath) == "" {
		return nil, fmt.Errorf("Env var AVATAR_FONT is empty")
	}
	serviceLog.Info("Loading avatar font", "font", fontPath)

	face, err := loadFontFace(fontPath, 206)
	if err != nil {
		return nil, fmt.Errorf("could not load avatar font: %w", err)
	}

	return &avatarService{
		db:            db,
		log:           serviceLog,
		userRepo:      userRepo,
		bucketService: bucketService,
		ai:            ai,
		bgColors:      bgColors,
		colorByHex:    colorByHex,
		colorHexes:    colorHexes,
		fontFace:      face,
	}, nil
}

func (as *avatarService) CreateAndUploadUserAvatar(dbc dbctx.Context, user *types.User) error {
	as.ensureUserAvatarColor(user)

	buf, err := as.GenerateUserAvatar(dbc, user)
	if err != nil {
		return err
	}

	// Save old key so we can delete after success
	oldKey := strings.TrimSpace(user.AvatarBucketKey)

	// NEW: versioned key (fixes CDN ignoring query params)
	newKey := fmt.Sprintf("user_avatar/%s/%d.png", user.ID.String(), time.Now().UnixNano())

	// Upload new
	if err := as.bucketService.UploadFile(dbc, gcp.BucketCategoryAvatar, newKey, bytes.NewReader(buf.Bytes())); err != nil {
		return fmt.Errorf("failed to upload user avatar: %w", err)
	}

	// Point user at new object
	user.AvatarBucketKey = newKey
	user.AvatarURL = as.bucketService.GetPublicURL(gcp.BucketCategoryAvatar, newKey)

	// Best-effort delete old AFTER we have a new one
	if oldKey != "" && oldKey != newKey {
		if err := as.bucketService.DeleteFile(dbctx.Context{Ctx: dbc.Ctx}, gcp.BucketCategoryAvatar, oldKey); err != nil {
			as.log.Warn("failed to delete old avatar (ignored)", "oldKey", oldKey, "error", err)
		}
	}

	return nil
}

func (as *avatarService) GenerateUserAvatar(dbc dbctx.Context, user *types.User) (bytes.Buffer, error) {
	const size = 512
	as.ensureUserAvatarColor(user)

	dc := gg.NewContext(size, size)

	// Clip to circle
	dc.DrawCircle(float64(size)/2, float64(size)/2, float64(size)/2)
	dc.Clip()

	// Fill bg
	base := as.pickColor(user.AvatarColor)
	dc.SetColor(base)
	dc.DrawRectangle(0, 0, float64(size), float64(size))
	dc.Fill()

	// Initials
	initials := computeInitials(user.FirstName, user.LastName)

	dc.SetFontFace(as.fontFace)
	tw, th := dc.MeasureString(initials)
	cx, cy := float64(size)/2, float64(size)/2

	dc.SetColor(color.White)
	dc.DrawString(initials, cx-(tw/2)+5, cy+(th/2)-10)

	var buf bytes.Buffer
	if err := dc.EncodePNG(&buf); err != nil {
		return buf, fmt.Errorf("failed to encode PNG: %w", err)
	}
	return buf, nil
}

func (as *avatarService) CreateAndUploadUserAvatarFromImage(dbc dbctx.Context, user *types.User, raw []byte) error {
	if user == nil || user.ID == uuid.Nil {
		return fmt.Errorf("user required")
	}

	processed, err := processUploadedAvatar(raw, 512)
	if err != nil {
		return err
	}

	// Save old key so we can delete it after we successfully upload the new avatar
	oldKey := strings.TrimSpace(user.AvatarBucketKey)

	// NEW: versioned key so CDN/browser can’t serve stale cached content
	newKey := fmt.Sprintf("user_avatar/%s/%d.png", user.ID.String(), time.Now().UnixNano())

	if err := as.bucketService.UploadFile(
		dbc,
		gcp.BucketCategoryAvatar,
		newKey,
		bytes.NewReader(processed.Bytes()),
	); err != nil {
		return fmt.Errorf("failed to upload user avatar: %w", err)
	}

	user.AvatarBucketKey = newKey
	user.AvatarURL = as.bucketService.GetPublicURL(gcp.BucketCategoryAvatar, newKey)

	// Best-effort delete old avatar object (do NOT fail the request if delete fails).
	if oldKey != "" && oldKey != newKey {
		if err := as.bucketService.DeleteFile(dbctx.Context{Ctx: dbc.Ctx}, gcp.BucketCategoryAvatar, oldKey); err != nil {
			as.log.Warn("failed to delete old avatar (ignored)", "oldKey", oldKey, "error", err)
		}
	}

	return nil
}

func (as *avatarService) CreateAndUploadPathAvatar(dbc dbctx.Context, path *types.Path, prompt string) error {
	if path == nil || path.ID == uuid.Nil {
		return fmt.Errorf("path required")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("prompt required")
	}
	if as == nil || as.ai == nil || as.bucketService == nil {
		return fmt.Errorf("avatar service not configured")
	}

	ctx := dbc.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	img, err := as.ai.GenerateImage(ctx, prompt)
	if err != nil {
		return fmt.Errorf("generate path avatar: %w", err)
	}
	if len(img.Bytes) == 0 {
		return fmt.Errorf("generate path avatar: empty image")
	}

	square, err := cropAndResizeSquare(img.Bytes, 512)
	if err != nil {
		return fmt.Errorf("process path avatar: %w", err)
	}

	squarePNG, err := encodeSquarePNG(square, 512)
	if err != nil {
		return fmt.Errorf("process path avatar (square): %w", err)
	}
	circlePNG, err := encodeCirclePNG(square, 512)
	if err != nil {
		return fmt.Errorf("process path avatar (circle): %w", err)
	}

	oldCircleKey := strings.TrimSpace(path.AvatarBucketKey)
	oldSquareKey := strings.TrimSpace(path.AvatarSquareBucketKey)

	ts := time.Now().UnixNano()
	newCircleKey := fmt.Sprintf("path_avatars/%s/%d.png", path.ID.String(), ts)
	newSquareKey := fmt.Sprintf("path_avatars/%s/%d_square.png", path.ID.String(), ts)

	if err := as.bucketService.UploadFile(dbc, gcp.BucketCategoryAvatar, newSquareKey, bytes.NewReader(squarePNG.Bytes())); err != nil {
		return fmt.Errorf("upload path avatar (square): %w", err)
	}
	if err := as.bucketService.UploadFile(dbc, gcp.BucketCategoryAvatar, newCircleKey, bytes.NewReader(circlePNG.Bytes())); err != nil {
		_ = as.bucketService.DeleteFile(dbctx.Context{Ctx: dbc.Ctx}, gcp.BucketCategoryAvatar, newSquareKey)
		return fmt.Errorf("upload path avatar (circle): %w", err)
	}

	path.AvatarBucketKey = newCircleKey
	path.AvatarURL = as.bucketService.GetPublicURL(gcp.BucketCategoryAvatar, newCircleKey)
	path.AvatarSquareBucketKey = newSquareKey
	path.AvatarSquareURL = as.bucketService.GetPublicURL(gcp.BucketCategoryAvatar, newSquareKey)

	if oldCircleKey != "" && oldCircleKey != newCircleKey {
		if err := as.bucketService.DeleteFile(dbctx.Context{Ctx: dbc.Ctx}, gcp.BucketCategoryAvatar, oldCircleKey); err != nil {
			as.log.Warn("failed to delete old path avatar (ignored)", "oldKey", oldCircleKey, "error", err)
		}
	}
	if oldSquareKey != "" && oldSquareKey != newSquareKey {
		if err := as.bucketService.DeleteFile(dbctx.Context{Ctx: dbc.Ctx}, gcp.BucketCategoryAvatar, oldSquareKey); err != nil {
			as.log.Warn("failed to delete old path avatar square (ignored)", "oldKey", oldSquareKey, "error", err)
		}
	}

	return nil
}

func (as *avatarService) CreateAndUploadPathNodeAvatar(dbc dbctx.Context, node *types.PathNode, prompt string) error {
	if node == nil || node.ID == uuid.Nil || node.PathID == uuid.Nil {
		return fmt.Errorf("path node required")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("prompt required")
	}
	if as == nil || as.ai == nil || as.bucketService == nil {
		return fmt.Errorf("avatar service not configured")
	}

	ctx := dbc.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	img, err := as.ai.GenerateImage(ctx, prompt)
	if err != nil {
		return fmt.Errorf("generate unit avatar: %w", err)
	}
	if len(img.Bytes) == 0 {
		return fmt.Errorf("generate unit avatar: empty image")
	}

	square, err := cropAndResizeSquare(img.Bytes, 512)
	if err != nil {
		return fmt.Errorf("process unit avatar: %w", err)
	}

	squarePNG, err := encodeSquarePNG(square, 512)
	if err != nil {
		return fmt.Errorf("process unit avatar (square): %w", err)
	}
	circlePNG, err := encodeCirclePNG(square, 512)
	if err != nil {
		return fmt.Errorf("process unit avatar (circle): %w", err)
	}

	oldCircleKey := strings.TrimSpace(node.AvatarBucketKey)
	oldSquareKey := strings.TrimSpace(node.AvatarSquareBucketKey)

	ts := time.Now().UnixNano()
	newCircleKey := fmt.Sprintf("unit_avatars/%s/%s/%d.png", node.PathID.String(), node.ID.String(), ts)
	newSquareKey := fmt.Sprintf("unit_avatars/%s/%s/%d_square.png", node.PathID.String(), node.ID.String(), ts)

	if err := as.bucketService.UploadFile(dbc, gcp.BucketCategoryAvatar, newSquareKey, bytes.NewReader(squarePNG.Bytes())); err != nil {
		return fmt.Errorf("upload unit avatar (square): %w", err)
	}
	if err := as.bucketService.UploadFile(dbc, gcp.BucketCategoryAvatar, newCircleKey, bytes.NewReader(circlePNG.Bytes())); err != nil {
		_ = as.bucketService.DeleteFile(dbctx.Context{Ctx: dbc.Ctx}, gcp.BucketCategoryAvatar, newSquareKey)
		return fmt.Errorf("upload unit avatar (circle): %w", err)
	}

	node.AvatarBucketKey = newCircleKey
	node.AvatarURL = as.bucketService.GetPublicURL(gcp.BucketCategoryAvatar, newCircleKey)
	node.AvatarSquareBucketKey = newSquareKey
	node.AvatarSquareURL = as.bucketService.GetPublicURL(gcp.BucketCategoryAvatar, newSquareKey)

	if oldCircleKey != "" && oldCircleKey != newCircleKey {
		if err := as.bucketService.DeleteFile(dbctx.Context{Ctx: dbc.Ctx}, gcp.BucketCategoryAvatar, oldCircleKey); err != nil {
			as.log.Warn("failed to delete old unit avatar (ignored)", "oldKey", oldCircleKey, "error", err)
		}
	}
	if oldSquareKey != "" && oldSquareKey != newSquareKey {
		if err := as.bucketService.DeleteFile(dbctx.Context{Ctx: dbc.Ctx}, gcp.BucketCategoryAvatar, oldSquareKey); err != nil {
			as.log.Warn("failed to delete old unit avatar square (ignored)", "oldKey", oldSquareKey, "error", err)
		}
	}

	return nil
}

func processUploadedAvatar(raw []byte, size int) (bytes.Buffer, error) {
	square, err := cropAndResizeSquare(raw, size)
	if err != nil {
		return bytes.Buffer{}, err
	}
	return encodeCirclePNG(square, size)
}

func cropAndResizeSquare(raw []byte, size int) (*image.RGBA, error) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	// Center-crop to square.
	b := img.Bounds()
	w := b.Dx()
	h := b.Dy()
	side := w
	if h < w {
		side = h
	}
	x0 := b.Min.X + (w-side)/2
	y0 := b.Min.Y + (h-side)/2

	cropRect := image.Rect(0, 0, side, side)
	cropped := image.NewRGBA(cropRect)
	draw.Draw(cropped, cropRect, img, image.Point{X: x0, Y: y0}, draw.Src)

	// Resize to NxN.
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), cropped, cropped.Bounds(), draw.Over, nil)
	return dst, nil
}

func encodeCirclePNG(img image.Image, size int) (bytes.Buffer, error) {
	var out bytes.Buffer
	dc := gg.NewContext(size, size)
	dc.DrawCircle(float64(size)/2, float64(size)/2, float64(size)/2)
	dc.Clip()
	dc.DrawImage(img, 0, 0)
	if err := dc.EncodePNG(&out); err != nil {
		return out, fmt.Errorf("encode png: %w", err)
	}
	return out, nil
}

func encodeSquarePNG(img image.Image, size int) (bytes.Buffer, error) {
	var out bytes.Buffer
	dc := gg.NewContext(size, size)
	dc.DrawImage(img, 0, 0)
	if err := dc.EncodePNG(&out); err != nil {
		return out, fmt.Errorf("encode png: %w", err)
	}
	return out, nil
}

// -------------------- Color helpers --------------------

func (as *avatarService) ensureUserAvatarColor(user *types.User) {
	// keep if valid
	if strings.TrimSpace(user.AvatarColor) != "" {
		n := normalizeHex(user.AvatarColor)
		if n != "" {
			if _, ok := as.colorByHex[n]; ok {
				user.AvatarColor = n
				return
			}
		}
	}

	// pick allowed random and store as hex
	pick := as.bgColors[rand.Intn(len(as.bgColors))]
	user.AvatarColor = nrgbaToHex(pick)
}

func (as *avatarService) pickColor(hexStr string) color.NRGBA {
	h := normalizeHex(hexStr)
	if h != "" {
		if c, ok := as.colorByHex[h]; ok {
			return c
		}
	}
	return as.bgColors[rand.Intn(len(as.bgColors))]
}

func normalizeHex(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "#") {
		s = "#" + s
	}
	s = strings.ToUpper(s)
	if len(s) != 7 {
		return ""
	}
	_, _, _, err := parseHexRGB(s)
	if err != nil {
		return ""
	}
	return s
}

func parseHexRGB(s string) (r, g, b uint8, err error) {
	if strings.HasPrefix(s, "#") {
		s = s[1:]
	}
	if len(s) != 6 {
		return 0, 0, 0, fmt.Errorf("expected 6 hex chars")
	}

	raw, err := hex.DecodeString(s)
	if err != nil || len(raw) != 3 {
		return 0, 0, 0, fmt.Errorf("invalid hex")
	}
	return raw[0], raw[1], raw[2], nil
}

func nrgbaToHex(c color.NRGBA) string {
	return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
}

// -------------------- Misc helpers --------------------

func computeInitials(first, last string) string {
	fInit := "?"
	if len(first) > 0 {
		fInit = strings.ToUpper(first[:1])
	}
	lInit := "?"
	if len(last) > 0 {
		lInit = strings.ToUpper(last[:1])
	}
	return fInit + lInit
}

func loadColorsFromFile(jsonPath string) ([]color.NRGBA, error) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("read file error: %w", err)
	}
	var colors []color.NRGBA
	if err := json.Unmarshal(data, &colors); err != nil {
		return nil, fmt.Errorf("json unmarshal error: %w", err)
	}
	return colors, nil
}

func loadFontFace(fontPath string, size float64) (font.Face, error) {
	fontBytes, err := os.ReadFile(fontPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read font file: %w", err)
	}
	parsedFont, err := truetype.Parse(fontBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse TTF: %w", err)
	}
	face := truetype.NewFace(parsedFont, &truetype.Options{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingNone,
	})
	return face, nil
}
