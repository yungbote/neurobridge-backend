package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	docgen "github.com/yungbote/neurobridge-backend/internal/modules/learning/docgen"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type NodeDocPrefetchDeps struct {
	NodeDocBuildDeps
}

type NodeDocPrefetchInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
	NodeLimit     int
	NodeSelect    string
	Report        func(stage string, pct int, message string)
}

type NodeDocPrefetchOutput struct {
	PathID        uuid.UUID `json:"path_id"`
	Lookahead     int       `json:"lookahead"`
	NodesSelected int       `json:"nodes_selected"`
	DocsWritten   int       `json:"docs_written"`
	DocsExisting  int       `json:"docs_existing"`
}

func NodeDocPrefetch(ctx context.Context, deps NodeDocPrefetchDeps, in NodeDocPrefetchInput) (NodeDocPrefetchOutput, error) {
	out := NodeDocPrefetchOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.PathNodes == nil || deps.NodeDocs == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("node_doc_prefetch: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("node_doc_prefetch: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("node_doc_prefetch: missing material_set_id")
	}

	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	pathRow, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID)
	if err != nil {
		return out, err
	}
	pathKind := ""
	if pathRow != nil {
		pathKind = strings.TrimSpace(pathRow.Kind)
	}

	lookahead := in.NodeLimit
	if lookahead <= 0 {
		lookahead = docgen.DocLookaheadForPathKind(pathKind)
	}
	if lookahead <= 0 {
		return out, nil
	}
	out.Lookahead = lookahead

	mode := strings.TrimSpace(in.NodeSelect)
	if mode == "" {
		mode = "outline_and_lesson"
	}

	nodes, err := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{pathID})
	if err != nil {
		return out, err
	}
	if len(nodes) == 0 {
		return out, nil
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Index < nodes[j].Index })

	nodeKindByID := map[uuid.UUID]string{}
	for _, node := range nodes {
		if node == nil || node.ID == uuid.Nil {
			continue
		}
		nodeMeta := map[string]any{}
		if len(node.Metadata) > 0 && string(node.Metadata) != "null" {
			_ = json.Unmarshal(node.Metadata, &nodeMeta)
		}
		nodeKindByID[node.ID] = normalizePathNodeKind(stringFromAny(nodeMeta["node_kind"]))
	}

	selectedNodes, _ := selectPathNodesForBuild(nodes, nodeKindByID, nil, lookahead, mode)
	if len(selectedNodes) == 0 {
		return out, nil
	}
	out.NodesSelected = len(selectedNodes)

	selectedIDs := make([]uuid.UUID, 0, len(selectedNodes))
	for _, n := range selectedNodes {
		if n != nil && n.ID != uuid.Nil {
			selectedIDs = append(selectedIDs, n.ID)
		}
	}
	if len(selectedIDs) == 0 {
		return out, nil
	}

	existingDocs, err := deps.NodeDocs.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, selectedIDs)
	if err != nil {
		return out, err
	}
	existing := map[uuid.UUID]bool{}
	for _, d := range existingDocs {
		if d != nil && d.PathNodeID != uuid.Nil {
			existing[d.PathNodeID] = true
		}
	}
	out.DocsExisting = len(existing)

	missing := make([]uuid.UUID, 0, len(selectedIDs))
	for _, id := range selectedIDs {
		if !existing[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return out, nil
	}

	buildOut, err := NodeDocBuild(ctx, deps.NodeDocBuildDeps, NodeDocBuildInput{
		OwnerUserID:   in.OwnerUserID,
		MaterialSetID: in.MaterialSetID,
		SagaID:        in.SagaID,
		PathID:        pathID,
		NodeIDs:       missing,
		MarkPending:   true,
		Report:        in.Report,
	})
	if err != nil {
		return out, err
	}
	out.DocsWritten = buildOut.DocsWritten
	return out, nil
}
