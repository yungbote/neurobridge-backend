package node_doc_prefetch

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

	stageCfg := stageConfig(jc.Payload())
	nodeLimit := intFromAny(stageCfg["node_limit"], 0)
	nodeSelect := strings.TrimSpace(fmt.Sprint(stageCfg["node_select_mode"]))
	if nodeLimit == 0 {
		nodeLimit = intFromAny(jc.Payload()["node_limit"], 0)
	}
	if nodeSelect == "" {
		nodeSelect = strings.TrimSpace(fmt.Sprint(jc.Payload()["node_select_mode"]))
	}

	jc.Progress("docs", 2, "Prefetching unit docs")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:                p.db,
		Log:               p.log,
		Path:              p.path,
		PathNodes:         p.nodes,
		NodeDocs:          p.docs,
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
	}).NodeDocPrefetch(jc.Ctx, learningmod.NodeDocPrefetchInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
		NodeLimit:     nodeLimit,
		NodeSelect:    nodeSelect,
		Report: func(stage string, pct int, message string) {
			jc.Progress(stage, pct, message)
		},
	})
	if err != nil {
		jc.Fail("docs", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
		"docs_written":    out.DocsWritten,
		"docs_existing":   out.DocsExisting,
		"lookahead":       out.Lookahead,
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

