package user_model_update

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
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
	start := time.Now()

	ctx.Progress("scan", 1, "Scanning user events")

	updatedConceptIDs := map[uuid.UUID]bool{} // canonical concept IDs

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
				case types.EventQuestionAnswered, types.EventScrollDepth, types.EventBlockViewed, types.EventHintUsed:
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

			dirty := map[uuid.UUID]bool{}

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
						applyQuestionAnsweredToState(st, seenAt, it.data)
						stateByConcept[cc] = st
						dirty[cc] = true
						updatedConceptIDs[cc] = true
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
						applyHintUsedToState(st, seenAt, it.data)
						stateByConcept[cc] = st
						dirty[cc] = true
						updatedConceptIDs[cc] = true
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
						applyActivityCompletedToState(st, seenAt, it.data)
						stateByConcept[cc] = st
						dirty[cc] = true
						updatedConceptIDs[cc] = true
					}

				case types.EventScrollDepth, types.EventBlockViewed:
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
						applyExposureToState(st, seenAt, it.data)
						stateByConcept[cc] = st
						dirty[cc] = true
						updatedConceptIDs[cc] = true
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

	ctx.Succeed("done", map[string]any{"processed": processed})
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
