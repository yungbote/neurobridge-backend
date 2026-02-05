package user_model_update

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	graphstore "github.com/yungbote/neurobridge-backend/internal/data/graph"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func (p *Pipeline) Run(ctx *jobrt.Context) error {
	if ctx == nil || ctx.Job == nil {
		return nil
	}

	userID := ctx.Job.OwnerUserID
	if userID == uuid.Nil {
		ctx.Fail("validate", fmt.Errorf("missing owner_user_id"))
		return nil
	}
	if p.db == nil || p.events == nil || p.cursors == nil {
		ctx.Fail("validate", fmt.Errorf("user_model_update: missing deps"))
		return nil
	}

	consumer := "user_model_update"

	// Load cursor (if missing, start from beginning)
	var afterAt *time.Time
	var afterID *uuid.UUID

	dbc := dbctx.Context{Ctx: ctx.Ctx}

	cur, err := p.cursors.Get(dbc, userID, consumer)
	if err == nil && cur != nil {
		afterAt = cur.LastCreatedAt
		afterID = cur.LastEventID
	}

	const pageSize = 500
	processed := 0
	incorrectSignals := 0
	hintSignals := 0
	retrySignals := 0
	exposureSignals := 0
	structuralUpdates := 0
	propagatedEdges := 0
	bridgeTransfers := 0
	bridgeValidationRequests := 0
	bridgeFalseTransfers := 0
	bridgeBlocks := 0
	start := time.Now()

	ctx.Progress("scan", 1, "Scanning user events")

	updatedConceptIDs := map[uuid.UUID]bool{} // canonical concept IDs
	directCorrectAt := map[uuid.UUID]time.Time{}
	directIncorrectAt := map[uuid.UUID]time.Time{}

	for {
		events, err := p.events.ListAfterCursor(dbc, userID, afterAt, afterID, pageSize)
		if err != nil {
			ctx.Fail("scan", err)
			return nil
		}
		if len(events) == 0 {
			break
		}

		var pageLastAt *time.Time
		var pageLastID *uuid.UUID

		// Transactionally apply derived state + advance cursor (idempotent under retries).
		if err := p.db.WithContext(ctx.Ctx).Transaction(func(tx *gorm.DB) error {
			tdbc := dbctx.Context{Ctx: ctx.Ctx, Tx: tx}

			type evItem struct {
				ev   *types.UserEvent
				data map[string]any
			}
			items := make([]evItem, 0, len(events))

			// Collect IDs we may need to resolve in bulk.
			rawConceptIDs := make([]uuid.UUID, 0, 64)
			activityIDs := make([]uuid.UUID, 0, 32)

			for _, ev := range events {
				if ev == nil || ev.ID == uuid.Nil {
					continue
				}
				processed++

				var d map[string]any
				if len(ev.Data) > 0 && string(ev.Data) != "null" {
					_ = json.Unmarshal(ev.Data, &d)
				} else {
					d = map[string]any{}
				}
				// Attach client_event_id for idempotent structural support pointers.
				if ev.ClientEventID != "" {
					d["client_event_id"] = ev.ClientEventID
				}
				if ev.ID != uuid.Nil {
					d["event_id"] = ev.ID.String()
				}
				items = append(items, evItem{ev: ev, data: d})

				// Extract concept IDs (as provided by the client) for mapping.
				cids := extractUUIDsFromAny(d["concept_ids"])
				if ev.ConceptID != nil && *ev.ConceptID != uuid.Nil && len(cids) == 0 {
					cids = []uuid.UUID{*ev.ConceptID}
				}
				rawConceptIDs = append(rawConceptIDs, cids...)

				// Activity concepts are derived server-side for activity_* events.
				if ev.ActivityID != nil && *ev.ActivityID != uuid.Nil {
					typ := strings.TrimSpace(ev.Type)
					if typ == types.EventActivityCompleted || typ == types.EventActivityStarted || typ == types.EventActivityAbandoned {
						activityIDs = append(activityIDs, *ev.ActivityID)
					}
				}

				// Cursor advance (tie-safe cursor = created_at + id)
				t := ev.CreatedAt
				id := ev.ID
				pageLastAt = &t
				pageLastID = &id
			}

			activityIDs = dedupeUUIDs(activityIDs)
			rawConceptIDs = dedupeUUIDs(rawConceptIDs)

			// ActivityID -> raw concept IDs (path-scoped concept IDs).
			rawConceptIDsByActivity := map[uuid.UUID][]uuid.UUID{}
			if p.actConcepts != nil && len(activityIDs) > 0 {
				if rows, err := p.actConcepts.GetByActivityIDs(tdbc, activityIDs); err == nil {
					for _, r := range rows {
						if r == nil || r.ActivityID == uuid.Nil || r.ConceptID == uuid.Nil {
							continue
						}
						rawConceptIDsByActivity[r.ActivityID] = append(rawConceptIDsByActivity[r.ActivityID], r.ConceptID)
						rawConceptIDs = append(rawConceptIDs, r.ConceptID)
					}
				}
			}
			rawConceptIDs = dedupeUUIDs(rawConceptIDs)

			// Resolve canonical concept IDs in bulk (path concept id -> canonical/global concept id).
			canonicalByRaw := map[uuid.UUID]uuid.UUID{}
			for _, id := range rawConceptIDs {
				if id != uuid.Nil {
					canonicalByRaw[id] = id
				}
			}
			if p.concepts != nil && len(rawConceptIDs) > 0 {
				if rows, err := p.concepts.GetByIDs(tdbc, rawConceptIDs); err == nil {
					for _, c := range rows {
						if c == nil || c.ID == uuid.Nil {
							continue
						}
						if c.CanonicalConceptID != nil && *c.CanonicalConceptID != uuid.Nil {
							canonicalByRaw[c.ID] = *c.CanonicalConceptID
						} else {
							canonicalByRaw[c.ID] = c.ID
						}
					}
				}
			}

			// ActivityID -> canonical concept IDs.
			canonicalByActivity := map[uuid.UUID][]uuid.UUID{}
			for aid, cids := range rawConceptIDsByActivity {
				out := make([]uuid.UUID, 0, len(cids))
				for _, cid := range cids {
					cc := canonicalByRaw[cid]
					if cc != uuid.Nil {
						out = append(out, cc)
					}
				}
				canonicalByActivity[aid] = dedupeUUIDs(out)
			}

			// Load existing concept states for any canonical IDs we might touch.
			touchedCanonical := map[uuid.UUID]bool{}
			for _, it := range items {
				if it.ev == nil {
					continue
				}
				typ := strings.TrimSpace(it.ev.Type)
				switch typ {
				case types.EventQuestionAnswered, types.EventConceptClaimEvaluated, types.EventScrollDepth, types.EventBlockViewed, types.EventBlockRead, types.EventHintUsed:
					for _, rawID := range extractUUIDsFromAny(it.data["concept_ids"]) {
						cc := canonicalByRaw[rawID]
						if cc != uuid.Nil {
							touchedCanonical[cc] = true
						}
					}
					if it.ev.ConceptID != nil && *it.ev.ConceptID != uuid.Nil {
						cc := canonicalByRaw[*it.ev.ConceptID]
						if cc != uuid.Nil {
							touchedCanonical[cc] = true
						}
					}
				case types.EventActivityCompleted, types.EventActivityStarted, types.EventActivityAbandoned:
					if it.ev.ActivityID != nil && *it.ev.ActivityID != uuid.Nil {
						for _, cc := range canonicalByActivity[*it.ev.ActivityID] {
							if cc != uuid.Nil {
								touchedCanonical[cc] = true
							}
						}
					}
				}
			}

			stateByConcept := map[uuid.UUID]*types.UserConceptState{}
			if p.conceptState != nil && len(touchedCanonical) > 0 {
				ids := make([]uuid.UUID, 0, len(touchedCanonical))
				for id := range touchedCanonical {
					if id != uuid.Nil {
						ids = append(ids, id)
					}
				}
				sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
				if rows, err := p.conceptState.ListByUserAndConceptIDs(tdbc, userID, ids); err == nil {
					for _, r := range rows {
						if r == nil || r.UserID == uuid.Nil || r.ConceptID == uuid.Nil {
							continue
						}
						stateByConcept[r.ConceptID] = r
					}
				}
			}

			modelByConcept := map[uuid.UUID]*types.UserConceptModel{}
			if p.conceptModel != nil && len(touchedCanonical) > 0 {
				ids := make([]uuid.UUID, 0, len(touchedCanonical))
				for id := range touchedCanonical {
					if id != uuid.Nil {
						ids = append(ids, id)
					}
				}
				sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
				if rows, err := p.conceptModel.ListByUserAndConceptIDs(tdbc, userID, ids); err == nil {
					for _, r := range rows {
						if r == nil || r.UserID == uuid.Nil || r.CanonicalConceptID == uuid.Nil {
							continue
						}
						modelByConcept[r.CanonicalConceptID] = r
					}
				}
			}

			// Preload canonical concept rows for parent/cluster priors.
			conceptByID := map[uuid.UUID]*types.Concept{}
			if p.concepts != nil && len(touchedCanonical) > 0 {
				ids := make([]uuid.UUID, 0, len(touchedCanonical))
				for id := range touchedCanonical {
					if id != uuid.Nil {
						ids = append(ids, id)
					}
				}
				sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
				if rows, err := p.concepts.GetByIDs(tdbc, ids); err == nil {
					for _, c := range rows {
						if c == nil || c.ID == uuid.Nil {
							continue
						}
						conceptByID[c.ID] = c
					}
				}
			}

			// Preload cluster membership for hierarchical priors.
			clusterMembersByConcept := map[uuid.UUID][]*types.ConceptClusterMember{}
			clusterMembersByCluster := map[uuid.UUID][]*types.ConceptClusterMember{}
			clusterIDs := []uuid.UUID{}
			if p.clusterMembers != nil && len(touchedCanonical) > 0 {
				ids := make([]uuid.UUID, 0, len(touchedCanonical))
				for id := range touchedCanonical {
					if id != uuid.Nil {
						ids = append(ids, id)
					}
				}
				sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
				if rows, err := p.clusterMembers.GetByConceptIDs(tdbc, ids); err == nil {
					for _, r := range rows {
						if r == nil || r.ConceptID == uuid.Nil || r.ClusterID == uuid.Nil {
							continue
						}
						clusterMembersByConcept[r.ConceptID] = append(clusterMembersByConcept[r.ConceptID], r)
						clusterIDs = append(clusterIDs, r.ClusterID)
					}
				}
			}
			clusterIDs = dedupeUUIDs(clusterIDs)
			if p.clusterMembers != nil && len(clusterIDs) > 0 {
				if rows, err := p.clusterMembers.GetByClusterIDs(tdbc, clusterIDs); err == nil {
					for _, r := range rows {
						if r == nil || r.ClusterID == uuid.Nil || r.ConceptID == uuid.Nil {
							continue
						}
						clusterMembersByCluster[r.ClusterID] = append(clusterMembersByCluster[r.ClusterID], r)
					}
				}
			}

			// Preload parent states and cluster member states for priors.
			missingStateIDs := []uuid.UUID{}
			for _, c := range conceptByID {
				if c == nil || c.ParentID == nil || *c.ParentID == uuid.Nil {
					continue
				}
				if stateByConcept[*c.ParentID] == nil {
					missingStateIDs = append(missingStateIDs, *c.ParentID)
				}
			}
			for _, members := range clusterMembersByCluster {
				for _, m := range members {
					if m == nil || m.ConceptID == uuid.Nil {
						continue
					}
					if stateByConcept[m.ConceptID] == nil {
						missingStateIDs = append(missingStateIDs, m.ConceptID)
					}
				}
			}
			missingStateIDs = dedupeUUIDs(missingStateIDs)
			if p.conceptState != nil && len(missingStateIDs) > 0 {
				if rows, err := p.conceptState.ListByUserAndConceptIDs(tdbc, userID, missingStateIDs); err == nil {
					for _, r := range rows {
						if r == nil || r.UserID == uuid.Nil || r.ConceptID == uuid.Nil {
							continue
						}
						stateByConcept[r.ConceptID] = r
					}
				}
			}

			// Preload testlet states for adaptive testing (question answered events).
			testletIDs := []string{}
			testletKeyTypes := map[string]map[string]bool{}
			for _, it := range items {
				if it.ev == nil {
					continue
				}
				if strings.TrimSpace(it.ev.Type) != types.EventQuestionAnswered {
					continue
				}
				tid := strings.TrimSpace(inferTestletID(it.data))
				tt := strings.TrimSpace(inferTestletType(it.data))
				if tid == "" {
					continue
				}
				testletIDs = append(testletIDs, tid)
				if _, ok := testletKeyTypes[tid]; !ok {
					testletKeyTypes[tid] = map[string]bool{}
				}
				testletKeyTypes[tid][tt] = true
			}
			testletIDs = dedupeStrings(testletIDs)
			testletByKey := map[string]*types.UserTestletState{}
			if p.testletState != nil && len(testletIDs) > 0 {
				if rows, err := p.testletState.ListByUserAndTestletIDs(tdbc, userID, testletIDs); err == nil {
					for _, row := range rows {
						if row == nil || row.UserID == uuid.Nil {
							continue
						}
						key := testletKey(row.TestletID, row.TestletType)
						testletByKey[key] = row
					}
				}
			}

			dirty := map[uuid.UUID]bool{}
			dirtyModel := map[uuid.UUID]bool{}
			misconRows := []*types.UserMisconceptionInstance{}
			questionAttempts := map[string]int{}
			evidenceRows := []*types.UserConceptEvidence{}
			testletDirty := map[string]*types.UserTestletState{}

			type calibDelta struct {
				count       int
				expectedSum float64
				observedSum float64
				brierSum    float64
				absErrSum   float64
				lastAt      time.Time
			}
			calibUpdates := map[uuid.UUID]*calibDelta{}

			// Apply events in order.
			for _, it := range items {
				ev := it.ev
				if ev == nil {
					continue
				}
				typ := strings.TrimSpace(ev.Type)

				seenAt := ev.OccurredAt
				if seenAt.IsZero() {
					seenAt = ev.CreatedAt
				}
				if seenAt.IsZero() {
					seenAt = time.Now().UTC()
				}

				switch typ {
				case types.EventQuestionAnswered:
					qid := strings.TrimSpace(fmt.Sprint(it.data["question_id"]))
					if qid != "" {
						questionAttempts[qid]++
					}
					isCorrect := boolFromAny(it.data["is_correct"], false)
					if p.testletState != nil {
						tid := strings.TrimSpace(inferTestletID(it.data))
						tt := strings.TrimSpace(inferTestletType(it.data))
						if tid != "" {
							key := testletKey(tid, tt)
							ts := testletByKey[key]
							if ts == nil {
								ts = &types.UserTestletState{
									ID:          uuid.New(),
									UserID:      userID,
									TestletID:   tid,
									TestletType: tt,
									BetaA:       testletBetaPrior,
									BetaB:       testletBetaPrior,
								}
								testletByKey[key] = ts
							}
							updateTestletState(ts, isCorrect, seenAt)
							testletDirty[key] = ts
						}
					}
					cids := extractUUIDsFromAny(it.data["concept_ids"])
					if ev.ConceptID != nil && *ev.ConceptID != uuid.Nil && len(cids) == 0 {
						cids = []uuid.UUID{*ev.ConceptID}
					}
					for _, rawID := range cids {
						cc := canonicalByRaw[rawID]
						if cc == uuid.Nil {
							continue
						}
						st := ensureConceptState(stateByConcept[cc], userID, cc)
						prevM := st.Mastery
						prevC := st.Confidence
						expectedCorrect := expectedCorrectnessForQuestion(st, seenAt, it.data)
						applyQuestionAnsweredToState(st, seenAt, it.data)
						stateByConcept[cc] = st
						dirty[cc] = true
						updatedConceptIDs[cc] = true
						if isCorrect {
							upsertLatestTime(directCorrectAt, cc, seenAt)
						} else {
							upsertLatestTime(directIncorrectAt, cc, seenAt)
						}
						if shouldRecordEvidence(prevM, prevC, st.Mastery, st.Confidence) && p.evidenceRepo != nil {
							strength, conf := evidenceSignalForEvent(typ, it.data)
							evidenceRows = append(evidenceRows, &types.UserConceptEvidence{
								UserID:           userID,
								ConceptID:        cc,
								Source:           typ,
								SourceRef:        eventSourceRef(ev),
								EventID:          &ev.ID,
								EventType:        typ,
								OccurredAt:       seenAt.UTC(),
								PriorMastery:     prevM,
								PriorConfidence:  prevC,
								PostMastery:      st.Mastery,
								PostConfidence:   st.Confidence,
								MasteryDelta:     st.Mastery - prevM,
								ConfidenceDelta:  st.Confidence - prevC,
								SignalStrength:   strength,
								SignalConfidence: conf,
								Payload:          datatypes.JSON(mustJSON(it.data)),
							})
						}
						if p.calibRepo != nil {
							acc := calibUpdates[cc]
							if acc == nil {
								acc = &calibDelta{}
								calibUpdates[cc] = acc
							}
							obs := 0.0
							if isCorrect {
								obs = 1.0
							}
							diff := expectedCorrect - obs
							acc.count += 1
							acc.expectedSum += expectedCorrect
							acc.observedSum += obs
							acc.brierSum += diff * diff
							acc.absErrSum += math.Abs(diff)
							if acc.lastAt.IsZero() || seenAt.After(acc.lastAt) {
								acc.lastAt = seenAt
							}
						}

						// Structural signal: incorrect answers -> misconception candidate
						if !boolFromAny(it.data["is_correct"], false) && p.conceptModel != nil {
							model := ensureConceptModel(modelByConcept[cc], userID, cc)
							if updated, mis := applyIncorrectAnswerToModel(model, seenAt, it.data); updated {
								modelByConcept[cc] = model
								dirtyModel[cc] = true
								incorrectSignals++
								structuralUpdates++
								if mis != nil {
									misconRows = append(misconRows, mis)
								}
							}
						}
						// Structural signal: retries on the same question -> procedural uncertainty
						if p.conceptModel != nil && qid != "" && questionAttempts[qid] >= 2 {
							model := ensureConceptModel(modelByConcept[cc], userID, cc)
							if updated := applyRetryToModel(model, seenAt, it.data, questionAttempts[qid]); updated {
								modelByConcept[cc] = model
								dirtyModel[cc] = true
								retrySignals++
								structuralUpdates++
							}
						}
					}

				case types.EventConceptClaimEvaluated:
					hasTruth := boolFromAny(it.data["has_truth"], false)
					isCorrect := boolFromAny(it.data["is_correct"], false)
					cids := extractUUIDsFromAny(it.data["concept_ids"])
					if ev.ConceptID != nil && *ev.ConceptID != uuid.Nil && len(cids) == 0 {
						cids = []uuid.UUID{*ev.ConceptID}
					}
					for _, rawID := range cids {
						cc := canonicalByRaw[rawID]
						if cc == uuid.Nil {
							continue
						}
						st := ensureConceptState(stateByConcept[cc], userID, cc)
						prevM := st.Mastery
						prevC := st.Confidence
						applyClaimEvaluatedToState(st, seenAt, it.data)
						stateByConcept[cc] = st
						dirty[cc] = true
						updatedConceptIDs[cc] = true
						if hasTruth {
							if isCorrect {
								upsertLatestTime(directCorrectAt, cc, seenAt)
							} else {
								upsertLatestTime(directIncorrectAt, cc, seenAt)
							}
						}
						if shouldRecordEvidence(prevM, prevC, st.Mastery, st.Confidence) && p.evidenceRepo != nil {
							strength, conf := evidenceSignalForEvent(typ, it.data)
							evidenceRows = append(evidenceRows, &types.UserConceptEvidence{
								UserID:           userID,
								ConceptID:        cc,
								Source:           typ,
								SourceRef:        eventSourceRef(ev),
								EventID:          &ev.ID,
								EventType:        typ,
								OccurredAt:       seenAt.UTC(),
								PriorMastery:     prevM,
								PriorConfidence:  prevC,
								PostMastery:      st.Mastery,
								PostConfidence:   st.Confidence,
								MasteryDelta:     st.Mastery - prevM,
								ConfidenceDelta:  st.Confidence - prevC,
								SignalStrength:   strength,
								SignalConfidence: conf,
								Payload:          datatypes.JSON(mustJSON(it.data)),
							})
						}
					}

				case types.EventHintUsed:
					cids := extractUUIDsFromAny(it.data["concept_ids"])
					if ev.ConceptID != nil && *ev.ConceptID != uuid.Nil && len(cids) == 0 {
						cids = []uuid.UUID{*ev.ConceptID}
					}
					for _, rawID := range cids {
						cc := canonicalByRaw[rawID]
						if cc == uuid.Nil {
							continue
						}
						st := ensureConceptState(stateByConcept[cc], userID, cc)
						prevM := st.Mastery
						prevC := st.Confidence
						applyHintUsedToState(st, seenAt, it.data)
						stateByConcept[cc] = st
						dirty[cc] = true
						updatedConceptIDs[cc] = true
						if shouldRecordEvidence(prevM, prevC, st.Mastery, st.Confidence) && p.evidenceRepo != nil {
							strength, conf := evidenceSignalForEvent(typ, it.data)
							evidenceRows = append(evidenceRows, &types.UserConceptEvidence{
								UserID:           userID,
								ConceptID:        cc,
								Source:           typ,
								SourceRef:        eventSourceRef(ev),
								EventID:          &ev.ID,
								EventType:        typ,
								OccurredAt:       seenAt.UTC(),
								PriorMastery:     prevM,
								PriorConfidence:  prevC,
								PostMastery:      st.Mastery,
								PostConfidence:   st.Confidence,
								MasteryDelta:     st.Mastery - prevM,
								ConfidenceDelta:  st.Confidence - prevC,
								SignalStrength:   strength,
								SignalConfidence: conf,
								Payload:          datatypes.JSON(mustJSON(it.data)),
							})
						}

						// Structural signal: hints -> uncertainty region (procedural gap)
						if p.conceptModel != nil {
							model := ensureConceptModel(modelByConcept[cc], userID, cc)
							if updated := applyHintToModel(model, seenAt, it.data); updated {
								modelByConcept[cc] = model
								dirtyModel[cc] = true
								hintSignals++
								structuralUpdates++
							}
						}
					}

				case types.EventActivityCompleted:
					if ev.ActivityID == nil || *ev.ActivityID == uuid.Nil {
						break
					}
					for _, cc := range canonicalByActivity[*ev.ActivityID] {
						if cc == uuid.Nil {
							continue
						}
						st := ensureConceptState(stateByConcept[cc], userID, cc)
						prevM := st.Mastery
						prevC := st.Confidence
						applyActivityCompletedToState(st, seenAt, it.data)
						stateByConcept[cc] = st
						dirty[cc] = true
						updatedConceptIDs[cc] = true
						if shouldRecordEvidence(prevM, prevC, st.Mastery, st.Confidence) && p.evidenceRepo != nil {
							strength, conf := evidenceSignalForEvent(typ, it.data)
							evidenceRows = append(evidenceRows, &types.UserConceptEvidence{
								UserID:           userID,
								ConceptID:        cc,
								Source:           typ,
								SourceRef:        eventSourceRef(ev),
								EventID:          &ev.ID,
								EventType:        typ,
								OccurredAt:       seenAt.UTC(),
								PriorMastery:     prevM,
								PriorConfidence:  prevC,
								PostMastery:      st.Mastery,
								PostConfidence:   st.Confidence,
								MasteryDelta:     st.Mastery - prevM,
								ConfidenceDelta:  st.Confidence - prevC,
								SignalStrength:   strength,
								SignalConfidence: conf,
								Payload:          datatypes.JSON(mustJSON(it.data)),
							})
						}
					}

				case types.EventScrollDepth, types.EventBlockViewed, types.EventBlockRead:
					cids := extractUUIDsFromAny(it.data["concept_ids"])
					if ev.ConceptID != nil && *ev.ConceptID != uuid.Nil && len(cids) == 0 {
						cids = []uuid.UUID{*ev.ConceptID}
					}
					for _, rawID := range cids {
						cc := canonicalByRaw[rawID]
						if cc == uuid.Nil {
							continue
						}
						st := ensureConceptState(stateByConcept[cc], userID, cc)
						prevM := st.Mastery
						prevC := st.Confidence
						applyExposureToState(st, seenAt, it.data)
						stateByConcept[cc] = st
						dirty[cc] = true
						updatedConceptIDs[cc] = true
						if shouldRecordEvidence(prevM, prevC, st.Mastery, st.Confidence) && p.evidenceRepo != nil {
							strength, conf := evidenceSignalForEvent(typ, it.data)
							evidenceRows = append(evidenceRows, &types.UserConceptEvidence{
								UserID:           userID,
								ConceptID:        cc,
								Source:           typ,
								SourceRef:        eventSourceRef(ev),
								EventID:          &ev.ID,
								EventType:        typ,
								OccurredAt:       seenAt.UTC(),
								PriorMastery:     prevM,
								PriorConfidence:  prevC,
								PostMastery:      st.Mastery,
								PostConfidence:   st.Confidence,
								MasteryDelta:     st.Mastery - prevM,
								ConfidenceDelta:  st.Confidence - prevC,
								SignalStrength:   strength,
								SignalConfidence: conf,
								Payload:          datatypes.JSON(mustJSON(it.data)),
							})
						}
						if p.conceptModel != nil {
							model := ensureConceptModel(modelByConcept[cc], userID, cc)
							if updated := applyExposureToModel(model, seenAt, it.data); updated {
								modelByConcept[cc] = model
								dirtyModel[cc] = true
								exposureSignals++
								structuralUpdates++
							}
						}
					}

				case "style_feedback":
					// Expect frontend event:
					// type="style_feedback"
					// data: { "modality":"diagram", "variant":"flowchart", "reward":0.7, "binary":true|false, "concept_id":"..." }
					if p.stylePrefs != nil {
						modality := strings.TrimSpace(fmt.Sprint(it.data["modality"]))
						variant := strings.TrimSpace(fmt.Sprint(it.data["variant"]))
						reward := floatFromAny(it.data["reward"], 0)

						var conceptID *uuid.UUID
						if s := strings.TrimSpace(fmt.Sprint(it.data["concept_id"])); s != "" {
							if id, e := uuid.Parse(s); e == nil && id != uuid.Nil {
								// Canonicalize if possible.
								cc := canonicalByRaw[id]
								if cc != uuid.Nil {
									conceptID = &cc
								} else {
									conceptID = &id
								}
							}
						}

						var binary *bool
						if bv, ok := it.data["binary"].(bool); ok {
							binary = &bv
						}

						if modality != "" && variant != "" {
							_ = p.stylePrefs.UpsertEMA(tdbc, userID, conceptID, modality, variant, reward, binary)
						}
					}
				}
			}

			// Apply hierarchical priors (cluster + parent) for low-confidence concepts.
			if ktHierarchyEnabled && len(touchedCanonical) > 0 && (len(clusterMembersByCluster) > 0 || len(conceptByID) > 0) {
				type clusterStat struct {
					sum     float64
					confSum float64
					weight  float64
				}
				clusterStats := map[uuid.UUID]*clusterStat{}
				for cid, members := range clusterMembersByCluster {
					if cid == uuid.Nil {
						continue
					}
					stat := clusterStats[cid]
					if stat == nil {
						stat = &clusterStat{}
						clusterStats[cid] = stat
					}
					for _, m := range members {
						if m == nil || m.ConceptID == uuid.Nil {
							continue
						}
						st := stateByConcept[m.ConceptID]
						if st == nil {
							continue
						}
						w := m.Weight
						if w <= 0 {
							w = 1
						}
						stat.sum += clamp01(st.Mastery) * w
						stat.confSum += clamp01(st.Confidence) * w
						stat.weight += w
					}
				}

				for cid := range touchedCanonical {
					if cid == uuid.Nil {
						continue
					}
					st := stateByConcept[cid]
					if st == nil {
						continue
					}
					if st.Confidence >= ktPriorApplyConfBelow {
						continue
					}

					weightSum := 0.0
					priorSum := 0.0
					payload := map[string]any{
						"cluster_ids": []string{},
					}

					if members := clusterMembersByConcept[cid]; len(members) > 0 && ktClusterPriorWeight > 0 {
						clusterIDs := []string{}
						for _, m := range members {
							if m == nil || m.ClusterID == uuid.Nil {
								continue
							}
							stat := clusterStats[m.ClusterID]
							if stat == nil || stat.weight <= 0 {
								continue
							}
							clusterAvg := stat.sum / stat.weight
							w := ktClusterPriorWeight * clampRange(m.Weight, 0.1, 5)
							priorSum += clusterAvg * w
							weightSum += w
							clusterIDs = append(clusterIDs, m.ClusterID.String())
						}
						if len(clusterIDs) > 0 {
							payload["cluster_ids"] = clusterIDs
						}
					}

					if c := conceptByID[cid]; c != nil && c.ParentID != nil && *c.ParentID != uuid.Nil && ktParentPriorWeight > 0 {
						if parentState := stateByConcept[*c.ParentID]; parentState != nil {
							priorSum += clamp01(parentState.Mastery) * ktParentPriorWeight
							weightSum += ktParentPriorWeight
							payload["parent_id"] = c.ParentID.String()
						}
					}

					if weightSum <= 0 {
						continue
					}
					prior := clamp01(priorSum / weightSum)
					prevM := st.Mastery
					prevC := st.Confidence
					st.Mastery = clamp01(prevM + (prior-prevM)*ktPriorApplyWeight)
					if st.EpistemicUncertainty > 0 {
						st.EpistemicUncertainty = clamp01(st.EpistemicUncertainty * (1.0 - 0.15*ktPriorApplyWeight))
					}
					stateByConcept[cid] = st
					dirty[cid] = true
					updatedConceptIDs[cid] = true

					if shouldRecordEvidence(prevM, prevC, st.Mastery, st.Confidence) && p.evidenceRepo != nil {
						payload["prior"] = prior
						payload["weight"] = ktPriorApplyWeight
						payload["source"] = "hierarchy_prior"
						evidenceRows = append(evidenceRows, &types.UserConceptEvidence{
							UserID:           userID,
							ConceptID:        cid,
							Source:           "hierarchy_prior",
							SourceRef:        "hierarchy_prior",
							EventID:          nil,
							EventType:        "hierarchy_prior",
							OccurredAt:       time.Now().UTC(),
							PriorMastery:     prevM,
							PriorConfidence:  prevC,
							PostMastery:      st.Mastery,
							PostConfidence:   st.Confidence,
							MasteryDelta:     st.Mastery - prevM,
							ConfidenceDelta:  st.Confidence - prevC,
							SignalStrength:   clamp01(ktPriorApplyWeight),
							SignalConfidence: clamp01(1.0 - st.EpistemicUncertainty),
							Payload:          datatypes.JSON(mustJSON(payload)),
						})
					}
				}
			}

			// Propagate mastery across concept edges (including bridge safety).
			if p.edges != nil && len(touchedCanonical) > 0 {
				seedIDs := make([]uuid.UUID, 0, len(touchedCanonical))
				for id := range touchedCanonical {
					if id != uuid.Nil {
						seedIDs = append(seedIDs, id)
					}
				}
				sort.Slice(seedIDs, func(i, j int) bool { return seedIDs[i].String() < seedIDs[j].String() })
				edges, err := p.edges.GetByConceptIDs(tdbc, seedIDs)
				if err == nil && len(edges) > 0 {
					edgeConceptIDs := make([]uuid.UUID, 0, len(edges)*2)
					for _, e := range edges {
						if e == nil {
							continue
						}
						if e.FromConceptID != uuid.Nil {
							edgeConceptIDs = append(edgeConceptIDs, e.FromConceptID)
						}
						if e.ToConceptID != uuid.Nil {
							edgeConceptIDs = append(edgeConceptIDs, e.ToConceptID)
						}
					}
					edgeConceptIDs = dedupeUUIDs(edgeConceptIDs)

					// Load neighbor states not already loaded.
					if p.conceptState != nil && len(edgeConceptIDs) > 0 {
						missing := make([]uuid.UUID, 0, len(edgeConceptIDs))
						for _, id := range edgeConceptIDs {
							if id == uuid.Nil {
								continue
							}
							if stateByConcept[id] == nil {
								missing = append(missing, id)
							}
						}
						if len(missing) > 0 {
							if rows, err := p.conceptState.ListByUserAndConceptIDs(tdbc, userID, missing); err == nil {
								for _, r := range rows {
									if r == nil || r.UserID == uuid.Nil || r.ConceptID == uuid.Nil {
										continue
									}
									stateByConcept[r.ConceptID] = r
								}
							}
						}
					}

					edgeStatsByKey := map[string]*types.UserConceptEdgeStat{}
					if p.edgeStats != nil && len(edgeConceptIDs) > 0 {
						if rows, err := p.edgeStats.ListByUserAndConceptIDs(tdbc, userID, edgeConceptIDs); err == nil {
							for _, r := range rows {
								if r == nil || r.UserID == uuid.Nil || r.FromConceptID == uuid.Nil || r.ToConceptID == uuid.Nil {
									continue
								}
								edgeStatsByKey[edgeStatKey(r.FromConceptID, r.ToConceptID, r.EdgeType)] = r
							}
						}
					}
					edgeStatsDirty := map[string]*types.UserConceptEdgeStat{}

					conceptKeyByID := map[uuid.UUID]string{}
					if p.concepts != nil && len(edgeConceptIDs) > 0 {
						if rows, err := p.concepts.GetByIDs(tdbc, edgeConceptIDs); err == nil {
							for _, c := range rows {
								if c == nil || c.ID == uuid.Nil {
									continue
								}
								if strings.TrimSpace(c.Key) != "" {
									conceptKeyByID[c.ID] = strings.TrimSpace(c.Key)
								}
							}
						}
					}

					now := time.Now().UTC()
					falseWindow := hoursToDuration(bridgeFalseWindowHours)
					validationCooldown := hoursToDuration(bridgeValidationCooldownHours)
					bridgeBlockDuration := hoursToDuration(bridgeBlockHours)

					for _, edge := range edges {
						if edge == nil || edge.FromConceptID == uuid.Nil || edge.ToConceptID == uuid.Nil {
							continue
						}
						if edge.FromConceptID == edge.ToConceptID {
							continue
						}
						edgeType := strings.ToLower(strings.TrimSpace(edge.EdgeType))
						if edgeType == "" {
							continue
						}

						fromState := ensureConceptState(stateByConcept[edge.FromConceptID], userID, edge.FromConceptID)
						toState := ensureConceptState(stateByConcept[edge.ToConceptID], userID, edge.ToConceptID)
						stateByConcept[edge.FromConceptID] = fromState
						stateByConcept[edge.ToConceptID] = toState

						if edgeType == "bridge" {
							if p.edgeStats == nil {
								continue
							}
							stat := ensureEdgeStat(edgeStatsByKey, userID, edge.FromConceptID, edge.ToConceptID, edgeType)
							statKey := edgeStatKey(edge.FromConceptID, edge.ToConceptID, edgeType)

							// Update validation status on direct correct evidence.
							if t, ok := directCorrectAt[edge.ToConceptID]; ok {
								if stat.ValidatedAt == nil || t.After(*stat.ValidatedAt) {
									tt := t.UTC()
									stat.ValidatedAt = &tt
									edgeStatsDirty[statKey] = stat
								}
							}

							// Track false transfers if incorrect evidence follows a recent transfer.
							if tBad, ok := directIncorrectAt[edge.ToConceptID]; ok && stat.LastTransferAt != nil && falseWindow > 0 {
								if tBad.After(*stat.LastTransferAt) && tBad.Sub(*stat.LastTransferAt) <= falseWindow {
									stat.FalseTransfers += 1
									tt := tBad.UTC()
									stat.LastFalseAt = &tt
									edgeStatsDirty[statKey] = stat
									bridgeFalseTransfers++

									if stat.Attempts > 0 {
										rate := float64(stat.FalseTransfers) / float64(stat.Attempts)
										if rate >= bridgeHardBlockRate {
											blockUntil := now.Add(bridgeBlockDuration)
											stat.BlockedUntil = &blockUntil
											edgeStatsDirty[statKey] = stat
											bridgeBlocks++
										} else if rate >= bridgeFalseRateTighten {
											stat.ThresholdBoost = clampRange(stat.ThresholdBoost+bridgeThresholdBoostStep, 0, 0.5)
											edgeStatsDirty[statKey] = stat
										}
									}
								}
							}

							effectiveMinScore := clampRange(bridgeMinScore+stat.ThresholdBoost, 0, 0.98)
							if edge.Strength < effectiveMinScore {
								continue
							}
							if stat.BlockedUntil != nil && now.Before(*stat.BlockedUntil) {
								continue
							}
							if !canPropagateFrom(fromState, bridgeMinMastery, bridgeMinConfidence, bridgeMaxEpi, bridgeMaxAlea) {
								continue
							}

							if stat.ValidatedAt == nil {
								// Request validation with cooldown.
								if shouldRequestBridgeValidation(stat.LastValidationRequestedAt, now, validationCooldown) {
									evData := map[string]any{
										"from_concept_id": edge.FromConceptID.String(),
										"to_concept_id":   edge.ToConceptID.String(),
										"edge_type":       edgeType,
										"strength":        edge.Strength,
									}
									if key := conceptKeyByID[edge.FromConceptID]; key != "" {
										evData["from_concept_key"] = key
									}
									if key := conceptKeyByID[edge.ToConceptID]; key != "" {
										evData["to_concept_key"] = key
									}
									bucket := bridgeValidationBucket(now, validationCooldown)
									clientEventID := fmt.Sprintf("bridge_validation:%s:%s:%s:%s:%d", userID.String(), edge.FromConceptID.String(), edge.ToConceptID.String(), edgeType, bucket)
									reqAt := now.UTC()
									ev := &types.UserEvent{
										ID:            uuid.New(),
										UserID:        userID,
										ClientEventID: clientEventID,
										OccurredAt:    reqAt,
										ConceptID:     &edge.ToConceptID,
										Type:          types.EventBridgeValidationNeeded,
										Data:          datatypes.JSON(mustJSON(evData)),
									}
									if p.events != nil {
										_, _ = p.events.CreateIgnoreDuplicates(tdbc, []*types.UserEvent{ev})
									}
									stat.LastValidationRequestedAt = &reqAt
									edgeStatsDirty[statKey] = stat
									bridgeValidationRequests++
								}
								continue
							}

							prevM := toState.Mastery
							prevC := toState.Confidence
							if applyPropagationDelta(fromState, toState, bridgePropagationWeight, propagationMaxDelta) {
								dirty[edge.ToConceptID] = true
								updatedConceptIDs[edge.ToConceptID] = true
								propagatedEdges++
								bridgeTransfers++
								stat.Attempts += 1
								stat.LastTransferAt = ptrTime(now)
								edgeStatsDirty[statKey] = stat
								if shouldRecordEvidence(prevM, prevC, toState.Mastery, toState.Confidence) && p.evidenceRepo != nil {
									payload := map[string]any{
										"from_concept_id": edge.FromConceptID.String(),
										"to_concept_id":   edge.ToConceptID.String(),
										"edge_type":       edgeType,
										"strength":        edge.Strength,
										"weight":          bridgePropagationWeight,
										"page_last_event": fmt.Sprint(pageLastID),
									}
									if key := conceptKeyByID[edge.FromConceptID]; key != "" {
										payload["from_concept_key"] = key
									}
									if key := conceptKeyByID[edge.ToConceptID]; key != "" {
										payload["to_concept_key"] = key
									}
									evidenceRows = append(evidenceRows, &types.UserConceptEvidence{
										UserID:           userID,
										ConceptID:        edge.ToConceptID,
										Source:           "bridge_transfer",
										SourceRef:        propagationSourceRef(pageLastID, edge, "bridge_transfer"),
										EventType:        "bridge_transfer",
										OccurredAt:       now,
										PriorMastery:     prevM,
										PriorConfidence:  prevC,
										PostMastery:      toState.Mastery,
										PostConfidence:   toState.Confidence,
										MasteryDelta:     toState.Mastery - prevM,
										ConfidenceDelta:  toState.Confidence - prevC,
										SignalStrength:   clamp01(bridgePropagationWeight * edge.Strength),
										SignalConfidence: clamp01(fromState.Confidence),
										Payload:          datatypes.JSON(mustJSON(payload)),
									})
								}
							}
							continue
						}

						weight := propagationWeightForEdge(edgeType)
						if weight <= 0 {
							continue
						}
						if !canPropagateFrom(fromState, propagationMinMastery, propagationMinConfidence, propagationMaxEpi, propagationMaxAlea) {
							continue
						}
						if edge.Strength <= 0 {
							continue
						}
						prevM := toState.Mastery
						prevC := toState.Confidence
						if applyPropagationDelta(fromState, toState, weight*edge.Strength, propagationMaxDelta) {
							dirty[edge.ToConceptID] = true
							updatedConceptIDs[edge.ToConceptID] = true
							propagatedEdges++
							if shouldRecordEvidence(prevM, prevC, toState.Mastery, toState.Confidence) && p.evidenceRepo != nil {
								payload := map[string]any{
									"from_concept_id": edge.FromConceptID.String(),
									"to_concept_id":   edge.ToConceptID.String(),
									"edge_type":       edgeType,
									"strength":        edge.Strength,
									"weight":          weight,
									"page_last_event": fmt.Sprint(pageLastID),
								}
								if key := conceptKeyByID[edge.FromConceptID]; key != "" {
									payload["from_concept_key"] = key
								}
								if key := conceptKeyByID[edge.ToConceptID]; key != "" {
									payload["to_concept_key"] = key
								}
								evidenceRows = append(evidenceRows, &types.UserConceptEvidence{
									UserID:           userID,
									ConceptID:        edge.ToConceptID,
									Source:           "propagation",
									SourceRef:        propagationSourceRef(pageLastID, edge, "propagation"),
									EventType:        "propagation",
									OccurredAt:       now,
									PriorMastery:     prevM,
									PriorConfidence:  prevC,
									PostMastery:      toState.Mastery,
									PostConfidence:   toState.Confidence,
									MasteryDelta:     toState.Mastery - prevM,
									ConfidenceDelta:  toState.Confidence - prevC,
									SignalStrength:   clamp01(weight * edge.Strength),
									SignalConfidence: clamp01(fromState.Confidence),
									Payload:          datatypes.JSON(mustJSON(payload)),
								})
							}
						}
					}

					if p.edgeStats != nil && len(edgeStatsDirty) > 0 {
						keys := make([]string, 0, len(edgeStatsDirty))
						for k := range edgeStatsDirty {
							keys = append(keys, k)
						}
						sort.Strings(keys)
						for _, k := range keys {
							if row := edgeStatsDirty[k]; row != nil {
								_ = p.edgeStats.Upsert(tdbc, row)
							}
						}
					}
				}
			}

			// Persist updated concept states.
			if p.conceptState != nil && len(dirty) > 0 {
				ids := make([]uuid.UUID, 0, len(dirty))
				for id := range dirty {
					if id != uuid.Nil {
						ids = append(ids, id)
					}
				}
				sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
				for _, id := range ids {
					if st := stateByConcept[id]; st != nil {
						_ = p.conceptState.Upsert(tdbc, st)
					}
				}
			}

			// Persist updated structural models.
			if p.conceptModel != nil && len(dirtyModel) > 0 {
				ids := make([]uuid.UUID, 0, len(dirtyModel))
				for id := range dirtyModel {
					if id != uuid.Nil {
						ids = append(ids, id)
					}
				}
				sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
				for _, id := range ids {
					if row := modelByConcept[id]; row != nil {
						_ = p.conceptModel.Upsert(tdbc, row)
					}
				}
			}

			// Persist misconception instances.
			if p.misconRepo != nil && len(misconRows) > 0 {
				for _, row := range misconRows {
					if row != nil {
						_ = p.misconRepo.Upsert(tdbc, row)
					}
				}
			}

			// Persist testlet states (adaptive testing).
			if p.testletState != nil && len(testletDirty) > 0 {
				keys := make([]string, 0, len(testletDirty))
				for k := range testletDirty {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					if row := testletDirty[k]; row != nil {
						_ = p.testletState.Upsert(tdbc, row)
					}
				}
			}

			// Persist evidence ledger rows.
			if p.evidenceRepo != nil && len(evidenceRows) > 0 {
				_ = p.evidenceRepo.CreateIgnoreDuplicates(tdbc, evidenceRows)
			}

			// Persist calibration updates + emit drift alerts.
			if p.calibRepo != nil && len(calibUpdates) > 0 {
				ids := make([]uuid.UUID, 0, len(calibUpdates))
				for id := range calibUpdates {
					if id != uuid.Nil {
						ids = append(ids, id)
					}
				}
				sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })

				existing := map[uuid.UUID]*types.UserConceptCalibration{}
				if rows, err := p.calibRepo.ListByUserAndConceptIDs(tdbc, userID, ids); err == nil {
					for _, row := range rows {
						if row == nil || row.UserID == uuid.Nil || row.ConceptID == uuid.Nil {
							continue
						}
						existing[row.ConceptID] = row
					}
				}

				alertsByConcept := map[uuid.UUID]*types.UserModelAlert{}
				if p.alertRepo != nil && len(ids) > 0 {
					if rows, err := p.alertRepo.ListByUserAndConceptIDs(tdbc, userID, ids); err == nil {
						for _, row := range rows {
							if row == nil || row.UserID == uuid.Nil || row.ConceptID == uuid.Nil {
								continue
							}
							if strings.TrimSpace(strings.ToLower(row.Kind)) == "calibration_drift" {
								alertsByConcept[row.ConceptID] = row
							}
						}
					}
				}

				now := time.Now().UTC()
				for _, id := range ids {
					delta := calibUpdates[id]
					if delta == nil || delta.count <= 0 {
						continue
					}
					row := existing[id]
					if row == nil {
						row = &types.UserConceptCalibration{
							ID:        uuid.New(),
							UserID:    userID,
							ConceptID: id,
						}
					}
					row.Count += delta.count
					row.ExpectedSum += delta.expectedSum
					row.ObservedSum += delta.observedSum
					row.BrierSum += delta.brierSum
					row.AbsErrSum += delta.absErrSum
					if delta.lastAt.IsZero() {
						delta.lastAt = now
					}
					if row.LastEventAt == nil || row.LastEventAt.IsZero() || delta.lastAt.After(*row.LastEventAt) {
						t := delta.lastAt.UTC()
						row.LastEventAt = &t
					}
					_ = p.calibRepo.Upsert(tdbc, row)

					if p.alertRepo == nil {
						continue
					}
					if row.Count < calibMinSamples {
						continue
					}
					denom := float64(row.Count)
					avgExpected := row.ExpectedSum / denom
					avgObserved := row.ObservedSum / denom
					avgBrier := row.BrierSum / denom
					avgAbsErr := row.AbsErrSum / denom
					gap := math.Abs(avgExpected - avgObserved)

					severity := ""
					if gap >= calibGapCrit || avgAbsErr >= calibAbsErrCrit || avgBrier >= calibBrierCrit {
						severity = "critical"
					} else if gap >= calibGapWarn || avgAbsErr >= calibAbsErrWarn || avgBrier >= calibBrierWarn {
						severity = "warning"
					}

					details := map[string]any{
						"count":           row.Count,
						"expected_avg":    avgExpected,
						"observed_avg":    avgObserved,
						"gap":             gap,
						"brier_avg":       avgBrier,
						"abs_err_avg":     avgAbsErr,
						"gap_warn":        calibGapWarn,
						"gap_crit":        calibGapCrit,
						"abs_err_warn":    calibAbsErrWarn,
						"abs_err_crit":    calibAbsErrCrit,
						"brier_warn":      calibBrierWarn,
						"brier_crit":      calibBrierCrit,
						"min_sample_size": calibMinSamples,
					}

					if severity == "" {
						if existingAlert := alertsByConcept[id]; existingAlert != nil {
							resolvedAt := now
							_ = p.alertRepo.Upsert(tdbc, &types.UserModelAlert{
								ID:         uuid.New(),
								UserID:     userID,
								ConceptID:  id,
								Kind:       "calibration_drift",
								Severity:   "info",
								Score:      0,
								Details:    datatypes.JSON(mustJSON(details)),
								LastSeenAt: &now,
								ResolvedAt: &resolvedAt,
								Occurrences: 0,
							})
						}
						continue
					}

					score := math.Max(math.Max(gap, avgAbsErr), avgBrier)
					_ = p.alertRepo.Upsert(tdbc, &types.UserModelAlert{
						ID:         uuid.New(),
						UserID:     userID,
						ConceptID:  id,
						Kind:       "calibration_drift",
						Severity:   severity,
						Score:      score,
						Details:    datatypes.JSON(mustJSON(details)),
						LastSeenAt: &now,
						Occurrences: 1,
					})
				}
			}

			// Persist cursor within the same transaction for idempotency.
			if pageLastAt != nil && pageLastID != nil {
				curRow := &types.UserEventCursor{
					ID:            uuid.New(),
					UserID:        userID,
					Consumer:      consumer,
					LastCreatedAt: pageLastAt,
					LastEventID:   pageLastID,
					UpdatedAt:     time.Now().UTC(),
				}
				_ = p.cursors.Upsert(tdbc, curRow)
			}

			return nil
		}); err != nil {
			ctx.Fail("scan", err)
			return nil
		}

		afterAt = pageLastAt
		afterID = pageLastID

		// Budget guard: don’t monopolize worker for huge backlogs
		if time.Since(start) > 20*time.Second || processed >= 4000 {
			break
		}

		// Lightweight progress updates
		if processed%500 == 0 {
			ctx.Progress("scan", 25, fmt.Sprintf("Processed %d events", processed))
		}
	}

	// Best-effort: sync updated mastery edges into Neo4j.
	if p.graph != nil && p.graph.Driver != nil && p.conceptState != nil && len(updatedConceptIDs) > 0 {
		conceptIDs := make([]uuid.UUID, 0, len(updatedConceptIDs))
		for id := range updatedConceptIDs {
			if id != uuid.Nil {
				conceptIDs = append(conceptIDs, id)
			}
		}
		if len(conceptIDs) > 0 {
			if rows, err := p.conceptState.ListByUserAndConceptIDs(dbc, userID, conceptIDs); err == nil && len(rows) > 0 {
				if err := graphstore.UpsertUserConceptStates(ctx.Ctx, p.graph, p.log, userID, rows); err != nil && p.log != nil {
					p.log.Warn("neo4j user learning graph sync failed (continuing)", "error", err, "user_id", userID.String())
				}
			}
		}
	}

	// Optional: enqueue PSU promotion when structural signals changed.
	if p.jobSvc != nil && len(updatedConceptIDs) > 0 {
		if strings.EqualFold(strings.TrimSpace(os.Getenv("PSU_PROMOTION_AUTO")), "true") {
			repoCtx := dbctx.Context{Ctx: ctx.Ctx}
			entityID := userID
			_, _ = p.jobSvc.Enqueue(repoCtx, userID, "psu_promote", "user", &entityID, map[string]any{
				"user_id": userID.String(),
				"trigger": "user_model_update",
			})
		}
	}

	ctx.Succeed("done", map[string]any{
		"processed":          processed,
		"concepts_updated":   len(updatedConceptIDs),
		"structural_updates": structuralUpdates,
		"incorrect_signals":  incorrectSignals,
		"hint_signals":       hintSignals,
		"retry_signals":      retrySignals,
		"exposure_signals":   exposureSignals,
		"propagated_edges":   propagatedEdges,
		"bridge_transfers":   bridgeTransfers,
		"bridge_requests":    bridgeValidationRequests,
		"bridge_false":       bridgeFalseTransfers,
		"bridge_blocks":      bridgeBlocks,
	})
	return nil
}

func dedupeUUIDs(in []uuid.UUID) []uuid.UUID {
	if len(in) == 0 {
		return nil
	}
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(in))
	for _, id := range in {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func testletKey(id string, typ string) string {
	id = strings.TrimSpace(id)
	typ = strings.TrimSpace(typ)
	if typ == "" {
		typ = "question"
	}
	return fmt.Sprintf("%s|%s", id, strings.ToLower(typ))
}

func inferTestletID(data map[string]any) string {
	if data == nil {
		return ""
	}
	if v := strings.TrimSpace(fmt.Sprint(data["testlet_id"])); v != "" && v != "<nil>" {
		return v
	}
	if v := strings.TrimSpace(fmt.Sprint(data["block_id"])); v != "" && v != "<nil>" {
		return v
	}
	if v := strings.TrimSpace(fmt.Sprint(data["question_id"])); v != "" && v != "<nil>" {
		return v
	}
	if v := strings.TrimSpace(fmt.Sprint(data["activity_id"])); v != "" && v != "<nil>" {
		return v
	}
	return ""
}

func inferTestletType(data map[string]any) string {
	if data == nil {
		return "question"
	}
	for _, key := range []string{"testlet_type", "prompt_type", "item_type", "activity_kind", "block_type"} {
		if v := strings.TrimSpace(fmt.Sprint(data[key])); v != "" && v != "<nil>" {
			return strings.ToLower(v)
		}
	}
	return "question"
}

func updateTestletState(state *types.UserTestletState, correct bool, seenAt time.Time) {
	if state == nil {
		return
	}
	state.Attempts += 1
	if correct {
		state.Correct += 1
		state.BetaA += 1.0
	} else {
		state.BetaB += 1.0
	}
	score := 0.0
	if correct {
		score = 1.0
	}
	if state.Attempts == 1 {
		state.EMA = score
	} else {
		state.EMA = (1.0-testletEmaAlpha)*state.EMA + testletEmaAlpha*score
	}
	if !seenAt.IsZero() {
		t := seenAt.UTC()
		state.LastSeenAt = &t
	}
}

func edgeStatKey(fromID uuid.UUID, toID uuid.UUID, edgeType string) string {
	return fmt.Sprintf("%s|%s|%s", fromID.String(), toID.String(), strings.ToLower(strings.TrimSpace(edgeType)))
}

func ensureEdgeStat(store map[string]*types.UserConceptEdgeStat, userID uuid.UUID, fromID uuid.UUID, toID uuid.UUID, edgeType string) *types.UserConceptEdgeStat {
	key := edgeStatKey(fromID, toID, edgeType)
	if existing, ok := store[key]; ok && existing != nil {
		return existing
	}
	row := &types.UserConceptEdgeStat{
		ID:            uuid.New(),
		UserID:        userID,
		FromConceptID: fromID,
		ToConceptID:   toID,
		EdgeType:      strings.ToLower(strings.TrimSpace(edgeType)),
	}
	store[key] = row
	return row
}

func upsertLatestTime(store map[uuid.UUID]time.Time, id uuid.UUID, t time.Time) {
	if id == uuid.Nil || t.IsZero() {
		return
	}
	if prev, ok := store[id]; !ok || t.After(prev) {
		store[id] = t
	}
}

func eventSourceRef(ev *types.UserEvent) string {
	if ev == nil {
		return ""
	}
	if ev.ID != uuid.Nil {
		return ev.ID.String()
	}
	if s := strings.TrimSpace(ev.ClientEventID); s != "" {
		return s
	}
	if !ev.CreatedAt.IsZero() {
		return fmt.Sprintf("%s:%d", strings.TrimSpace(ev.Type), ev.CreatedAt.UTC().UnixNano())
	}
	return strings.TrimSpace(ev.Type)
}

func propagationSourceRef(pageLastID *uuid.UUID, edge *types.ConceptEdge, kind string) string {
	base := strings.TrimSpace(kind)
	if base == "" {
		base = "propagation"
	}
	fromID := uuid.Nil
	toID := uuid.Nil
	edgeType := "unknown"
	if edge != nil {
		fromID = edge.FromConceptID
		toID = edge.ToConceptID
		if s := strings.TrimSpace(edge.EdgeType); s != "" {
			edgeType = strings.ToLower(s)
		}
	}
	ref := fmt.Sprintf("%s:%s:%s:%s", base, fromID.String(), toID.String(), edgeType)
	if pageLastID != nil && *pageLastID != uuid.Nil {
		ref = ref + ":" + pageLastID.String()
	}
	return ref
}

func shouldRecordEvidence(prevM float64, prevC float64, nextM float64, nextC float64) bool {
	const minDelta = 0.0005
	if math.Abs(nextM-prevM) >= minDelta {
		return true
	}
	if math.Abs(nextC-prevC) >= minDelta {
		return true
	}
	return false
}
