package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	docgen "github.com/yungbote/neurobridge-backend/internal/modules/learning/docgen"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type NodeDocProgressiveBuildDeps struct {
	NodeDocBuildDeps

	PathRuns          repos.PathRunRepo
	NodeRuns          repos.NodeRunRepo
	DocVariants       repos.LearningNodeDocVariantRepo
	SignalSnapshots   repos.UserDocSignalSnapshotRepo
	InterventionPlans repos.InterventionPlanRepo
}

type NodeDocProgressiveBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
	AnchorNodeID  uuid.UUID
	Lookahead     int
	Report        func(stage string, pct int, message string)
}

type NodeDocProgressiveBuildOutput struct {
	PathID           uuid.UUID `json:"path_id"`
	Lookahead        int       `json:"lookahead"`
	NodesConsidered  int       `json:"nodes_considered"`
	NodesMissing     int       `json:"nodes_missing"`
	DocsWritten      int       `json:"docs_written"`
	VariantsWritten  int       `json:"variants_written"`
	SnapshotsWritten int       `json:"snapshots_written"`
}

func NodeDocProgressiveBuild(ctx context.Context, deps NodeDocProgressiveBuildDeps, in NodeDocProgressiveBuildInput) (NodeDocProgressiveBuildOutput, error) {
	out := NodeDocProgressiveBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.PathNodes == nil || deps.NodeDocs == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("node_doc_progressive_build: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("node_doc_progressive_build: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("node_doc_progressive_build: missing material_set_id")
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
	pathMeta := map[string]any{}
	if pathRow != nil {
		pathKind = strings.TrimSpace(pathRow.Kind)
		if len(pathRow.Metadata) > 0 && string(pathRow.Metadata) != "null" {
			_ = json.Unmarshal(pathRow.Metadata, &pathMeta)
		}
	}

	lookahead := in.Lookahead
	if lookahead <= 0 {
		lookahead = docgen.DocLookaheadForPathKind(pathKind)
	}
	if lookahead <= 0 {
		return out, nil
	}
	out.Lookahead = lookahead

	anchorID := in.AnchorNodeID
	if anchorID == uuid.Nil && deps.PathRuns != nil {
		if pr, err := deps.PathRuns.GetByUserAndPathID(dbctx.Context{Ctx: ctx}, in.OwnerUserID, pathID); err == nil && pr != nil {
			if pr.ActiveNodeID != nil && *pr.ActiveNodeID != uuid.Nil {
				anchorID = *pr.ActiveNodeID
			}
		}
	}

	nodes, err := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{pathID})
	if err != nil {
		return out, err
	}
	if len(nodes) == 0 {
		return out, nil
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Index < nodes[j].Index })

	idxByID := map[uuid.UUID]int{}
	for i, n := range nodes {
		if n != nil && n.ID != uuid.Nil {
			idxByID[n.ID] = i
		}
	}
	anchorIdx := -1
	if anchorID != uuid.Nil {
		if idx, ok := idxByID[anchorID]; ok {
			anchorIdx = idx
		}
	}

	start := anchorIdx + 1
	if start < 0 {
		start = 0
	}
	if start >= len(nodes) {
		return out, nil
	}
	end := start + lookahead
	if end > len(nodes) {
		end = len(nodes)
	}

	window := nodes[start:end]
	if len(window) == 0 {
		return out, nil
	}
	out.NodesConsidered = len(window)

	windowIDs := make([]uuid.UUID, 0, len(window))
	for _, n := range window {
		if n != nil && n.ID != uuid.Nil {
			windowIDs = append(windowIDs, n.ID)
		}
	}
	if len(windowIDs) == 0 {
		return out, nil
	}

	existingDocs, err := deps.NodeDocs.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, windowIDs)
	if err != nil {
		return out, err
	}
	existing := map[uuid.UUID]*types.LearningNodeDoc{}
	for _, d := range existingDocs {
		if d != nil && d.PathNodeID != uuid.Nil {
			existing[d.PathNodeID] = d
		}
	}

	missingNodes := make([]*types.PathNode, 0, len(window))
	for _, n := range window {
		if n == nil || n.ID == uuid.Nil {
			continue
		}
		if existing[n.ID] == nil {
			missingNodes = append(missingNodes, n)
		}
	}
	out.NodesMissing = len(missingNodes)

	anchorRun := (*types.NodeRun)(nil)
	if anchorID != uuid.Nil && deps.NodeRuns != nil {
		if nr, err := deps.NodeRuns.GetByUserAndNodeID(dbctx.Context{Ctx: ctx}, in.OwnerUserID, anchorID); err == nil {
			anchorRun = nr
		}
	}
	pathRun := (*types.PathRun)(nil)
	if deps.PathRuns != nil {
		if pr, err := deps.PathRuns.GetByUserAndPathID(dbctx.Context{Ctx: ctx}, in.OwnerUserID, pathID); err == nil {
			pathRun = pr
		}
	}

	reading := readingProfileFromNodeRun(anchorRun)
	assessment := assessmentProfileFromNodeRun(anchorRun)
	fatigue := fatigueProfileFromPathRun(pathRun, time.Now().UTC())

	nodeKeysByID := map[uuid.UUID][]string{}
	windowByID := map[uuid.UUID]*types.PathNode{}
	for _, n := range window {
		if n == nil || n.ID == uuid.Nil {
			continue
		}
		windowByID[n.ID] = n
		meta := map[string]any{}
		if len(n.Metadata) > 0 && string(n.Metadata) != "null" {
			_ = json.Unmarshal(n.Metadata, &meta)
		}
		keys := dedupeStrings(stringSliceFromAny(meta["concept_keys"]))
		prereq := dedupeStrings(stringSliceFromAny(meta["prereq_concept_keys"]))
		if len(prereq) > 0 {
			keys = dedupeStrings(append(keys, prereq...))
		}
		nodeKeysByID[n.ID] = keys
	}

	optionalSlotsByNode := map[uuid.UUID][]docgen.DocOptionalSlot{}
	planByNode := map[uuid.UUID]*types.InterventionPlan{}
	if docSlotInjectionEnabled() && deps.InterventionPlans != nil {
		for _, n := range window {
			if n == nil || n.ID == uuid.Nil {
				continue
			}
			plan, err := deps.InterventionPlans.GetLatestByUserAndNode(dbctx.Context{Ctx: ctx}, in.OwnerUserID, n.ID)
			if err != nil || plan == nil {
				continue
			}
			slots := optionalSlotsFromPlan(plan, nodeKeysByID[n.ID])
			if len(slots) == 0 {
				continue
			}
			optionalSlotsByNode[n.ID] = slots
			planByNode[n.ID] = plan
		}
	}

	if len(missingNodes) == 0 && len(optionalSlotsByNode) == 0 {
		return out, nil
	}

	targetNodes := map[uuid.UUID]*types.PathNode{}
	for _, n := range missingNodes {
		if n != nil && n.ID != uuid.Nil {
			targetNodes[n.ID] = n
		}
	}
	for id := range optionalSlotsByNode {
		if n := windowByID[id]; n != nil && n.ID != uuid.Nil {
			targetNodes[id] = n
		}
	}

	allKeys := map[string]bool{}
	for id := range targetNodes {
		keys := nodeKeysByID[id]
		for _, k := range keys {
			if s := strings.TrimSpace(strings.ToLower(k)); s != "" {
				allKeys[s] = true
			}
		}
	}

	conceptByKey := map[string]*types.Concept{}
	canonicalIDByKey := map[string]uuid.UUID{}
	if deps.Concepts != nil {
		if rows, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID); err == nil {
			for _, c := range rows {
				if c == nil || c.ID == uuid.Nil {
					continue
				}
				key := strings.TrimSpace(strings.ToLower(c.Key))
				if key == "" {
					continue
				}
				conceptByKey[key] = c
				cid := c.ID
				if c.CanonicalConceptID != nil && *c.CanonicalConceptID != uuid.Nil {
					cid = *c.CanonicalConceptID
				}
				if cid != uuid.Nil {
					canonicalIDByKey[key] = cid
				}
			}
		}
	}

	conceptIDs := map[uuid.UUID]bool{}
	for key := range allKeys {
		if id := canonicalIDByKey[key]; id != uuid.Nil {
			conceptIDs[id] = true
		}
	}
	conceptIDList := make([]uuid.UUID, 0, len(conceptIDs))
	for id := range conceptIDs {
		conceptIDList = append(conceptIDList, id)
	}

	stateByID := map[uuid.UUID]*types.UserConceptState{}
	if deps.ConceptState != nil && len(conceptIDList) > 0 {
		if rows, err := deps.ConceptState.ListByUserAndConceptIDs(dbctx.Context{Ctx: ctx}, in.OwnerUserID, conceptIDList); err == nil {
			for _, st := range rows {
				if st != nil && st.ConceptID != uuid.Nil {
					stateByID[st.ConceptID] = st
				}
			}
		}
	}

	modelByID := map[uuid.UUID]*types.UserConceptModel{}
	if deps.ConceptModel != nil && len(conceptIDList) > 0 {
		if rows, err := deps.ConceptModel.ListByUserAndConceptIDs(dbctx.Context{Ctx: ctx}, in.OwnerUserID, conceptIDList); err == nil {
			for _, m := range rows {
				if m != nil && m.CanonicalConceptID != uuid.Nil {
					modelByID[m.CanonicalConceptID] = m
				}
			}
		}
	}

	misconByID := map[uuid.UUID][]*types.UserMisconceptionInstance{}
	if deps.MisconRepo != nil && len(conceptIDList) > 0 {
		if rows, err := deps.MisconRepo.ListActiveByUserAndConceptIDs(dbctx.Context{Ctx: ctx}, in.OwnerUserID, conceptIDList); err == nil {
			for _, m := range rows {
				if m != nil && m.CanonicalConceptID != uuid.Nil {
					misconByID[m.CanonicalConceptID] = append(misconByID[m.CanonicalConceptID], m)
				}
			}
		}
	}

	snapshotByNodeID := map[uuid.UUID]docgen.DocSignalsSnapshotV1{}
	for _, n := range targetNodes {
		keys := nodeKeysByID[n.ID]
		snap := buildDocSignalsSnapshot(
			in.OwnerUserID,
			pathID,
			n.ID,
			keys,
			canonicalIDByKey,
			conceptByKey,
			stateByID,
			modelByID,
			misconByID,
			reading,
			assessment,
			fatigue,
		)
		snapshotByNodeID[n.ID] = snap

		if deps.SignalSnapshots != nil {
			raw, _ := json.Marshal(snap)
			row := &types.UserDocSignalSnapshot{
				UserID:        in.OwnerUserID,
				PathID:        pathID,
				PathNodeID:    n.ID,
				SnapshotID:    snap.SnapshotID,
				PolicyVersion: snap.PolicyVersion,
				SchemaVersion: snap.SchemaVersion,
				SnapshotJSON:  datatypes.JSON(raw),
				CreatedAt:     time.Now().UTC(),
			}
			if err := deps.SignalSnapshots.Upsert(dbctx.Context{Ctx: ctx}, row); err == nil {
				out.SnapshotsWritten++
			}
		}
	}

	missingIDs := make([]uuid.UUID, 0, len(missingNodes))
	for _, n := range missingNodes {
		if n != nil && n.ID != uuid.Nil {
			missingIDs = append(missingIDs, n.ID)
		}
	}
	if len(missingIDs) > 0 {
		buildOut, err := NodeDocBuild(ctx, deps.NodeDocBuildDeps, NodeDocBuildInput{
			OwnerUserID:   in.OwnerUserID,
			MaterialSetID: in.MaterialSetID,
			SagaID:        in.SagaID,
			PathID:        pathID,
			NodeIDs:       missingIDs,
			MarkPending:   true,
			Report:        in.Report,
		})
		if err != nil {
			return out, err
		}
		out.DocsWritten = buildOut.DocsWritten
	}

	if deps.DocVariants != nil && len(optionalSlotsByNode) > 0 {
		variantIDs := make([]uuid.UUID, 0, len(optionalSlotsByNode))
		variantSnapshotByNode := map[uuid.UUID]string{}
		variantPolicyByNode := map[uuid.UUID]string{}
		for nodeID, slots := range optionalSlotsByNode {
			if len(slots) == 0 {
				continue
			}
			snap, ok := snapshotByNodeID[nodeID]
			if !ok || strings.TrimSpace(snap.SnapshotID) == "" {
				continue
			}
			variantIDs = append(variantIDs, nodeID)
			variantSnapshotByNode[nodeID] = strings.TrimSpace(snap.SnapshotID)
			if plan := planByNode[nodeID]; plan != nil {
				if v := strings.TrimSpace(plan.PolicyVersion); v != "" {
					variantPolicyByNode[nodeID] = v
				}
			}
		}
		if len(variantIDs) > 0 {
			buildOut, err := NodeDocBuild(ctx, deps.NodeDocBuildDeps, NodeDocBuildInput{
				OwnerUserID:                in.OwnerUserID,
				MaterialSetID:              in.MaterialSetID,
				SagaID:                     in.SagaID,
				PathID:                     pathID,
				NodeIDs:                    variantIDs,
				VariantOnly:                true,
				VariantKind:                "progressive_slot",
				VariantSnapshotIDByNode:    variantSnapshotByNode,
				VariantPolicyVersionByNode: variantPolicyByNode,
				OptionalSlotsByNode:        optionalSlotsByNode,
				Report:                     in.Report,
			})
			if err != nil {
				return out, err
			}
			out.VariantsWritten += buildOut.DocsWritten
		}
	}

	if deps.DocVariants != nil && len(missingIDs) > 0 {
		now := time.Now().UTC()
		latestDocs, _ := deps.NodeDocs.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, missingIDs)
		latestByNode := map[uuid.UUID]*types.LearningNodeDoc{}
		for _, d := range latestDocs {
			if d != nil && d.PathNodeID != uuid.Nil {
				latestByNode[d.PathNodeID] = d
			}
		}
		for _, n := range missingNodes {
			if n == nil || n.ID == uuid.Nil {
				continue
			}
			if len(optionalSlotsByNode[n.ID]) > 0 {
				continue
			}
			docRow := latestByNode[n.ID]
			if docRow == nil {
				continue
			}
			snap, ok := snapshotByNodeID[n.ID]
			if !ok || strings.TrimSpace(snap.SnapshotID) == "" {
				continue
			}
			retrievalPackID := ""
			traceID := ""
			if deps.DocTraces != nil {
				if rows, err := deps.DocTraces.ListByUserAndNode(dbctx.Context{Ctx: ctx}, in.OwnerUserID, n.ID, 1); err == nil && len(rows) > 0 && rows[0] != nil {
					retrievalPackID = strings.TrimSpace(rows[0].RetrievalPackID)
					traceID = strings.TrimSpace(rows[0].TraceID)
				}
			}
			docText := strings.TrimSpace(docRow.DocText)
			if docText == "" && len(docRow.DocJSON) > 0 && string(docRow.DocJSON) != "null" {
				var doc content.NodeDocV1
				if err := json.Unmarshal(docRow.DocJSON, &doc); err == nil {
					if m := content.NodeDocMetrics(doc); m != nil {
						if v, ok := m["doc_text"].(string); ok {
							docText = strings.TrimSpace(v)
						}
					}
				}
			}

			baseID := docRow.ID
			row := &types.LearningNodeDocVariant{
				UserID:          in.OwnerUserID,
				PathID:          pathID,
				PathNodeID:      n.ID,
				BaseDocID:       &baseID,
				VariantKind:     "progressive",
				PolicyVersion:   snap.PolicyVersion,
				SchemaVersion:   1,
				SnapshotID:      snap.SnapshotID,
				RetrievalPackID: retrievalPackID,
				TraceID:         traceID,
				DocJSON:         docRow.DocJSON,
				DocText:         docText,
				ContentHash:     docRow.ContentHash,
				SourcesHash:     docRow.SourcesHash,
				Status:          "active",
				CreatedAt:       now,
				UpdatedAt:       now,
			}
			if err := deps.DocVariants.Upsert(dbctx.Context{Ctx: ctx}, row); err == nil {
				out.VariantsWritten++
			}
		}
	}

	if len(nodes) > 0 {
		updatePathMeta(ctx, deps.NodeDocBuildDeps, pathID, pathMeta, map[string]any{
			"progressive_mode":             true,
			"progressive_nodes_total":      len(nodes),
			"progressive_last_prefetch_at": time.Now().UTC().Format(time.RFC3339Nano),
		})
	}

	return out, nil
}

type docFrameSignal struct {
	Frame      string  `json:"frame"`
	Confidence float64 `json:"confidence"`
}

func buildDocSignalsSnapshot(
	userID uuid.UUID,
	pathID uuid.UUID,
	nodeID uuid.UUID,
	conceptKeys []string,
	canonicalIDByKey map[string]uuid.UUID,
	conceptByKey map[string]*types.Concept,
	stateByID map[uuid.UUID]*types.UserConceptState,
	modelByID map[uuid.UUID]*types.UserConceptModel,
	misconByID map[uuid.UUID][]*types.UserMisconceptionInstance,
	reading docgen.ReadingProfile,
	assessment docgen.AssessmentProfile,
	fatigue docgen.FatigueProfile,
) docgen.DocSignalsSnapshotV1 {
	now := time.Now().UTC()
	keys := dedupeStrings(conceptKeys)
	for i := range keys {
		keys[i] = strings.TrimSpace(strings.ToLower(keys[i]))
	}
	sort.Strings(keys)

	concepts := make([]docgen.ConceptSignal, 0, len(keys))
	for _, k := range keys {
		if k == "" {
			continue
		}
		cid := uuid.Nil
		if canonicalIDByKey != nil {
			cid = canonicalIDByKey[k]
		}
		if cid == uuid.Nil {
			if c := conceptByKey[k]; c != nil && c.ID != uuid.Nil {
				cid = c.ID
			}
		}
		st := (*types.UserConceptState)(nil)
		if cid != uuid.Nil && stateByID != nil {
			st = stateByID[cid]
		}
		signal := docgen.ConceptSignal{
			ConceptKey: k,
		}
		if cid != uuid.Nil {
			signal.ConceptID = cid.String()
		}
		if st != nil {
			signal.Mastery = clamp01(st.Mastery)
			signal.Confidence = clamp01(st.Confidence)
			signal.EpistemicUncertainty = clamp01(st.EpistemicUncertainty)
			signal.AleatoricUncertainty = clamp01(st.AleatoricUncertainty)
			signal.CoverageDebt = clamp01(computeCoverageDebtSignal(st, now))
			if !st.UpdatedAt.IsZero() {
				signal.LastUpdatedAt = st.UpdatedAt.UTC().Format(time.RFC3339Nano)
			}
		} else if coverageDebtSignalsEnabled() {
			signal.CoverageDebt = 1
		}
		concepts = append(concepts, signal)
	}
	sort.Slice(concepts, func(i, j int) bool {
		if concepts[i].ConceptKey == concepts[j].ConceptKey {
			return concepts[i].ConceptID < concepts[j].ConceptID
		}
		return concepts[i].ConceptKey < concepts[j].ConceptKey
	})

	miscons := make([]docgen.MisconceptionSignal, 0)
	if misconByID != nil {
		for _, k := range keys {
			cid := uuid.Nil
			if canonicalIDByKey != nil {
				cid = canonicalIDByKey[k]
			}
			if cid == uuid.Nil {
				continue
			}
			rows := misconByID[cid]
			if len(rows) == 0 {
				continue
			}
			for _, r := range rows {
				if r == nil {
					continue
				}
				key := strings.TrimSpace(r.Description)
				if r.PatternID != nil && strings.TrimSpace(*r.PatternID) != "" {
					key = strings.TrimSpace(*r.PatternID)
				}
				if key == "" {
					continue
				}
				entry := docgen.MisconceptionSignal{
					ConceptID:        cid.String(),
					MisconceptionKey: key,
					Confidence:       clamp01(r.Confidence),
				}
				if r.FirstSeenAt != nil && !r.FirstSeenAt.IsZero() {
					entry.FirstSeenAt = r.FirstSeenAt.UTC().Format(time.RFC3339Nano)
				}
				if r.LastSeenAt != nil && !r.LastSeenAt.IsZero() {
					entry.LastSeenAt = r.LastSeenAt.UTC().Format(time.RFC3339Nano)
				}
				miscons = append(miscons, entry)
			}
		}
	}
	sort.Slice(miscons, func(i, j int) bool {
		if miscons[i].ConceptID == miscons[j].ConceptID {
			return miscons[i].MisconceptionKey < miscons[j].MisconceptionKey
		}
		return miscons[i].ConceptID < miscons[j].ConceptID
	})

	frameProfile := map[string]float64{}
	if modelByID != nil {
		for _, k := range keys {
			cid := uuid.Nil
			if canonicalIDByKey != nil {
				cid = canonicalIDByKey[k]
			}
			if cid == uuid.Nil {
				continue
			}
			m := modelByID[cid]
			if m == nil || len(m.ActiveFrames) == 0 {
				continue
			}
			var frames []docFrameSignal
			_ = json.Unmarshal(m.ActiveFrames, &frames)
			for _, f := range frames {
				name := strings.TrimSpace(f.Frame)
				if name == "" {
					continue
				}
				score := clamp01(f.Confidence)
				if prev, ok := frameProfile[name]; !ok || score > prev {
					frameProfile[name] = score
				}
			}
		}
	}

	snap := docgen.DocSignalsSnapshotV1{
		SchemaVersion:  docgen.DocSignalsSnapshotSchemaVersion,
		PolicyVersion:  docgen.DocPolicyVersion(),
		UserID:         userID.String(),
		PathID:         pathID.String(),
		PathNodeID:     nodeID.String(),
		Concepts:       concepts,
		Misconceptions: miscons,
		FrameProfile:   frameProfile,
		Reading:        reading,
		Assessment:     assessment,
		Fatigue:        fatigue,
		CreatedAt:      now.Format(time.RFC3339Nano),
	}
	snap.SnapshotID = docgen.ComputeSnapshotID(snap)
	return snap
}

func readingProfileFromNodeRun(nr *types.NodeRun) docgen.ReadingProfile {
	out := docgen.ReadingProfile{}
	if nr == nil || len(nr.Metadata) == 0 || string(nr.Metadata) == "null" {
		return out
	}
	meta := decodeJSONMap(nr.Metadata)
	runtime := mapFromAny(meta["runtime"])
	if runtime == nil {
		return out
	}
	blocksSeen := intFromAny(runtime["blocks_seen"], 0)
	readBlocks := stringSliceFromAny(runtime["read_blocks"])
	readCount := len(readBlocks)
	readDepth := 0.0
	if blocksSeen > 0 {
		readDepth = float64(readCount) / float64(blocksSeen)
	} else if readCount > 0 {
		readDepth = 1
	}
	out.ReadDepth = clamp01(readDepth)
	out.SkipRate = clamp01(1 - out.ReadDepth)
	out.RereadRate = clamp01(floatFromAny(runtime["reread_rate"], 0))
	out.AvgDwellMs = intFromAny(runtime["avg_dwell_ms"], 0)
	if out.AvgDwellMs == 0 {
		out.AvgDwellMs = intFromAny(runtime["avg_block_dwell_ms"], 0)
	}
	out.ProgressConfidence = clamp01(floatFromAny(runtime["last_progress_confidence"], 0))
	return out
}

func assessmentProfileFromNodeRun(nr *types.NodeRun) docgen.AssessmentProfile {
	out := docgen.AssessmentProfile{}
	if nr == nil || len(nr.Metadata) == 0 || string(nr.Metadata) == "null" {
		return out
	}
	meta := decodeJSONMap(nr.Metadata)
	runtime := mapFromAny(meta["runtime"])
	if runtime == nil {
		return out
	}
	out.QuickCheckCount = intFromAny(runtime["quick_checks_shown"], 0)
	out.ActivityCount = intFromAny(runtime["activities_completed"], 0)
	out.HintUsageCount = intFromAny(runtime["hint_usage_count"], 0)

	// Estimate accuracy from bandit stats when explicit counters are unavailable.
	bandit := mapFromAny(runtime["bandit"])
	blocks := mapFromAny(bandit["blocks"])
	attempts := 0
	correct := 0
	for _, raw := range blocks {
		m := mapFromAny(raw)
		if m == nil {
			continue
		}
		attempts += intFromAny(m["attempts"], 0)
		correct += intFromAny(m["correct"], 0)
	}
	if attempts > 0 {
		out.QuickCheckAccuracy = clamp01(float64(correct) / float64(attempts))
	}
	return out
}

func fatigueProfileFromPathRun(pr *types.PathRun, now time.Time) docgen.FatigueProfile {
	out := docgen.FatigueProfile{}
	if pr == nil || len(pr.Metadata) == 0 || string(pr.Metadata) == "null" {
		return out
	}
	meta := decodeJSONMap(pr.Metadata)
	runtime := mapFromAny(meta["runtime"])
	if runtime == nil {
		return out
	}
	out.PromptsInWindow = intFromAny(runtime["prompts_in_window"], 0)
	out.FatigueScore = clamp01(floatFromAny(runtime["fatigue_score"], 0))

	if ts := timeFromAny(runtime["session_started_at"]); ts != nil && !ts.IsZero() {
		out.SessionMinutes = now.Sub(*ts).Minutes()
		if out.SessionMinutes < 0 {
			out.SessionMinutes = 0
		}
	}
	return out
}

func coverageDebtSignalsEnabled() bool {
	return envBool("RUNTIME_COVERAGE_DEBT_ENABLED", true)
}

func coverageDebtDueDays() float64 {
	days := envFloatAllowZero("RUNTIME_COVERAGE_DEBT_DUE_DAYS", 14)
	if days < 1 {
		days = 1
	}
	return days
}

func coverageDebtMax() float64 {
	maxDebt := envFloatAllowZero("RUNTIME_COVERAGE_DEBT_MAX", 1.0)
	if maxDebt < 0 {
		return 0
	}
	if maxDebt > 1 {
		return 1
	}
	return maxDebt
}

func computeCoverageDebtSignal(st *types.UserConceptState, now time.Time) float64 {
	if st == nil {
		if coverageDebtSignalsEnabled() {
			return 1
		}
		return 0
	}
	if !coverageDebtSignalsEnabled() {
		return 0
	}
	debt := 0.0
	dueDays := coverageDebtDueDays()
	if st.NextReviewAt != nil && !st.NextReviewAt.IsZero() && !st.NextReviewAt.After(now) {
		overdue := now.Sub(*st.NextReviewAt).Hours() / 24.0
		if overdue > 0 {
			debt = math.Max(debt, clamp01(overdue/math.Max(dueDays, 1)))
		}
	}
	if st.LastSeenAt != nil && !st.LastSeenAt.IsZero() {
		gap := now.Sub(*st.LastSeenAt).Hours() / 24.0
		if gap > dueDays {
			debt = math.Max(debt, clamp01((gap-dueDays)/math.Max(dueDays, 1)))
		}
	}
	if maxDebt := coverageDebtMax(); maxDebt > 0 && debt > maxDebt {
		debt = maxDebt
	}
	return clamp01(debt)
}

func docSlotInjectionEnabled() bool {
	return envBool("DOC_SLOT_INJECTION_ENABLED", true)
}

func optionalSlotsFromPlan(plan *types.InterventionPlan, nodeKeys []string) []docgen.DocOptionalSlot {
	if plan == nil {
		return nil
	}
	actions := planActionsFromRow(plan)
	if len(actions) == 0 {
		return nil
	}
	sort.Slice(actions, func(i, j int) bool {
		pi := intFromAny(actions[i]["priority"], 0)
		pj := intFromAny(actions[j]["priority"], 0)
		if pi == pj {
			return stringFromAny(actions[i]["type"]) < stringFromAny(actions[j]["type"])
		}
		return pi < pj
	})

	slots := []docgen.DocOptionalSlot{}
	counter := 0
	for _, action := range actions {
		slotKind := strings.ToLower(strings.TrimSpace(stringFromAny(action["slot"])))
		if slotKind == "" || slotKind == "none" {
			continue
		}
		allowedKinds := slotAllowedBlockKinds(slotKind)
		if len(allowedKinds) == 0 {
			continue
		}
		minBlocks, maxBlocks := slotMinMaxBlocks(slotKind)
		counter++
		slotID := fmt.Sprintf("%s_%02d", slotKind, counter)

		purposeParts := []string{}
		if t := strings.TrimSpace(stringFromAny(action["type"])); t != "" {
			purposeParts = append(purposeParts, t)
		}
		if r := strings.TrimSpace(stringFromAny(action["reason"])); r != "" {
			purposeParts = append(purposeParts, r)
		}
		purpose := strings.Join(purposeParts, ":")

		miscons := dedupeStrings(stringSliceFromAny(action["target_misconceptions"]))
		if len(miscons) > 0 {
			if purpose != "" {
				purpose = purpose + " "
			}
			purpose = purpose + "misconceptions=" + strings.Join(miscons, ",")
		}

		concepts := dedupeStrings(stringSliceFromAny(action["target_concepts"]))
		if len(concepts) == 0 {
			concepts = dedupeStrings(nodeKeys)
		}
		slots = append(slots, docgen.DocOptionalSlot{
			SlotID:            slotID,
			Purpose:           purpose,
			MinBlocks:         minBlocks,
			MaxBlocks:         maxBlocks,
			AllowedBlockKinds: allowedKinds,
			ConceptKeys:       concepts,
		})
	}
	return normalizeOptionalSlots(slots)
}

func planActionsFromRow(plan *types.InterventionPlan) []map[string]any {
	if plan == nil {
		return nil
	}
	actions := []map[string]any{}
	if len(plan.ActionsJSON) > 0 && string(plan.ActionsJSON) != "null" {
		_ = json.Unmarshal(plan.ActionsJSON, &actions)
	}
	if len(actions) == 0 && len(plan.PlanJSON) > 0 && string(plan.PlanJSON) != "null" {
		var payload map[string]any
		if err := json.Unmarshal(plan.PlanJSON, &payload); err == nil {
			actions = mapSliceFromAny(payload["actions"])
		}
	}
	return actions
}

func mapSliceFromAny(v any) []map[string]any {
	out := []map[string]any{}
	switch raw := v.(type) {
	case []map[string]any:
		return raw
	case []any:
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

func slotAllowedBlockKinds(slot string) []string {
	switch strings.ToLower(strings.TrimSpace(slot)) {
	case "prereq_bridge":
		return []string{"paragraph", "callout"}
	case "reframe", "misconception_fix":
		return []string{"callout", "paragraph"}
	case "transfer_check":
		return []string{"quick_check"}
	default:
		return nil
	}
}

func slotMinMaxBlocks(slot string) (int, int) {
	switch strings.ToLower(strings.TrimSpace(slot)) {
	case "transfer_check":
		return 1, 1
	case "reframe", "misconception_fix":
		return 1, 2
	case "prereq_bridge":
		return 1, 2
	default:
		return 1, 1
	}
}

func decodeJSONMap(raw datatypes.JSON) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func timeFromAny(v any) *time.Time {
	switch t := v.(type) {
	case time.Time:
		return &t
	case *time.Time:
		return t
	case string:
		if strings.TrimSpace(t) == "" {
			return nil
		}
		if ts, err := time.Parse(time.RFC3339, t); err == nil {
			return &ts
		}
		if ts, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return &ts
		}
	}
	return nil
}
