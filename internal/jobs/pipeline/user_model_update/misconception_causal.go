package user_model_update

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

const (
	misconCausalEdgeTypePrereq = "prereq_miscon"
	misconCausalSchemaVersion  = 1
)

type misconSignal struct {
	ConceptID     uuid.UUID
	SeenAt        time.Time
	Confidence    float64
	SourceID      string
	SignatureType string
}

type causalSupportPointer struct {
	SourceType      string  `json:"source_type"`
	SourceID        string  `json:"source_id"`
	OccurredAt      string  `json:"occurred_at"`
	Confidence      float64 `json:"confidence"`
	TargetConceptID string  `json:"target_concept_id,omitempty"`
	SignatureType   string  `json:"signature_type,omitempty"`
}

type causalEvidence struct {
	SchemaVersion int                    `json:"schema_version"`
	Source        string                 `json:"source,omitempty"`
	Count         int                    `json:"count,omitempty"`
	LastSeenAt    string                 `json:"last_seen_at,omitempty"`
	Support       []causalSupportPointer `json:"support,omitempty"`
}

func (p *Pipeline) maybeUpsertMisconceptionCausalEdges(
	dbc dbctx.Context,
	userID uuid.UUID,
	signals []misconSignal,
	misconRows []*types.UserMisconceptionInstance,
	now time.Time,
) {
	if p == nil || p.misconEdges == nil || p.edges == nil || p.misconRepo == nil {
		return
	}
	if userID == uuid.Nil || len(signals) == 0 {
		return
	}

	targets := map[uuid.UUID]misconSignal{}
	for _, sig := range signals {
		if sig.ConceptID == uuid.Nil {
			continue
		}
		prev, ok := targets[sig.ConceptID]
		if !ok || sig.SeenAt.After(prev.SeenAt) || sig.Confidence > prev.Confidence {
			targets[sig.ConceptID] = sig
		}
	}
	if len(targets) == 0 {
		return
	}

	targetIDs := make([]uuid.UUID, 0, len(targets))
	for id := range targets {
		targetIDs = append(targetIDs, id)
	}
	sort.Slice(targetIDs, func(i, j int) bool { return targetIDs[i].String() < targetIDs[j].String() })

	edges, err := p.edges.GetByToConceptIDs(dbc, targetIDs)
	if err != nil || len(edges) == 0 {
		return
	}

	prereqByTarget := map[uuid.UUID][]*types.ConceptEdge{}
	prereqIDs := map[uuid.UUID]bool{}
	for _, e := range edges {
		if e == nil || e.FromConceptID == uuid.Nil || e.ToConceptID == uuid.Nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(e.EdgeType), "prereq") {
			continue
		}
		if e.Strength < 0.2 {
			continue
		}
		prereqByTarget[e.ToConceptID] = append(prereqByTarget[e.ToConceptID], e)
		prereqIDs[e.FromConceptID] = true
	}
	if len(prereqIDs) == 0 {
		return
	}

	prereqList := make([]uuid.UUID, 0, len(prereqIDs))
	for id := range prereqIDs {
		prereqList = append(prereqList, id)
	}
	sort.Slice(prereqList, func(i, j int) bool { return prereqList[i].String() < prereqList[j].String() })

	activeMiscon := map[uuid.UUID]float64{}
	if rows, err := p.misconRepo.ListActiveByUserAndConceptIDs(dbc, userID, prereqList); err == nil {
		for _, row := range rows {
			if row == nil || row.CanonicalConceptID == uuid.Nil {
				continue
			}
			conf := clamp01(row.Confidence)
			if prev, ok := activeMiscon[row.CanonicalConceptID]; !ok || conf > prev {
				activeMiscon[row.CanonicalConceptID] = conf
			}
		}
	}
	if len(misconRows) > 0 {
		for _, row := range misconRows {
			if row == nil || row.CanonicalConceptID == uuid.Nil || !strings.EqualFold(row.Status, "active") {
				continue
			}
			if !prereqIDs[row.CanonicalConceptID] {
				continue
			}
			conf := clamp01(row.Confidence)
			if prev, ok := activeMiscon[row.CanonicalConceptID]; !ok || conf > prev {
				activeMiscon[row.CanonicalConceptID] = conf
			}
		}
	}
	if len(activeMiscon) == 0 {
		return
	}

	union := append([]uuid.UUID{}, prereqList...)
	union = append(union, targetIDs...)
	existingRows, _ := p.misconEdges.ListByUserAndConceptIDs(dbc, userID, union)
	existing := map[string]*types.MisconceptionCausalEdge{}
	for _, row := range existingRows {
		if row == nil || row.UserID == uuid.Nil {
			continue
		}
		key := causalEdgeKey(row.FromConceptID, row.ToConceptID, row.EdgeType)
		existing[key] = row
	}

	for targetID, sig := range targets {
		prereqs := prereqByTarget[targetID]
		if len(prereqs) == 0 {
			continue
		}
		for _, edge := range prereqs {
			if edge == nil || edge.FromConceptID == uuid.Nil {
				continue
			}
			causeConf := activeMiscon[edge.FromConceptID]
			if causeConf <= 0 {
				continue
			}
			strength := clamp01(edge.Strength * (0.4 + 0.6*causeConf) * (0.4 + 0.6*clamp01(sig.Confidence)))
			if strength <= 0 {
				continue
			}
			key := causalEdgeKey(edge.FromConceptID, targetID, misconCausalEdgeTypePrereq)
			row := existing[key]
			count := 0
			prevStrength := 0.0
			if row != nil {
				count = row.Count
				prevStrength = row.Strength
			}
			newStrength := strength
			if count > 0 {
				newStrength = clamp01((prevStrength*float64(count) + strength) / float64(count+1))
			}

			ev := decodeCausalEvidence(nil)
			if row != nil {
				ev = decodeCausalEvidence(row.Evidence)
			}
			ptr := causalSupportPointer{
				SourceType:      "user_event",
				SourceID:        sig.SourceID,
				OccurredAt:      sig.SeenAt.UTC().Format(time.RFC3339Nano),
				Confidence:      clamp01(sig.Confidence),
				TargetConceptID: targetID.String(),
				SignatureType:   sig.SignatureType,
			}
			ev = mergeCausalEvidence(ev, ptr, 20, sig.SeenAt)

			row = &types.MisconceptionCausalEdge{
				UserID:        userID,
				FromConceptID: edge.FromConceptID,
				ToConceptID:   targetID,
				EdgeType:      misconCausalEdgeTypePrereq,
				Strength:      newStrength,
				Count:         ev.Count,
				SchemaVersion: misconCausalSchemaVersion,
				Evidence:      datatypes.JSON(mustJSON(ev)),
				LastSeenAt:    timePtr(sig.SeenAt),
			}
			_ = p.misconEdges.Upsert(dbc, row)
		}
	}
}

func causalEdgeKey(fromID uuid.UUID, toID uuid.UUID, edgeType string) string {
	return fmt.Sprintf("%s:%s:%s", fromID.String(), toID.String(), strings.ToLower(strings.TrimSpace(edgeType)))
}

func decodeCausalEvidence(raw datatypes.JSON) causalEvidence {
	ev := causalEvidence{SchemaVersion: misconCausalSchemaVersion, Source: "user_model_update"}
	if len(raw) == 0 || string(raw) == "null" {
		return ev
	}
	_ = json.Unmarshal(raw, &ev)
	if ev.SchemaVersion == 0 {
		ev.SchemaVersion = misconCausalSchemaVersion
	}
	if ev.Source == "" {
		ev.Source = "user_model_update"
	}
	return ev
}

func mergeCausalEvidence(ev causalEvidence, ptr causalSupportPointer, max int, seenAt time.Time) causalEvidence {
	ev.Count++
	ev.LastSeenAt = seenAt.UTC().Format(time.RFC3339Nano)
	if ptr.SourceID != "" {
		dup := false
		for _, p := range ev.Support {
			if p.SourceID == ptr.SourceID && p.SourceType == ptr.SourceType {
				dup = true
				break
			}
		}
		if !dup {
			ev.Support = append(ev.Support, ptr)
		}
	}
	if max > 0 && len(ev.Support) > max {
		ev.Support = ev.Support[len(ev.Support)-max:]
	}
	return ev
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	tt := t.UTC()
	return &tt
}
