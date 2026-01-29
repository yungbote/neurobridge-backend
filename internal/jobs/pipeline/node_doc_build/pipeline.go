package node_doc_build

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
	mediaPatch := false
	nodeLimit := 0
	nodeSelect := ""
	markPending := false
	if stageCfg != nil {
		mediaPatch = boolFromAny(stageCfg["media_patch"])
		nodeLimit = intFromAny(stageCfg["node_limit"], 0)
		nodeSelect = strings.TrimSpace(fmt.Sprint(stageCfg["node_select_mode"]))
		markPending = boolFromAny(stageCfg["mark_remaining_pending"])
	}

	jc.Progress("docs", 2, "Writing unit docs")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:               p.db,
		Log:              p.log,
		Path:             p.path,
		PathNodes:        p.nodes,
		NodeDocs:         p.docs,
		Figures:          p.figures,
		Videos:           p.videos,
		GenRuns:          p.genRuns,
		Files:            p.files,
		Chunks:           p.chunks,
		UserProfile:      p.userProf,
		TeachingPatterns: p.patterns,
		Concepts:         p.concepts,
		ConceptState:     p.mastery,
		AI:               p.ai,
		Vec:              p.vec,
		Bucket:           p.bucket,
		Bootstrap:        p.bootstrap,
	}).NodeDocBuild(jc.Ctx, learningmod.NodeDocBuildInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
		MediaPatch:    mediaPatch,
		NodeLimit:     nodeLimit,
		NodeSelect:    nodeSelect,
		MarkPending:   markPending,
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

func boolFromAny(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "true" || s == "1" || s == "yes" || s == "y"
	default:
		s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		return s == "true" || s == "1" || s == "yes" || s == "y"
	}
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
