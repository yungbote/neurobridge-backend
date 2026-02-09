package doc_probe_select

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

	nodeIDs := uuidSliceFromAny(jc.Payload()["node_ids"])

	jc.Progress("select", 2, "Selecting doc probes")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:               p.db,
		Log:              p.log,
		Path:             p.path,
		PathRuns:         p.pathRuns,
		PathNodes:        p.nodes,
		NodeDocs:         p.docs,
		DocVariants:      p.docVariants,
		Concepts:         p.concepts,
		ConceptState:     p.conceptState,
		MisconRepo:       p.miscon,
		UserTestletState: p.testlets,
		DocProbes:        p.docProbes,
		Bootstrap:        p.bootstrap,
	}).DocProbeSelect(jc.Ctx, learningmod.DocProbeSelectInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		PathID:        pathID,
		AnchorNodeID:  anchorID,
		Lookahead:     lookahead,
		NodeIDs:       nodeIDs,
	})
	if err != nil {
		jc.Fail("select", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id":   setID.String(),
		"path_id":           out.PathID.String(),
		"lookahead":         out.Lookahead,
		"nodes_considered":  out.NodesConsidered,
		"docs_considered":   out.DocsConsidered,
		"blocks_considered": out.BlocksConsidered,
		"probes_selected":   out.ProbesSelected,
		"docs_updated":      out.DocsUpdated,
		"rate_limited":      out.RateLimited,
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

func uuidSliceFromAny(v any) []uuid.UUID {
	if v == nil {
		return nil
	}
	out := []uuid.UUID{}
	switch arr := v.(type) {
	case []uuid.UUID:
		return arr
	case []string:
		for _, s := range arr {
			if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil && id != uuid.Nil {
				out = append(out, id)
			}
		}
	case []any:
		for _, raw := range arr {
			s := strings.TrimSpace(fmt.Sprint(raw))
			if s == "" {
				continue
			}
			if id, err := uuid.Parse(s); err == nil && id != uuid.Nil {
				out = append(out, id)
			}
		}
	case string:
		s := strings.TrimSpace(arr)
		if s != "" {
			parts := strings.Split(s, ",")
			for _, p := range parts {
				if id, err := uuid.Parse(strings.TrimSpace(p)); err == nil && id != uuid.Nil {
					out = append(out, id)
				}
			}
		}
	}
	return out
}
