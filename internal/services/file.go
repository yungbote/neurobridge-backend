package services

import (
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type FileService interface {
	UploadMaterialFile(dbc dbctx.Context, mf *types.MaterialFile, file io.Reader) error
	DeleteMaterialFile(dbc dbctx.Context, mf *types.MaterialFile) error
	UploadMaterialFiles(dbc dbctx.Context, files []*types.MaterialFile, readers []io.Reader) error
	DeleteMaterialFiles(dbc dbctx.Context, files []*types.MaterialFile) error
}

type fileService struct {
	db               *gorm.DB
	log              *logger.Logger
	bucketService    gcp.BucketService
	materialFileRepo repos.MaterialFileRepo
}

func NewFileService(
	db *gorm.DB,
	baseLog *logger.Logger,
	bucketService gcp.BucketService,
	materialFileRepo repos.MaterialFileRepo,
) FileService {
	serviceLog := baseLog.With("service", "FileService")
	return &fileService{
		db:               db,
		log:              serviceLog,
		bucketService:    bucketService,
		materialFileRepo: materialFileRepo,
	}
}

func (fs *fileService) UploadMaterialFile(dbc dbctx.Context, mf *types.MaterialFile, file io.Reader) error {
	return fs.UploadMaterialFiles(dbc, []*types.MaterialFile{mf}, []io.Reader{file})
}

func (fs *fileService) DeleteMaterialFile(dbc dbctx.Context, mf *types.MaterialFile) error {
	return fs.DeleteMaterialFiles(dbc, []*types.MaterialFile{mf})
}

func (fs *fileService) UploadMaterialFiles(dbc dbctx.Context, files []*types.MaterialFile, readers []io.Reader) error {
	transaction := dbc.Tx
	if transaction == nil {
		return fmt.Errorf("UploadMaterialFiles requires non-nil transaction")
	}
	if len(files) == 0 {
		return nil
	}
	if len(files) != len(readers) {
		return fmt.Errorf("UploadMaterialFiles: files and readers length mismatch")
	}

	now := time.Now()
	for i, mf := range files {
		if mf == nil {
			return fmt.Errorf("UploadMaterialFiles: material file at index %d is nil", i)
		}
		if mf.ID == uuid.Nil {
			return fmt.Errorf("UploadMaterialFiles: material file at index %d has no ID", i)
		}
		reader := readers[i]
		if reader == nil {
			return fmt.Errorf("UploadMaterialFiles: reader at index %d is nil", i)
		}

		if mf.StorageKey == "" {
			mf.StorageKey = fmt.Sprintf("materials/%s/%s", mf.MaterialSetID.String(), mf.ID.String())
		}
		key := mf.StorageKey

		fs.log.Info("Uploading material file to bucket",
			"material_file_id", mf.ID,
			"storage_key", key,
		)

		if err := fs.bucketService.UploadFile(dbc, gcp.BucketCategoryMaterial, key, reader); err != nil {
			fs.log.Error("UploadFile failed",
				"error", err,
				"material_file_id", mf.ID,
				"storage_key", key,
			)
			if uErr := transaction.Model(&types.MaterialFile{}).
				Where("id = ?", mf.ID).
				Updates(map[string]interface{}{
					"status":     "upload_failed",
					"updated_at": now,
				}).Error; uErr != nil {
				fs.log.Error("failed to mark material file as upload_failed", "error", uErr, "material_file_id", mf.ID)
			}
			return fmt.Errorf("UploadMaterialFiles: upload failed for material_file_id=%s: %w", mf.ID, err)
		}

		fileURL := fs.bucketService.GetPublicURL(gcp.BucketCategoryMaterial, key)
		updates := map[string]interface{}{
			"storage_key": mf.StorageKey,
			"status":      "uploaded",
			"updated_at":  now,
			"file_url":    fileURL,
		}

		if err := transaction.Model(&types.MaterialFile{}).
			Where("id = ?", mf.ID).
			Updates(updates).Error; err != nil {
			fs.log.Error("failed to update material file after upload", "error", err, "material_file_id", mf.ID)
			return fmt.Errorf("UploadMaterialFiles: failed to update db for material_file_id=%s: %w", mf.ID, err)
		}

		mf.StorageKey = key
		mf.Status = "uploaded"
		mf.FileURL = fileURL
	}

	return nil
}

func (fs *fileService) DeleteMaterialFiles(dbc dbctx.Context, files []*types.MaterialFile) error {
	transaction := dbc.Tx
	if transaction == nil {
		return fmt.Errorf("DeleteMaterialFiles requires a non-nil transaction")
	}
	if len(files) == 0 {
		return nil
	}

	ids := make([]uuid.UUID, 0, len(files))

	for _, mf := range files {
		if mf == nil {
			return fmt.Errorf("DeleteMaterialFiles: encountered nil file")
		}
		if mf.ID == uuid.Nil {
			return fmt.Errorf("DeleteMaterialFiles: material file has no ID")
		}

		ids = append(ids, mf.ID)

		if mf.StorageKey != "" {
			fs.log.Info("Deleting material file from bucket",
				"material_file_id", mf.ID,
				"storage_key", mf.StorageKey,
			)
			if err := fs.bucketService.DeleteFile(dbc, gcp.BucketCategoryMaterial, mf.StorageKey); err != nil {
				fs.log.Error("DeleteFile failed",
					"error", err,
					"material_file_id", mf.ID,
					"storage_key", mf.StorageKey,
				)
				return fmt.Errorf("DeleteMaterialFiles: failed to delete object from bucket: %w", err)
			}
		}
	}

	if err := fs.materialFileRepo.SoftDeleteByIDs(dbc, ids); err != nil {
		fs.log.Error("SoftDeleteByIDs failed for material files", "error", err)
		return fmt.Errorf("DeleteMaterialFiles: failed to soft-delete material files: %w", err)
	}

	return nil
}
