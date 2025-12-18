package services

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type MaterialService interface {
	CreateMaterialSet(ctx context.Context, tx *gorm.DB, userID uuid.UUID) (*types.MaterialSet, error)
	AddMaterialFile(ctx context.Context, tx *gorm.DB, setID uuid.UUID, originalName, mimeType string, sizeBytes int64) (*types.MaterialFile, error)
	AddMaterialFiles(ctx context.Context, tx *gorm.DB, setID uuid.UUID, inputs []MaterialFileInput) ([]*types.MaterialFile, error)
	UploadMaterialFiles(ctx context.Context, tx *gorm.DB, userID uuid.UUID, files []UploadedFileInfo) (*types.MaterialSet, []*types.MaterialFile, error)
}

type UploadedFileInfo struct {
	OriginalName string
	MimeType     string
	SizeBytes    int64
	Reader       io.Reader
}

type MaterialFileInput struct {
	OriginalName string
	MimeType     string
	SizeBytes    int64
}

type materialService struct {
	db               *gorm.DB
	log              *logger.Logger
	materialSetRepo  repos.MaterialSetRepo
	materialFileRepo repos.MaterialFileRepo
	fileService      FileService
}

func NewMaterialService(
	db *gorm.DB,
	baseLog *logger.Logger,
	materialSetRepo repos.MaterialSetRepo,
	materialFileRepo repos.MaterialFileRepo,
	fileService FileService,
) MaterialService {
	serviceLog := baseLog.With("service", "MaterialService")
	return &materialService{
		db:               db,
		log:              serviceLog,
		materialSetRepo:  materialSetRepo,
		materialFileRepo: materialFileRepo,
		fileService:      fileService,
	}
}

// =====================================
// Core DB ops
// =====================================

func (ms *materialService) CreateMaterialSet(ctx context.Context, tx *gorm.DB, userID uuid.UUID) (*types.MaterialSet, error) {
	transaction := tx
	if transaction == nil {
		transaction = ms.db
	}
	ms.log.Info("CreateMaterialSet", "user_id", userID)
	set := &types.MaterialSet{
		ID:        uuid.New(),
		UserID:    userID,
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if _, err := ms.materialSetRepo.Create(ctx, transaction, []*types.MaterialSet{set}); err != nil {
		ms.log.Error("CreateMaterialSet failed", "error", err)
		return nil, fmt.Errorf("create material set: %w", err)
	}
	return set, nil
}

func (ms *materialService) AddMaterialFile(ctx context.Context, tx *gorm.DB, setID uuid.UUID, originalName, mimeType string, sizeBytes int64) (*types.MaterialFile, error) {
	inputs := []MaterialFileInput{{
		OriginalName: originalName,
		MimeType:     mimeType,
		SizeBytes:    sizeBytes,
	}}
	files, err := ms.AddMaterialFiles(ctx, tx, setID, inputs)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no material file created")
	}
	return files[0], nil
}

func (ms *materialService) AddMaterialFiles(ctx context.Context, tx *gorm.DB, setID uuid.UUID, inputs []MaterialFileInput) ([]*types.MaterialFile, error) {
	transaction := tx
	if transaction == nil {
		transaction = ms.db
	}
	ms.log.Info("AddMaterialFiles", "material_set_id", setID)
	if len(inputs) == 0 {
		return []*types.MaterialFile{}, nil
	}
	now := time.Now()
	files := make([]*types.MaterialFile, len(inputs))
	for i, in := range inputs {
		fileID := uuid.New()
		storageKey := fmt.Sprintf("materials/%s/%s", setID.String(), fileID.String())
		files[i] = &types.MaterialFile{
			ID:            fileID,
			MaterialSetID: setID,
			OriginalName:  in.OriginalName,
			MimeType:      in.MimeType,
			SizeBytes:     in.SizeBytes,
			StorageKey:    storageKey,
			Status:        "pending_upload",
			CreatedAt:     now,
			UpdatedAt:     now,
		}
	}
	created, err := ms.materialFileRepo.Create(ctx, transaction, files)
	if err != nil {
		ms.log.Error("AddMaterialFiles failed", "error", err)
		return nil, fmt.Errorf("add material files: %w", err)
	}
	return created, nil
}

// =====================================
// UploadMaterialFiles
// =====================================

func (ms *materialService) UploadMaterialFiles(
	ctx context.Context,
	tx *gorm.DB,
	userID uuid.UUID,
	uploaded []UploadedFileInfo,
) (*types.MaterialSet, []*types.MaterialFile, error) {
	if len(uploaded) == 0 {
		return nil, nil, fmt.Errorf("no files provided")
	}

	transaction := tx
	createdTx := false
	if transaction == nil {
		createdTx = true
		transaction = ms.db.Begin()
		if transaction.Error != nil {
			return nil, nil, fmt.Errorf("failed to begin transaction: %w", transaction.Error)
		}
	}

	var (
		set   *types.MaterialSet
		files []*types.MaterialFile
		err   error
	)

	defer func() {
		if !createdTx {
			return
		}
		if err != nil {
			transaction.Rollback()
		} else {
			_ = transaction.Commit().Error
		}
	}()

	// 1) MaterialSet
	set, err = ms.CreateMaterialSet(ctx, transaction, userID)
	if err != nil {
		return nil, nil, err
	}

	// 2) MaterialFile rows
	inputs := make([]MaterialFileInput, len(uploaded))
	for i, uf := range uploaded {
		inputs[i] = MaterialFileInput{
			OriginalName: uf.OriginalName,
			MimeType:     uf.MimeType,
			SizeBytes:    uf.SizeBytes,
		}
	}

	files, err = ms.AddMaterialFiles(ctx, transaction, set.ID, inputs)
	if err != nil {
		return nil, nil, err
	}

	if len(files) != len(uploaded) {
		return nil, nil, fmt.Errorf("mismatch between created files and upload inputs")
	}

	// 3) Upload to bucket and update MaterialFiles
	readers := make([]io.Reader, len(uploaded))
	for i := range uploaded {
		readers[i] = uploaded[i].Reader
	}

	if err = ms.fileService.UploadMaterialFiles(ctx, transaction, files, readers); err != nil {
		return nil, nil, err
	}

	return set, files, nil
}
