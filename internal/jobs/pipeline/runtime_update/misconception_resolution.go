package runtime_update

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func (p *Pipeline) updateMisconceptionResolution(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID, isCorrect bool, now time.Time, evidence map[string]any) {
	if p.misconRes == nil || p.miscons == nil || userID == uuid.Nil || len(conceptIDs) == 0 {
		return
	}
	if p.db != nil && !hasTable(p.db, &types.MisconceptionResolutionState{}) {
		return
	}

	miscons, _ := p.miscons.ListActiveByUserAndConceptIDs(dbc, userID, conceptIDs)
	if len(miscons) == 0 {
		return
	}
	activeSet := map[uuid.UUID]bool{}
	misconsByConcept := map[uuid.UUID][]*types.UserMisconceptionInstance{}
	for _, m := range miscons {
		if m == nil || m.CanonicalConceptID == uuid.Nil {
			continue
		}
		activeSet[m.CanonicalConceptID] = true
		misconsByConcept[m.CanonicalConceptID] = append(misconsByConcept[m.CanonicalConceptID], m)
	}
	if len(activeSet) == 0 {
		return
	}

	activeIDs := make([]uuid.UUID, 0, len(activeSet))
	for id := range activeSet {
		activeIDs = append(activeIDs, id)
	}

	stateByID := map[uuid.UUID]*types.MisconceptionResolutionState{}
	if rows, _ := p.misconRes.ListByUserAndConceptIDs(dbc, userID, activeIDs); len(rows) > 0 {
		for _, r := range rows {
			if r != nil && r.ConceptID != uuid.Nil {
				stateByID[r.ConceptID] = r
			}
		}
	}

	for _, id := range activeIDs {
		st := stateByID[id]
		if st == nil {
			st = &types.MisconceptionResolutionState{
				UserID:    userID,
				ConceptID: id,
				Status:    "open",
			}
		}
		prevStatus := st.Status
		if st.RequiredCorrect <= 0 {
			st.RequiredCorrect = misconResolveMinCorrect()
		}

		if isCorrect {
			credit := 1
			if evidence != nil && boolFromAny(evidence["transfer_success"]) {
				credit++
			}
			st.CorrectCount += credit
			st.LastCorrectAt = &now
			if st.Status == "" || st.Status == "open" {
				st.Status = "resolving"
			}
			if st.CorrectCount >= st.RequiredCorrect {
				st.Status = "resolved"
				st.ResolvedAt = &now
				if days := misconReviewDays(); days > 0 {
					next := now.Add(time.Duration(days) * 24 * time.Hour)
					st.NextReviewAt = &next
				}
			}
		} else {
			st.IncorrectCount++
			st.LastIncorrectAt = &now
			if st.Status == "resolved" {
				st.Status = "relapsed"
				st.RelapsedAt = &now
				if misconRelapseReset() {
					st.CorrectCount = 0
				}
			} else if st.Status == "" {
				st.Status = "open"
			}
		}

		if evidence != nil && len(evidence) > 0 {
			st.EvidenceJSON = datatypes.JSON(mustJSON(evidence))
		}

		_ = p.misconRes.Upsert(dbc, st)
		if p.metrics != nil && st.Status != "" && st.Status != prevStatus {
			p.metrics.IncConvergenceMisconceptionResolution(st.Status)
		}

		if p.miscons == nil {
			continue
		}
		misList := misconsByConcept[id]
		if len(misList) == 0 {
			continue
		}
		failureCtx := misconceptionFailureContext(evidence)
		for _, mis := range misList {
			if mis == nil {
				continue
			}
			changed := false
			support := types.DecodeMisconceptionSupport(mis.Support)
			if isCorrect {
				support.ResolutionEvidenceCount++
				changed = true
				if st.RequiredCorrect > 0 {
					target := float64(st.CorrectCount) / float64(st.RequiredCorrect)
					if target > 1 {
						target = 1
					}
					if target > support.ResolutionConfidence {
						support.ResolutionConfidence = target
					}
				}
			} else if prevStatus == "resolved" || st.Status == "relapsed" {
				if support.ResolutionConfidence == 0 {
					support.ResolutionConfidence = 0.25
					changed = true
				} else if support.ResolutionConfidence > 0.35 {
					support.ResolutionConfidence = 0.35
					changed = true
				}
				if failureCtx != "" && failureCtx != support.LastFailedContextAfterResolution {
					support.LastFailedContextAfterResolution = failureCtx
					changed = true
				}
			}
			if changed {
				mis.Support = types.EncodeMisconceptionSupport(support)
				_ = p.miscons.Upsert(dbc, mis)
			}
		}
	}
}

func misconceptionFailureContext(evidence map[string]any) string {
	if evidence == nil {
		return ""
	}
	if v := strings.TrimSpace(stringFromAny(evidence["question_id"])); v != "" {
		return "question:" + v
	}
	if v := strings.TrimSpace(stringFromAny(evidence["block_id"])); v != "" {
		return "block:" + v
	}
	if v := strings.TrimSpace(stringFromAny(evidence["node_id"])); v != "" {
		return "node:" + v
	}
	if v := strings.TrimSpace(stringFromAny(evidence["event_type"])); v != "" {
		return "event:" + v
	}
	return ""
}
