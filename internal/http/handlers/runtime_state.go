package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
	learningsteps "github.com/yungbote/neurobridge-backend/internal/modules/learning/steps"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type RuntimeStateHandler struct {
	pathRuns      repos.PathRunRepo
	nodeRuns      repos.NodeRunRepo
	actRuns       repos.ActivityRunRepo
	pathNodes     repos.PathNodeRepo
	concepts      repos.ConceptRepo
	conceptStates repos.UserConceptStateRepo
	conceptModels repos.UserConceptModelRepo
	misconRepo    repos.UserMisconceptionInstanceRepo
	calibRepo     repos.UserConceptCalibrationRepo
	alertRepo     repos.UserModelAlertRepo
	readinessRepo repos.ConceptReadinessSnapshotRepo
	gateRepo      repos.PrereqGateDecisionRepo
}

type RuntimeStateRunRepos struct {
	PathRuns repos.PathRunRepo
	NodeRuns repos.NodeRunRepo
	ActRuns  repos.ActivityRunRepo
}

type RuntimeStateLearningRepos struct {
	PathNodes     repos.PathNodeRepo
	Concepts      repos.ConceptRepo
	ConceptStates repos.UserConceptStateRepo
	ConceptModels repos.UserConceptModelRepo
	MisconRepo    repos.UserMisconceptionInstanceRepo
	CalibRepo     repos.UserConceptCalibrationRepo
	AlertRepo     repos.UserModelAlertRepo
	ReadinessRepo repos.ConceptReadinessSnapshotRepo
	GateRepo      repos.PrereqGateDecisionRepo
}

type RuntimeStateHandlerDeps struct {
	Runs     RuntimeStateRunRepos
	Learning RuntimeStateLearningRepos
}

func NewRuntimeStateHandlerWithDeps(deps RuntimeStateHandlerDeps) *RuntimeStateHandler {
	return &RuntimeStateHandler{
		pathRuns:      deps.Runs.PathRuns,
		nodeRuns:      deps.Runs.NodeRuns,
		actRuns:       deps.Runs.ActRuns,
		pathNodes:     deps.Learning.PathNodes,
		concepts:      deps.Learning.Concepts,
		conceptStates: deps.Learning.ConceptStates,
		conceptModels: deps.Learning.ConceptModels,
		misconRepo:    deps.Learning.MisconRepo,
		calibRepo:     deps.Learning.CalibRepo,
		alertRepo:     deps.Learning.AlertRepo,
		readinessRepo: deps.Learning.ReadinessRepo,
		gateRepo:      deps.Learning.GateRepo,
	}
}

// NewRuntimeStateHandler is kept as a compatibility shim while callers migrate to typed deps.
func NewRuntimeStateHandler(
	pathRuns repos.PathRunRepo,
	nodeRuns repos.NodeRunRepo,
	actRuns repos.ActivityRunRepo,
	pathNodes repos.PathNodeRepo,
	concepts repos.ConceptRepo,
	conceptStates repos.UserConceptStateRepo,
	conceptModels repos.UserConceptModelRepo,
	misconRepo repos.UserMisconceptionInstanceRepo,
	calibRepo repos.UserConceptCalibrationRepo,
	alertRepo repos.UserModelAlertRepo,
	readinessRepo repos.ConceptReadinessSnapshotRepo,
	gateRepo repos.PrereqGateDecisionRepo,
) *RuntimeStateHandler {
	return NewRuntimeStateHandlerWithDeps(RuntimeStateHandlerDeps{
		Runs: RuntimeStateRunRepos{
			PathRuns: pathRuns,
			NodeRuns: nodeRuns,
			ActRuns:  actRuns,
		},
		Learning: RuntimeStateLearningRepos{
			PathNodes:     pathNodes,
			Concepts:      concepts,
			ConceptStates: conceptStates,
			ConceptModels: conceptModels,
			MisconRepo:    misconRepo,
			CalibRepo:     calibRepo,
			AlertRepo:     alertRepo,
			ReadinessRepo: readinessRepo,
			GateRepo:      gateRepo,
		},
	})
}

// GET /api/paths/:id/runtime
func (h *RuntimeStateHandler) GetPathRuntime(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	pathID, err := uuid.Parse(c.Param("id"))
	if err != nil || pathID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_path_id", err)
		return
	}

	dbc := dbctx.Context{Ctx: c.Request.Context()}
	pathRun, _ := h.pathRuns.GetByUserAndPathID(dbc, rd.UserID, pathID)

	var nodeRun any
	var activityRun any
	if pathRun != nil && pathRun.ActiveNodeID != nil && *pathRun.ActiveNodeID != uuid.Nil {
		if nr, _ := h.nodeRuns.GetByUserAndNodeID(dbc, rd.UserID, *pathRun.ActiveNodeID); nr != nil {
			nodeRun = nr
		}
	}
	if pathRun != nil && pathRun.ActiveActivityID != nil && *pathRun.ActiveActivityID != uuid.Nil {
		if ar, _ := h.actRuns.GetByUserAndActivityID(dbc, rd.UserID, *pathRun.ActiveActivityID); ar != nil {
			activityRun = ar
		}
	}

	var knowledgeContext any
	var knowledgeCalibration any
	var knowledgeAlerts any
	var prereqGate any
	var readinessSnapshot any
	if pathRun != nil && pathRun.ActiveNodeID != nil && *pathRun.ActiveNodeID != uuid.Nil && h.pathNodes != nil {
		if node, err := h.pathNodes.GetByID(dbc, *pathRun.ActiveNodeID); err == nil && node != nil {
			var meta map[string]any
			if len(node.Metadata) > 0 && string(node.Metadata) != "null" {
				_ = json.Unmarshal(node.Metadata, &meta)
			}
			keys := append([]string{}, stringSliceFromAny(meta["concept_keys"])...)
			keys = append(keys, stringSliceFromAny(meta["prereq_concept_keys"])...)
			keys = normalizeKeys(keys)

			if len(keys) > 0 && h.concepts != nil {
				canonicalByKey := map[string]uuid.UUID{}
				canonicalKeyByID := map[uuid.UUID]string{}
				seenConceptIDs := map[uuid.UUID]bool{}

				rows, _ := h.concepts.GetByScopeAndKeys(dbc, "path", &node.PathID, keys)
				if len(rows) == 0 {
					rows, _ = h.concepts.GetByScopeAndKeys(dbc, "global", nil, keys)
				}
				for _, c := range rows {
					if c == nil || c.ID == uuid.Nil {
						continue
					}
					key := strings.TrimSpace(strings.ToLower(c.Key))
					if key == "" {
						continue
					}
					id := c.ID
					if c.CanonicalConceptID != nil && *c.CanonicalConceptID != uuid.Nil {
						id = *c.CanonicalConceptID
					}
					if id == uuid.Nil {
						continue
					}
					canonicalByKey[key] = id
					if canonicalKeyByID[id] == "" {
						canonicalKeyByID[id] = key
					}
					seenConceptIDs[id] = true
				}

				conceptIDs := make([]uuid.UUID, 0, len(seenConceptIDs))
				for id := range seenConceptIDs {
					conceptIDs = append(conceptIDs, id)
				}

				stateByID := map[uuid.UUID]*types.UserConceptState{}
				if h.conceptStates != nil && len(conceptIDs) > 0 {
					if rows, err := h.conceptStates.ListByUserAndConceptIDs(dbc, rd.UserID, conceptIDs); err == nil {
						for _, r := range rows {
							if r == nil || r.ConceptID == uuid.Nil {
								continue
							}
							stateByID[r.ConceptID] = r
						}
					}
				}

				modelByID := map[uuid.UUID]*types.UserConceptModel{}
				if h.conceptModels != nil && len(conceptIDs) > 0 {
					if rows, err := h.conceptModels.ListByUserAndConceptIDs(dbc, rd.UserID, conceptIDs); err == nil {
						for _, r := range rows {
							if r == nil || r.CanonicalConceptID == uuid.Nil {
								continue
							}
							modelByID[r.CanonicalConceptID] = r
						}
					}
				}

				misconByID := map[uuid.UUID][]*types.UserMisconceptionInstance{}
				if h.misconRepo != nil && len(conceptIDs) > 0 {
					if rows, err := h.misconRepo.ListActiveByUserAndConceptIDs(dbc, rd.UserID, conceptIDs); err == nil {
						for _, r := range rows {
							if r == nil || r.CanonicalConceptID == uuid.Nil {
								continue
							}
							misconByID[r.CanonicalConceptID] = append(misconByID[r.CanonicalConceptID], r)
						}
					}
				}

				knowledgeContext = learningsteps.BuildUserKnowledgeContextV2(keys, canonicalByKey, stateByID, modelByID, misconByID, time.Now().UTC(), nil)

				if h.calibRepo != nil && len(conceptIDs) > 0 {
					conceptCalibs := make([]map[string]any, 0, len(conceptIDs))
					totalCount := 0
					totalExpected := 0.0
					totalObserved := 0.0
					totalBrier := 0.0
					totalAbsErr := 0.0
					if rows, err := h.calibRepo.ListByUserAndConceptIDs(dbc, rd.UserID, conceptIDs); err == nil {
						for _, row := range rows {
							if row == nil || row.ConceptID == uuid.Nil || row.Count <= 0 {
								continue
							}
							denom := float64(row.Count)
							expAvg := row.ExpectedSum / denom
							obsAvg := row.ObservedSum / denom
							brierAvg := row.BrierSum / denom
							absErrAvg := row.AbsErrSum / denom
							gap := expAvg - obsAvg
							conceptCalibs = append(conceptCalibs, map[string]any{
								"concept_id":  row.ConceptID.String(),
								"concept_key": canonicalKeyByID[row.ConceptID],
								"count":       row.Count,
								"expected":    expAvg,
								"observed":    obsAvg,
								"gap":         gap,
								"brier":       brierAvg,
								"abs_err":     absErrAvg,
							})
							totalCount += row.Count
							totalExpected += row.ExpectedSum
							totalObserved += row.ObservedSum
							totalBrier += row.BrierSum
							totalAbsErr += row.AbsErrSum
						}
					}
					var totals map[string]any
					if totalCount > 0 {
						denom := float64(totalCount)
						totals = map[string]any{
							"count":    totalCount,
							"expected": totalExpected / denom,
							"observed": totalObserved / denom,
							"gap":      (totalExpected / denom) - (totalObserved / denom),
							"brier":    totalBrier / denom,
							"abs_err":  totalAbsErr / denom,
						}
					}
					knowledgeCalibration = map[string]any{
						"totals":   totals,
						"concepts": conceptCalibs,
					}
				}

				if h.alertRepo != nil && len(conceptIDs) > 0 {
					if rows, err := h.alertRepo.ListByUserAndConceptIDs(dbc, rd.UserID, conceptIDs); err == nil && len(rows) > 0 {
						filtered := make([]*types.UserModelAlert, 0, len(rows))
						for _, r := range rows {
							if r == nil || r.ConceptID == uuid.Nil {
								continue
							}
							filtered = append(filtered, r)
						}
						knowledgeAlerts = filtered
					}
				}
			}
		}
	}
	if pathRun != nil && pathRun.ActiveNodeID != nil && *pathRun.ActiveNodeID != uuid.Nil {
		if h.gateRepo != nil {
			if row, err := h.gateRepo.GetLatestByUserAndNode(dbc, rd.UserID, *pathRun.ActiveNodeID); err == nil && row != nil {
				prereqGate = row
			}
		}
		if h.readinessRepo != nil {
			if row, err := h.readinessRepo.GetLatestByUserAndNode(dbc, rd.UserID, *pathRun.ActiveNodeID); err == nil && row != nil {
				readinessSnapshot = row
			}
		}
	}

	response.RespondOK(c, gin.H{
		"path_run":              pathRun,
		"node_run":              nodeRun,
		"activity_run":          activityRun,
		"knowledge_context":     knowledgeContext,
		"knowledge_calibration": knowledgeCalibration,
		"knowledge_alerts":      knowledgeAlerts,
		"prereq_gate":           prereqGate,
		"readiness_snapshot":    readinessSnapshot,
	})
}

func normalizeKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(strings.ToLower(k))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}
