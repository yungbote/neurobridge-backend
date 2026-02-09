package runtime_update

import (
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning/runtime"
	docgen "github.com/yungbote/neurobridge-backend/internal/modules/learning/docgen"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type progressiveDocCandidate struct {
	PathID        uuid.UUID
	NodeID        uuid.UUID
	ProgressState string
	ProgressConf  float64
	EventType     string
	EventAt       time.Time
}

func shouldConsiderProgressiveDoc(typ string) bool {
	switch typ {
	case types.EventBlockRead,
		types.EventBlockViewed,
		types.EventScrollDepth,
		types.EventQuestionAnswered,
		types.EventActivityCompleted,
		types.EventQuizCompleted,
		types.EventRuntimePromptCompleted,
		types.EventRuntimePromptDismissed:
		return true
	default:
		return false
	}
}

func (p *Pipeline) maybeEnqueueDocProgressiveBuild(dbc dbctx.Context, userID uuid.UUID, cand progressiveDocCandidate) {
	if p == nil || p.jobSvc == nil || p.paths == nil || p.pathNodes == nil || p.nodeDocs == nil {
		return
	}
	if userID == uuid.Nil || cand.PathID == uuid.Nil {
		return
	}
	pathRow, err := p.paths.GetByID(dbc, cand.PathID)
	if err != nil || pathRow == nil {
		return
	}
	if pathRow.MaterialSetID == nil || *pathRow.MaterialSetID == uuid.Nil {
		return
	}
	if strings.ToLower(strings.TrimSpace(pathRow.Status)) != "ready" {
		return
	}

	lookahead := docgen.DocLookaheadForPathKind(pathRow.Kind)
	if lookahead <= 0 {
		return
	}

	pathMeta := decodeJSONMap(pathRow.Metadata)
	if ts := timeFromAny(pathMeta["progressive_last_prefetch_at"]); ts != nil {
		minGap := docgen.DocProgressiveMinGapMinutes()
		if minGap > 0 && time.Since(*ts) < time.Duration(minGap)*time.Minute {
			return
		}
	}

	anchorID := cand.NodeID
	if anchorID == uuid.Nil && p.pathRuns != nil {
		if pr, err := p.pathRuns.GetByUserAndPathID(dbc, userID, cand.PathID); err == nil && pr != nil {
			if pr.ActiveNodeID != nil && *pr.ActiveNodeID != uuid.Nil {
				anchorID = *pr.ActiveNodeID
			}
		}
	}

	nodes, err := p.pathNodes.GetByPathIDs(dbc, []uuid.UUID{cand.PathID})
	if err != nil || len(nodes) == 0 {
		return
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Index < nodes[j].Index })

	anchorIdx := -1
	if anchorID != uuid.Nil {
		for i, n := range nodes {
			if n != nil && n.ID == anchorID {
				anchorIdx = i
				break
			}
		}
	}
	start := anchorIdx + 1
	if start < 0 {
		start = 0
	}
	if start >= len(nodes) {
		return
	}
	end := start + lookahead
	if end > len(nodes) {
		end = len(nodes)
	}
	if end <= start {
		return
	}

	windowIDs := make([]uuid.UUID, 0, end-start)
	for _, n := range nodes[start:end] {
		if n != nil && n.ID != uuid.Nil {
			windowIDs = append(windowIDs, n.ID)
		}
	}
	if len(windowIDs) == 0 {
		return
	}

	docs, err := p.nodeDocs.GetByPathNodeIDs(dbc, windowIDs)
	if err != nil {
		return
	}
	hasDoc := map[uuid.UUID]bool{}
	for _, d := range docs {
		if d != nil && d.PathNodeID != uuid.Nil {
			hasDoc[d.PathNodeID] = true
		}
	}
	missing := 0
	for _, id := range windowIDs {
		if !hasDoc[id] {
			missing++
		}
	}
	if missing == 0 {
		return
	}

	progressSignal := cand.ProgressState != "" || cand.ProgressConf > 0
	progressOk := false
	if progressSignal {
		progressOk = progressEligible(cand.ProgressState, cand.ProgressConf)
	}
	if !progressOk && anchorID != uuid.Nil && p.nodeRuns != nil {
		if nr, err := p.nodeRuns.GetByUserAndNodeID(dbc, userID, anchorID); err == nil && nr != nil {
			if nr.State == runtime.NodeRunCompleted {
				progressOk = true
			} else {
				meta := decodeJSONMap(nr.Metadata)
				rt := mapFromAny(meta["runtime"])
				state := strings.TrimSpace(stringFromAny(rt["last_progress_state"]))
				conf := floatFromAny(rt["last_progress_confidence"], 0)
				if (state != "" || conf > 0) && progressEligible(state, conf) {
					progressOk = true
				}
			}
		}
	}
	if !progressOk {
		return
	}

	_, _, err = p.jobSvc.EnqueueNodeDocProgressiveBuildIfNeeded(dbctx.Context{Ctx: dbc.Ctx}, userID, cand.PathID, *pathRow.MaterialSetID, anchorID, "runtime_progress")
	if err != nil && p.log != nil {
		p.log.Warn("Failed to enqueue node_doc_progressive_build", "error", err, "path_id", cand.PathID.String())
	}
}

func (p *Pipeline) maybeEnqueueDocProbeSelect(dbc dbctx.Context, userID uuid.UUID, cand progressiveDocCandidate) {
	if p == nil || p.jobSvc == nil || p.paths == nil {
		return
	}
	if userID == uuid.Nil || cand.PathID == uuid.Nil {
		return
	}
	if docgen.DocProbeRatePerHour() <= 0 {
		return
	}

	pathRow, err := p.paths.GetByID(dbc, cand.PathID)
	if err != nil || pathRow == nil {
		return
	}
	if pathRow.MaterialSetID == nil || *pathRow.MaterialSetID == uuid.Nil {
		return
	}
	if strings.ToLower(strings.TrimSpace(pathRow.Status)) != "ready" {
		return
	}

	anchorID := cand.NodeID
	if anchorID == uuid.Nil && p.pathRuns != nil {
		if pr, err := p.pathRuns.GetByUserAndPathID(dbc, userID, cand.PathID); err == nil && pr != nil {
			if pr.ActiveNodeID != nil && *pr.ActiveNodeID != uuid.Nil {
				anchorID = *pr.ActiveNodeID
			}
		}
	}

	progressSignal := cand.ProgressState != "" || cand.ProgressConf > 0
	progressOk := false
	if progressSignal {
		progressOk = progressEligible(cand.ProgressState, cand.ProgressConf)
	}
	if !progressOk && anchorID != uuid.Nil && p.nodeRuns != nil {
		if nr, err := p.nodeRuns.GetByUserAndNodeID(dbc, userID, anchorID); err == nil && nr != nil {
			if nr.State == runtime.NodeRunCompleted {
				progressOk = true
			} else {
				meta := decodeJSONMap(nr.Metadata)
				rt := mapFromAny(meta["runtime"])
				state := strings.TrimSpace(stringFromAny(rt["last_progress_state"]))
				conf := floatFromAny(rt["last_progress_confidence"], 0)
				if (state != "" || conf > 0) && progressEligible(state, conf) {
					progressOk = true
				}
			}
		}
	}
	if !progressOk {
		return
	}

	_, _, err = p.jobSvc.EnqueueDocProbeSelectIfNeeded(dbctx.Context{Ctx: dbc.Ctx}, userID, cand.PathID, *pathRow.MaterialSetID, anchorID, "runtime_progress")
	if err != nil && p.log != nil {
		p.log.Warn("Failed to enqueue doc_probe_select", "error", err, "path_id", cand.PathID.String())
	}
}
