package steps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	infclient "github.com/yungbote/neurobridge-backend/internal/inference/client"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type PathGroupingRefineDeps struct {
	DB       *gorm.DB
	Log      *logger.Logger
	Path     repos.PathRepo
	Files    repos.MaterialFileRepo
	FileSigs repos.MaterialFileSignatureRepo
	Prefs    repos.UserPersonalizationPrefsRepo
	Threads  repos.ChatThreadRepo
	Messages repos.ChatMessageRepo
	Notify   services.ChatNotifier
}

type PathGroupingRefineInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	PathID        uuid.UUID
	ThreadID      uuid.UUID
	JobID         uuid.UUID
	WaitForUser   bool
}

type PathGroupingRefineOutput struct {
	PathID          uuid.UUID      `json:"path_id"`
	Status          string         `json:"status"`
	PathsBefore     int            `json:"paths_before"`
	PathsAfter      int            `json:"paths_after"`
	FilesConsidered int            `json:"files_considered"`
	Confidence      float64        `json:"confidence"`
	ThreadID        uuid.UUID      `json:"thread_id,omitempty"`
	Meta            map[string]any `json:"meta,omitempty"`
	Intake          map[string]any `json:"intake,omitempty"`
}

func PathGroupingRefine(ctx context.Context, deps PathGroupingRefineDeps, in PathGroupingRefineInput) (PathGroupingRefineOutput, error) {
	out := PathGroupingRefineOutput{Status: "skipped"}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.Files == nil || deps.FileSigs == nil {
		return out, fmt.Errorf("path_grouping_refine: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("path_grouping_refine: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("path_grouping_refine: missing material_set_id")
	}

	pathID := in.PathID
	if pathID == uuid.Nil {
		return out, fmt.Errorf("path_grouping_refine: missing path_id")
	}
	out.PathID = pathID

	path, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID)
	if err != nil {
		return out, err
	}
	if path == nil || path.ID == uuid.Nil || path.UserID == nil || *path.UserID != in.OwnerUserID {
		return out, fmt.Errorf("path_grouping_refine: path not found")
	}

	meta := map[string]any{}
	if len(path.Metadata) > 0 && strings.TrimSpace(string(path.Metadata)) != "" && strings.TrimSpace(string(path.Metadata)) != "null" {
		_ = json.Unmarshal(path.Metadata, &meta)
	}
	if boolFromAny(meta["intake_locked"]) || boolFromAny(meta["intake_confirmed_by_user"]) {
		return out, nil
	}
	intake := mapFromAny(meta["intake"])
	if intake == nil {
		return out, nil
	}
	pathsAny := sliceAny(intake["paths"])
	if len(pathsAny) == 0 {
		return out, nil
	}
	out.PathsBefore = len(pathsAny)
	out.Intake = intake

	if boolFromAny(intake["paths_refined"]) && boolFromAny(intake["paths_confirmed"]) {
		out.Status = "confirmed"
		out.PathsAfter = len(pathsAny)
		return out, nil
	}

	files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	if len(files) < 2 {
		return out, nil
	}

	maxFiles := envIntAllowZero("PATH_GROUPING_MAX_FILES", 40)
	if maxFiles > 0 && len(files) > maxFiles {
		out.Status = "skipped_too_many_files"
		return out, nil
	}

	sigs, err := deps.FileSigs.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	if len(sigs) == 0 {
		return out, nil
	}

	sigByFile := map[uuid.UUID]*types.MaterialFileSignature{}
	for _, s := range sigs {
		if s == nil || s.MaterialFileID == uuid.Nil {
			continue
		}
		sigByFile[s.MaterialFileID] = s
	}

	fileIDs := make([]uuid.UUID, 0, len(files))
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		fileIDs = append(fileIDs, f.ID)
	}
	out.FilesConsidered = len(fileIDs)
	if len(fileIDs) < 2 {
		return out, nil
	}

	prefs := loadGroupingPrefs(ctx, deps, in.OwnerUserID)

	pairScores := computePairScores(files, sigByFile)
	pairScores = applyCrossEncoderScores(ctx, files, sigByFile, pairScores)

	mergeThreshold := envFloatAllowZero("PATH_GROUPING_MIN_CONFIDENCE_MERGE", 0.60)
	splitThreshold := envFloatAllowZero("PATH_GROUPING_MIN_CONFIDENCE_SPLIT", 0.55)
	bridgeStrong := envFloatAllowZero("PATH_GROUPING_BRIDGE_STRONG", 0.70)
	bridgeWeak := envFloatAllowZero("PATH_GROUPING_BRIDGE_WEAK", 0.40)

	applyGroupingPrefs(&mergeThreshold, &splitThreshold, &bridgeStrong, &bridgeWeak, prefs)

	clusters := clusterByThreshold(fileIDs, pairScores, mergeThreshold)
	if len(clusters) == 0 {
		return out, nil
	}

	avgIntra, avgInter := clusterSeparationScores(clusters, pairScores)
	conf := clamp01((avgIntra - avgInter + 1) / 2)
	out.Confidence = conf

	bridgeInfo := detectBridgeStrengths(clusters, pairScores, bridgeStrong, bridgeWeak)

	candidateMode := groupingMode(len(pathsAny), len(clusters), len(bridgeInfo.Strong) > 0, len(bridgeInfo.Medium) > 0)
	candidatePaths := buildCandidatePaths(clusters, candidateMode, intake, files, sigByFile, avgIntra, avgInter, bridgeInfo)
	if len(candidatePaths) == 0 {
		out.Status = "skipped_empty"
		return out, nil
	}

	if groupingEquivalent(candidatePaths, pathsAny) {
		out.Status = "no_change"
		out.PathsAfter = len(candidatePaths)
		return out, nil
	}

	shouldApply := shouldApplyGrouping(candidateMode, avgIntra, avgInter, mergeThreshold, splitThreshold, bridgeWeak, len(bridgeInfo.Strong) > 0, len(bridgeInfo.Medium) > 0)

	if !shouldApply {
		if in.WaitForUser && in.ThreadID != uuid.Nil && in.JobID != uuid.Nil && deps.Threads != nil && deps.Messages != nil {
			intake["needs_clarification"] = true
			intake["paths_confirmed"] = false
			intake["paths_refined"] = false
			intake["paths_refine_candidate"] = candidatePaths
			intake["paths_refine_mode"] = candidateMode
			intake["paths_refine_confidence"] = conf
			intake["paths_refine_reason"] = groupingReason(candidateMode, avgIntra, avgInter, bridgeInfo)
			if conf > floatFromAny(intake["confidence"], 0) {
				intake["confidence"] = conf
			}

			question := fmt.Sprintf("I can keep the current grouping or use a refined grouping (%s). Reply 1 to keep current, or 2 to use the refined grouping.", groupingReason(candidateMode, avgIntra, avgInter, bridgeInfo))
			intake["clarifying_questions"] = []any{
				map[string]any{
					"id":       "structure_choice",
					"question": question,
				},
			}

			meta["intake"] = intake
			meta["intake_refine_pending"] = true
			meta["intake_updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
			if err := deps.Path.UpdateFields(dbctx.Context{Ctx: ctx}, pathID, map[string]interface{}{
				"metadata": datatypes.JSON(mustJSON(meta)),
			}); err != nil {
				return out, err
			}

			content := formatGroupingChoiceMD(intake, pathsAny, candidatePaths)
			created, err := appendIntakeQuestionsMessage(ctx, PathIntakeDeps{
				DB:       deps.DB,
				Threads:  deps.Threads,
				Messages: deps.Messages,
				Notify:   deps.Notify,
			}, in.OwnerUserID, in.ThreadID, in.JobID, in.MaterialSetID, pathID, content, nil)
			if err != nil {
				out.Status = "skipped_low_confidence"
				return out, nil
			}

			out.Status = "waiting_user"
			out.ThreadID = in.ThreadID
			out.Meta = map[string]any{
				"question_id":  created.ID.String(),
				"question_seq": created.Seq,
				"options": []map[string]any{
					{
						"id":                 "keep_current",
						"choice":             "1",
						"label":              "Keep current grouping",
						"paths":              pathsAny,
						"prefer_single_path": len(pathsAny) == 1,
					},
					{
						"id":                 "use_refined",
						"choice":             "2",
						"label":              "Use refined grouping",
						"paths":              candidatePaths,
						"prefer_single_path": len(candidatePaths) == 1,
					},
				},
			}
			out.Intake = intake
			return out, nil
		}

		out.Status = "skipped_low_confidence"
		return out, nil
	}

	intake["paths"] = candidatePaths
	intake["needs_clarification"] = false
	intake["paths_confirmed"] = true
	intake["paths_refined"] = true
	intake["paths_refined_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	intake["paths_refine_mode"] = candidateMode
	delete(intake, "paths_refine_candidate")
	delete(intake, "clarifying_questions")
	if conf > floatFromAny(intake["confidence"], 0) {
		intake["confidence"] = conf
	}

	// Preserve primary_path_id when possible.
	primary := strings.TrimSpace(stringFromAny(intake["primary_path_id"]))
	if primary != "" {
		keep := false
		for _, p := range sliceAny(intake["paths"]) {
			if m, ok := p.(map[string]any); ok && strings.TrimSpace(stringFromAny(m["path_id"])) == primary {
				keep = true
				break
			}
		}
		if !keep {
			primary = ""
		}
	}
	if primary == "" && len(candidatePaths) > 0 {
		if m, ok := candidatePaths[0].(map[string]any); ok {
			intake["primary_path_id"] = strings.TrimSpace(stringFromAny(m["path_id"]))
		}
	}

	meta["intake"] = intake
	meta["intake_updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	meta["intake_refined_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	meta["intake_refine_pending"] = false

	if err := deps.Path.UpdateFields(dbctx.Context{Ctx: ctx}, pathID, map[string]interface{}{
		"metadata": datatypes.JSON(mustJSON(meta)),
	}); err != nil {
		return out, err
	}

	out.PathsAfter = len(candidatePaths)
	out.Intake = intake
	out.Status = "refined"
	return out, nil
}

type disjointSet struct {
	parent map[uuid.UUID]uuid.UUID
	rank   map[uuid.UUID]int
}

func newDisjointSet(ids []uuid.UUID) *disjointSet {
	ds := &disjointSet{
		parent: map[uuid.UUID]uuid.UUID{},
		rank:   map[uuid.UUID]int{},
	}
	for _, id := range ids {
		ds.parent[id] = id
		ds.rank[id] = 0
	}
	return ds
}

func (d *disjointSet) find(id uuid.UUID) uuid.UUID {
	p := d.parent[id]
	if p == id {
		return id
	}
	root := d.find(p)
	d.parent[id] = root
	return root
}

func (d *disjointSet) union(a, b uuid.UUID) {
	ra := d.find(a)
	rb := d.find(b)
	if ra == rb {
		return
	}
	if d.rank[ra] < d.rank[rb] {
		d.parent[ra] = rb
		return
	}
	if d.rank[ra] > d.rank[rb] {
		d.parent[rb] = ra
		return
	}
	d.parent[rb] = ra
	d.rank[ra]++
}

func clusterByThreshold(ids []uuid.UUID, scores map[string]float64, threshold float64) [][]uuid.UUID {
	ds := newDisjointSet(ids)
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			key := pairKey(ids[i], ids[j])
			if scores[key] >= threshold {
				ds.union(ids[i], ids[j])
			}
		}
	}
	clusters := map[uuid.UUID][]uuid.UUID{}
	for _, id := range ids {
		root := ds.find(id)
		clusters[root] = append(clusters[root], id)
	}
	out := make([][]uuid.UUID, 0, len(clusters))
	for _, group := range clusters {
		sort.Slice(group, func(i, j int) bool { return group[i].String() < group[j].String() })
		out = append(out, group)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i][0].String() < out[j][0].String()
	})
	return out
}

func clusterSeparationScores(clusters [][]uuid.UUID, scores map[string]float64) (float64, float64) {
	var intraSum float64
	var intraN int
	for _, group := range clusters {
		if len(group) < 2 {
			continue
		}
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				intraSum += scores[pairKey(group[i], group[j])]
				intraN++
			}
		}
	}

	var interSum float64
	var interN int
	if len(clusters) > 1 {
		for i := 0; i < len(clusters); i++ {
			for j := i + 1; j < len(clusters); j++ {
				for _, a := range clusters[i] {
					for _, b := range clusters[j] {
						interSum += scores[pairKey(a, b)]
						interN++
					}
				}
			}
		}
	}

	intraAvg := 0.0
	interAvg := 0.0
	if intraN > 0 {
		intraAvg = intraSum / float64(intraN)
	}
	if interN > 0 {
		interAvg = interSum / float64(interN)
	}
	return intraAvg, interAvg
}

type bridgeInfo struct {
	Strong map[uuid.UUID]bool
	Medium map[uuid.UUID]bool
}

func detectBridgeStrengths(clusters [][]uuid.UUID, scores map[string]float64, strong float64, weak float64) bridgeInfo {
	info := bridgeInfo{
		Strong: map[uuid.UUID]bool{},
		Medium: map[uuid.UUID]bool{},
	}
	if len(clusters) < 2 || strong <= 0 {
		return info
	}
	if weak <= 0 {
		weak = strong * 0.6
	}
	for _, group := range clusters {
		for _, id := range group {
			avgByCluster := make([]float64, 0, len(clusters))
			for _, other := range clusters {
				if len(other) == 0 {
					continue
				}
				var sum float64
				var n int
				for _, oid := range other {
					if oid == id {
						continue
					}
					sum += scores[pairKey(id, oid)]
					n++
				}
				if n > 0 {
					avgByCluster = append(avgByCluster, sum/float64(n))
				}
			}
			if len(avgByCluster) < 2 {
				continue
			}
			sort.Slice(avgByCluster, func(i, j int) bool { return avgByCluster[i] > avgByCluster[j] })
			top := avgByCluster[0]
			second := avgByCluster[1]
			if top >= strong && second >= strong {
				info.Strong[id] = true
				continue
			}
			if top >= strong && second >= weak {
				info.Medium[id] = true
				continue
			}
			if top >= weak && second >= weak {
				info.Medium[id] = true
			}
		}
	}
	return info
}

func computePairScores(files []*types.MaterialFile, sigs map[uuid.UUID]*types.MaterialFileSignature) map[string]float64 {
	out := map[string]float64{}
	for i := 0; i < len(files); i++ {
		fa := files[i]
		if fa == nil || fa.ID == uuid.Nil {
			continue
		}
		sa := sigs[fa.ID]
		var embA []float32
		if sa != nil {
			embA, _ = decodeEmbedding(sa.SummaryEmbedding)
		}
		toksA := signatureTokens(sa)
		domA := signatureDomains(sa)
		outlineA := signatureOutlineTokens(sa)
		for j := i + 1; j < len(files); j++ {
			fb := files[j]
			if fb == nil || fb.ID == uuid.Nil {
				continue
			}
			sb := sigs[fb.ID]
			var embB []float32
			if sb != nil {
				embB, _ = decodeEmbedding(sb.SummaryEmbedding)
			}
			embScore := cosineSim(embA, embB)
			toksB := signatureTokens(sb)
			topicScore := jaccardScore(toksA, toksB)
			domScore := jaccardScore(domA, signatureDomains(sb))
			outlineScore := jaccardScore(outlineA, signatureOutlineTokens(sb))

			score := 0.65*embScore + 0.2*topicScore + 0.1*domScore + 0.05*outlineScore
			score -= difficultyPenalty(sa, sb)

			if domScore >= 0.6 && topicScore >= 0.3 {
				score += 0.08
			}
			if domScore == 0 && topicScore < 0.05 && embScore < 0.25 {
				score *= 0.7
			}

			out[pairKey(fa.ID, fb.ID)] = clamp01(score)
		}
	}
	return out
}

func applyCrossEncoderScores(ctx context.Context, files []*types.MaterialFile, sigs map[uuid.UUID]*types.MaterialFileSignature, base map[string]float64) map[string]float64 {
	model := strings.TrimSpace(os.Getenv("NB_INFERENCE_SCORE_MODEL"))
	if model == "" {
		return base
	}
	client, err := infclient.NewFromEnv()
	if err != nil {
		return base
	}

	topK := envIntAllowZero("PATH_GROUPING_PAIR_TOPK", 12)
	if topK <= 0 {
		return base
	}

	pairs := selectTopPairs(files, sigs, base, topK)
	if len(pairs) == 0 {
		return base
	}
	reqPairs := make([]infclient.TextScorePair, 0, len(pairs))
	for _, p := range pairs {
		reqPairs = append(reqPairs, infclient.TextScorePair{A: p.A, B: p.B})
	}
	scores, err := client.ScorePairs(ctx, reqPairs)
	if err != nil {
		if errors.Is(err, infclient.ErrScoreNotConfigured) || errors.Is(err, infclient.ErrScoreNotSupported) {
			return base
		}
		return base
	}
	if len(scores) != len(pairs) {
		return base
	}

	for i, p := range pairs {
		key := pairKey(p.IDA, p.IDB)
		baseScore := base[key]
		ceScore := float64(scores[i])
		base[key] = 0.6*ceScore + 0.4*baseScore
	}
	return base
}

type scorePair struct {
	IDA uuid.UUID
	IDB uuid.UUID
	A   string
	B   string
}

func selectTopPairs(files []*types.MaterialFile, sigs map[uuid.UUID]*types.MaterialFileSignature, base map[string]float64, topK int) []scorePair {
	if topK <= 0 {
		return nil
	}
	perFile := map[uuid.UUID][]scorePair{}
	for i := 0; i < len(files); i++ {
		fa := files[i]
		if fa == nil || fa.ID == uuid.Nil {
			continue
		}
		for j := i + 1; j < len(files); j++ {
			fb := files[j]
			if fb == nil || fb.ID == uuid.Nil {
				continue
			}
			pair := scorePair{
				IDA: fa.ID,
				IDB: fb.ID,
				A:   describeFileForScore(fa, sigs[fa.ID]),
				B:   describeFileForScore(fb, sigs[fb.ID]),
			}
			perFile[fa.ID] = append(perFile[fa.ID], pair)
			perFile[fb.ID] = append(perFile[fb.ID], pair)
		}
	}

	seen := map[string]bool{}
	out := []scorePair{}
	for _, pairs := range perFile {
		sort.Slice(pairs, func(i, j int) bool {
			ai := base[pairKey(pairs[i].IDA, pairs[i].IDB)]
			aj := base[pairKey(pairs[j].IDA, pairs[j].IDB)]
			return ai > aj
		})
		for i := 0; i < len(pairs) && i < topK; i++ {
			p := pairs[i]
			key := pairKey(p.IDA, p.IDB)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, p)
		}
	}
	return out
}

type groupingPrefs struct {
	PreferSingle bool
	PreferMulti  bool
	MergeBias    float64
}

func loadGroupingPrefs(ctx context.Context, deps PathGroupingRefineDeps, userID uuid.UUID) groupingPrefs {
	if deps.Prefs == nil || userID == uuid.Nil {
		return groupingPrefs{}
	}
	row, err := deps.Prefs.GetByUserID(dbctx.Context{Ctx: ctx}, userID)
	if err != nil || row == nil || len(row.PrefsJSON) == 0 {
		return groupingPrefs{}
	}
	var prefs map[string]any
	if err := json.Unmarshal(row.PrefsJSON, &prefs); err != nil {
		return groupingPrefs{}
	}
	pg := mapFromAny(prefs["path_grouping"])
	if pg == nil {
		return groupingPrefs{}
	}
	return groupingPrefs{
		PreferSingle: boolFromAny(pg["prefer_single_path"]),
		PreferMulti:  boolFromAny(pg["prefer_multi_path"]),
		MergeBias:    floatFromAny(pg["merge_bias"], 0),
	}
}

func applyGroupingPrefs(merge, split, strong, weak *float64, prefs groupingPrefs) {
	if merge == nil || split == nil || strong == nil || weak == nil {
		return
	}
	if prefs.PreferSingle {
		*split = clamp01(*split - 0.05)
		*merge = clamp01(*merge - 0.03)
	}
	if prefs.PreferMulti {
		*split = clamp01(*split + 0.05)
		*merge = clamp01(*merge + 0.03)
	}
	if prefs.MergeBias != 0 {
		*merge = clamp01(*merge + prefs.MergeBias)
	}
	if *weak > *strong {
		*weak = *strong * 0.85
	}
}

func groupingMode(pathsBefore int, clusterCount int, hasStrongBridge bool, hasMediumBridge bool) string {
	if clusterCount <= 1 {
		if pathsBefore > 1 {
			return "merge"
		}
		return "single"
	}
	if hasStrongBridge || hasMediumBridge {
		return "segmented"
	}
	if pathsBefore <= 1 {
		return "split"
	}
	return "recluster"
}

func shouldApplyGrouping(mode string, intra, inter, mergeTh, splitTh, bridgeWeak float64, hasStrongBridge bool, hasMediumBridge bool) bool {
	switch mode {
	case "merge":
		return intra >= mergeTh
	case "split":
		return inter <= splitTh && !hasStrongBridge && !hasMediumBridge
	case "recluster":
		return intra >= mergeTh && inter <= splitTh && !hasStrongBridge && !hasMediumBridge
	case "segmented":
		return (hasStrongBridge || hasMediumBridge) && inter >= bridgeWeak
	default:
		return false
	}
}

func groupingReason(mode string, intra, inter float64, bridges bridgeInfo) string {
	parts := []string{}
	switch mode {
	case "merge":
		parts = append(parts, fmt.Sprintf("high coherence (intra %.2f)", intra))
	case "split":
		parts = append(parts, fmt.Sprintf("low cross-similarity (inter %.2f)", inter))
	case "recluster":
		parts = append(parts, fmt.Sprintf("clusters separate (intra %.2f / inter %.2f)", intra, inter))
	case "segmented":
		parts = append(parts, "bridge files connect clusters")
	}
	if len(bridges.Strong) > 0 {
		parts = append(parts, fmt.Sprintf("%d strong bridge file(s)", len(bridges.Strong)))
	} else if len(bridges.Medium) > 0 {
		parts = append(parts, fmt.Sprintf("%d bridge file(s)", len(bridges.Medium)))
	}
	if len(parts) == 0 {
		return "uncertain similarity"
	}
	return strings.Join(parts, "; ")
}

func buildCandidatePaths(clusters [][]uuid.UUID, mode string, intake map[string]any, files []*types.MaterialFile, sigs map[uuid.UUID]*types.MaterialFileSignature, intra, inter float64, bridges bridgeInfo) []any {
	if mode == "segmented" {
		return buildSinglePathWithSegments(clusters, intake, files, sigs, intra, inter, bridges)
	}
	return buildPathsFromClusters(clusters, intake, files, sigs, intra, inter, bridges)
}

func buildSinglePathWithSegments(clusters [][]uuid.UUID, intake map[string]any, files []*types.MaterialFile, sigs map[uuid.UUID]*types.MaterialFileSignature, intra, inter float64, bridges bridgeInfo) []any {
	if len(clusters) == 0 {
		return nil
	}
	allIDs := make([]string, 0, 32)
	for _, group := range clusters {
		for _, id := range group {
			allIDs = append(allIDs, id.String())
		}
	}
	allIDs = dedupeStrings(allIDs)

	pathID := "path_1"
	title := ""
	goal := ""
	if len(sliceAny(intake["paths"])) == 1 {
		if m, ok := sliceAny(intake["paths"])[0].(map[string]any); ok && m != nil {
			if v := strings.TrimSpace(stringFromAny(m["path_id"])); v != "" {
				pathID = v
			}
			title = strings.TrimSpace(stringFromAny(m["title"]))
			goal = strings.TrimSpace(stringFromAny(m["goal"]))
		}
	}
	if title == "" {
		title = deriveTitleFromSignatures(flattenClusterIDs(clusters), files, sigs)
	}
	if goal == "" {
		if title == "" {
			goal = "Learn the uploaded materials"
		} else {
			goal = "Learn " + title
		}
	}

	notes := buildEvidenceNote(flattenClusterIDs(clusters), sigs, intra, inter)
	if len(clusters) > 1 {
		notes = strings.TrimSpace(notes) + " Segments reflect bridged subtopics."
	}
	if len(bridges.Strong) > 0 || len(bridges.Medium) > 0 {
		bridgeNames := bridgeNamesInGroup(flattenClusterIDs(clusters), files, bridges.Strong, bridges.Medium)
		if len(bridgeNames) > 0 {
			notes = strings.TrimSpace(notes) + " Bridge file(s): " + strings.Join(bridgeNames, ", ") + "."
		}
	}

	segments := buildSegmentsFromClusters(clusters, files, sigs, intra, inter)
	bridgeIDs := make([]string, 0, len(bridges.Strong)+len(bridges.Medium))
	for id := range bridges.Strong {
		bridgeIDs = append(bridgeIDs, id.String())
	}
	for id := range bridges.Medium {
		if !containsString(bridgeIDs, id.String()) {
			bridgeIDs = append(bridgeIDs, id.String())
		}
	}

	path := map[string]any{
		"path_id":               pathID,
		"title":                 title,
		"goal":                  goal,
		"core_file_ids":         allIDs,
		"support_file_ids":      []string{},
		"confidence":            clamp01((intra - inter + 1) / 2),
		"notes":                 notes,
		"segments":              segments,
		"segment_bridge_file_ids": bridgeIDs,
	}
	return []any{path}
}

func buildSegmentsFromClusters(clusters [][]uuid.UUID, files []*types.MaterialFile, sigs map[uuid.UUID]*types.MaterialFileSignature, intra, inter float64) []any {
	out := make([]any, 0, len(clusters))
	for i, group := range clusters {
		ids := make([]string, 0, len(group))
		for _, id := range group {
			ids = append(ids, id.String())
		}
		ids = dedupeStrings(ids)
		title := deriveTitleFromSignatures(group, files, sigs)
		notes := buildEvidenceNote(group, sigs, intra, inter)
		out = append(out, map[string]any{
			"segment_id": fmt.Sprintf("segment_%d", i+1),
			"title":      title,
			"file_ids":   ids,
			"notes":      notes,
		})
	}
	return out
}

func buildEvidenceNote(group []uuid.UUID, sigs map[uuid.UUID]*types.MaterialFileSignature, intra, inter float64) string {
	topics := map[string]int{}
	domains := map[string]int{}
	difficulties := map[string]int{}
	for _, id := range group {
		sig := sigs[id]
		for _, t := range signatureTopics(sig) {
			topics[t]++
		}
		for _, d := range signatureDomains(sig) {
			domains[d]++
		}
		if sig != nil {
			diff := strings.TrimSpace(strings.ToLower(sig.Difficulty))
			if diff != "" && diff != "unknown" {
				difficulties[diff]++
			}
		}
	}
	sharedMin := 1
	if len(group) >= 3 {
		sharedMin = 2
	}
	if len(group) >= 6 {
		sharedMin = 3
	}

	domainTop := topTokens(domains, 3, sharedMin)
	if len(domainTop) == 0 {
		domainTop = topTokens(domains, 3, 1)
	}
	topicTop := topTokens(topics, 3, sharedMin)
	if len(topicTop) == 0 {
		topicTop = topTokens(topics, 3, 1)
	}

	diff := difficultySummary(difficulties, len(group))

	parts := []string{}
	if len(domainTop) > 0 {
		parts = append(parts, "Domains: "+strings.Join(domainTop, ", "))
	}
	if len(topicTop) > 0 {
		parts = append(parts, "Topics: "+strings.Join(topicTop, ", "))
	}
	if diff != "" {
		parts = append(parts, "Difficulty: "+diff)
	}
	parts = append(parts, fmt.Sprintf("intra-sim=%.2f, inter-sim=%.2f", intra, inter))
	return strings.Join(parts, ". ") + "."
}

func topTokens(counts map[string]int, max int, minCount int) []string {
	if len(counts) == 0 || max <= 0 {
		return nil
	}
	type pair struct {
		Token string
		Count int
	}
	arr := make([]pair, 0, len(counts))
	for k, v := range counts {
		if k == "" || v < minCount {
			continue
		}
		arr = append(arr, pair{Token: k, Count: v})
	}
	sort.Slice(arr, func(i, j int) bool {
		if arr[i].Count != arr[j].Count {
			return arr[i].Count > arr[j].Count
		}
		return arr[i].Token < arr[j].Token
	})
	if len(arr) > max {
		arr = arr[:max]
	}
	out := make([]string, 0, len(arr))
	for _, p := range arr {
		out = append(out, titleCase(p.Token))
	}
	return out
}

func difficultySummary(counts map[string]int, total int) string {
	if total <= 0 || len(counts) == 0 {
		return ""
	}
	if len(counts) == 1 {
		for k := range counts {
			return k
		}
	}
	if counts["mixed"] > 0 {
		return "mixed"
	}
	return "mixed"
}

func formatGroupingChoiceMD(intake map[string]any, current []any, refined []any) string {
	var b strings.Builder
	b.WriteString("I can keep the current grouping or use a refined grouping.\n\n")
	if md := formatPathsPreview(intake, current); md != "" {
		b.WriteString("Option 1 — Keep current:\n")
		b.WriteString(md)
		b.WriteString("\n\n")
	}
	if md := formatPathsPreview(intake, refined); md != "" {
		b.WriteString("Option 2 — Use refined:\n")
		b.WriteString(md)
		b.WriteString("\n\n")
	}
	b.WriteString("Reply 1 or 2.")
	return strings.TrimSpace(b.String())
}

func formatPathsPreview(intake map[string]any, paths []any) string {
	if intake == nil || len(paths) == 0 {
		return ""
	}
	tmp := map[string]any{"paths": paths}
	if v, ok := intake["file_intents"]; ok {
		tmp["file_intents"] = v
	}
	return formatProposedPathsMD(tmp)
}

func containsString(in []string, target string) bool {
	for _, v := range in {
		if v == target {
			return true
		}
	}
	return false
}

func flattenClusterIDs(clusters [][]uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(clusters))
	for _, group := range clusters {
		out = append(out, group...)
	}
	return out
}

func buildPathsFromClusters(clusters [][]uuid.UUID, intake map[string]any, files []*types.MaterialFile, sigs map[uuid.UUID]*types.MaterialFileSignature, intra, inter float64, bridges bridgeInfo) []any {
	if len(clusters) == 0 {
		return nil
	}
	existing := map[string]map[string]any{}
	for _, raw := range sliceAny(intake["paths"]) {
		m, ok := raw.(map[string]any)
		if !ok || m == nil {
			continue
		}
		groupKey := groupKeyFromPath(m)
		if groupKey != "" {
			existing[groupKey] = m
		}
	}

	out := make([]any, 0, len(clusters))
	for i, group := range clusters {
		ids := make([]string, 0, len(group))
		for _, id := range group {
			ids = append(ids, id.String())
		}
		groupKey := strings.Join(dedupeStrings(ids), "|")
		pathID := fmt.Sprintf("path_%d", i+1)
		title := ""
		goal := ""
		notes := buildEvidenceNote(group, sigs, intra, inter)
		if existing[groupKey] != nil {
			title = strings.TrimSpace(stringFromAny(existing[groupKey]["title"]))
			goal = strings.TrimSpace(stringFromAny(existing[groupKey]["goal"]))
			notes = strings.TrimSpace(stringFromAny(existing[groupKey]["notes"]))
			if notes != "" {
				notes = notes + " "
			}
			notes += buildEvidenceNote(group, sigs, intra, inter)
			if id := strings.TrimSpace(stringFromAny(existing[groupKey]["path_id"])); id != "" {
				pathID = id
			}
		}
		if title == "" {
			title = deriveTitleFromSignatures(group, files, sigs)
		}
		if goal == "" {
			if title == "" {
				goal = "Learn the uploaded materials"
			} else {
				goal = "Learn " + title
			}
		}
		if len(bridges.Strong) > 0 || len(bridges.Medium) > 0 {
			bridgeNames := bridgeNamesInGroup(group, files, bridges.Strong, bridges.Medium)
			if len(bridgeNames) > 0 {
				notes = strings.TrimSpace(notes) + " Bridge file(s): " + strings.Join(bridgeNames, ", ") + "."
			}
		}
		out = append(out, map[string]any{
			"path_id":          pathID,
			"title":            title,
			"goal":             goal,
			"core_file_ids":    ids,
			"support_file_ids": []string{},
			"confidence":       clamp01((intra - inter + 1) / 2),
			"notes":            notes,
		})
	}
	return out
}

func groupKeyFromPath(m map[string]any) string {
	if m == nil {
		return ""
	}
	ids := dedupeStrings(append(
		stringSliceFromAny(m["core_file_ids"]),
		stringSliceFromAny(m["support_file_ids"])...,
	))
	sort.Strings(ids)
	return strings.Join(ids, "|")
}

func deriveTitleFromSignatures(group []uuid.UUID, files []*types.MaterialFile, sigs map[uuid.UUID]*types.MaterialFileSignature) string {
	tokenCounts := map[string]int{}
	for _, id := range group {
		sig := sigs[id]
		for _, t := range signatureTokens(sig) {
			tokenCounts[t]++
		}
	}
	type scored struct {
		Token string
		Count int
	}
	scoredArr := make([]scored, 0, len(tokenCounts))
	for k, v := range tokenCounts {
		scoredArr = append(scoredArr, scored{Token: k, Count: v})
	}
	sort.Slice(scoredArr, func(i, j int) bool {
		if scoredArr[i].Count != scoredArr[j].Count {
			return scoredArr[i].Count > scoredArr[j].Count
		}
		return scoredArr[i].Token < scoredArr[j].Token
	})
	if len(scoredArr) > 0 {
		return titleCase(scoredArr[0].Token)
	}
	for _, f := range files {
		if f == nil {
			continue
		}
		for _, id := range group {
			if f.ID == id && strings.TrimSpace(f.OriginalName) != "" {
				return strings.TrimSpace(f.OriginalName)
			}
		}
	}
	return ""
}

func bridgeNamesInGroup(group []uuid.UUID, files []*types.MaterialFile, strong map[uuid.UUID]bool, medium map[uuid.UUID]bool) []string {
	if len(group) == 0 || (len(strong) == 0 && len(medium) == 0) {
		return nil
	}
	nameByID := map[uuid.UUID]string{}
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		nameByID[f.ID] = strings.TrimSpace(f.OriginalName)
	}
	out := []string{}
	for _, id := range group {
		if !strong[id] && !medium[id] {
			continue
		}
		name := nameByID[id]
		if name == "" {
			name = id.String()
		}
		out = append(out, name)
	}
	return dedupeStrings(out)
}

func signatureTokens(sig *types.MaterialFileSignature) []string {
	if sig == nil {
		return nil
	}
	out := []string{}
	out = append(out, parseJSONStrings(sig.Topics)...)
	out = append(out, parseJSONStrings(sig.DomainTags)...)
	out = append(out, parseJSONStrings(sig.ConceptKeys)...)
	for i := range out {
		out[i] = strings.ToLower(strings.TrimSpace(out[i]))
	}
	return dedupeStrings(out)
}

func signatureDomains(sig *types.MaterialFileSignature) []string {
	if sig == nil {
		return nil
	}
	return normalizeTokens(parseJSONStrings(sig.DomainTags))
}

func signatureTopics(sig *types.MaterialFileSignature) []string {
	if sig == nil {
		return nil
	}
	return normalizeTokens(parseJSONStrings(sig.Topics))
}

func signatureConcepts(sig *types.MaterialFileSignature) []string {
	if sig == nil {
		return nil
	}
	return normalizeTokens(parseJSONStrings(sig.ConceptKeys))
}

func signatureOutlineTokens(sig *types.MaterialFileSignature) []string {
	if sig == nil || len(sig.OutlineJSON) == 0 {
		return nil
	}
	var outline map[string]any
	if err := json.Unmarshal(sig.OutlineJSON, &outline); err != nil {
		return nil
	}
	sections := sliceAny(outline["sections"])
	out := make([]string, 0, len(sections))
	for _, raw := range sections {
		m, ok := raw.(map[string]any)
		if !ok || m == nil {
			continue
		}
		title := strings.TrimSpace(stringFromAny(m["title"]))
		if title == "" {
			continue
		}
		out = append(out, normalizeToken(title))
	}
	return dedupeStrings(out)
}

func normalizeTokens(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, t := range in {
		if v := normalizeToken(t); v != "" {
			out = append(out, v)
		}
	}
	return dedupeStrings(out)
}

func normalizeToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func difficultyPenalty(a *types.MaterialFileSignature, b *types.MaterialFileSignature) float64 {
	ra := difficultyRank("")
	rb := difficultyRank("")
	if a != nil {
		ra = difficultyRank(a.Difficulty)
	}
	if b != nil {
		rb = difficultyRank(b.Difficulty)
	}
	if ra < 0 || rb < 0 {
		return 0
	}
	diff := ra - rb
	if diff < 0 {
		diff = -diff
	}
	switch diff {
	case 0:
		return 0
	case 1:
		return 0.07
	default:
		return 0.15
	}
}

func difficultyRank(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "intro", "beginner":
		return 0
	case "intermediate":
		return 1
	case "advanced":
		return 2
	case "mixed":
		return 1
	default:
		return -1
	}
}

func describeFileForScore(f *types.MaterialFile, sig *types.MaterialFileSignature) string {
	var b strings.Builder
	if f != nil {
		b.WriteString("name: ")
		b.WriteString(strings.TrimSpace(f.OriginalName))
		b.WriteString("\n")
	}
	if sig != nil {
		if s := strings.TrimSpace(sig.SummaryMD); s != "" {
			b.WriteString("summary: ")
			b.WriteString(shorten(s, 400))
			b.WriteString("\n")
		}
		toks := signatureTokens(sig)
		if len(toks) > 0 {
			b.WriteString("topics: ")
			if len(toks) > 12 {
				toks = toks[:12]
			}
			b.WriteString(strings.Join(toks, ", "))
		}
	}
	return strings.TrimSpace(b.String())
}

func jaccardScore(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setA := map[string]bool{}
	for _, t := range a {
		if t != "" {
			setA[t] = true
		}
	}
	if len(setA) == 0 {
		return 0
	}
	inter := 0
	union := len(setA)
	for _, t := range b {
		if t == "" {
			continue
		}
		if setA[t] {
			inter++
		} else {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func pairKey(a, b uuid.UUID) string {
	if a.String() < b.String() {
		return a.String() + "|" + b.String()
	}
	return b.String() + "|" + a.String()
}

func groupingEquivalent(candidate []any, paths []any) bool {
	candKeys := make([]string, 0, len(candidate))
	for _, raw := range candidate {
		m, ok := raw.(map[string]any)
		if !ok || m == nil {
			continue
		}
		ids := dedupeStrings(append(stringSliceFromAny(m["core_file_ids"]), stringSliceFromAny(m["support_file_ids"])...))
		sort.Strings(ids)
		candKeys = append(candKeys, strings.Join(ids, "|"))
	}
	sort.Strings(candKeys)

	pathKeys := make([]string, 0, len(paths))
	for _, raw := range paths {
		m, ok := raw.(map[string]any)
		if !ok || m == nil {
			continue
		}
		ids := dedupeStrings(append(stringSliceFromAny(m["core_file_ids"]), stringSliceFromAny(m["support_file_ids"])...))
		sort.Strings(ids)
		pathKeys = append(pathKeys, strings.Join(ids, "|"))
	}
	sort.Strings(pathKeys)

	if len(candKeys) != len(pathKeys) {
		return false
	}
	for i := range candKeys {
		if candKeys[i] != pathKeys[i] {
			return false
		}
	}
	return true
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	parts := strings.Fields(strings.ReplaceAll(s, "_", " "))
	for i := range parts {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:])
	}
	return strings.Join(parts, " ")
}
