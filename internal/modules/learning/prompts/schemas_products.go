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

func FileSignatureSchema() map[string]any {
	section := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":      map[string]any{"type": "string"},
			"path":       map[string]any{"type": "string"},
			"start_page": IntOrNullSchema(),
			"end_page":   IntOrNullSchema(),
			"start_sec":  NumberOrNullSchema(),
			"end_sec":    NumberOrNullSchema(),
		},
		"required":             []string{"title", "path", "start_page", "end_page", "start_sec", "end_sec"},
		"additionalProperties": false,
	}

	outline := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":    map[string]any{"type": "string"},
			"sections": map[string]any{"type": "array", "items": section},
		},
		"required":             []string{"title", "sections"},
		"additionalProperties": false,
	}

	quality := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text_quality": map[string]any{"type": "string"},
			"coverage":     NumberSchema(),
			"notes":        map[string]any{"type": "string"},
		},
		"required":             []string{"text_quality", "coverage", "notes"},
		"additionalProperties": false,
	}

	return SchemaVersionedObject(1, map[string]any{
		"summary_md":         map[string]any{"type": "string"},
		"topics":             StringArraySchema(),
		"concept_keys":       StringArraySchema(),
		"difficulty":         EnumSchema("intro", "intermediate", "advanced", "mixed", "unknown"),
		"domain_tags":        StringArraySchema(),
		"citations":          StringArraySchema(),
		"outline_json":       outline,
		"outline_confidence": NumberSchema(),
		"language":           map[string]any{"type": "string"},
		"quality":            quality,
	}, []string{
		"summary_md",
		"topics",
		"concept_keys",
		"difficulty",
		"domain_tags",
		"citations",
		"outline_json",
		"outline_confidence",
		"language",
		"quality",
	})
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

func ConceptInventoryDeltaSchema() map[string]any {
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
	return SchemaVersionedObject(1, map[string]any{
		"new_concepts": map[string]any{"type": "array", "items": concept},
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
	}, []string{"new_concepts", "coverage"})
}

func AssumedKnowledgeSchema() map[string]any {
	item := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key":         map[string]any{"type": "string"},
			"name":        map[string]any{"type": "string"},
			"summary":     map[string]any{"type": "string"},
			"aliases":     StringArraySchema(),
			"importance":  IntSchema(),
			"citations":   StringArraySchema(),
			"required_by": StringArraySchema(),
		},
		"required":             []string{"key", "name", "summary", "aliases", "importance", "citations", "required_by"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(1, map[string]any{
		"assumed_concepts": map[string]any{"type": "array", "items": item},
		"notes":            map[string]any{"type": "string"},
	}, []string{"assumed_concepts", "notes"})
}

func ConceptAlignmentSchema() map[string]any {
	alias := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"canonical_key": map[string]any{"type": "string"},
			"alias_keys":    StringArraySchema(),
			"rationale":     map[string]any{"type": "string"},
		},
		"required":             []string{"canonical_key", "alias_keys", "rationale"},
		"additionalProperties": false,
	}
	meaning := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key":       map[string]any{"type": "string"},
			"name":      map[string]any{"type": "string"},
			"summary":   map[string]any{"type": "string"},
			"aliases":   StringArraySchema(),
			"citations": StringArraySchema(),
			"rationale": map[string]any{"type": "string"},
		},
		"required":             []string{"key", "name", "summary", "aliases", "citations", "rationale"},
		"additionalProperties": false,
	}
	split := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ambiguous_key": map[string]any{"type": "string"},
			"meanings":      map[string]any{"type": "array", "items": meaning},
		},
		"required":             []string{"ambiguous_key", "meanings"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(1, map[string]any{
		"aliases": map[string]any{"type": "array", "items": alias},
		"splits":  map[string]any{"type": "array", "items": split},
	}, []string{"aliases", "splits"})
}

func FormulaExtractionSchema() map[string]any {
	formula := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"raw":       map[string]any{"type": "string"},
			"latex":     map[string]any{"type": "string"},
			"symbolic":  map[string]any{"type": "string"},
			"notes":     map[string]any{"type": "string"},
		},
		"required":             []string{"raw", "latex", "symbolic", "notes"},
		"additionalProperties": false,
	}
	item := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"chunk_id": map[string]any{"type": "string"},
			"formulas": map[string]any{"type": "array", "items": formula},
		},
		"required":             []string{"chunk_id", "formulas"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(1, map[string]any{
		"items": map[string]any{"type": "array", "items": item},
	}, []string{"items"})
}

func StyleManifestSchema() map[string]any {
	return SchemaVersionedObject(1, map[string]any{
		"tone":               map[string]any{"type": "string"},
		"register":           map[string]any{"type": "string"},
		"verbosity":          map[string]any{"type": "string"},
		"metaphors_allowed":  map[string]any{"type": "boolean"},
		"preferred_phrases":  StringArraySchema(),
		"banned_phrases":     StringArraySchema(),
		"do_list":            StringArraySchema(),
		"dont_list":          StringArraySchema(),
		"sentence_length":    map[string]any{"type": "string"},
		"voice_notes":        map[string]any{"type": "string"},
	}, []string{"tone", "register", "verbosity", "metaphors_allowed", "preferred_phrases", "banned_phrases", "do_list", "dont_list", "sentence_length", "voice_notes"})
}

func PathNarrativePlanSchema() map[string]any {
	return SchemaVersionedObject(1, map[string]any{
		"arc_summary":        map[string]any{"type": "string"},
		"continuity_rules":   StringArraySchema(),
		"recurring_terms":    StringArraySchema(),
		"preferred_transitions": StringArraySchema(),
		"forbidden_phrases":  StringArraySchema(),
		"back_reference_rules": StringArraySchema(),
		"forward_reference_rules": StringArraySchema(),
		"tone_notes":         map[string]any{"type": "string"},
	}, []string{"arc_summary", "continuity_rules", "recurring_terms", "preferred_transitions", "forbidden_phrases", "back_reference_rules", "forward_reference_rules", "tone_notes"})
}

func NodeNarrativePlanSchema() map[string]any {
	return SchemaVersionedObject(1, map[string]any{
		"opening_intent":  map[string]any{"type": "string"},
		"closing_intent":  map[string]any{"type": "string"},
		"back_references": StringArraySchema(),
		"forward_link":    map[string]any{"type": "string"},
		"anchor_terms":    StringArraySchema(),
		"avoid_phrases":   StringArraySchema(),
	}, []string{"opening_intent", "closing_intent", "back_references", "forward_link", "anchor_terms", "avoid_phrases"})
}

func MediaRankSchema() map[string]any {
	item := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"section_heading": map[string]any{"type": "string"},
			"purpose":         map[string]any{"type": "string"},
			"asset_url":       map[string]any{"type": "string"},
			"asset_kind":      map[string]any{"type": "string"},
			"rationale":       map[string]any{"type": "string"},
			"chunk_ids":       StringArraySchema(),
		},
		"required":             []string{"section_heading", "purpose", "asset_url", "asset_kind", "rationale", "chunk_ids"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(1, map[string]any{
		"selections": map[string]any{"type": "array", "items": item},
	}, []string{"selections"})
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

func TeachingPatternHierarchySchema() map[string]any {
	path := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sequencing": EnumSchema(
				"linear", "spiral", "modular", "branching", "layered", "thematic",
				"chronological", "whole_to_part", "part_to_whole", "concentric", "comparative", "problem_arc",
			),
			"pedagogy": EnumSchema(
				"direct_instruction", "project_based", "problem_based", "case_based", "inquiry_based",
				"discovery", "narrative", "apprenticeship", "simulation", "socratic",
				"challenge_ladder", "competency",
			),
			"mastery": EnumSchema(
				"mastery_gated", "soft_gated", "ungated", "diagnostic_adaptive", "xp_progression",
			),
			"reinforcement": EnumSchema(
				"spaced_review", "interleaved", "cumulative", "end_review", "just_in_time", "none",
			),
			"rationale": map[string]any{"type": "string"},
		},
		"required":             []string{"sequencing", "pedagogy", "mastery", "reinforcement"},
		"additionalProperties": false,
	}

	module := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"module_index": IntSchema(),
			"sequencing": EnumSchema(
				"linear_lessons", "sandwich", "hub_spoke", "funnel", "expansion", "spiral_mini",
				"parallel", "comparative_pairs", "chronological", "simple_to_complex", "dependency_driven",
			),
			"pedagogy": EnumSchema(
				"theory_then_practice", "practice_then_theory", "interleaved", "immersion", "survey",
				"case_driven", "project_milestone", "problem_solution", "skill_build", "concept_build",
				"question_driven", "workshop",
			),
			"assessment": EnumSchema(
				"quiz_per_lesson", "module_end_only", "pre_post", "continuous_embedded",
				"diagnostic_entry", "none", "portfolio", "peer_review",
			),
			"content_mix": EnumSchema(
				"explanation_heavy", "activity_heavy", "balanced", "example_rich",
				"visual_rich", "discussion_rich", "reading_heavy", "multimedia_mix",
			),
			"rationale": map[string]any{"type": "string"},
		},
		"required":             []string{"module_index", "sequencing", "pedagogy", "assessment", "content_mix"},
		"additionalProperties": false,
	}

	lesson := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"lesson_index": IntSchema(),
			"opening": EnumSchema(
				"hook_question", "hook_problem", "hook_story", "hook_surprise", "hook_relevance",
				"hook_challenge", "objectives_first", "recap_prior", "diagnostic_check",
				"advance_organizer", "direct_start", "tldr_first", "context_setting", "misconception_address",
			),
			"core": EnumSchema(
				"direct_instruction", "worked_example", "faded_example", "multiple_examples", "non_example",
				"example_non_example_pairs", "analogy_based", "metaphor_extended", "compare_contrast", "cause_effect",
				"process_steps", "classification", "definition_elaboration", "rule_then_apply", "cases_then_rule",
				"principle_illustration", "concept_attainment", "narrative_embed", "dialogue_format",
				"socratic_questioning", "discovery_guided", "simulation_walkthrough", "demonstration",
				"explanation_then_demo", "demo_then_explanation", "chunked_progressive", "layered_depth",
				"problem_solution_reveal", "debate_format", "q_and_a_format", "interview_format",
			),
			"example": EnumSchema(
				"single_canonical", "multiple_varied", "progression", "edge_cases", "real_world",
				"abstract_formal", "relatable_everyday", "domain_specific", "counterexample",
				"minimal_pairs", "annotated",
			),
			"visual": EnumSchema(
				"text_only", "diagram_supported", "diagram_primary", "dual_coded", "sequential_visual",
				"before_after", "comparison_visual", "infographic", "flowchart", "concept_map", "timeline",
				"table_matrix", "annotated_image", "animation_described",
			),
			"practice": EnumSchema(
				"immediate", "delayed_end", "interleaved_throughout", "scaffolded", "faded_support",
				"massed", "varied", "retrieval", "application", "generation", "error_analysis",
				"self_explanation", "teach_back", "prediction", "comparison", "reflection", "none",
			),
			"closing": EnumSchema(
				"summary", "single_takeaway", "connection_forward", "connection_backward", "connection_lateral",
				"reflection_prompt", "application_prompt", "check_understanding", "open_question",
				"call_to_action", "cliff_hanger", "consolidation", "none",
			),
			"depth": EnumSchema(
				"eli5", "concise", "standard", "thorough", "exhaustive", "layered", "adaptive",
			),
			"engagement": EnumSchema(
				"passive", "active_embedded", "active_end", "gamified", "challenge_framed",
				"curiosity_driven", "choice_driven", "personalized_reference", "social_framed",
				"timed", "untimed",
			),
			"rationale": map[string]any{"type": "string"},
		},
		"required":             []string{"lesson_index", "opening", "core", "example", "visual", "practice", "closing", "depth", "engagement"},
		"additionalProperties": false,
	}

	return SchemaVersionedObject(1, map[string]any{
		"path":    path,
		"modules": map[string]any{"type": "array", "items": module},
		"lessons": map[string]any{"type": "array", "items": lesson},
	}, []string{"path", "modules", "lessons"})
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
	return SchemaVersionedObject(2, map[string]any{
		"coverage": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"uncovered_concept_keys":       StringArraySchema(),
				"duplicate_concepts_suspected": StringArraySchema(),
			},
			"required":             []string{"uncovered_concept_keys", "duplicate_concepts_suspected"},
			"additionalProperties": false,
		},
		"curriculum": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"uncovered_section_keys": StringArraySchema(),
				"notes":                  StringArraySchema(),
			},
			"required":             []string{"uncovered_section_keys", "notes"},
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
	}, []string{"coverage", "curriculum", "coherence", "recommended_fixes"})
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
