package services

import (
  "context"
  "fmt"
  "io"
  "os"
  "time"
  "cloud.google.com/go/storage"
  "google.golang.org/api/option"
  "gorm.io/gorm"
  "github.com/yungbote/neurobridge-backend/internal/logger"
)

type BucketService interface {
  UploadFile(ctx context.Context, tx *gorm.DB, key string, file io.Reader) error
  DeleteFile(ctx context.Context, tx *gorm.DB, key string) error
  ReplaceFile(ctx context.Context, tx *gorm.DB, key string, newFile io.Reader) error
  GetPublicURL(key string) string
}

type bucketService struct {
  log             *logger.Logger
  storageClient   *storage.Client
  bucketName      string
  cdnDomain       string
}

func NewBucketService(log *logger.Logger) (BucketService, error) {
  serviceLog := log.With("service", "BucketService")
  bucket := os.Getenv("GCS_BUCKET_NAME")
  if bucket == "" {
    return nil, fmt.Errorf("missing env var GCS_BUCKET_NAME")
  }
  cdnDomain := os.Getenv("CDN_DOMAIN")
  saPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS_JSON")
  if saPath == "" {
    serviceLog.Warn("The storage client may rely on other ADC or fail because GOOGLE_APPLICATION_CLIENT_JSON env var missing...")
  }
  ctx := context.Background()
  var stClient *storage.Client
  var err error
  if saPath != "" {
    stClient, err = storage.NewClient(ctx, option.WithCredentialsFile(saPath), option.WithScopes(storage.ScopeReadWrite))
  } else {
    stClient, err = storage.NewClient(ctx, option.WithScopes(storage.ScopeReadWrite))
  }
  if err != nil {
    return nil, fmt.Errorf("Failed to create storage client: %w", err)
  }
  return &bucketService{
    log:            serviceLog,
    storageClient:  stClient,
    bucketName:     bucket,
    cdnDomain:      cdnDomain,
  }, nil
}

func (bs *bucketService) UploadFile(ctx context.Context, tx *gorm.DB, key string, file io.Reader) error {
  ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
  defer cancel()
  w := bs.storageClient.Bucket(bs.bucketName).Object(key).NewWriter(ctx)
  if _, err := io.Copy(w, file); err != nil {
    _ = w.Close()
    return fmt.Errorf("Failed to write data to GCS: %w", err)
  }
  if err := w.Close(); err != nil {
    return fmt.Errorf("Failed to write GCS writer: %w", err)
  }
  return nil
}

func (bs *bucketService) DeleteFile(ctx context.Context, tx *gorm.DB, key string) error {
  ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
  defer cancel()
  o := bs.storageClient.Bucket(bs.bucketName).Object(key)
  if err := o.Delete(ctx); err != nil {
    return fmt.Errorf("Failed to delete GCS object %q: %w", key, err)
  }
  return nil
}

func (bs *bucketService) ReplaceFile(ctx context.Context, tx *gorm.DB, key string, newFile io.Reader) error {
  if err := bs.DeleteFile(ctx, tx, key); err != nil {
    return fmt.Errorf("Failed deleting old file: %w", err)
  }
  if err := bs.UploadFile(ctx, tx, key, newFile); err != nil {
    return fmt.Errorf("Failed uploading new file: %w", err)
  }
  return nil
}

func (bs *bucketService) GetPublicURL(key string) string {
  if bs.cdnDomain != "" {
    return fmt.Sprintf("https://%s/%s", bs.cdnDomain, key)
  }
  return fmt.Sprintf("https://storage.googleapis.com/%s/%s", bs.bucketName, key)
}

