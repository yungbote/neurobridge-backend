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

	infclient "github.com/yungbote/neurobridge-backend/internal/inference/client"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type PathGroupingRefineDeps struct {
	DB       *gorm.DB
	Log      *logger.Logger
	Path     repos.PathRepo
	Files    repos.MaterialFileRepo
	FileSigs repos.MaterialFileSignatureRepo
}

type PathGroupingRefineInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	PathID        uuid.UUID
}

type PathGroupingRefineOutput struct {
	PathID        uuid.UUID `json:"path_id"`
	Status        string    `json:"status"`
	PathsBefore   int       `json:"paths_before"`
	PathsAfter    int       `json:"paths_after"`
	FilesConsidered int     `json:"files_considered"`
	Confidence      float64 `json:"confidence"`
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

	pairScores := computePairScores(files, sigByFile)
	pairScores = applyCrossEncoderScores(ctx, files, sigByFile, pairScores)

	mergeThreshold := envFloatAllowZero("PATH_GROUPING_MIN_CONFIDENCE_MERGE", 0.60)
	splitThreshold := envFloatAllowZero("PATH_GROUPING_MIN_CONFIDENCE_SPLIT", 0.55)
	bridgeStrong := envFloatAllowZero("PATH_GROUPING_BRIDGE_STRONG", 0.70)

	clusters := clusterByThreshold(fileIDs, pairScores, mergeThreshold)
	if len(clusters) == 0 {
		return out, nil
	}
	if groupingEquivalent(clusters, pathsAny) {
		out.Status = "no_change"
		out.PathsAfter = len(clusters)
		return out, nil
	}

	avgIntra, avgInter := clusterSeparationScores(clusters, pairScores)
	conf := clamp01((avgIntra - avgInter + 1) / 2)
	out.Confidence = conf

	bridgeIDs := detectBridgeFiles(clusters, pairScores, bridgeStrong)

	shouldApply := false
	if len(pathsAny) == 1 && len(clusters) > 1 && avgInter <= splitThreshold && len(bridgeIDs) == 0 {
		shouldApply = true
	}
	if len(pathsAny) > 1 && len(clusters) == 1 && avgIntra >= mergeThreshold {
		shouldApply = true
	}
	if len(pathsAny) > 1 && len(clusters) > 1 && avgIntra >= mergeThreshold && avgInter <= splitThreshold && len(bridgeIDs) == 0 {
		shouldApply = true
	}
	if !shouldApply {
		out.Status = "skipped_low_confidence"
		return out, nil
	}

	newPaths := buildPathsFromClusters(clusters, intake, files, sigByFile, avgIntra, avgInter, bridgeIDs)
	if len(newPaths) == 0 {
		out.Status = "skipped_empty"
		return out, nil
	}

	intake["paths"] = newPaths
	intake["needs_clarification"] = false
	intake["paths_confirmed"] = true
	intake["paths_refined"] = true
	intake["paths_refined_at"] = time.Now().UTC().Format(time.RFC3339Nano)
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
	if primary == "" && len(newPaths) > 0 {
		if m, ok := newPaths[0].(map[string]any); ok {
			intake["primary_path_id"] = strings.TrimSpace(stringFromAny(m["path_id"]))
		}
	}

	meta["intake"] = intake
	meta["intake_updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	meta["intake_refined_at"] = time.Now().UTC().Format(time.RFC3339Nano)

	if err := deps.Path.UpdateFields(dbctx.Context{Ctx: ctx}, pathID, map[string]interface{}{
		"metadata": datatypes.JSON(mustJSON(meta)),
	}); err != nil {
		return out, err
	}

	out.PathsAfter = len(newPaths)
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

func detectBridgeFiles(clusters [][]uuid.UUID, scores map[string]float64, strong float64) map[uuid.UUID]bool {
	if len(clusters) < 2 || strong <= 0 {
		return nil
	}
	out := map[uuid.UUID]bool{}
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
			if avgByCluster[0] >= strong && avgByCluster[1] >= strong {
				out[id] = true
			}
		}
	}
	return out
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
			score := 0.7*embScore + 0.3*topicScore
			out[pairKey(fa.ID, fb.ID)] = score
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

func buildPathsFromClusters(clusters [][]uuid.UUID, intake map[string]any, files []*types.MaterialFile, sigs map[uuid.UUID]*types.MaterialFileSignature, intra, inter float64, bridgeIDs map[uuid.UUID]bool) []any {
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
		notes := fmt.Sprintf("Refined grouping: intra-sim=%.2f, inter-sim=%.2f.", intra, inter)
		if existing[groupKey] != nil {
			title = strings.TrimSpace(stringFromAny(existing[groupKey]["title"]))
			goal = strings.TrimSpace(stringFromAny(existing[groupKey]["goal"]))
			notes = strings.TrimSpace(stringFromAny(existing[groupKey]["notes"]))
			if notes != "" {
				notes = notes + " "
			}
			notes += fmt.Sprintf("Refined grouping: intra-sim=%.2f, inter-sim=%.2f.", intra, inter)
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
		if len(bridgeIDs) > 0 {
			bridgeNames := bridgeNamesInGroup(group, files, bridgeIDs)
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

func bridgeNamesInGroup(group []uuid.UUID, files []*types.MaterialFile, bridgeIDs map[uuid.UUID]bool) []string {
	if len(group) == 0 || len(bridgeIDs) == 0 {
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
		if !bridgeIDs[id] {
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

func groupingEquivalent(clusters [][]uuid.UUID, paths []any) bool {
	clusterKeys := make([]string, 0, len(clusters))
	for _, group := range clusters {
		ids := make([]string, 0, len(group))
		for _, id := range group {
			ids = append(ids, id.String())
		}
		sort.Strings(ids)
		clusterKeys = append(clusterKeys, strings.Join(ids, "|"))
	}
	sort.Strings(clusterKeys)

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

	if len(clusterKeys) != len(pathKeys) {
		return false
	}
	for i := range clusterKeys {
		if clusterKeys[i] != pathKeys[i] {
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
