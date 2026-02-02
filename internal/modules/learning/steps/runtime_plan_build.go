package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

const runtimePlanModelEnv = "RUNTIME_PLAN_MODEL"

type RuntimePlanBuildDeps struct {
	DB          *gorm.DB
	Log         *logger.Logger
	Path        repos.PathRepo
	PathNodes   repos.PathNodeRepo
	NodeDocs    repos.LearningNodeDocRepo
	Summaries   repos.MaterialSetSummaryRepo
	UserProfile repos.UserProfileVectorRepo
	ProgEvents  repos.UserProgressionEventRepo
	AI          openai.Client
	Bootstrap   services.LearningBuildBootstrapService
}

type RuntimePlanBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
	Force         bool
	Model         string
}

type RuntimePlanBuildOutput struct {
	PathID   uuid.UUID      `json:"path_id"`
	Nodes    int            `json:"nodes"`
	Plan     map[string]any `json:"plan,omitempty"`
	Adaptive map[string]any `json:"adaptive,omitempty"`
}

type runtimePlanBreakPolicy struct {
	AfterMinutes    int `json:"after_minutes"`
	MinBreakMinutes int `json:"min_break_minutes"`
	MaxBreakMinutes int `json:"max_break_minutes"`
}

type runtimePlanQuickCheckPolicy struct {
	AfterBlocks  int `json:"after_blocks"`
	AfterMinutes int `json:"after_minutes"`
	MaxPerLesson int `json:"max_per_lesson"`
	MinGapBlocks int `json:"min_gap_blocks"`
}

type runtimePlanFlashcardPolicy struct {
	AfterBlocks     int `json:"after_blocks"`
	AfterMinutes    int `json:"after_minutes"`
	AfterFailStreak int `json:"after_fail_streak"`
	MaxPerLesson    int `json:"max_per_lesson"`
}

type runtimePlanWeights struct {
	Mastery   float64 `json:"mastery"`
	Retention float64 `json:"retention"`
	Pace      float64 `json:"pace"`
	Fatigue   float64 `json:"fatigue"`
}

type runtimePlanMultipliers struct {
	Break      float64 `json:"break"`
	QuickCheck float64 `json:"quick_check"`
	Flashcard  float64 `json:"flashcard"`
}

type runtimePlanPolicy struct {
	TargetSessionMinutes int                         `json:"target_session_minutes"`
	MaxPromptsPerHour    int                         `json:"max_prompts_per_hour"`
	BreakPolicy          runtimePlanBreakPolicy      `json:"break_policy"`
	QuickCheckPolicy     runtimePlanQuickCheckPolicy `json:"quick_check_policy"`
	FlashcardPolicy      runtimePlanFlashcardPolicy  `json:"flashcard_policy"`
	PolicyProfile        string                      `json:"policy_profile"`
	ObjectiveWeights     runtimePlanWeights          `json:"objective_weights"`
	CadenceMultipliers   runtimePlanMultipliers      `json:"cadence_multipliers"`
}

type runtimePlanModule struct {
	ModuleIndex          int                         `json:"module_index"`
	TargetSessionMinutes int                         `json:"target_session_minutes"`
	BreakPolicy          runtimePlanBreakPolicy      `json:"break_policy"`
	QuickCheckPolicy     runtimePlanQuickCheckPolicy `json:"quick_check_policy"`
	FlashcardPolicy      runtimePlanFlashcardPolicy  `json:"flashcard_policy"`
	PolicyProfile        string                      `json:"policy_profile"`
}

type runtimePlanLesson struct {
	NodeID           uuid.UUID                   `json:"node_id"`
	NodeIndex        int                         `json:"node_index"`
	LessonIndex      int                         `json:"lesson_index"`
	EstimatedMinutes int                         `json:"estimated_minutes"`
	BreakPolicy      runtimePlanBreakPolicy      `json:"break_policy"`
	QuickCheckPolicy runtimePlanQuickCheckPolicy `json:"quick_check_policy"`
	FlashcardPolicy  runtimePlanFlashcardPolicy  `json:"flashcard_policy"`
	PolicyProfile    string                      `json:"policy_profile"`
}

type runtimePlan struct {
	SchemaVersion int                 `json:"schema_version"`
	GeneratedAt   string              `json:"generated_at"`
	Model         string              `json:"model,omitempty"`
	Source        string              `json:"source"`
	Path          runtimePlanPolicy   `json:"path"`
	Modules       []runtimePlanModule `json:"modules"`
	Lessons       []runtimePlanLesson `json:"lessons"`
}

type runtimeUserStats struct {
	EventCount     int
	AvgScore       float64
	AvgAttempts    float64
	AvgDwellMS     float64
	CompletionRate float64
	RecentCount    int
	LastEventAt    *time.Time
}

type runtimePlanNodeSummary struct {
	NodeID           uuid.UUID `json:"node_id"`
	Index            int       `json:"index"`
	Title            string    `json:"title"`
	NodeKind         string    `json:"node_kind"`
	ModuleIndex      int       `json:"module_index"`
	LessonIndex      int       `json:"lesson_index"`
	ParentIndex      int       `json:"parent_index"`
	WordCount        int       `json:"word_count"`
	BlockCount       int       `json:"block_count"`
	QuickChecks      int       `json:"quick_checks"`
	Flashcards       int       `json:"flashcards"`
	EstimatedMinutes int       `json:"estimated_minutes"`
}

func RuntimePlanBuild(ctx context.Context, deps RuntimePlanBuildDeps, in RuntimePlanBuildInput) (RuntimePlanBuildOutput, error) {
	out := RuntimePlanBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.PathNodes == nil || deps.NodeDocs == nil || deps.UserProfile == nil || deps.ProgEvents == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("runtime_plan_build: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("runtime_plan_build: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("runtime_plan_build: missing material_set_id")
	}

	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	adaptiveEnabled := adaptiveParamsEnabledForStage("runtime_plan_build")
	signals := AdaptiveSignals{}
	adaptiveParams := map[string]any{}
	if adaptiveEnabled {
		signals = loadAdaptiveSignals(ctx, deps.DB, in.MaterialSetID, pathID)
	}
	defer func() {
		if deps.Log != nil && adaptiveEnabled {
			deps.Log.Info("runtime_plan_build: adaptive params", "adaptive", adaptiveStageMeta("runtime_plan_build", adaptiveEnabled, signals, adaptiveParams))
		}
		out.Adaptive = adaptiveStageMeta("runtime_plan_build", adaptiveEnabled, signals, adaptiveParams)
	}()

	var (
		pathRow    *types.Path
		nodes      []*types.PathNode
		nodeDocs   []*types.LearningNodeDoc
		summaryRow *types.MaterialSetSummary
		userProf   *types.UserProfileVector
		progEvents []*types.UserProgressionEvent
	)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		row, err := deps.Path.GetByID(dbctx.Context{Ctx: gctx}, pathID)
		if err != nil {
			return err
		}
		pathRow = row
		return nil
	})
	g.Go(func() error {
		rows, err := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: gctx}, []uuid.UUID{pathID})
		if err != nil {
			return err
		}
		nodes = rows
		return nil
	})
	g.Go(func() error {
		up, err := deps.UserProfile.GetByUserID(dbctx.Context{Ctx: gctx}, in.OwnerUserID)
		if err != nil {
			return err
		}
		userProf = up
		return nil
	})
	g.Go(func() error {
		if deps.Summaries != nil {
			if rows, err := deps.Summaries.GetByMaterialSetIDs(dbctx.Context{Ctx: gctx}, []uuid.UUID{in.MaterialSetID}); err == nil && len(rows) > 0 {
				summaryRow = rows[0]
			}
		}
		return nil
	})
	g.Go(func() error {
		rows, err := deps.ProgEvents.ListByUserAndPathID(dbctx.Context{Ctx: gctx}, in.OwnerUserID, pathID, 1500)
		if err != nil {
			return err
		}
		progEvents = rows
		return nil
	})
	if err := g.Wait(); err != nil {
		return out, err
	}

	if len(nodes) == 0 {
		return out, fmt.Errorf("runtime_plan_build: no path nodes")
	}
	out.Nodes = len(nodes)

	meta := map[string]any{}
	if pathRow != nil && len(pathRow.Metadata) > 0 && string(pathRow.Metadata) != "null" {
		_ = json.Unmarshal(pathRow.Metadata, &meta)
	}
	if !in.Force {
		if existing, ok := meta["runtime_plan"].(map[string]any); ok && len(existing) > 0 {
			out.Plan = existing
			return out, nil
		}
	}

	nodeIDs := make([]uuid.UUID, 0, len(nodes))
	for _, n := range nodes {
		if n != nil && n.ID != uuid.Nil {
			nodeIDs = append(nodeIDs, n.ID)
		}
	}
	if len(nodeIDs) > 0 {
		if rows, err := deps.NodeDocs.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, nodeIDs); err == nil {
			nodeDocs = rows
		}
	}
	docByNode := map[uuid.UUID]*types.LearningNodeDoc{}
	for _, doc := range nodeDocs {
		if doc != nil && doc.PathNodeID != uuid.Nil {
			docByNode[doc.PathNodeID] = doc
		}
	}

	nodeSummaries := buildRuntimePlanNodeSummaries(nodes, docByNode, userProf)
	userStats := summarizeRuntimeUserStats(progEvents)
	fallbackPlan := defaultRuntimePlan(nodeSummaries, userStats)

	model := strings.TrimSpace(in.Model)
	if model == "" {
		model = strings.TrimSpace(os.Getenv(runtimePlanModelEnv))
	}
	plan := fallbackPlan
	source := "heuristic"
	if deps.AI != nil {
		ctxJSON := mustJSON(buildRuntimePlanContext(pathRow, summaryRow, meta))
		nodesJSON := mustJSON(nodeSummaries)
		userJSON := mustJSON(buildRuntimePlanUserContext(userProf, userStats))
		signalsJSON := mustJSON(buildRuntimePlanSignals(signals, adaptiveParams))
		prompt, err := prompts.Build(prompts.PromptRuntimePlan, prompts.Input{
			RuntimePlanContextJSON: string(ctxJSON),
			RuntimePlanNodesJSON:   string(nodesJSON),
			RuntimePlanUserJSON:    string(userJSON),
			RuntimePlanSignalsJSON: string(signalsJSON),
		})
		if err == nil {
			client := openai.WithModel(deps.AI, model)
			obj, err := client.GenerateJSON(ctx, prompt.System, prompt.User, prompt.SchemaName, prompt.Schema)
			if err == nil && obj != nil {
				if normalized, ok := normalizeRuntimePlan(obj, fallbackPlan, nodeSummaries); ok {
					plan = normalized
					source = "llm"
				}
			}
		}
	}

	now := time.Now().UTC()
	plan.SchemaVersion = 1
	plan.GeneratedAt = now.Format(time.RFC3339Nano)
	plan.Source = source
	if model != "" {
		plan.Model = model
	}

	planMap := runtimePlanToMap(plan)
	out.Plan = planMap

	if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		if pathRow != nil {
			pathMeta := map[string]any{}
			if len(pathRow.Metadata) > 0 && string(pathRow.Metadata) != "null" {
				_ = json.Unmarshal(pathRow.Metadata, &pathMeta)
			}
			pathMeta["runtime_plan"] = planMap
			pathMeta["runtime_plan_version"] = plan.SchemaVersion
			pathMeta["runtime_plan_generated_at"] = plan.GeneratedAt
			pathMeta["runtime_plan_source"] = plan.Source
			if plan.Model != "" {
				pathMeta["runtime_plan_model"] = plan.Model
			}
			pathMeta["runtime_plan_signals"] = buildRuntimePlanSignals(signals, adaptiveParams)
			if err := deps.Path.UpdateFields(dbc, pathID, map[string]interface{}{
				"metadata": datatypes.JSON(mustJSON(pathMeta)),
			}); err != nil {
				return err
			}
		}

		moduleByIndex := map[int]runtimePlanModule{}
		for _, m := range plan.Modules {
			if m.ModuleIndex <= 0 {
				continue
			}
			moduleByIndex[m.ModuleIndex] = m
		}
		lessonByNode := map[uuid.UUID]runtimePlanLesson{}
		for _, l := range plan.Lessons {
			if l.NodeID == uuid.Nil {
				continue
			}
			lessonByNode[l.NodeID] = l
		}

		for _, node := range nodes {
			if node == nil || node.ID == uuid.Nil {
				continue
			}
			metaMap := map[string]any{}
			if len(node.Metadata) > 0 && string(node.Metadata) != "null" {
				_ = json.Unmarshal(node.Metadata, &metaMap)
			}
			scope := "lesson"
			nodeKind := strings.ToLower(strings.TrimSpace(nodeMetaString(metaMap, "node_kind")))
			if nodeKind == "module" {
				scope = "module"
			}
			var planPayload map[string]any
			if scope == "module" {
				if mp, ok := moduleByIndex[node.Index]; ok {
					planPayload = runtimePlanModuleToMap(mp)
				}
			} else {
				if lp, ok := lessonByNode[node.ID]; ok {
					planPayload = runtimePlanLessonToMap(lp)
				}
			}
			if planPayload != nil {
				metaMap["runtime_plan"] = planPayload
				metaMap["runtime_plan_scope"] = scope
				metaMap["runtime_plan_version"] = plan.SchemaVersion
				metaMap["runtime_plan_updated_at"] = plan.GeneratedAt
			}
			if err := deps.PathNodes.UpdateFields(dbc, node.ID, map[string]interface{}{
				"metadata": datatypes.JSON(mustJSON(metaMap)),
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return out, err
	}

	return out, nil
}

func buildRuntimePlanContext(pathRow *types.Path, summaryRow *types.MaterialSetSummary, meta map[string]any) map[string]any {
	out := map[string]any{}
	if pathRow != nil {
		out["path_id"] = pathRow.ID.String()
		out["title"] = strings.TrimSpace(pathRow.Title)
		out["description"] = strings.TrimSpace(pathRow.Description)
	}
	if summaryRow != nil {
		out["summary_md"] = strings.TrimSpace(summaryRow.SummaryMD)
		out["tags"] = summaryRow.Tags
	}
	if meta != nil {
		if v, ok := meta["charter"]; ok {
			out["charter"] = v
		}
		if v, ok := meta["structure"]; ok {
			out["structure"] = v
		}
		if v, ok := meta["pattern_hierarchy"]; ok {
			out["pattern_hierarchy"] = v
		}
		if v, ok := meta["pattern_signals"]; ok {
			out["pattern_signals"] = v
		}
	}
	return out
}

func buildRuntimePlanSignals(signals AdaptiveSignals, adaptiveParams map[string]any) map[string]any {
	out := adaptiveSignalsMeta(signals)
	for k, v := range adaptiveParams {
		out[k] = v
	}
	return out
}

func buildRuntimePlanUserContext(up *types.UserProfileVector, stats runtimeUserStats) map[string]any {
	out := map[string]any{
		"event_count":        stats.EventCount,
		"avg_score":          stats.AvgScore,
		"avg_attempts":       stats.AvgAttempts,
		"avg_dwell_ms":       stats.AvgDwellMS,
		"completion_rate":    stats.CompletionRate,
		"recent_event_count": stats.RecentCount,
	}
	if stats.LastEventAt != nil {
		out["last_event_at"] = stats.LastEventAt.Format(time.RFC3339Nano)
	}
	if up != nil {
		out["profile_doc"] = strings.TrimSpace(up.ProfileDoc)
	}
	return out
}

func buildRuntimePlanNodeSummaries(nodes []*types.PathNode, docs map[uuid.UUID]*types.LearningNodeDoc, up *types.UserProfileVector) []runtimePlanNodeSummary {
	out := make([]runtimePlanNodeSummary, 0, len(nodes))
	wpm := 180.0
	if up != nil && strings.TrimSpace(up.ProfileDoc) != "" {
		// Keep deterministic; optional heuristic can be added later.
		wpm = 180.0
	}
	for _, n := range nodes {
		if n == nil || n.ID == uuid.Nil {
			continue
		}
		meta := map[string]any{}
		if len(n.Metadata) > 0 && string(n.Metadata) != "null" {
			_ = json.Unmarshal(n.Metadata, &meta)
		}
		nodeKind := strings.ToLower(strings.TrimSpace(nodeMetaString(meta, "node_kind")))
		if nodeKind == "" {
			nodeKind = "lesson"
		}
		moduleIndex := nodeMetaInt(meta, "module_index")
		parentIndex := nodeMetaInt(meta, "parent_index")
		if nodeKind == "module" {
			moduleIndex = n.Index
		}
		if moduleIndex == 0 && parentIndex > 0 {
			moduleIndex = parentIndex
		}
		lessonIndex := nodeMetaInt(meta, "lesson_index")
		if lessonIndex == 0 {
			lessonIndex = n.Index
		}

		wordCount := 0
		blockCount := 0
		quickChecks := 0
		flashcards := 0
		if doc, ok := docs[n.ID]; ok && doc != nil && len(doc.DocJSON) > 0 && string(doc.DocJSON) != "null" {
			var parsed content.NodeDocV1
			if err := json.Unmarshal(doc.DocJSON, &parsed); err == nil {
				metrics := content.NodeDocMetrics(parsed)
				wordCount = intFromAny(metrics["word_count"], 0)
				if counts := blockCountsFromMetrics(metrics); len(counts) > 0 {
					for k, v := range counts {
						blockCount += v
						switch strings.ToLower(strings.TrimSpace(k)) {
						case "quick_check":
							quickChecks += v
						case "flashcard":
							flashcards += v
						}
					}
				}
			}
		}
		estimated := estimateRuntimeMinutes(wordCount, quickChecks, flashcards, wpm)
		out = append(out, runtimePlanNodeSummary{
			NodeID:           n.ID,
			Index:            n.Index,
			Title:            strings.TrimSpace(n.Title),
			NodeKind:         nodeKind,
			ModuleIndex:      moduleIndex,
			LessonIndex:      lessonIndex,
			ParentIndex:      parentIndex,
			WordCount:        wordCount,
			BlockCount:       blockCount,
			QuickChecks:      quickChecks,
			Flashcards:       flashcards,
			EstimatedMinutes: estimated,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

func summarizeRuntimeUserStats(events []*types.UserProgressionEvent) runtimeUserStats {
	stats := runtimeUserStats{}
	if len(events) == 0 {
		return stats
	}
	var (
		totalScore    float64
		totalAttempts float64
		totalDwell    float64
		dwellCount    float64
		completed     float64
	)
	now := time.Now().UTC()
	for _, e := range events {
		if e == nil {
			continue
		}
		stats.EventCount++
		totalScore += e.Score
		totalAttempts += float64(e.Attempts)
		if e.DwellMS > 0 {
			totalDwell += float64(e.DwellMS)
			dwellCount++
		}
		if e.Completed {
			completed++
		}
		if now.Sub(e.OccurredAt) <= 30*24*time.Hour {
			stats.RecentCount++
		}
		if stats.LastEventAt == nil || e.OccurredAt.After(*stats.LastEventAt) {
			t := e.OccurredAt
			stats.LastEventAt = &t
		}
	}
	if stats.EventCount > 0 {
		stats.AvgScore = totalScore / float64(stats.EventCount)
		stats.AvgAttempts = totalAttempts / float64(stats.EventCount)
		stats.CompletionRate = completed / float64(stats.EventCount)
	}
	if dwellCount > 0 {
		stats.AvgDwellMS = totalDwell / dwellCount
	}
	return stats
}

func defaultRuntimePlan(nodes []runtimePlanNodeSummary, stats runtimeUserStats) runtimePlan {
	avgLessonMinutes := 0.0
	avgBlocks := 0.0
	avgQuick := 0.0
	avgFlash := 0.0
	lessonCount := 0.0
	for _, n := range nodes {
		if n.NodeKind == "module" {
			continue
		}
		lessonCount++
		avgLessonMinutes += float64(n.EstimatedMinutes)
		avgBlocks += float64(n.BlockCount)
		avgQuick += float64(n.QuickChecks)
		avgFlash += float64(n.Flashcards)
	}
	if lessonCount > 0 {
		avgLessonMinutes /= lessonCount
		avgBlocks /= lessonCount
		avgQuick /= lessonCount
		avgFlash /= lessonCount
	}
	if avgLessonMinutes <= 0 {
		avgLessonMinutes = 10
	}
	if avgBlocks <= 0 {
		avgBlocks = 6
	}

	targetSession := clampInt(int(math.Round(avgLessonMinutes*2.0)), 10, 45)
	if stats.CompletionRate > 0 && stats.CompletionRate < 0.5 {
		targetSession = clampInt(int(math.Round(float64(targetSession)*0.85)), 8, 45)
	}
	if stats.AvgScore > 0.85 && stats.CompletionRate > 0.8 {
		targetSession = clampInt(int(math.Round(float64(targetSession)*1.1)), 12, 60)
	}

	breakAfter := clampInt(int(math.Round(float64(targetSession)*0.7)), 8, targetSession)
	minBreak := clampInt(int(math.Round(float64(targetSession)*0.12)), 2, 12)
	maxBreak := clampInt(minBreak+6, minBreak+2, 20)

	qAfterBlocks := clampInt(int(math.Round(math.Max(2, avgBlocks/3))), 2, 8)
	qAfterMinutes := clampInt(int(math.Round(math.Max(3, avgLessonMinutes/2))), 3, 15)
	qMax := clampInt(int(math.Round(math.Max(1, avgQuick))), 1, 8)
	qGap := clampInt(int(math.Round(math.Max(1, float64(qAfterBlocks)/2))), 1, 5)

	fAfterBlocks := clampInt(int(math.Round(math.Max(3, avgBlocks/2))), 3, 12)
	fAfterMinutes := clampInt(int(math.Round(math.Max(4, avgLessonMinutes*0.6))), 4, 18)
	fMax := clampInt(int(math.Round(math.Max(1, avgFlash))), 1, 10)
	failStreak := 2
	if stats.CompletionRate > 0 && stats.CompletionRate < 0.55 {
		failStreak = 1
	}

	profile := "balanced"
	if stats.CompletionRate > 0 && stats.CompletionRate < 0.6 {
		profile = "gentle"
	}
	if stats.AvgScore > 0.85 && stats.CompletionRate > 0.8 {
		profile = "intensive"
	}

	weights := runtimePlanWeights{
		Mastery:   0.35,
		Retention: 0.25,
		Pace:      0.25,
		Fatigue:   0.15,
	}
	if stats.CompletionRate > 0 && stats.CompletionRate < 0.6 {
		weights.Mastery += 0.1
		weights.Fatigue += 0.05
		weights.Pace -= 0.1
	}
	if stats.AvgScore > 0.85 {
		weights.Pace += 0.05
		weights.Mastery -= 0.05
	}
	weights = normalizeWeights(weights)

	pathPolicy := runtimePlanPolicy{
		TargetSessionMinutes: targetSession,
		MaxPromptsPerHour:    clampInt(int(math.Round(float64(targetSession)*0.6)), 4, 20),
		BreakPolicy: runtimePlanBreakPolicy{
			AfterMinutes:    breakAfter,
			MinBreakMinutes: minBreak,
			MaxBreakMinutes: maxBreak,
		},
		QuickCheckPolicy: runtimePlanQuickCheckPolicy{
			AfterBlocks:  qAfterBlocks,
			AfterMinutes: qAfterMinutes,
			MaxPerLesson: qMax,
			MinGapBlocks: qGap,
		},
		FlashcardPolicy: runtimePlanFlashcardPolicy{
			AfterBlocks:     fAfterBlocks,
			AfterMinutes:    fAfterMinutes,
			AfterFailStreak: failStreak,
			MaxPerLesson:    fMax,
		},
		PolicyProfile:    profile,
		ObjectiveWeights: weights,
		CadenceMultipliers: runtimePlanMultipliers{
			Break:      1.0,
			QuickCheck: 1.0,
			Flashcard:  1.0,
		},
	}

	modules := buildModulePlans(nodes, pathPolicy)
	lessons := buildLessonPlans(nodes, pathPolicy)

	return runtimePlan{
		SchemaVersion: 1,
		Path:          pathPolicy,
		Modules:       modules,
		Lessons:       lessons,
	}
}

func buildModulePlans(nodes []runtimePlanNodeSummary, base runtimePlanPolicy) []runtimePlanModule {
	byModule := map[int]float64{}
	for _, n := range nodes {
		if n.NodeKind == "module" {
			if _, ok := byModule[n.Index]; !ok {
				byModule[n.Index] = 0
			}
			continue
		}
		if n.ModuleIndex > 0 {
			byModule[n.ModuleIndex] += float64(n.EstimatedMinutes)
		}
	}
	modules := make([]runtimePlanModule, 0, len(byModule))
	for idx, minutes := range byModule {
		target := base.TargetSessionMinutes
		if minutes > 0 {
			target = clampInt(int(math.Round(math.Min(float64(base.TargetSessionMinutes), minutes))), 8, base.TargetSessionMinutes)
		}
		modules = append(modules, runtimePlanModule{
			ModuleIndex:          idx,
			TargetSessionMinutes: target,
			BreakPolicy:          base.BreakPolicy,
			QuickCheckPolicy:     base.QuickCheckPolicy,
			FlashcardPolicy:      base.FlashcardPolicy,
			PolicyProfile:        base.PolicyProfile,
		})
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].ModuleIndex < modules[j].ModuleIndex })
	return modules
}

func buildLessonPlans(nodes []runtimePlanNodeSummary, base runtimePlanPolicy) []runtimePlanLesson {
	lessons := make([]runtimePlanLesson, 0, len(nodes))
	for _, n := range nodes {
		if n.NodeKind == "module" {
			continue
		}
		est := n.EstimatedMinutes
		if est <= 0 {
			est = clampInt(int(math.Round(float64(base.TargetSessionMinutes)*0.5)), 6, base.TargetSessionMinutes)
		}
		breakAfter := clampInt(int(math.Round(math.Max(float64(est), float64(base.BreakPolicy.AfterMinutes)*0.6))), 6, base.BreakPolicy.AfterMinutes)
		lessons = append(lessons, runtimePlanLesson{
			NodeID:           n.NodeID,
			NodeIndex:        n.Index,
			LessonIndex:      n.LessonIndex,
			EstimatedMinutes: est,
			BreakPolicy: runtimePlanBreakPolicy{
				AfterMinutes:    breakAfter,
				MinBreakMinutes: base.BreakPolicy.MinBreakMinutes,
				MaxBreakMinutes: base.BreakPolicy.MaxBreakMinutes,
			},
			QuickCheckPolicy: base.QuickCheckPolicy,
			FlashcardPolicy:  base.FlashcardPolicy,
			PolicyProfile:    base.PolicyProfile,
		})
	}
	sort.Slice(lessons, func(i, j int) bool { return lessons[i].NodeIndex < lessons[j].NodeIndex })
	return lessons
}

func normalizeRuntimePlan(obj map[string]any, fallback runtimePlan, nodes []runtimePlanNodeSummary) (runtimePlan, bool) {
	if obj == nil {
		return runtimePlan{}, false
	}
	plan := fallback

	pathObj, _ := obj["path"].(map[string]any)
	if pathObj != nil {
		plan.Path = coercePolicy(pathObj, fallback.Path)
	}

	if arr, ok := obj["modules"].([]any); ok {
		plan.Modules = coerceModules(arr, fallback.Modules)
	}
	if arr, ok := obj["lessons"].([]any); ok {
		plan.Lessons = coerceLessons(arr, fallback.Lessons)
	}

	// Ensure coverage for all nodes.
	lessonByNode := map[uuid.UUID]runtimePlanLesson{}
	for _, l := range plan.Lessons {
		if l.NodeID != uuid.Nil {
			lessonByNode[l.NodeID] = l
		}
	}
	for _, n := range nodes {
		if n.NodeKind == "module" {
			continue
		}
		if _, ok := lessonByNode[n.NodeID]; !ok {
			plan.Lessons = append(plan.Lessons, runtimePlanLesson{
				NodeID:           n.NodeID,
				NodeIndex:        n.Index,
				LessonIndex:      n.LessonIndex,
				EstimatedMinutes: n.EstimatedMinutes,
				BreakPolicy:      plan.Path.BreakPolicy,
				QuickCheckPolicy: plan.Path.QuickCheckPolicy,
				FlashcardPolicy:  plan.Path.FlashcardPolicy,
				PolicyProfile:    plan.Path.PolicyProfile,
			})
		}
	}
	sort.Slice(plan.Lessons, func(i, j int) bool { return plan.Lessons[i].NodeIndex < plan.Lessons[j].NodeIndex })

	return plan, true
}

func coercePolicy(obj map[string]any, fallback runtimePlanPolicy) runtimePlanPolicy {
	p := fallback
	p.TargetSessionMinutes = clampInt(intFromAny(obj["target_session_minutes"], p.TargetSessionMinutes), 8, 90)
	p.MaxPromptsPerHour = clampInt(intFromAny(obj["max_prompts_per_hour"], p.MaxPromptsPerHour), 2, 30)

	if raw, ok := obj["break_policy"].(map[string]any); ok {
		p.BreakPolicy = coerceBreakPolicy(raw, p.BreakPolicy)
	}
	if raw, ok := obj["quick_check_policy"].(map[string]any); ok {
		p.QuickCheckPolicy = coerceQuickCheckPolicy(raw, p.QuickCheckPolicy)
	}
	if raw, ok := obj["flashcard_policy"].(map[string]any); ok {
		p.FlashcardPolicy = coerceFlashcardPolicy(raw, p.FlashcardPolicy)
	}
	if s := strings.ToLower(strings.TrimSpace(stringFromAny(obj["policy_profile"]))); s != "" {
		switch s {
		case "balanced", "gentle", "intensive", "review":
			p.PolicyProfile = s
		}
	}
	if raw, ok := obj["objective_weights"].(map[string]any); ok {
		p.ObjectiveWeights = normalizeWeights(runtimePlanWeights{
			Mastery:   floatFromAny(raw["mastery"], p.ObjectiveWeights.Mastery),
			Retention: floatFromAny(raw["retention"], p.ObjectiveWeights.Retention),
			Pace:      floatFromAny(raw["pace"], p.ObjectiveWeights.Pace),
			Fatigue:   floatFromAny(raw["fatigue"], p.ObjectiveWeights.Fatigue),
		})
	}
	if raw, ok := obj["cadence_multipliers"].(map[string]any); ok {
		p.CadenceMultipliers = runtimePlanMultipliers{
			Break:      clampFloat(floatFromAny(raw["break"], p.CadenceMultipliers.Break), 0.5, 2.0),
			QuickCheck: clampFloat(floatFromAny(raw["quick_check"], p.CadenceMultipliers.QuickCheck), 0.5, 2.0),
			Flashcard:  clampFloat(floatFromAny(raw["flashcard"], p.CadenceMultipliers.Flashcard), 0.5, 2.0),
		}
	}
	return p
}

func coerceBreakPolicy(obj map[string]any, fallback runtimePlanBreakPolicy) runtimePlanBreakPolicy {
	p := fallback
	p.AfterMinutes = clampInt(intFromAny(obj["after_minutes"], p.AfterMinutes), 4, 120)
	p.MinBreakMinutes = clampInt(intFromAny(obj["min_break_minutes"], p.MinBreakMinutes), 1, 20)
	p.MaxBreakMinutes = clampInt(intFromAny(obj["max_break_minutes"], p.MaxBreakMinutes), p.MinBreakMinutes, 30)
	return p
}

func coerceQuickCheckPolicy(obj map[string]any, fallback runtimePlanQuickCheckPolicy) runtimePlanQuickCheckPolicy {
	p := fallback
	p.AfterBlocks = clampInt(intFromAny(obj["after_blocks"], p.AfterBlocks), 1, 12)
	p.AfterMinutes = clampInt(intFromAny(obj["after_minutes"], p.AfterMinutes), 2, 30)
	p.MaxPerLesson = clampInt(intFromAny(obj["max_per_lesson"], p.MaxPerLesson), 1, 12)
	p.MinGapBlocks = clampInt(intFromAny(obj["min_gap_blocks"], p.MinGapBlocks), 1, 8)
	return p
}

func coerceFlashcardPolicy(obj map[string]any, fallback runtimePlanFlashcardPolicy) runtimePlanFlashcardPolicy {
	p := fallback
	p.AfterBlocks = clampInt(intFromAny(obj["after_blocks"], p.AfterBlocks), 1, 20)
	p.AfterMinutes = clampInt(intFromAny(obj["after_minutes"], p.AfterMinutes), 2, 30)
	p.AfterFailStreak = clampInt(intFromAny(obj["after_fail_streak"], p.AfterFailStreak), 1, 5)
	p.MaxPerLesson = clampInt(intFromAny(obj["max_per_lesson"], p.MaxPerLesson), 1, 20)
	return p
}

func coerceModules(arr []any, fallback []runtimePlanModule) []runtimePlanModule {
	out := []runtimePlanModule{}
	for _, x := range arr {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		idx := intFromAny(m["module_index"], 0)
		if idx <= 0 {
			continue
		}
		mod := runtimePlanModule{
			ModuleIndex:          idx,
			TargetSessionMinutes: clampInt(intFromAny(m["target_session_minutes"], 10), 6, 90),
			BreakPolicy:          runtimePlanBreakPolicy{},
			QuickCheckPolicy:     runtimePlanQuickCheckPolicy{},
			FlashcardPolicy:      runtimePlanFlashcardPolicy{},
			PolicyProfile:        strings.ToLower(strings.TrimSpace(stringFromAny(m["policy_profile"]))),
		}
		if raw, ok := m["break_policy"].(map[string]any); ok {
			mod.BreakPolicy = coerceBreakPolicy(raw, runtimePlanBreakPolicy{AfterMinutes: 12, MinBreakMinutes: 2, MaxBreakMinutes: 10})
		}
		if raw, ok := m["quick_check_policy"].(map[string]any); ok {
			mod.QuickCheckPolicy = coerceQuickCheckPolicy(raw, runtimePlanQuickCheckPolicy{AfterBlocks: 3, AfterMinutes: 6, MaxPerLesson: 4, MinGapBlocks: 1})
		}
		if raw, ok := m["flashcard_policy"].(map[string]any); ok {
			mod.FlashcardPolicy = coerceFlashcardPolicy(raw, runtimePlanFlashcardPolicy{AfterBlocks: 4, AfterMinutes: 8, AfterFailStreak: 2, MaxPerLesson: 6})
		}
		if mod.PolicyProfile == "" {
			mod.PolicyProfile = "balanced"
		}
		out = append(out, mod)
	}
	if len(out) == 0 {
		return fallback
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModuleIndex < out[j].ModuleIndex })
	return out
}

func coerceLessons(arr []any, fallback []runtimePlanLesson) []runtimePlanLesson {
	out := []runtimePlanLesson{}
	for _, x := range arr {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		nodeID, _ := uuid.Parse(strings.TrimSpace(stringFromAny(m["node_id"])))
		if nodeID == uuid.Nil {
			continue
		}
		lesson := runtimePlanLesson{
			NodeID:           nodeID,
			NodeIndex:        intFromAny(m["node_index"], 0),
			LessonIndex:      intFromAny(m["lesson_index"], 0),
			EstimatedMinutes: clampInt(intFromAny(m["estimated_minutes"], 0), 1, 120),
			PolicyProfile:    strings.ToLower(strings.TrimSpace(stringFromAny(m["policy_profile"]))),
		}
		if raw, ok := m["break_policy"].(map[string]any); ok {
			lesson.BreakPolicy = coerceBreakPolicy(raw, runtimePlanBreakPolicy{AfterMinutes: 12, MinBreakMinutes: 2, MaxBreakMinutes: 10})
		}
		if raw, ok := m["quick_check_policy"].(map[string]any); ok {
			lesson.QuickCheckPolicy = coerceQuickCheckPolicy(raw, runtimePlanQuickCheckPolicy{AfterBlocks: 3, AfterMinutes: 6, MaxPerLesson: 4, MinGapBlocks: 1})
		}
		if raw, ok := m["flashcard_policy"].(map[string]any); ok {
			lesson.FlashcardPolicy = coerceFlashcardPolicy(raw, runtimePlanFlashcardPolicy{AfterBlocks: 4, AfterMinutes: 8, AfterFailStreak: 2, MaxPerLesson: 6})
		}
		if lesson.PolicyProfile == "" {
			lesson.PolicyProfile = "balanced"
		}
		out = append(out, lesson)
	}
	if len(out) == 0 {
		return fallback
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeIndex < out[j].NodeIndex })
	return out
}

func runtimePlanToMap(plan runtimePlan) map[string]any {
	out := map[string]any{
		"schema_version": plan.SchemaVersion,
		"generated_at":   plan.GeneratedAt,
		"source":         plan.Source,
		"path":           runtimePlanPolicyToMap(plan.Path),
		"modules":        runtimePlanModulesToMap(plan.Modules),
		"lessons":        runtimePlanLessonsToMap(plan.Lessons),
	}
	if plan.Model != "" {
		out["model"] = plan.Model
	}
	return out
}

func runtimePlanPolicyToMap(p runtimePlanPolicy) map[string]any {
	return map[string]any{
		"target_session_minutes": p.TargetSessionMinutes,
		"max_prompts_per_hour":   p.MaxPromptsPerHour,
		"break_policy":           runtimePlanBreakPolicyToMap(p.BreakPolicy),
		"quick_check_policy":     runtimePlanQuickCheckPolicyToMap(p.QuickCheckPolicy),
		"flashcard_policy":       runtimePlanFlashcardPolicyToMap(p.FlashcardPolicy),
		"policy_profile":         p.PolicyProfile,
		"objective_weights": map[string]any{
			"mastery":   p.ObjectiveWeights.Mastery,
			"retention": p.ObjectiveWeights.Retention,
			"pace":      p.ObjectiveWeights.Pace,
			"fatigue":   p.ObjectiveWeights.Fatigue,
		},
		"cadence_multipliers": map[string]any{
			"break":       p.CadenceMultipliers.Break,
			"quick_check": p.CadenceMultipliers.QuickCheck,
			"flashcard":   p.CadenceMultipliers.Flashcard,
		},
	}
}

func runtimePlanModulesToMap(mods []runtimePlanModule) []map[string]any {
	out := make([]map[string]any, 0, len(mods))
	for _, m := range mods {
		out = append(out, runtimePlanModuleToMap(m))
	}
	return out
}

func runtimePlanModuleToMap(m runtimePlanModule) map[string]any {
	return map[string]any{
		"module_index":           m.ModuleIndex,
		"target_session_minutes": m.TargetSessionMinutes,
		"break_policy":           runtimePlanBreakPolicyToMap(m.BreakPolicy),
		"quick_check_policy":     runtimePlanQuickCheckPolicyToMap(m.QuickCheckPolicy),
		"flashcard_policy":       runtimePlanFlashcardPolicyToMap(m.FlashcardPolicy),
		"policy_profile":         m.PolicyProfile,
	}
}

func runtimePlanLessonsToMap(lessons []runtimePlanLesson) []map[string]any {
	out := make([]map[string]any, 0, len(lessons))
	for _, l := range lessons {
		out = append(out, runtimePlanLessonToMap(l))
	}
	return out
}

func runtimePlanLessonToMap(l runtimePlanLesson) map[string]any {
	return map[string]any{
		"node_id":            l.NodeID.String(),
		"node_index":         l.NodeIndex,
		"lesson_index":       l.LessonIndex,
		"estimated_minutes":  l.EstimatedMinutes,
		"break_policy":       runtimePlanBreakPolicyToMap(l.BreakPolicy),
		"quick_check_policy": runtimePlanQuickCheckPolicyToMap(l.QuickCheckPolicy),
		"flashcard_policy":   runtimePlanFlashcardPolicyToMap(l.FlashcardPolicy),
		"policy_profile":     l.PolicyProfile,
	}
}

func runtimePlanBreakPolicyToMap(p runtimePlanBreakPolicy) map[string]any {
	return map[string]any{
		"after_minutes":     p.AfterMinutes,
		"min_break_minutes": p.MinBreakMinutes,
		"max_break_minutes": p.MaxBreakMinutes,
	}
}

func runtimePlanQuickCheckPolicyToMap(p runtimePlanQuickCheckPolicy) map[string]any {
	return map[string]any{
		"after_blocks":   p.AfterBlocks,
		"after_minutes":  p.AfterMinutes,
		"max_per_lesson": p.MaxPerLesson,
		"min_gap_blocks": p.MinGapBlocks,
	}
}

func runtimePlanFlashcardPolicyToMap(p runtimePlanFlashcardPolicy) map[string]any {
	return map[string]any{
		"after_blocks":      p.AfterBlocks,
		"after_minutes":     p.AfterMinutes,
		"after_fail_streak": p.AfterFailStreak,
		"max_per_lesson":    p.MaxPerLesson,
	}
}

func blockCountsFromMetrics(metrics map[string]any) map[string]int {
	out := map[string]int{}
	if metrics == nil {
		return out
	}
	switch v := metrics["block_counts"].(type) {
	case map[string]int:
		for k, c := range v {
			out[k] = c
		}
	case map[string]any:
		for k, c := range v {
			out[k] = intFromAny(c, 0)
		}
	}
	return out
}

func estimateRuntimeMinutes(wordCount int, quickChecks int, flashcards int, wpm float64) int {
	if wpm <= 0 {
		wpm = 180
	}
	est := 0
	if wordCount > 0 {
		est = int(math.Ceil(float64(wordCount) / wpm))
	}
	extra := int(math.Ceil(float64(quickChecks)*0.6 + float64(flashcards)*0.3))
	est += extra
	if est < 4 {
		est = 4
	}
	return est
}

func nodeMetaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta[key]; ok {
		return strings.TrimSpace(stringFromAny(v))
	}
	if patterns, ok := meta["patterns"].(map[string]any); ok {
		if v, ok := patterns[key]; ok {
			return strings.TrimSpace(stringFromAny(v))
		}
	}
	return ""
}

func nodeMetaInt(meta map[string]any, key string) int {
	if meta == nil {
		return 0
	}
	if v, ok := meta[key]; ok {
		return intFromAny(v, 0)
	}
	if patterns, ok := meta["patterns"].(map[string]any); ok {
		if v, ok := patterns[key]; ok {
			return intFromAny(v, 0)
		}
	}
	return 0
}

func clampInt(v int, min int, max int) int {
	if v < min {
		return min
	}
	if max > 0 && v > max {
		return max
	}
	return v
}

func clampFloat(v float64, min float64, max float64) float64 {
	if v < min {
		return min
	}
	if max > 0 && v > max {
		return max
	}
	return v
}

func normalizeWeights(w runtimePlanWeights) runtimePlanWeights {
	w.Mastery = clampFloat(w.Mastery, 0, 1)
	w.Retention = clampFloat(w.Retention, 0, 1)
	w.Pace = clampFloat(w.Pace, 0, 1)
	w.Fatigue = clampFloat(w.Fatigue, 0, 1)
	sum := w.Mastery + w.Retention + w.Pace + w.Fatigue
	if sum <= 0 {
		return runtimePlanWeights{Mastery: 0.35, Retention: 0.25, Pace: 0.25, Fatigue: 0.15}
	}
	w.Mastery /= sum
	w.Retention /= sum
	w.Pace /= sum
	w.Fatigue /= sum
	return w
}
