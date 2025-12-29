package services

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type LearningBuildBootstrapService interface {
	EnsurePath(dbc dbctx.Context, userID uuid.UUID, materialSetID uuid.UUID) (uuid.UUID, error)
}

type learningBuildBootstrapService struct {
	db   *gorm.DB
	log  *logger.Logger
	path repos.PathRepo
	uli  repos.UserLibraryIndexRepo
}

func NewLearningBuildBootstrapService(
	db *gorm.DB,
	baseLog *logger.Logger,
	path repos.PathRepo,
	uli repos.UserLibraryIndexRepo,
) LearningBuildBootstrapService {
	return &learningBuildBootstrapService{
		db:   db,
		log:  baseLog.With("service", "LearningBuildBootstrapService"),
		path: path,
		uli:  uli,
	}
}

func (s *learningBuildBootstrapService) EnsurePath(dbc dbctx.Context, userID uuid.UUID, materialSetID uuid.UUID) (uuid.UUID, error) {
	if s == nil || s.path == nil || s.uli == nil {
		return uuid.Nil, fmt.Errorf("bootstrap service not configured")
	}
	if userID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("missing user_id")
	}
	if materialSetID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("missing material_set_id")
	}

	if dbc.Tx != nil {
		return s.ensurePathInTx(dbc, userID, materialSetID)
	}
	if s.db == nil {
		return uuid.Nil, fmt.Errorf("db missing")
	}

	var out uuid.UUID
	err := s.db.WithContext(dbc.Ctx).Transaction(func(txx *gorm.DB) error {
		id, err := s.ensurePathInTx(dbctx.Context{Ctx: dbc.Ctx, Tx: txx}, userID, materialSetID)
		if err != nil {
			return err
		}
		out = id
		return nil
	})
	return out, err
}

func (s *learningBuildBootstrapService) ensurePathInTx(dbc dbctx.Context, userID uuid.UUID, materialSetID uuid.UUID) (uuid.UUID, error) {
	// Lock the index row for (user, material_set) to be race-safe.
	idx, err := s.uli.GetByUserAndMaterialSetForUpdate(dbc, userID, materialSetID)
	if err != nil {
		return uuid.Nil, err
	}
	if idx != nil && idx.PathID != nil && *idx.PathID != uuid.Nil {
		return *idx.PathID, nil
	}

	now := time.Now().UTC()
	path := &types.Path{
		ID:          uuid.New(),
		UserID:      &userID,
		Title:       "Generating path…",
		Description: "",
		Status:      "draft",
		Metadata:    datatypes.JSON([]byte(`{}`)),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := s.path.Create(dbc, []*types.Path{path}); err != nil {
		return uuid.Nil, fmt.Errorf("create path: %w", err)
	}
	if err := s.uli.UpsertPathID(dbc, userID, materialSetID, path.ID); err != nil {
		return uuid.Nil, fmt.Errorf("upsert user_library_index.path_id: %w", err)
	}
	return path.ID, nil
}
