package steps

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	repolearning "github.com/yungbote/neurobridge-backend/internal/data/repos/learning"
	repomaterials "github.com/yungbote/neurobridge-backend/internal/data/repos/materials"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type stubOpenAI struct{}

func (s *stubOpenAI) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	_ = ctx
	out := make([][]float32, len(inputs))
	for i := range inputs {
		out[i] = []float32{0.1, 0.2, 0.3}
	}
	return out, nil
}

func (s *stubOpenAI) GenerateJSON(ctx context.Context, system string, user string, schemaName string, schema map[string]any) (map[string]any, error) {
	_ = ctx
	_ = system
	_ = user
	_ = schemaName
	_ = schema
	return map[string]any{
		"summary_md":        "Test summary for file signature.",
		"topics":            []string{"test_topic"},
		"concept_keys":      []string{"test_topic"},
		"difficulty":        "intro",
		"domain_tags":       []string{"test"},
		"citations":         []string{},
		"outline_json":      map[string]any{"title": "Doc", "sections": []map[string]any{{"title": "Intro", "path": "1"}}},
		"outline_confidence": 0.6,
		"language":          "en",
		"quality":           map[string]any{"text_quality": "high", "coverage": 0.7},
	}, nil
}

func (s *stubOpenAI) GenerateText(ctx context.Context, system string, user string) (string, error) {
	_ = ctx
	_ = system
	_ = user
	return "", errors.New("not implemented")
}

func (s *stubOpenAI) GenerateTextWithImages(ctx context.Context, system string, user string, images []openai.ImageInput) (string, error) {
	_ = ctx
	_ = system
	_ = user
	_ = images
	return "", errors.New("not implemented")
}

func (s *stubOpenAI) GenerateImage(ctx context.Context, prompt string) (openai.ImageGeneration, error) {
	_ = ctx
	_ = prompt
	return openai.ImageGeneration{}, errors.New("not implemented")
}

func (s *stubOpenAI) GenerateVideo(ctx context.Context, prompt string, opts openai.VideoGenerationOptions) (openai.VideoGeneration, error) {
	_ = ctx
	_ = prompt
	_ = opts
	return openai.VideoGeneration{}, errors.New("not implemented")
}

func (s *stubOpenAI) StreamText(ctx context.Context, system string, user string, onDelta func(delta string)) (string, error) {
	_ = ctx
	_ = system
	_ = user
	_ = onDelta
	return "", errors.New("not implemented")
}

func (s *stubOpenAI) CreateConversation(ctx context.Context) (string, error) {
	_ = ctx
	return "", errors.New("not implemented")
}

func (s *stubOpenAI) GenerateTextInConversation(ctx context.Context, conversationID string, instructions string, user string) (string, error) {
	_ = ctx
	_ = conversationID
	_ = instructions
	_ = user
	return "", errors.New("not implemented")
}

func (s *stubOpenAI) StreamTextInConversation(ctx context.Context, conversationID string, instructions string, user string, onDelta func(delta string)) (string, error) {
	_ = ctx
	_ = conversationID
	_ = instructions
	_ = user
	_ = onDelta
	return "", errors.New("not implemented")
}

type noopSaga struct{}

func (n *noopSaga) CreateOrGetSaga(ctx context.Context, ownerUserID uuid.UUID, rootJobID uuid.UUID) (uuid.UUID, error) {
	_ = ctx
	_ = ownerUserID
	_ = rootJobID
	return uuid.New(), nil
}
func (n *noopSaga) AppendAction(dbc dbctx.Context, sagaID uuid.UUID, kind string, payload map[string]any) error {
	_ = dbc
	_ = sagaID
	_ = kind
	_ = payload
	return nil
}
func (n *noopSaga) Compensate(ctx context.Context, sagaID uuid.UUID) error {
	_ = ctx
	_ = sagaID
	return nil
}
func (n *noopSaga) MarkSagaStatus(ctx context.Context, sagaID uuid.UUID, status string) error {
	_ = ctx
	_ = sagaID
	_ = status
	return nil
}

func TestFileSignatureBuildPersistsSignatureAndSections(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)
	log := testutil.Logger(t)

	user := &types.User{
		ID:        uuid.New(),
		Email:     "sigtest@example.com",
		Password:  "pw",
		FirstName: "Sig",
		LastName:  "Test",
	}
	if err := tx.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	set := &types.MaterialSet{
		ID:     uuid.New(),
		UserID: user.ID,
		Title:  "Test Set",
		Status: "ready",
	}
	if err := tx.Create(set).Error; err != nil {
		t.Fatalf("create set: %v", err)
	}

	file := &types.MaterialFile{
		ID:            uuid.New(),
		MaterialSetID: set.ID,
		OriginalName:  "test.pdf",
		StorageKey:    "test/key.pdf",
		Status:        "uploaded",
		ExtractedKind: "pdf",
	}
	if err := tx.Create(file).Error; err != nil {
		t.Fatalf("create file: %v", err)
	}

	chunk := &types.MaterialChunk{
		ID:             uuid.New(),
		MaterialFileID: file.ID,
		Index:          0,
		Text:           "Intro\nHeading One\nBody content.",
		Embedding:      datatypes.JSON(nil),
	}
	if err := tx.Create(chunk).Error; err != nil {
		t.Fatalf("create chunk: %v", err)
	}

	filesRepo := repomaterials.NewMaterialFileRepo(tx, log)
	chunkRepo := repomaterials.NewMaterialChunkRepo(tx, log)
	sigRepo := repomaterials.NewMaterialFileSignatureRepo(tx, log)
	secRepo := repomaterials.NewMaterialFileSectionRepo(tx, log)
	pathRepo := repolearning.NewPathRepo(tx, log)
	uliRepo := repolearning.NewUserLibraryIndexRepo(tx, log)
	bootstrap := services.NewLearningBuildBootstrapService(tx, log, pathRepo, uliRepo)

	deps := FileSignatureBuildDeps{
		DB:           tx,
		Log:          log,
		Files:        filesRepo,
		FileSigs:     sigRepo,
		FileSections: secRepo,
		Chunks:       chunkRepo,
		AI:           &stubOpenAI{},
		Vec:          nil,
		Saga:         &noopSaga{},
		Bootstrap:    bootstrap,
	}

	out, err := FileSignatureBuild(context.Background(), deps, FileSignatureBuildInput{
		OwnerUserID:   user.ID,
		MaterialSetID: set.ID,
		SagaID:        uuid.New(),
		PathID:        uuid.Nil,
	})
	if err != nil {
		t.Fatalf("FileSignatureBuild error: %v", err)
	}
	if out.SignaturesUpserted != 1 {
		t.Fatalf("expected 1 signature upserted, got %d", out.SignaturesUpserted)
	}

	sigs, err := sigRepo.GetByMaterialFileIDs(dbctx.Context{Ctx: context.Background(), Tx: tx}, []uuid.UUID{file.ID})
	if err != nil {
		t.Fatalf("load signatures: %v", err)
	}
	if len(sigs) != 1 {
		t.Fatalf("expected 1 signature, got %d", len(sigs))
	}

	sections, err := secRepo.GetByMaterialFileIDs(dbctx.Context{Ctx: context.Background(), Tx: tx}, []uuid.UUID{file.ID})
	if err != nil {
		t.Fatalf("load sections: %v", err)
	}
	if len(sections) == 0 {
		t.Fatalf("expected sections to be created")
	}
}
