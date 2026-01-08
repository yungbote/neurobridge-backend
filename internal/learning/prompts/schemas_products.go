package prompts

// ---------- shared fragments ----------

func TermDefinitionSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"term":       map[string]any{"type": "string"},
			"definition": map[string]any{"type": "string"},
		},
		"required":             []string{"term", "definition"},
		"additionalProperties": false,
	}
}

func StyleUsedSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tone":          map[string]any{"type": "string"},
			"reading_level": map[string]any{"type": "string"},
			"verbosity":     map[string]any{"type": "string"},
			"format_bias":   map[string]any{"type": "string"},
			"voice":         map[string]any{"type": "string"},
		},
		"required":             []string{"tone", "reading_level", "verbosity", "format_bias", "voice"},
		"additionalProperties": false,
	}
}

// IMPORTANT FIX:
// Only require "kind". Different kinds use different fields.
// Enforce per-kind rules in code (renderer/validator), not in JSON schema.
// IMPORTANT FIX:
// OpenAI strict JSON schema requires that for object schemas:
// - additionalProperties must be present and false
// - required must include EVERY key listed in properties
//
// So we require all fields here and enforce per-kind semantics in code/prompting
// by allowing empty-string / empty-array values for unused fields.
func BlockSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			// Restrict kind to known options so it doesn't drift.
			"kind": EnumSchema(
				"heading",
				"paragraph",
				"bullets",
				"steps",
				"callout",
				"divider",
				"image",
				"video_embed",
				"diagram",
			),

			// Always present (may be empty depending on kind).
			"content_md": map[string]any{"type": "string"},
			"items":      StringArraySchema(),
			"asset_refs": StringArraySchema(),
		},
		"required":             []string{"kind", "content_md", "items", "asset_refs"},
		"additionalProperties": false,
	}
}

func ContentJSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"version":    map[string]any{"type": "integer"},
			"style_used": StyleUsedSchema(),
			"blocks": map[string]any{
				"type":  "array",
				"items": BlockSchema(),
			},
			"citations": StringArraySchema(),
		},
		"required":             []string{"version", "style_used", "blocks", "citations"},
		"additionalProperties": false,
	}
}

func TextStyleSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tone":             map[string]any{"type": "string"},
			"verbosity":        EnumSchema("low", "medium", "high"),
			"explanation_mode": EnumSchema("direct", "analogy", "step_by_step", "clinical_reasoning"),
			"example_density":  EnumSchema("low", "medium", "high"),
		},
		"required":             []string{"tone", "verbosity", "explanation_mode", "example_density"},
		"additionalProperties": false,
	}
}

func DiagramStyleSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"diagram_type": EnumSchema("flowchart", "table", "timeline", "decision_tree", "causal_graph", "taxonomy", "protocol", "none"),
			"format":       EnumSchema("mermaid", "dot", "json"),
			"density":      EnumSchema("sparse", "normal", "dense"),
			"labeling":     EnumSchema("minimal", "standard", "heavy"),
		},
		"required":             []string{"diagram_type", "format", "density", "labeling"},
		"additionalProperties": false,
	}
}

func VideoStyleSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"use_video":      BoolSchema(),
			"video_kind":     EnumSchema("none", "micro_lecture", "walkthrough", "demo"),
			"length_minutes": IntSchema(),
		},
		"required":             []string{"use_video", "video_kind", "length_minutes"},
		"additionalProperties": false,
	}
}

func RepresentationSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"primary_modality":   EnumSchema("text", "diagram", "video", "audio", "interactive"),
			"secondary_modality": EnumSchema("text", "diagram", "video", "audio", "interactive", "none"),

			"variant": map[string]any{"type": "string"}, // freeform but should be from your policy space

			"text_style": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tone":        map[string]any{"type": "string"},
					"verbosity":   map[string]any{"type": "string"},
					"analogy":     EnumSchema("none", "light", "heavy"),
					"tempo":       EnumSchema("slow", "normal", "fast"),
					"format_bias": map[string]any{"type": "string"},
				},
				"required":             []string{"tone", "verbosity", "analogy", "tempo", "format_bias"},
				"additionalProperties": false,
			},

			"diagram_style": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"diagram_type": EnumSchema("flowchart", "concept_map", "table", "timeline", "graph", "schema", "none"),
					"density":      EnumSchema("sparse", "normal", "dense"),
					"labeling":     EnumSchema("minimal", "normal", "heavy"),
				},
				"required":             []string{"diagram_type", "density", "labeling"},
				"additionalProperties": false,
			},

			"video_style": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"include": EnumSchema("never", "optional", "prefer"),
					"length":  EnumSchema("short", "medium", "long"),
				},
				"required":             []string{"include", "length"},
				"additionalProperties": false,
			},

			"study_cycle": StudyCycleSchema(),
		},
		"required":             []string{"primary_modality", "secondary_modality", "variant", "text_style", "diagram_style", "video_style", "study_cycle"},
		"additionalProperties": false,
	}
}

func StudyCycleSchema() map[string]any {
	step := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"offset_hours": IntSchema(),
			"mode":         EnumSchema("quiz", "drill", "flash_review", "summary_rewrite"),
		},
		"required":             []string{"offset_hours", "mode"},
		"additionalProperties": false,
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"explain_pct":            NumberSchema(),
			"worked_examples_pct":    NumberSchema(),
			"retrieval_practice_pct": NumberSchema(),
			"drill_pct":              NumberSchema(),
			"review_schedule":        map[string]any{"type": "array", "items": step},
		},
		"required":             []string{"explain_pct", "worked_examples_pct", "retrieval_practice_pct", "drill_pct", "review_schedule"},
		"additionalProperties": false,
	}
}

// ---------- product schemas ----------

func MaterialSetSummarySchema() map[string]any {
	return SchemaVersionedObject(1, map[string]any{
		"subject":      map[string]any{"type": "string"},
		"level":        EnumSchema("intro", "intermediate", "advanced"),
		"summary_md":   map[string]any{"type": "string"},
		"tags":         StringArraySchema(),
		"concept_keys": StringArraySchema(),
	}, []string{"subject", "level", "summary_md", "tags", "concept_keys"})
}

func ConceptInventorySchema() map[string]any {
	concept := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key":        map[string]any{"type": "string"},
			"name":       map[string]any{"type": "string"},
			"parent_key": StringOrNullSchema(),
			"depth":      IntSchema(),
			"summary":    map[string]any{"type": "string"},
			"key_points": StringArraySchema(),
			"aliases":    StringArraySchema(),
			"importance": IntSchema(),
			"citations":  StringArraySchema(),
		},
		"required": []string{
			"key", "name", "parent_key", "depth", "summary",
			"key_points", "aliases", "importance", "citations",
		},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(3, map[string]any{
		"concepts": map[string]any{"type": "array", "items": concept},
		"coverage": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"confidence":               NumberSchema(),
				"notes":                    map[string]any{"type": "string"},
				"missing_topics_suspected": StringArraySchema(),
			},
			"required":             []string{"confidence", "notes", "missing_topics_suspected"},
			"additionalProperties": false,
		},
	}, []string{"concepts", "coverage"})
}

func ConceptEdgesSchema() map[string]any {
	edge := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"from_key":  map[string]any{"type": "string"},
			"to_key":    map[string]any{"type": "string"},
			"edge_type": EnumSchema("prereq", "related", "analogy"),
			"strength":  NumberSchema(),
			"rationale": map[string]any{"type": "string"},
			"citations": StringArraySchema(),
		},
		"required":             []string{"from_key", "to_key", "edge_type", "strength", "rationale", "citations"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(1, map[string]any{
		"edges": map[string]any{"type": "array", "items": edge},
	}, []string{"edges"})
}

func ConceptClustersSchema() map[string]any {
	cluster := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"label":        map[string]any{"type": "string"},
			"concept_keys": StringArraySchema(),
			"tags":         StringArraySchema(),
			"rationale":    map[string]any{"type": "string"},
		},
		"required":             []string{"label", "concept_keys", "tags", "rationale"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(1, map[string]any{
		"clusters": map[string]any{"type": "array", "items": cluster},
	}, []string{"clusters"})
}

func UserProfileDocSchema() map[string]any {
	return SchemaVersionedObject(1, map[string]any{
		"profile_doc": map[string]any{"type": "string"},
		"style_preferences": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tone":          map[string]any{"type": "string"},
				"verbosity":     EnumSchema("low", "medium", "high"),
				"format_bias":   map[string]any{"type": "string"},
				"diagram_bias":  EnumSchema("low", "medium", "high"),
				"examples_bias": EnumSchema("low", "medium", "high"),
			},
			"required":             []string{"tone", "verbosity", "format_bias", "diagram_bias", "examples_bias"},
			"additionalProperties": false,
		},
	}, []string{"profile_doc", "style_preferences"})
}

func PopulationCohortFeaturesSchema() map[string]any {
	cohort := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"cohort_key": map[string]any{"type": "string"},
			"weight":     NumberSchema(),
			"confidence": NumberSchema(),
			"rationale":  map[string]any{"type": "string"},
		},
		"required":             []string{"cohort_key", "weight", "confidence", "rationale"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(1, map[string]any{
		"cohorts": map[string]any{"type": "array", "items": cohort},
	}, []string{"cohorts"})
}

func PathCharterSchema() map[string]any {
	return SchemaVersionedObject(1, map[string]any{
		"path_style": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tone":          map[string]any{"type": "string"},
				"reading_level": map[string]any{"type": "string"},
				"verbosity":     EnumSchema("low", "medium", "high"),
				"pace":          EnumSchema("slow", "normal", "fast"),
				"analogy_style": EnumSchema("none", "light", "heavy"),
				"terminology_policy": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"must_use_terms": map[string]any{"type": "array", "items": TermDefinitionSchema()},
						"avoid_terms":    StringArraySchema(),
					},
					"required":             []string{"must_use_terms", "avoid_terms"},
					"additionalProperties": false,
				},
				"diagram_conventions": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"preferred_formats": map[string]any{"type": "array", "items": EnumSchema("mermaid", "dot", "json")},
						"labeling":          EnumSchema("minimal", "standard", "heavy"),
						"density":           EnumSchema("sparse", "normal", "dense"),
					},
					"required":             []string{"preferred_formats", "labeling", "density"},
					"additionalProperties": false,
				},
			},
			"required": []string{
				"tone", "reading_level", "verbosity", "pace", "analogy_style",
				"terminology_policy", "diagram_conventions",
			},
			"additionalProperties": false,
		},
	}, []string{"path_style"})
}

func PathStructureSchema() map[string]any {
	slot := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"slot":                 IntSchema(),
			"kind":                 EnumSchema("reading", "quiz", "drill", "case"),
			"primary_concept_keys": StringArraySchema(),
			"estimated_minutes":    IntSchema(),
		},
		"required":             []string{"slot", "kind", "primary_concept_keys", "estimated_minutes"},
		"additionalProperties": false,
	}

	node := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"index":               IntSchema(),
			"parent_index":        IntOrNullSchema(),
			"node_kind":           EnumSchema("module", "lesson", "capstone", "review"),
			"doc_template":        EnumSchema("overview", "concept", "practice", "cheatsheet", "project", "review"),
			"title":               map[string]any{"type": "string"},
			"goal":                map[string]any{"type": "string"},
			"concept_keys":        StringArraySchema(),
			"prereq_concept_keys": StringArraySchema(),
			"difficulty":          EnumSchema("intro", "intermediate", "advanced"),
			"activity_slots":      map[string]any{"type": "array", "items": slot},
		},
		"required": []string{
			"index",
			"parent_index",
			"node_kind",
			"doc_template",
			"title",
			"goal",
			"concept_keys",
			"prereq_concept_keys",
			"difficulty",
			"activity_slots",
		},
		"additionalProperties": false,
	}

	return SchemaVersionedObject(2, map[string]any{
		"title":       map[string]any{"type": "string"},
		"description": map[string]any{"type": "string"},
		"nodes":       map[string]any{"type": "array", "items": node},
		"coverage_check": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"uncovered_concept_keys": StringArraySchema(),
			},
			"required":             []string{"uncovered_concept_keys"},
			"additionalProperties": false,
		},
	}, []string{"title", "description", "nodes", "coverage_check"})
}

func NodeRepresentationPlanSchema() map[string]any {
	return SchemaVersionedObject(2, map[string]any{
		"node_index":             IntSchema(),
		"default_representation": RepresentationSchema(),
		"novelty_policy": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"avoid_dwelling_when_similarity_gte": NumberSchema(),
				"use_diagnostic_when_similarity_gte": NumberSchema(),
				"target_mastery_threshold":           NumberSchema(),
			},
			"required":             []string{"avoid_dwelling_when_similarity_gte", "use_diagnostic_when_similarity_gte", "target_mastery_threshold"},
			"additionalProperties": false,
		},
	}, []string{"node_index", "default_representation", "novelty_policy"})
}

func ChainRepresentationCandidatesSchema() map[string]any {
	candidate := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"chain_key":      map[string]any{"type": "string"},
			"concept_keys":   StringArraySchema(),
			"representation": RepresentationSchema(),
			"learning_value": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"why":          map[string]any{"type": "string"},
					"priority":     IntSchema(),    // 1..5
					"novelty_hint": NumberSchema(), // 0..1
				},
				"required":             []string{"why", "priority", "novelty_hint"},
				"additionalProperties": false,
			},
			"pattern_suggestion": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern_query": map[string]any{"type": "string"},
					"pattern_key":   map[string]any{"type": "string"},
				},
				"required":             []string{"pattern_query", "pattern_key"},
				"additionalProperties": false,
			},
			"signals": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"user_signals_used":       StringArraySchema(),
					"population_signals_used": StringArraySchema(),
					"coherence_risk":          EnumSchema("low", "medium", "high"),
					"cost_risk":               EnumSchema("low", "medium", "high"),
				},
				"required":             []string{"user_signals_used", "population_signals_used", "coherence_risk", "cost_risk"},
				"additionalProperties": false,
			},
			"notes": map[string]any{"type": "string"},
		},
		"required":             []string{"chain_key", "concept_keys", "representation", "learning_value", "pattern_suggestion", "signals", "notes"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(2, map[string]any{
		"candidates": map[string]any{"type": "array", "items": candidate},
	}, []string{"candidates"})
}

func OverrideResolutionSchema() map[string]any {
	dec := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"chain_key":     map[string]any{"type": "string"},
			"chosen_source": EnumSchema("node", "chain"),
			"chosen_reason": map[string]any{"type": "string"},
			"risks":         StringArraySchema(),
			"mitigations":   StringArraySchema(),
		},
		"required":             []string{"chain_key", "chosen_source", "chosen_reason", "risks", "mitigations"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(1, map[string]any{
		"decisions": map[string]any{"type": "array", "items": dec},
	}, []string{"decisions"})
}

func RetrievalSpecSchema() map[string]any {
	q := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"node_index": IntSchema(),
					"slot":       IntSchema(),
					"chain_key":  map[string]any{"type": "string"},
				},
				"required":             []string{"node_index", "slot", "chain_key"},
				"additionalProperties": false,
			},
			"query_text": map[string]any{"type": "string"},
			"namespaces": StringArraySchema(),
			"filters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"concept_keys":       StringArraySchema(),
					"modality":           map[string]any{"type": "string"},
					"variant":            map[string]any{"type": "string"},
					"representation_key": map[string]any{"type": "string"},
				},
				"required":             []string{"concept_keys", "modality", "variant", "representation_key"},
				"additionalProperties": false,
			},
			"top_k":           IntSchema(),
			"reuse_threshold": NumberSchema(),
		},
		"required":             []string{"target", "query_text", "namespaces", "filters", "top_k", "reuse_threshold"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(2, map[string]any{
		"queries": map[string]any{"type": "array", "items": q},
	}, []string{"queries"})
}

func ActivityContentSchema() map[string]any {
	return SchemaVersionedObject(1, map[string]any{
		"title":             map[string]any{"type": "string"},
		"kind":              map[string]any{"type": "string"},
		"estimated_minutes": IntSchema(),
		"content_json":      ContentJSONSchema(),
		"citations":         StringArraySchema(),
	}, []string{"title", "kind", "estimated_minutes", "content_json", "citations"})
}

func ActivityVariantSchema() map[string]any {
	return SchemaVersionedObject(1, map[string]any{
		"variant":      map[string]any{"type": "string"},
		"content_json": ContentJSONSchema(),
		"render_spec":  map[string]any{"type": "object"},
		"citations":    StringArraySchema(),
	}, []string{"variant", "content_json", "render_spec", "citations"})
}

func DiagramSpecSchema() map[string]any {
	return SchemaVersionedObject(1, map[string]any{
		"diagram_type": EnumSchema("flowchart", "table", "timeline", "decision_tree", "causal_graph", "taxonomy", "protocol"),
		"format":       EnumSchema("mermaid", "dot", "json"),

		// IMPORTANT FIX:
		// {} means "anything". We want either a string (mermaid/dot text) OR an object (json graph spec).
		"spec": map[string]any{
			"oneOf": []any{
				map[string]any{"type": "string"},
				map[string]any{"type": "object"},
			},
		},

		"alt_text":  map[string]any{"type": "string"},
		"citations": StringArraySchema(),
	}, []string{"diagram_type", "format", "spec", "alt_text", "citations"})
}

func VideoStoryboardSchema() map[string]any {
	beat := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"t_start_sec": IntSchema(),
			"t_end_sec":   IntSchema(),
			"goal":        map[string]any{"type": "string"},
			"on_screen":   StringArraySchema(),
		},
		"required":             []string{"t_start_sec", "t_end_sec", "goal", "on_screen"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(1, map[string]any{
		"video_kind":     EnumSchema("micro_lecture", "walkthrough", "demo"),
		"length_minutes": IntSchema(),
		"script_md":      map[string]any{"type": "string"},
		"beats":          map[string]any{"type": "array", "items": beat},
		"citations":      StringArraySchema(),
	}, []string{"video_kind", "length_minutes", "script_md", "beats", "citations"})
}

func QuizFromActivitySchema() map[string]any {
	q := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"type":           EnumSchema("mcq"),
			"prompt_md":      map[string]any{"type": "string"},
			"options":        StringArraySchema(),
			"correct_index":  IntSchema(),
			"explanation_md": map[string]any{"type": "string"},
			"citations":      StringArraySchema(),
		},
		"required":             []string{"type", "prompt_md", "options", "correct_index", "explanation_md", "citations"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(1, map[string]any{
		"questions": map[string]any{"type": "array", "items": q},
	}, []string{"questions"})
}

func CoverageAndCoherenceAuditSchema() map[string]any {
	termIssue := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"term":  map[string]any{"type": "string"},
			"issue": map[string]any{"type": "string"},
		},
		"required":             []string{"term", "issue"},
		"additionalProperties": false,
	}
	fix := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"type":   EnumSchema("add_node", "merge_nodes", "adjust_representation", "regen_variant"),
			"target": map[string]any{"type": "string"},
			"reason": map[string]any{"type": "string"},
		},
		"required":             []string{"type", "target", "reason"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(1, map[string]any{
		"coverage": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"uncovered_concept_keys":       StringArraySchema(),
				"duplicate_concepts_suspected": StringArraySchema(),
			},
			"required":             []string{"uncovered_concept_keys", "duplicate_concepts_suspected"},
			"additionalProperties": false,
		},
		"coherence": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"terminology_conflicts": map[string]any{"type": "array", "items": termIssue},
				"style_inconsistencies": StringArraySchema(),
				"sequence_issues":       StringArraySchema(),
			},
			"required":             []string{"terminology_conflicts", "style_inconsistencies", "sequence_issues"},
			"additionalProperties": false,
		},
		"recommended_fixes": map[string]any{"type": "array", "items": fix},
	}, []string{"coverage", "coherence", "recommended_fixes"})
}

func DecisionTraceExplanationSchema() map[string]any {
	return SchemaVersionedObject(1, map[string]any{
		"decision_type": map[string]any{"type": "string"},
		"summary":       map[string]any{"type": "string"},
		"reasoning":     map[string]any{"type": "string"},
		"risks":         StringArraySchema(),
		"mitigations":   StringArraySchema(),
	}, []string{"decision_type", "summary", "reasoning", "risks", "mitigations"})
}

func TeachingPatternsSchema() map[string]any {
	p := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern_key":    map[string]any{"type": "string"},
			"name":           map[string]any{"type": "string"},
			"when_to_use":    map[string]any{"type": "string"},
			"representation": RepresentationSchema(),
			"constraints": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"must_use_terms": map[string]any{"type": "array", "items": TermDefinitionSchema()},
					"avoid_terms":    StringArraySchema(),
				},
				"required":             []string{"must_use_terms", "avoid_terms"},
				"additionalProperties": false,
			},
		},
		"required":             []string{"pattern_key", "name", "when_to_use", "representation", "constraints"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(1, map[string]any{
		"patterns": map[string]any{"type": "array", "items": p},
	}, []string{"patterns"})
}

func DiagnosticGateSchema() map[string]any {
	q := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"type":           EnumSchema("mcq"),
			"prompt_md":      map[string]any{"type": "string"},
			"options":        StringArraySchema(),
			"correct_index":  IntSchema(),
			"explanation_md": map[string]any{"type": "string"},
			"citations":      StringArraySchema(),
		},
		"required":             []string{"type", "prompt_md", "options", "correct_index", "explanation_md", "citations"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(1, map[string]any{
		"purpose":   map[string]any{"type": "string", "const": "diagnostic"},
		"questions": map[string]any{"type": "array", "items": q},
	}, []string{"purpose", "questions"})
}
