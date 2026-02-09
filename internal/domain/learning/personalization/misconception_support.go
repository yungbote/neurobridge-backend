package personalization

import (
	"encoding/json"
	"strings"

	"gorm.io/datatypes"
)

const MisconceptionSupportSchemaVersion = 1

type MisconceptionSupportPointer struct {
	SourceType string  `json:"source_type"`
	SourceID   string  `json:"source_id"`
	OccurredAt string  `json:"occurred_at,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

type MisconceptionSupport struct {
	SchemaVersion                   int                         `json:"schema_version"`
	SignatureType                   string                      `json:"signature_type,omitempty"`
	FrameFrom                       string                      `json:"frame_from,omitempty"`
	FrameTo                         string                      `json:"frame_to,omitempty"`
	TriggerContexts                 []string                    `json:"trigger_contexts,omitempty"`
	StabilityScore                  float64                     `json:"stability_score,omitempty"`
	ResolutionConfidence            float64                     `json:"resolution_confidence,omitempty"`
	ResolutionEvidenceCount         int                         `json:"resolution_evidence_count,omitempty"`
	LastFailedContextAfterResolution string                     `json:"last_failed_context_after_resolution,omitempty"`
	Support                         []MisconceptionSupportPointer `json:"support,omitempty"`
}

func NormalizeMisconceptionSignature(sig string) string {
	sig = strings.TrimSpace(strings.ToLower(sig))
	switch sig {
	case "frame_error",
		"transfer_failure",
		"boundary_error",
		"causal_mislink",
		"procedural_gap",
		"overgeneralization",
		"ontology_swap":
		return sig
	default:
		return "unknown"
	}
}

func DecodeMisconceptionSupport(raw datatypes.JSON) MisconceptionSupport {
	out := MisconceptionSupport{SchemaVersion: MisconceptionSupportSchemaVersion}
	if len(raw) == 0 {
		return out
	}
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return out
	}

	var sup MisconceptionSupport
	if err := json.Unmarshal(raw, &sup); err == nil {
		if sup.SchemaVersion > 0 || sup.SignatureType != "" || len(sup.Support) > 0 || len(sup.TriggerContexts) > 0 {
			if sup.SchemaVersion == 0 {
				sup.SchemaVersion = MisconceptionSupportSchemaVersion
			}
			sup.SignatureType = NormalizeMisconceptionSignature(sup.SignatureType)
			return sup
		}
	}

	var ptr MisconceptionSupportPointer
	if err := json.Unmarshal(raw, &ptr); err == nil {
		if ptr.SourceType != "" || ptr.SourceID != "" {
			out.Support = []MisconceptionSupportPointer{ptr}
			return out
		}
	}

	var list []MisconceptionSupportPointer
	if err := json.Unmarshal(raw, &list); err == nil && len(list) > 0 {
		out.Support = list
		return out
	}

	return out
}

func EncodeMisconceptionSupport(s MisconceptionSupport) datatypes.JSON {
	if s.SchemaVersion == 0 {
		s.SchemaVersion = MisconceptionSupportSchemaVersion
	}
	s.SignatureType = NormalizeMisconceptionSignature(s.SignatureType)
	return datatypes.JSON(mustJSON(s))
}

func MergeMisconceptionSupportPointer(s MisconceptionSupport, ptr MisconceptionSupportPointer, max int) MisconceptionSupport {
	if ptr.SourceType == "" || ptr.SourceID == "" {
		return s
	}
	for _, it := range s.Support {
		if it.SourceType == ptr.SourceType && it.SourceID == ptr.SourceID {
			return s
		}
	}
	s.Support = append(s.Support, ptr)
	if max > 0 && len(s.Support) > max {
		s.Support = s.Support[len(s.Support)-max:]
	}
	return s
}

func AddMisconceptionTriggerContext(s MisconceptionSupport, ctx string, max int) MisconceptionSupport {
	ctx = strings.TrimSpace(ctx)
	if ctx == "" {
		return s
	}
	for _, v := range s.TriggerContexts {
		if v == ctx {
			return s
		}
	}
	s.TriggerContexts = append(s.TriggerContexts, ctx)
	if max > 0 && len(s.TriggerContexts) > max {
		s.TriggerContexts = s.TriggerContexts[len(s.TriggerContexts)-max:]
	}
	return s
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
