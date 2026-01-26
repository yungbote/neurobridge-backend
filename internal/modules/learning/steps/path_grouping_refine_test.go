package steps

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func TestPathGroupingRefine_MergesSimilarPaths(t *testing.T) {
	t.Setenv("NB_INFERENCE_SCORE_MODEL", "")
	t.Setenv("PATH_GROUPING_MIN_CONFIDENCE_MERGE", "0.6")
	t.Setenv("PATH_GROUPING_MIN_CONFIDENCE_SPLIT", "0.55")
	t.Setenv("PATH_GROUPING_BRIDGE_STRONG", "0.7")

	db := testutil.DB(t)
	tx := testutil.Tx(t, db)
	log := testutil.Logger(t)
	ctx := context.Background()
	repoCtx := dbctx.Context{Ctx: ctx, Tx: tx}

	user := testutil.SeedUser(t, repoCtx, "grouping@example.com")
	set := testutil.SeedMaterialSet(t, repoCtx, user.ID)
	fileA := testutil.SeedMaterialFile(t, repoCtx, set.ID, "file_a")
	fileB := testutil.SeedMaterialFile(t, repoCtx, set.ID, "file_b")

	emb := []float32{1, 0, 0}
	sigRepo := repos.NewMaterialFileSignatureRepo(db, log)
	if err := sigRepo.UpsertByMaterialFileID(repoCtx, &types.MaterialFileSignature{
		MaterialFileID:   fileA.ID,
		MaterialSetID:    set.ID,
		SummaryEmbedding: mustJSON(emb),
	}); err != nil {
		t.Fatalf("upsert sig A: %v", err)
	}
	if err := sigRepo.UpsertByMaterialFileID(repoCtx, &types.MaterialFileSignature{
		MaterialFileID:   fileB.ID,
		MaterialSetID:    set.ID,
		SummaryEmbedding: mustJSON(emb),
	}); err != nil {
		t.Fatalf("upsert sig B: %v", err)
	}

	intake := map[string]any{
		"combined_goal": "Learn related materials",
		"confidence":    0.4,
		"paths": []any{
			map[string]any{
				"path_id":          "p1",
				"title":            "File A",
				"goal":             "Learn A",
				"core_file_ids":    []string{fileA.ID.String()},
				"support_file_ids": []string{},
				"confidence":       0.4,
				"notes":            "",
			},
			map[string]any{
				"path_id":          "p2",
				"title":            "File B",
				"goal":             "Learn B",
				"core_file_ids":    []string{fileB.ID.String()},
				"support_file_ids": []string{},
				"confidence":       0.4,
				"notes":            "",
			},
		},
	}
	meta := map[string]any{"intake": intake}

	path := &types.Path{
		ID:            uuid.New(),
		UserID:        &user.ID,
		MaterialSetID: &set.ID,
		Title:         "Root",
		Status:        "draft",
		Metadata:      datatypes.JSON(mustJSON(meta)),
	}
	if err := tx.WithContext(ctx).Create(path).Error; err != nil {
		t.Fatalf("create path: %v", err)
	}

	deps := PathGroupingRefineDeps{
		DB:       db,
		Log:      log,
		Path:     repos.NewPathRepo(db, log),
		Files:    repos.NewMaterialFileRepo(db, log),
		FileSigs: sigRepo,
	}
	out, err := PathGroupingRefine(ctx, deps, PathGroupingRefineInput{
		OwnerUserID:   user.ID,
		MaterialSetID: set.ID,
		PathID:        path.ID,
	})
	if err != nil {
		t.Fatalf("PathGroupingRefine: %v", err)
	}
	if out.Status != "refined" {
		t.Fatalf("expected refined status, got %q", out.Status)
	}

	updated, err := deps.Path.GetByID(repoCtx, path.ID)
	if err != nil {
		t.Fatalf("get path: %v", err)
	}
	var updatedMeta map[string]any
	if err := json.Unmarshal(updated.Metadata, &updatedMeta); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	intakeAny := mapFromAny(updatedMeta["intake"])
	paths := sliceAny(intakeAny["paths"])
	if len(paths) != 1 {
		t.Fatalf("expected 1 path after merge, got %d", len(paths))
	}
	pathMap, ok := paths[0].(map[string]any)
	if !ok || pathMap == nil {
		t.Fatalf("expected path map")
	}
	gotIDs := stringSliceFromAny(pathMap["core_file_ids"])
	if !stringSliceHasAll(gotIDs, []string{fileA.ID.String(), fileB.ID.String()}) {
		t.Fatalf("expected merged core_file_ids, got %v", gotIDs)
	}
}

func stringSliceHasAll(got []string, want []string) bool {
	seen := map[string]bool{}
	for _, v := range got {
		seen[strings.TrimSpace(v)] = true
	}
	for _, w := range want {
		if !seen[strings.TrimSpace(w)] {
			return false
		}
	}
	return true
}
