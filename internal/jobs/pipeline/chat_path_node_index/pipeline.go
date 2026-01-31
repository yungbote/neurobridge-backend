package chat_path_node_index

import (
	"fmt"

	"github.com/google/uuid"

	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	chatmod "github.com/yungbote/neurobridge-backend/internal/modules/chat"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	pathID, ok := jc.PayloadUUID("path_id")
	if !ok || pathID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing path_id"))
		return nil
	}
	nodeID, ok := jc.PayloadUUID("path_node_id")
	if !ok || nodeID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing path_node_id"))
		return nil
	}

	jc.Progress("index", 10, "Indexing path node blocks for chat retrieval")
	out, err := chatmod.New(chatmod.UsecasesDeps{
		DB:        p.db,
		Log:       p.log,
		AI:        p.ai,
		Vec:       p.vec,
		Docs:      p.docs,
		Path:      p.path,
		PathNodes: p.pathNodes,
		NodeDocs:  p.nodeDocs,
	}).IndexPathNodeBlocksForChat(jc.Ctx, chatmod.PathNodeIndexInput{
		UserID:     jc.Job.OwnerUserID,
		PathID:     pathID,
		PathNodeID: nodeID,
	})
	if err != nil {
		jc.Fail("index", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"path_id":         pathID.String(),
		"path_node_id":    nodeID.String(),
		"docs_upserted":   out.DocsUpserted,
		"vector_upserted": out.VectorUpserted,
	})
	return nil
}
