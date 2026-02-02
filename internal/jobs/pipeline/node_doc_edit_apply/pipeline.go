package node_doc_edit_apply

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type proposalPayload struct {
	PathNodeID      string          `json:"path_node_id"`
	PathID          string          `json:"path_id"`
	DocID           string          `json:"doc_id"`
	BlockID         string          `json:"block_id"`
	BlockType       string          `json:"block_type"`
	Action          string          `json:"action"`
	CitationPolicy  string          `json:"citation_policy"`
	Instruction     string          `json:"instruction"`
	BeforeBlock     json.RawMessage `json:"before_block"`
	AfterBlock      json.RawMessage `json:"after_block"`
	BeforeBlockText string          `json:"before_block_text"`
	AfterBlockText  string          `json:"after_block_text"`
	Model           string          `json:"model"`
	PromptVersion   string          `json:"prompt_version"`
}

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	if p.db == nil || p.jobs == nil || p.nodes == nil || p.docs == nil || p.revisions == nil {
		jc.Fail("validate", fmt.Errorf("node_doc_edit_apply: missing deps"))
		return nil
	}

	proposalJobID, ok := jc.PayloadUUID("proposal_job_id")
	if !ok || proposalJobID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing proposal_job_id"))
		return nil
	}
	decision := strings.ToLower(strings.TrimSpace(stringFromAny(jc.Payload()["decision"])))
	if decision == "" {
		decision = "confirm"
	}

	rows, err := p.jobs.GetByIDs(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, []uuid.UUID{proposalJobID})
	if err != nil || len(rows) == 0 || rows[0] == nil {
		jc.Fail("load", fmt.Errorf("proposal job not found"))
		return nil
	}
	proposalJob := rows[0]
	if proposalJob.OwnerUserID != jc.Job.OwnerUserID {
		jc.Fail("load", fmt.Errorf("proposal job owner mismatch"))
		return nil
	}

	env := parseWaitpointEnvelope(proposalJob.Result)
	if env == nil || env.Data == nil {
		jc.Fail("load", fmt.Errorf("proposal missing waitpoint data"))
		return nil
	}
	rawProposal := env.Data["proposal"]
	b, _ := json.Marshal(rawProposal)
	var prop proposalPayload
	_ = json.Unmarshal(b, &prop)
	blockID := strings.TrimSpace(prop.BlockID)
	if blockID == "" {
		jc.Fail("validate", fmt.Errorf("proposal missing block_id"))
		return nil
	}
	if decision == "deny" {
		_ = p.markProposalResolved(jc, proposalJobID, "rejected")
		_ = p.clearThreadPendingWaitpoint(jc, env)
		jc.Succeed("done", map[string]any{"status": "rejected"})
		return nil
	}

	pathNodeID, err := uuid.Parse(strings.TrimSpace(prop.PathNodeID))
	if err != nil || pathNodeID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("invalid path_node_id"))
		return nil
	}

	node, err := p.nodes.GetByID(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, pathNodeID)
	if err != nil || node == nil {
		jc.Fail("load", fmt.Errorf("node not found"))
		return nil
	}

	docRow, err := p.docs.GetByPathNodeID(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, pathNodeID)
	if err != nil || docRow == nil || len(docRow.DocJSON) == 0 || string(docRow.DocJSON) == "null" {
		jc.Fail("load", fmt.Errorf("doc not found"))
		return nil
	}

	var docObj map[string]any
	if err := json.Unmarshal(docRow.DocJSON, &docObj); err != nil || docObj == nil {
		jc.Fail("load", fmt.Errorf("doc invalid json"))
		return nil
	}
	blocksAny, _ := docObj["blocks"].([]any)
	blocks := make([]map[string]any, 0, len(blocksAny))
	for _, raw := range blocksAny {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		blocks = append(blocks, m)
	}
	idx := -1
	for i, b := range blocks {
		if strings.TrimSpace(stringFromAny(b["id"])) == blockID {
			idx = i
			break
		}
	}
	if idx < 0 {
		jc.Fail("validate", fmt.Errorf("block not found"))
		return nil
	}

	// Guard: ensure the block hasn't changed since proposal.
	currentBlockJSON, _ := json.Marshal(blocks[idx])
	if len(prop.BeforeBlock) > 0 && string(prop.BeforeBlock) != "null" {
		if !jsonEqual(currentBlockJSON, prop.BeforeBlock) {
			jc.Fail("validate", fmt.Errorf("block changed since proposal"))
			return nil
		}
	}

	var updatedBlock map[string]any
	if err := json.Unmarshal(prop.AfterBlock, &updatedBlock); err != nil || updatedBlock == nil {
		jc.Fail("validate", fmt.Errorf("invalid proposed block"))
		return nil
	}
	blocks[idx] = updatedBlock
	docObj["blocks"] = blocks

	// Validate document.
	canonBytes, _ := json.Marshal(docObj)
	canon, err := content.CanonicalizeJSON(canonBytes)
	if err != nil {
		jc.Fail("apply", err)
		return nil
	}

	var updatedDoc content.NodeDocV1
	_ = json.Unmarshal(canon, &updatedDoc)
	reqs := content.NodeDocRequirements{}
	docAllowed := map[string]bool{}
	for _, id := range content.CitedChunkIDsFromNodeDocV1(updatedDoc) {
		if id != "" {
			docAllowed[id] = true
		}
	}
	if errs, _ := content.ValidateNodeDocV1(updatedDoc, docAllowed, reqs); len(errs) > 0 {
		jc.Fail("apply", fmt.Errorf("validation failed: %s", strings.Join(errs, "; ")))
		return nil
	}

	now := time.Now().UTC()
	contentHash := content.HashBytes(canon)
	sourcesHash := content.HashSources(strings.TrimSpace(prop.PromptVersion), 1, content.CitedChunkIDsFromNodeDocV1(updatedDoc))
	docText, _ := content.NodeDocMetrics(updatedDoc)["doc_text"].(string)
	docText = content.SanitizeStringForPostgres(docText)

	updatedRow := &types.LearningNodeDoc{
		ID:            docRow.ID,
		UserID:        jc.Job.OwnerUserID,
		PathID:        node.PathID,
		PathNodeID:    node.ID,
		SchemaVersion: 1,
		DocJSON:       datatypes.JSON(canon),
		DocText:       docText,
		ContentHash:   contentHash,
		SourcesHash:   sourcesHash,
		CreatedAt:     docRow.CreatedAt,
		UpdatedAt:     now,
	}

	revision := &types.LearningNodeDocRevision{
		ID:             uuid.New(),
		DocID:          updatedRow.ID,
		UserID:         jc.Job.OwnerUserID,
		PathID:         node.PathID,
		PathNodeID:     node.ID,
		BlockID:        blockID,
		BlockType:      strings.TrimSpace(prop.BlockType),
		Operation:      strings.TrimSpace(prop.Action),
		CitationPolicy: strings.TrimSpace(prop.CitationPolicy),
		Instruction:    strings.TrimSpace(prop.Instruction),
		Selection:      datatypes.JSON([]byte(`null`)),
		BeforeJSON:     datatypes.JSON(currentBlockJSON),
		AfterJSON:      datatypes.JSON(prop.AfterBlock),
		Status:         "succeeded",
		Error:          "",
		Model:          strings.TrimSpace(prop.Model),
		PromptVersion:  strings.TrimSpace(prop.PromptVersion),
		TokensIn:       0,
		TokensOut:      0,
		CreatedAt:      now,
	}

	err = p.db.WithContext(jc.Ctx).Transaction(func(tx *gorm.DB) error {
		inner := dbctx.Context{Ctx: jc.Ctx, Tx: tx}
		if err := p.docs.Upsert(inner, updatedRow); err != nil {
			return err
		}
		if _, err := p.revisions.Create(inner, []*types.LearningNodeDocRevision{revision}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		jc.Fail("apply", err)
		return nil
	}

	_ = p.markProposalResolved(jc, proposalJobID, "applied")
	_ = p.clearThreadPendingWaitpoint(jc, env)

	if p.jobSvc != nil {
		entityID := node.PathID
		payload := map[string]any{
			"path_id":      node.PathID.String(),
			"path_node_id": node.ID.String(),
		}
		if _, err := p.jobSvc.Enqueue(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, jc.Job.OwnerUserID, "chat_path_node_index", "path_node", &entityID, payload); err != nil {
			p.log.Warn("node_doc_edit_apply: enqueue chat_path_node_index failed", "error", err, "path_id", node.PathID.String(), "path_node_id", node.ID.String())
		}
	}

	jc.Succeed("done", map[string]any{
		"status":       "applied",
		"path_node_id": node.ID.String(),
		"block_id":     blockID,
	})
	return nil
}

func (p *Pipeline) markProposalResolved(jc *jobrt.Context, jobID uuid.UUID, status string) error {
	if p.jobs == nil || jobID == uuid.Nil {
		return nil
	}
	return p.jobs.UpdateFields(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, jobID, map[string]interface{}{
		"status":     "succeeded",
		"stage":      "resolved",
		"message":    fmt.Sprintf("node_doc_edit %s", status),
		"updated_at": time.Now().UTC(),
	})
}

func (p *Pipeline) clearThreadPendingWaitpoint(jc *jobrt.Context, env *jobrt.WaitpointEnvelope) error {
	if p.threads == nil || env == nil || env.Waitpoint.ThreadID == "" {
		return nil
	}
	threadID, err := uuid.Parse(strings.TrimSpace(env.Waitpoint.ThreadID))
	if err != nil || threadID == uuid.Nil {
		return nil
	}
	var meta map[string]any
	if err := p.db.WithContext(jc.Ctx).Model(&types.ChatThread{}).Select("metadata").Where("id = ?", threadID).Scan(&meta).Error; err != nil {
		return err
	}
	if meta == nil {
		return nil
	}
	delete(meta, "pending_waitpoint_job_id")
	delete(meta, "pending_waitpoint_kind")
	delete(meta, "pending_waitpoint_proposal")
	metaJSON, _ := json.Marshal(meta)
	return p.threads.UpdateFields(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, threadID, map[string]interface{}{"metadata": datatypes.JSON(metaJSON)})
}

func parseWaitpointEnvelope(raw datatypes.JSON) *jobrt.WaitpointEnvelope {
	if len(raw) == 0 {
		return nil
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return nil
	}
	var env jobrt.WaitpointEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil
	}
	return &env
}

func jsonEqual(a []byte, b []byte) bool {
	var o1 any
	var o2 any
	if err := json.Unmarshal(a, &o1); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &o2); err != nil {
		return false
	}
	return reflect.DeepEqual(o1, o2)
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []byte:
		return strings.TrimSpace(string(t))
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}
