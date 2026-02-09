package node_doc_progressive_build

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	setID, ok := jc.PayloadUUID("material_set_id")
	if !ok || setID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing material_set_id"))
		return nil
	}
	sagaID, _ := jc.PayloadUUID("saga_id")
	pathID, _ := jc.PayloadUUID("path_id")
	anchorID, _ := jc.PayloadUUID("anchor_node_id")
	if anchorID == uuid.Nil {
		anchorID, _ = jc.PayloadUUID("node_id")
	}

	stageCfg := stageConfig(jc.Payload())
	lookahead := intFromAny(stageCfg["lookahead"], 0)
	if lookahead == 0 {
		lookahead = intFromAny(jc.Payload()["lookahead"], 0)
	}

	jc.Progress("docs", 2, "Progressive unit docs")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:                p.db,
		Log:               p.log,
		Path:              p.path,
		PathRuns:          p.pathRuns,
		NodeRuns:          p.nodeRuns,
		PathNodes:         p.nodes,
		NodeDocs:          p.docs,
		DocVariants:       p.docVariants,
		DocSignals:        p.signalSnapshots,
		InterventionPlans: p.interventionPlans,
		Figures:           p.figures,
		Videos:            p.videos,
		GenRuns:           p.genRuns,
		Blueprints:        p.blueprints,
		RetrievalPacks:    p.retrievalPacks,
		DocTraces:         p.docTraces,
		ConstraintReports: p.constraintReports,
		Revisions:         p.revisions,
		Files:             p.files,
		Chunks:            p.chunks,
		UserProfile:       p.userProf,
		TeachingPatterns:  p.patterns,
		Concepts:          p.concepts,
		ConceptState:      p.mastery,
		ConceptModel:      p.model,
		MisconRepo:        p.miscon,
		AI:                p.ai,
		Vec:               p.vec,
		Bucket:            p.bucket,
		Bootstrap:         p.bootstrap,
	}).NodeDocProgressiveBuild(jc.Ctx, learningmod.NodeDocProgressiveBuildInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
		AnchorNodeID:  anchorID,
		Lookahead:     lookahead,
		Report: func(stage string, pct int, message string) {
			jc.Progress(stage, pct, message)
		},
	})
	if err != nil {
		jc.Fail("docs", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id":   setID.String(),
		"saga_id":           sagaID.String(),
		"path_id":           out.PathID.String(),
		"docs_written":      out.DocsWritten,
		"variants_written":  out.VariantsWritten,
		"snapshots_written": out.SnapshotsWritten,
		"lookahead":         out.Lookahead,
	})
	return nil
}

func stageConfig(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	raw, ok := payload["stage_config"]
	if !ok || raw == nil {
		return nil
	}
	if m, ok := raw.(map[string]any); ok {
		return m
	}
	return nil
}

func intFromAny(v any, def int) int {
	switch x := v.(type) {
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case float64:
		return int(x)
	case float32:
		return int(x)
	case string:
		if strings.TrimSpace(x) == "" {
			return def
		}
		if n, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
			return n
		}
	}
	return def
}
