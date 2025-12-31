package node_doc_patch

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/jobs/learning/steps"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	nodeID, ok := jc.PayloadUUID("path_node_id")
	if !ok || nodeID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing path_node_id"))
		return nil
	}

	payload := jc.Payload()
	blockID := strings.TrimSpace(fmt.Sprint(payload["block_id"]))
	blockIndex := parseInt(payload["block_index"], -1)
	action := strings.TrimSpace(fmt.Sprint(payload["action"]))
	instruction := strings.TrimSpace(fmt.Sprint(payload["instruction"]))
	citationPolicy := strings.TrimSpace(fmt.Sprint(payload["citation_policy"]))

	var sel steps.NodeDocPatchSelection
	if raw, ok := payload["selection"].(map[string]any); ok {
		sel.Text = strings.TrimSpace(fmt.Sprint(raw["text"]))
		sel.Start = parseInt(raw["start"], 0)
		sel.End = parseInt(raw["end"], 0)
	}

	jc.Progress("patch", 2, "Patching doc")
	out, err := steps.NodeDocPatch(jc.Ctx, steps.NodeDocPatchDeps{
		DB:        p.db,
		Log:       p.log,
		Path:      p.path,
		PathNodes: p.nodes,
		NodeDocs:  p.docs,
		Figures:   p.figures,
		Videos:    p.videos,
		Revisions: p.revisions,
		Files:     p.files,
		Chunks:    p.chunks,
		ULI:       p.uli,
		Assets:    p.assets,
		AI:        p.ai,
		Vec:       p.vec,
		Bucket:    p.bucket,
	}, steps.NodeDocPatchInput{
		OwnerUserID:    jc.Job.OwnerUserID,
		PathNodeID:     nodeID,
		BlockID:        blockID,
		BlockIndex:     blockIndex,
		Action:         action,
		Instruction:    instruction,
		CitationPolicy: citationPolicy,
		Selection:      sel,
		JobID:          jc.Job.ID,
	})
	if err != nil {
		jc.Fail("patch", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"path_node_id": nodeID.String(),
		"doc_id":       out.DocID.String(),
		"revision_id":  out.RevisionID.String(),
		"block_id":     out.BlockID,
		"block_type":   out.BlockType,
		"action":       out.Action,
	})
	return nil
}

func parseInt(v any, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case float32:
		return int(t)
	default:
		return def
	}
}
