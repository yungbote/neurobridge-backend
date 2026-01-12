//go:build ignore
// +build ignore

package material_set_summarize

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"sort"
	"strings"
	"time"
)

type Deps struct {
	DB    *gorm.DB
	Log   *logger.Logger
	Sets  repos.MaterialSetRepo
	Files repos.MaterialFileRepo
	Sums  repos.MaterialSetSummaryRepo
	AI    openai.Client
	Vec   pinecone.VectorStore
}

type Input struct {
	UserID        uuid.UUID
	MaterialSetID uuid.UUID
}

func RunStage(ctx context.Context, deps Deps, in Input) (*types.MaterialSetSummary, error) {
	if deps.DB == nil || deps.Log == nil || deps.AI == nil {
		return nil, fmt.Errorf("deps missing DB/Log/AI")
	}
	if in.UserID == uuid.Nil || in.MaterialSetID == uuid.Nil {
		return nil, fmt.Errorf("missing ids")
	}
	if deps.Sums != nil {
		ex, _ := deps.Sums.GetByMaterialSetIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{in.MaterialSetID})
		if len(ex) > 0 && ex[0] != nil && strings.TrimSpace(ex[0].SummaryMD) != "" {
			return ex[0], nil
		}
	}
	files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, in.MaterialSetID)
	if err != nil {
		return nil, err
	}
	fileIDs := make([]uuid.UUID, 0, len(files))
	for _, f := range files {
		if f != nil {
			fileIDs = append(fileIDs, f.ID)
		}
	}
	chunks, err := deps.Chunks.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("no chunks for material set")
	}
	excerpt := stratifiedExcerpts(chunks, 12, 700)
	if strings.TrimSpace(excerpt) == "" {
		return nil, fmt.Errorf("empty excerpt")
	}
	p, err := prompts.Build(prompts.PromptMaterialSetSummary, prompts.Input{
		BundleExcerpt: excerpt,
	})
	if err != nil {
		return nil, err
	}
	obj, err := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema)
	if err != nil {
		return nil, err
	}
	subject := strings.TrimSpace(fmt.Sprint(obj["subject"]))
	level := strings.TrimSpace(fmt.Sprint(obj["level"]))
	summaryMD := strings.TrimSpace(fmt.Sprint(obj["summary_md"]))
	tags := toStringSlice(obj["tags"])
	conceptKeys := toStringSlice(obj["concept_keys"])
	vecDoc := strings.TrimSpace(summaryMD)
	if vecDoc == "" {
		vecDoc = strings.TrimSpace(subhect + " " + level)
	}
	emb, err := deps.AI.Embed(ctx, []string{vecDoc})
	if err != nil {
		return nil, err
	}
	var embJSON datatypes.JSON
	if len(emb) > 0 {
		b, _ := json.Marshal(emb[0])
		embJSON = datatypes.JSON(b)
	}
	row := &types.MaterialSetSummary{
		ID:            uuid.New(),
		MaterialSetID: in.MaterialSetID,
		UserID:        in.UserID,
		Subject:       subject,
		Level:         level,
		SummaryMD:     summaryMD,
		Tags:          datatypes.JSON(mustJSON(tags)),
		ConceptKeys:   datatypes.JSON(mustJSON(conceptKeys)),
		Embedding:     embJSON,
		VectorID:      "material_set_summary:" + in.MaterialSetID.String(),
		UpdatedAt:     time.Now().UTC(),
	}
	if deps.Sums == nil {
		return nil, fmt.Errorf("summaries repo missing")
	}
	if err := deps.Sums.UpsertByMaterialSetID(dbctx.Context{Ctx: ctx, Tx: deps.DB}, row); err != nil {
		return nil, err
	}
	if deps.Vec != nil && len(emb) > 0 && emb[0] != nil {
		ns := index.MaterialSetSummariesNamespace(in.UserID)
		_ = deps.Vec.Upsert(ctx, ns, []pinecone.Vector{
			{
				ID:     row.VectorID,
				Values: emb[0],
				Metadata: map[string]any{
					"type":            "material_set_summary",
					"user_id":         in.UserID.String(),
					"material_set_id": in.MaterialSetID.String(),
					"subject":         subject,
					"level":           level,
				},
			},
		})
	}
	return row, nil
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func toStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	if ss, ok := v.([]string); ok {
		return ss
	}
	if arr, ok := v.([]any); !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		s := strings.TrimSpace(fmt.Sprint(x))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func stratifiedExcerpts(chunks []*types.MaterialChunk, perFile int, maxChars int) string {
	if perFile <= 0 {
		perFile = 10
	}
	if maxChars <= 0 {
		maxChars = 600
	}
	byFile := map[uuid.UUID][]*types.MaterialChunk{}
	for _, ch := range chunks {
		if ch == nil || ch.MaterialFileID == uuid.Nil {
			continue
		}
		t := strings.TrimSpace(ch.Text)
		if t == "" {
			continue
		}
		byFile[ch.MaterialFileID] = append(byFile[ch.MaterialFileID], ch)
	}
	fileIDs := make([]uuid.UUID, 0, len(byFile))
	for fid := range byFile {
		fileIDs = append(fileIDs, fid)
	}
	sort.Slice(fileIDs, func(i, j int) bool { return fileIDs[i].String() < fileIDs[j].String() })
	var b strings.Builder
	for _, fid := range fileIDs {
		arr := byFile[fid]
		sort.Slice(arr, func(i, j int) bool { return arr[i].Index < arr[j].Index })
		n := len(arr)
		if n == 0 {
			continue
		}
		k := perFile
		if k > n {
			k = n
		}
		step := float64(n) / float64(k)
		for i := 0; i < k; i++ {
			idx := int(float64(i) * step)
			if idx < 0 {
				idx = 0
			}
			if idx >= n {
				idx = n - 1
			}
			ch := arr[idx]
			txt := strings.TrimSpace(ch.Text)
			if len(txt) > maxChars {
				txt = txt[:maxChars] + "..."
			}
			b.WriteString(fmt.Sprintf("[chunk_id=%s] %s\n", ch.ID.String(), txt))
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
