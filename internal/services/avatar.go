package services

import (
  "context"
  "bytes"
  "fmt"
  "image"
  "image/color"
  "encoding/json"
  "io/ioutil"
  "math"
  "math/rand"
  "os"
  "path/filepath"
  "strings"
  "time"
  "github.com/disintegration/imaging"
  "github.com/fogleman/gg"
  "github.com/golang/freetype/truetype"
  "golang.org/x/image/font"
  "gorm.io/gorm"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/types"
  "github.com/yungbote/neurobridge-backend/internal/repos"
)

type AvatarServicen interface {
  CreateAndUploadUserAvatar(ctx context.Context, tx *gorm.DB, user *types.User) error
  GenerateUserAvatar(ctx context.Context, tx *gorm.DB, user *types.User) (bytes.Buffer, error)
}

type avatarService struct {
  db              *gorm.DB
  log             *logger.Logger
  userRepo        repos.UserRepo
  bucketService   BucketService
  bgColors        []color.NRGBA
  fontFace        font.Face
}

func NewAvatarService(db *gorm.DB, log *logger.Logger, userRepo repos.UserRepo, bucketService BucketService) (AvatarService, error) {
  serviceLog := log.With("service", "AvatarService")

  rand.Seed(time.Now().UnixNano())

  colorsJSONPath := os.Getenv("AVATAR_COLORS_JSON_PATH")
  if colorsJSONPath == "" {
    return nil, fmt.Errorf("Env var AVATAR_COLORS_JSON_PATH is empty")
  }
  serviceLog.Info("Loading avatar colors...", "path", colorsJSONPath)
  bgColors, err := loadColorsFromFile(colorsJSONPath)
  if err != nil {
    return nil, fmt.Errorf("Could not load avatar colors: %w", err)
  }

  fontPath := os.Getenv("AVATAR_FONT")
  if fontPath == "" {
    return nil, fmt.Errorf("Env var AVATAR_FONT is empty")
  }
  serviceLog.Info("Loading avatar font", "font", fontPath)
  face, err := loadFontFace(fontPath, 206)
  if err != nil {
    return nil, fmt.Errorf("Could not load avatar font: %w", err)
  }

  service := &avatarService{
    db:             db,
    log:            serviceLog,
    userRepo:       userRepo,
    bucketService:  bucketService,
    bgColors:       bgColors,
    fontFace:       face
  }
  return service, nil
}

func (as *avatarService) CreateAndUploadUserAvatar(ctx context.Context, tx *gorm.DB, user *types.User) error {
  buf, err := as.GenerateUserAvatar(ctx, tx, user)
  if err != nil {
    return err
  }
  bucketKey := fmt.Sprintf("user_avatar/%s.png", user.ID.String())
  if err := as.bucketService.UploadFile(ctx, tx, bucketKey, bytes.NewReader(bug.Bytes())); err != nil {
    return fmt.Errorf("Failed to upload user avatar: %w", err)
  }
  if user.AvatarBucketKey != bucketKey {
    user.AvatarBucketKey = bucketKey
  }
  finalURL := as.bucketService.GetPublicURL(bucketKey)
  if user.AvatarURL != finalURL {
    user.AvatarURL = finalURL
  }
  return nil
}

func (as *avatarService) GenerateUserAvatar(ctx context.Context, tx *gorm.DB, user *types.User) (bytes.Buffer, error) {
  const size = 512

  dc := gg.NewContext(size, size)

  dc.DrawCircle(float64(size)/2, float64(size)/2, float64(size)/2)
  dc.Clip()

  base := as.bgColors[rand.Intn(len(as.bgColors))]
  dc.SetColor(base)
  dc.DrawRectangle(0, 0, float64(size), float64(size))
  dc.Fill()

  initials := computeInitials(user.FirstName, user.LastName)

  dc.SetFontFace(as.fontFace)
  tw, th := dc.MeasureString(initials)
  cx, cy := float64(size)/2, float64(size)/2

  dc.SetColor(color.White)
  dc.DrawString(initials, cx-(tw/2)+5, cy+(th/2)-10)

  var buf bytes.Buffer
  if err := dc.EncodePNG(&buf); err != nil {
    return buf, fmt.Errorf("Failed to encode PNG: %w", err)
  }
  return buf, nil
}

// Helpers
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
  data, err := ioutil.ReadFile(jsonPath)
  if err != nil {
    return nil, fmt.Errorf("Read file error: %w", err)
  }
  var colors []color.NRGBA
  if err := json.Unmarshal(data, &colors); err != nil {
    return nil, fmt.Errorf("Json unmarhsal error: %w", err)
  }
  return colors, nil
}

func loadFontFace(fontPath string, size float64) (font.Face, error) {
  fontBytes, err := ioutil.ReadFile(fontPath)
  if err != nil {
    return nil, fmt.Errorf("Failed to read font file: %w", err)
  }
  parsedFont, err := truetype.Parse(fontBytes)
  if err != nil {
    return nil, fmt.Errorf("Failed to parse TTF: %w", err)
  }
  face := truetype.NewFace(parsedFont, &truetype.Options{
    Size:       size,
    DPI:        72,
    Hinting:    font.HintingNone
  })
  return face, nil
}
