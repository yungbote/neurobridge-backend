package runtime_update

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning/runtime"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

const runtimeConsumer = "runtime_update"

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	userID := jc.Job.OwnerUserID
	if userID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("runtime_update: missing owner_user_id"))
		return nil
	}
	if p.db == nil || p.events == nil || p.cursors == nil || p.pathRuns == nil || p.nodeRuns == nil || p.actRuns == nil || p.trans == nil {
		jc.Fail("validate", fmt.Errorf("runtime_update: missing deps"))
		return nil
	}

	dbc := dbctx.Context{Ctx: jc.Ctx}
	trigger := strings.TrimSpace(fmt.Sprint(jc.Payload()["trigger"]))

	var afterAt *time.Time
	var afterID *uuid.UUID
	if cur, err := p.cursors.Get(dbc, userID, runtimeConsumer); err == nil && cur != nil {
		afterAt = cur.LastCreatedAt
		afterID = cur.LastEventID
	}

	const pageSize = 500
	processed := 0
	start := time.Now()
	progressiveCandidates := map[uuid.UUID]progressiveDocCandidate{}

	jc.Progress("scan", 1, "Scanning runtime events")

	for {
		events, err := p.events.ListAfterCursor(dbc, userID, afterAt, afterID, pageSize)
		if err != nil {
			jc.Fail("scan", err)
			return nil
		}
		if len(events) == 0 {
			break
		}

		var pageLastAt *time.Time
		var pageLastID *uuid.UUID

		if err := p.db.WithContext(jc.Ctx).Transaction(func(tx *gorm.DB) error {
			tdbc := dbctx.Context{Ctx: jc.Ctx, Tx: tx}

			for _, ev := range events {
				if ev == nil || ev.ID == uuid.Nil {
					continue
				}
				processed++
				pageLastAt = &ev.CreatedAt
				pageLastID = &ev.ID

				// Idempotency: skip if we already recorded a transition for this event.
				if exists, _ := p.trans.ExistsByEventID(tdbc, userID, ev.ID); exists {
					continue
				}

				typ := strings.TrimSpace(strings.ToLower(ev.Type))
				if typ == "" {
					continue
				}
				if p.metrics != nil {
					p.metrics.IncRuntimeTrigger(trigger, typ)
				}

				var data map[string]any
				if len(ev.Data) > 0 && string(ev.Data) != "null" {
					_ = json.Unmarshal(ev.Data, &data)
				} else {
					data = map[string]any{}
				}
				if data == nil {
					data = map[string]any{}
				}
				if _, ok := data["event_id"]; !ok {
					data["event_id"] = ev.ID.String()
				}
				if _, ok := data["event_type"]; !ok {
					data["event_type"] = typ
				}
				if _, ok := data["session_id"]; !ok && ev.SessionID != uuid.Nil {
					data["session_id"] = ev.SessionID.String()
				}
				if _, ok := data["occurred_at"]; !ok && !ev.OccurredAt.IsZero() {
					data["occurred_at"] = ev.OccurredAt.UTC().Format(time.RFC3339)
				}

				pathID, nodeID, activityID := p.resolveContext(tdbc, ev)
				if pathID == uuid.Nil {
					continue
				}

				now := time.Now().UTC()

				pr, _ := p.pathRuns.GetByUserAndPathID(tdbc, userID, pathID)
				if pr == nil {
					pr = &types.PathRun{
						UserID: userID,
						PathID: pathID,
						State:  runtime.PathRunNotStarted,
					}
				}

				fromState := string(pr.State)
				toState := fromState

				switch typ {
				case types.EventPathOpened:
					toState = string(runtime.PathRunInNode)
				case types.EventPathClosed:
					toState = string(runtime.PathRunPaused)
				case types.EventNodeOpened:
					toState = string(runtime.PathRunInNode)
				case types.EventActivityOpened, types.EventActivityStarted:
					toState = string(runtime.PathRunInActivity)
				case types.EventQuestionAnswered:
					toState = string(runtime.PathRunAwaitingFeed)
				case types.EventActivityCompleted, types.EventQuizCompleted:
					toState = string(runtime.PathRunInNode)
				case types.EventHintUsed:
					if fromState == "" || fromState == string(runtime.PathRunNotStarted) {
						toState = string(runtime.PathRunInActivity)
					}
				case types.EventNodeClosed:
					if fromState == string(runtime.PathRunInNode) {
						toState = string(runtime.PathRunPaused)
					}
				}

				if nodeID != uuid.Nil {
					pr.ActiveNodeID = &nodeID
				}
				if activityID != uuid.Nil {
					pr.ActiveActivityID = &activityID
				}
				if typ == types.EventActivityCompleted || typ == types.EventQuizCompleted {
					pr.ActiveActivityID = nil
				}

				pr.State = runtime.PathRunState(toState)
				pr.LastEventID = &ev.ID
				pr.LastEventAt = &ev.OccurredAt
				if err := p.pathRuns.Upsert(tdbc, pr); err != nil {
					return err
				}

				if nodeID != uuid.Nil {
					if err := p.applyNodeRun(tdbc, userID, pathID, nodeID, typ, data, now); err != nil {
						return err
					}
					if typ == types.EventNodeOpened {
						_ = p.applyPrereqGate(tdbc, userID, pathID, nodeID, now)
					}
					if err := p.applyRuntimePlan(tdbc, userID, pathID, nodeID, typ, data, now); err != nil {
						return err
					}
				}
				if activityID != uuid.Nil {
					if err := p.applyActivityRun(tdbc, userID, pathID, nodeID, activityID, typ, data, now); err != nil {
						return err
					}
				}
				if nodeID != uuid.Nil && shouldConsiderProgressiveDoc(typ) {
					progressiveCandidates[pathID] = progressiveDocCandidate{
						PathID:        pathID,
						NodeID:        nodeID,
						ProgressState: strings.TrimSpace(stringFromAny(data["progress_state"])),
						ProgressConf:  floatFromAny(data["progress_confidence"], 0),
						EventType:     typ,
						EventAt:       now,
					}
				}

				payload := map[string]any{
					"event_type": typ,
				}
				if nodeID != uuid.Nil {
					payload["node_id"] = nodeID.String()
				}
				if activityID != uuid.Nil {
					payload["activity_id"] = activityID.String()
				}
				if len(data) > 0 {
					payload["data"] = data
				}
				b, _ := json.Marshal(payload)
				_ = p.trans.Create(tdbc, &types.PathRunTransition{
					UserID:     userID,
					EventID:    ev.ID,
					PathID:     pathID,
					EventType:  typ,
					FromState:  fromState,
					ToState:    toState,
					OccurredAt: ev.OccurredAt,
					Payload:    b,
				})
			}

			if pageLastAt != nil && pageLastID != nil {
				cur := &types.UserEventCursor{
					UserID:        userID,
					Consumer:      runtimeConsumer,
					LastCreatedAt: pageLastAt,
					LastEventID:   pageLastID,
					UpdatedAt:     time.Now().UTC(),
				}
				if err := p.cursors.Upsert(tdbc, cur); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			jc.Fail("apply", err)
			return nil
		}

		if len(progressiveCandidates) > 0 {
			for _, cand := range progressiveCandidates {
				p.maybeEnqueueDocProgressiveBuild(dbctx.Context{Ctx: jc.Ctx}, userID, cand)
				p.maybeEnqueueDocProbeSelect(dbctx.Context{Ctx: jc.Ctx}, userID, cand)
			}
			for k := range progressiveCandidates {
				delete(progressiveCandidates, k)
			}
		}

		afterAt = pageLastAt
		afterID = pageLastID
	}

	if processed == 0 && trigger != "" {
		if err := p.applySessionFallback(dbc, userID, trigger); err != nil {
			jc.Fail("fallback", err)
			return nil
		}
	}

	jc.Succeed("done", map[string]any{
		"processed":   processed,
		"duration_ms": time.Since(start).Milliseconds(),
	})
	return nil
}

func (p *Pipeline) resolveContext(dbc dbctx.Context, ev *types.UserEvent) (uuid.UUID, uuid.UUID, uuid.UUID) {
	var pathID uuid.UUID
	var nodeID uuid.UUID
	var activityID uuid.UUID

	if ev.PathID != nil && *ev.PathID != uuid.Nil {
		pathID = *ev.PathID
	}
	if ev.PathNodeID != nil && *ev.PathNodeID != uuid.Nil {
		nodeID = *ev.PathNodeID
	}
	if ev.ActivityID != nil && *ev.ActivityID != uuid.Nil {
		activityID = *ev.ActivityID
	}

	if pathID == uuid.Nil && nodeID != uuid.Nil && p.pathNodes != nil {
		if node, err := p.pathNodes.GetByID(dbc, nodeID); err == nil && node != nil && node.PathID != uuid.Nil {
			pathID = node.PathID
		}
	}

	if nodeID == uuid.Nil && activityID != uuid.Nil && p.nodeActs != nil {
		if rows, err := p.nodeActs.GetByActivityIDs(dbc, []uuid.UUID{activityID}); err == nil && len(rows) > 0 && rows[0] != nil {
			nodeID = rows[0].PathNodeID
			if pathID == uuid.Nil && p.pathNodes != nil {
				if node, err := p.pathNodes.GetByID(dbc, nodeID); err == nil && node != nil && node.PathID != uuid.Nil {
					pathID = node.PathID
				}
			}
		}
	}

	return pathID, nodeID, activityID
}

func (p *Pipeline) applyNodeRun(dbc dbctx.Context, userID uuid.UUID, pathID uuid.UUID, nodeID uuid.UUID, typ string, data map[string]any, now time.Time) error {
	nr, _ := p.nodeRuns.GetByUserAndNodeID(dbc, userID, nodeID)
	if nr == nil {
		nr = &types.NodeRun{
			UserID: userID,
			PathID: pathID,
			NodeID: nodeID,
			State:  runtime.NodeRunNotStarted,
		}
	}
	if nr.PathID == uuid.Nil {
		nr.PathID = pathID
	}

	switch typ {
	case types.EventNodeOpened:
		if nr.StartedAt == nil {
			nr.StartedAt = &now
		}
		nr.State = runtime.NodeRunReading
		nr.LastSeenAt = &now
	case types.EventScrollDepth, types.EventBlockViewed, types.EventTextSelected:
		nr.State = runtime.NodeRunReading
		nr.LastSeenAt = &now
	case types.EventActivityOpened, types.EventActivityStarted, types.EventQuestionAnswered, types.EventHintUsed:
		nr.State = runtime.NodeRunPractice
		nr.LastSeenAt = &now
	case types.EventActivityCompleted, types.EventQuizCompleted:
		nr.State = runtime.NodeRunPractice
		nr.LastSeenAt = &now
	}

	if typ == types.EventQuestionAnswered {
		nr.AttemptCount++
		if isCorrect, ok := data["is_correct"].(bool); ok {
			if isCorrect {
				nr.LastScore = 1
			} else {
				nr.LastScore = 0
			}
		}
	}

	if completed, ok := data["node_completed"].(bool); ok && completed {
		nr.State = runtime.NodeRunCompleted
		nr.CompletedAt = &now
	}

	return p.nodeRuns.Upsert(dbc, nr)
}

func (p *Pipeline) applyActivityRun(dbc dbctx.Context, userID uuid.UUID, pathID uuid.UUID, nodeID uuid.UUID, activityID uuid.UUID, typ string, data map[string]any, now time.Time) error {
	ar, _ := p.actRuns.GetByUserAndActivityID(dbc, userID, activityID)
	if ar == nil {
		ar = &types.ActivityRun{
			UserID:     userID,
			PathID:     pathID,
			NodeID:     nodeID,
			ActivityID: activityID,
			State:      runtime.ActivityRunNotStarted,
		}
	}
	if ar.PathID == uuid.Nil {
		ar.PathID = pathID
	}
	if ar.NodeID == uuid.Nil && nodeID != uuid.Nil {
		ar.NodeID = nodeID
	}

	switch typ {
	case types.EventActivityOpened, types.EventActivityStarted:
		ar.State = runtime.ActivityRunAttempting
		ar.LastAttemptAt = &now
	case types.EventQuestionAnswered:
		ar.State = runtime.ActivityRunEvaluated
		ar.Attempts++
		ar.LastAttemptAt = &now
		if isCorrect, ok := data["is_correct"].(bool); ok {
			if isCorrect {
				ar.LastScore = 1
			} else {
				ar.LastScore = 0
			}
		}
	case types.EventHintUsed:
		ar.State = runtime.ActivityRunAttempting
		ar.Attempts++
		ar.LastAttemptAt = &now
	case types.EventActivityCompleted, types.EventQuizCompleted:
		ar.State = runtime.ActivityRunCompleted
		ar.CompletedAt = &now
	}

	return p.actRuns.Upsert(dbc, ar)
}

func (p *Pipeline) applySessionFallback(dbc dbctx.Context, userID uuid.UUID, trigger string) error {
	if p.sessions == nil || p.pathRuns == nil || userID == uuid.Nil {
		return nil
	}
	state, err := p.sessions.GetLatestByUserID(dbc, userID)
	if err != nil || state == nil || state.ActivePathID == nil || *state.ActivePathID == uuid.Nil {
		return nil
	}

	pathID := *state.ActivePathID
	pr, _ := p.pathRuns.GetByUserAndPathID(dbc, userID, pathID)
	if pr == nil {
		pr = &types.PathRun{
			UserID: userID,
			PathID: pathID,
			State:  runtime.PathRunNotStarted,
		}
	}
	if state.ActivePathNodeID != nil && *state.ActivePathNodeID != uuid.Nil {
		pr.ActiveNodeID = state.ActivePathNodeID
	}

	pr.Metadata = mergeRuntimeMetadata(pr.Metadata, map[string]any{
		"last_runtime_trigger":    trigger,
		"last_runtime_trigger_at": time.Now().UTC().Format(time.RFC3339),
	})

	return p.pathRuns.Upsert(dbc, pr)
}

func mergeRuntimeMetadata(base datatypes.JSON, patch map[string]any) datatypes.JSON {
	if len(patch) == 0 {
		return base
	}
	out := map[string]any{}
	if len(base) > 0 && string(base) != "null" {
		_ = json.Unmarshal(base, &out)
	}
	if out == nil {
		out = map[string]any{}
	}
	for k, v := range patch {
		out[k] = v
	}
	b, _ := json.Marshal(out)
	return datatypes.JSON(b)
}
