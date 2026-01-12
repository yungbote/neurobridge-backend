package prompts

// register_all.go
//
// Full overwrite. This registers every prompt in the registry using RegisterSpec(Spec{...}).
// Assumes you already have:
// - names.go with Prompt* constants
// - schemas_products.go with *Schema() funcs
// - schemas_common.go with helpers
// - spec.go, registry.go, input.go, validators.go

func RegisterAll() {
	// ---------- Library + Concepts ----------

	RegisterSpec(Spec{
		Name:       PromptMaterialSetSummary,
		Version:    1,
		SchemaName: "material_set_summary",
		Schema:     MaterialSetSummarySchema,
		System: `
You are building a durable library index for uploaded learning materials.
You must produce a concise, high-signal summary and lightweight classification fields.
Do not invent topics not grounded in the excerpt.
Return JSON only.`,
		User: `
Materials excerpt (each line may include chunk_id):
{{.BundleExcerpt}}

Output rules:
- summary_md: 6-18 sentence markdown summary.
- tags: 8-18 single-word lowercase tags (letters/numbers only).
- concept_keys: 12-40 stable snake_case keys.
- subject: short subject string.
- level: intro|intermediate|advanced.
- warnings: e.g. low_text_signal, heavily_visual, noisy_ocr.`,
		Validators: []Validator{
			RequireNonEmpty("BundleExcerpt", func(in Input) string { return in.BundleExcerpt }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptConceptInventory,
		Version:    1, // schema version is inside ConceptInventorySchema() (currently const 3)
		SchemaName: "concept_inventory",
		Schema:     ConceptInventorySchema,
		System: `
You are constructing an exhaustive concept inventory that will drive a personalized learning path.
Every concept must be grounded in the excerpts with citations (chunk_id strings).
Concept keys must be stable snake_case.
Return JSON only.`,
		User: `
PATH_INTENT_MD (optional; user goal context for relevance/noise filtering):
{{.PathIntentMD}}

EXCERPTS (each line includes chunk_id):
{{.Excerpts}}

Task:
- Extract ALL distinct concepts present in excerpts, but prioritize those that support the PATH_INTENT_MD.
- If PATH_INTENT_MD implies deprioritized topics, include them only if they are prerequisite scaffolding.
- Organize into hierarchy via parent_key + depth.
- Provide summary + key_points + aliases + importance.
- citations must be chunk_id strings actually used.
- coverage: estimate completeness and list suspected missing topics.`,
		Validators: []Validator{
			RequireNonEmpty("Excerpts", func(in Input) string { return in.Excerpts }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptConceptInventoryDelta,
		Version:    1,
		SchemaName: "concept_inventory_delta",
		Schema:     ConceptInventoryDeltaSchema,
		System: `
You are extending an existing concept inventory using additional excerpts from the same material set.
You must only add concepts that are truly missing from the existing inventory.
Every new concept must be grounded in the excerpts with citations (chunk_id strings).
Concept keys must be stable snake_case and must not collide with existing keys.
Return JSON only.`,
		User: `
PATH_INTENT_MD (optional; user goal context for relevance/noise filtering):
{{.PathIntentMD}}

EXISTING_CONCEPTS_JSON (do not repeat these; use these keys for parent_key when appropriate):
{{.ConceptsJSON}}

NEW_EXCERPTS (each line includes chunk_id):
{{.Excerpts}}

Task:
- Extract NEW distinct concepts present in NEW_EXCERPTS that are missing from EXISTING_CONCEPTS_JSON.
- Prefer missing high-signal concepts and prerequisite scaffolding; avoid exploding into micro-topics.
- Organize into hierarchy via parent_key + depth (parent_key should reference an existing key when possible; otherwise null).
- Provide summary + key_points + aliases + importance.
- citations must be chunk_id strings actually used.
- coverage: estimate whether more passes are needed and list suspected missing topics.`,
		Validators: []Validator{
			RequireNonEmpty("ConceptsJSON", func(in Input) string { return in.ConceptsJSON }),
			RequireNonEmpty("Excerpts", func(in Input) string { return in.Excerpts }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptConceptEdges,
		Version:    1,
		SchemaName: "concept_edges",
		Schema:     ConceptEdgesSchema,
		System: `
You are building a concept graph for sequencing.
Edges must be supported by excerpts.
Avoid dense graphs; keep only meaningful edges.
Return JSON only.`,
		User: `
PATH_INTENT_MD (optional; user goal context for relevance/noise filtering):
{{.PathIntentMD}}

CONCEPTS_JSON:
{{.ConceptsJSON}}

EXCERPTS:
{{.Excerpts}}

Create edges between concept keys.
edge_type: prereq|related|analogy.
strength: 0..1.
citations: chunk_id strings you used.`,
		Validators: []Validator{
			RequireNonEmpty("ConceptsJSON", func(in Input) string { return in.ConceptsJSON }),
			RequireNonEmpty("Excerpts", func(in Input) string { return in.Excerpts }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptMaterialKGExtract,
		Version:    1,
		SchemaName: "material_kg_extract",
		Schema:     MaterialKGExtractSchema,
		System: `
You are extracting a grounded material knowledge graph for GraphRAG.
Only use information supported by the excerpts and cite evidence by chunk_id strings.
Do not invent entities or claims not grounded in the excerpts.
Return JSON only.`,
		User: `
PATH_INTENT_MD (optional; user goal context, for relevance/noise filtering):
{{.PathIntentMD}}

ALLOWED_CONCEPTS_JSON (use concept_keys only from this list; otherwise leave concept_keys empty):
{{.ConceptsJSON}}

EXCERPTS (each line includes chunk_id):
{{.Excerpts}}

Task:
- Output a deduplicated list of entities mentioned in the excerpts.
  - Each entity must have evidence_chunk_ids (1-6 chunk_id strings from the excerpts).
  - "type" can be freeform; prefer stable categories (person, org, tool, method, dataset, system, concept, variable, other).
- Output a list of atomic claims grounded in the excerpts.
  - Each claim must have evidence_chunk_ids (1-6 chunk_id strings from the excerpts).
  - entity_names should reference entities by their canonical name when possible; otherwise include the literal name from the excerpt.
  - concept_keys must be a subset of ALLOWED_CONCEPTS_JSON.concepts[].key (exact keys only).

Constraints:
- Prefer fewer, higher-signal entities/claims over exhaustive micro-fragments.
- Claims should be 1-2 sentences, specific, and useful for retrieval/explainability.`,
		Validators: []Validator{
			RequireNonEmpty("Excerpts", func(in Input) string { return in.Excerpts }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptConceptClusters,
		Version:    1,
		SchemaName: "concept_clusters",
		Schema:     ConceptClustersSchema,
		System: `
You are clustering concepts into higher-level families to transfer teaching priors.
Clusters must be meaningful and non-overlapping unless necessary.
Return JSON only.`,
		User: `
CONCEPTS_JSON:
{{.ConceptsJSON}}

Task:
Return 6-18 clusters with:
- label
- concept_keys
- tags
- rationale`,
		Validators: []Validator{
			RequireNonEmpty("ConceptsJSON", func(in Input) string { return in.ConceptsJSON }),
		},
	})

	// ---------- Personalization ----------

	RegisterSpec(Spec{
		Name:       PromptUserProfileDoc,
		Version:    1,
		SchemaName: "user_profile_doc",
		Schema:     UserProfileDocSchema,
		System: `
You generate a compact user teaching profile for personalization.
Use only provided facts; do not invent demographic attributes.
Return JSON only.`,
		User: `
USER_FACTS_JSON:
{{.UserFactsJSON}}

RECENT_EVENTS_SUMMARY:
{{.RecentEventsSummary}}

Task:
Write profile_doc (120-260 words) describing how to teach this user.
Also output style_preferences and warnings.

Guidance:
- USER_FACTS_JSON may include personalization_prefs (explicit user-controlled learning preferences). Treat those as ground truth.
- If learningDisabilities are present, focus on practical accommodations (formatting, pacing, practice). Avoid medical claims or labels.`,
		Validators: []Validator{
			RequireNonEmpty("UserFactsJSON", func(in Input) string { return in.UserFactsJSON }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptPopulationCohortFeatures,
		Version:    1,
		SchemaName: "population_cohort_features",
		Schema:     PopulationCohortFeaturesSchema,
		System: `
You assign a user to cohort segments for population-level priors.
Do not invent sensitive demographic attributes; use behavior/learning preferences only.
Return JSON only.`,
		User: `
USER_PROFILE_DOC:
{{.UserProfileDoc}}

RECENT_EVENTS_SUMMARY:
{{.RecentEventsSummary}}

Task:
Return 1-6 cohort keys with weight, confidence, and rationale.`,
		Validators: []Validator{
			RequireNonEmpty("UserProfileDoc", func(in Input) string { return in.UserProfileDoc }),
		},
	})

	// ---------- Library taxonomy (path organization) ----------

	RegisterSpec(Spec{
		Name:       PromptLibraryTaxonomyRoute,
		Version:    1,
		SchemaName: "library_taxonomy_route",
		Schema:     LibraryTaxonomyRouteSchema,
		System: `
You are organizing a user's learning paths into a multi-facet library taxonomy DAG.
You must keep the taxonomy clean, stable, and design-forward.
Prefer using existing nodes; never invent fine structure prematurely.
Return JSON only.`,
		User: `
TAXONOMY_FACET:
{{.TaxonomyFacet}}

CURRENT_TAXONOMY_CANDIDATES_JSON (existing nodes + edges, already pruned by the backend; do not assume other nodes exist):
{{.TaxonomyCandidatesJSON}}

PATH_SUMMARY_JSON:
{{.TaxonomyPathSummaryJSON}}

CONSTRAINTS_JSON:
{{.TaxonomyConstraintsJSON}}

Task:
- Output memberships: choose up to max_memberships existing node_id(s) for this path with weight in [0,1].
- Never assign the path directly to root_node_id.
- If max_new_nodes is 0 OR disallow_new_nodes is true, output new_nodes as an empty array.
- If require_seeded_anchor is true, you MUST include at least one membership to a node where kind == "anchor".
  - Prefer 1 anchor; choose 2 only if the path truly spans multiple domains.
  - You may additionally assign to existing non-anchor categories if they are clearly relevant.
- If new_nodes are allowed, each new node should be a single clean concept (no collage).
- New node parent_node_ids must reference existing node_id(s) in candidates (or the provided root_node_id).
- Keep names short (2-5 words), human, not "AI-y". No emojis. No quotes.
- Avoid near-duplicates of existing names; if a duplicate exists, use it instead.`,
		Validators: []Validator{
			RequireNonEmpty("TaxonomyFacet", func(in Input) string { return in.TaxonomyFacet }),
			RequireNonEmpty("TaxonomyCandidatesJSON", func(in Input) string { return in.TaxonomyCandidatesJSON }),
			RequireNonEmpty("TaxonomyPathSummaryJSON", func(in Input) string { return in.TaxonomyPathSummaryJSON }),
			RequireNonEmpty("TaxonomyConstraintsJSON", func(in Input) string { return in.TaxonomyConstraintsJSON }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptLibraryTaxonomyRefine,
		Version:    1,
		SchemaName: "library_taxonomy_refine",
		Schema:     LibraryTaxonomyRefineSchema,
		System: `
You help refine and stabilize a user's library taxonomy.
You are given proposed new taxonomy nodes (already derived from embeddings/heuristics).
Decide which ones are meaningful and provide polished names and descriptions.
Return JSON only.`,
		User: `
TAXONOMY_FACET:
{{.TaxonomyFacet}}

PROPOSED_NEW_NODES_JSON:
{{.TaxonomyCandidatesJSON}}

CONSTRAINTS_JSON:
{{.TaxonomyConstraintsJSON}}

Task:
For each proposed node:
- should_create: true only if it adds clear value (non-duplicate, coherent abstraction).
- Parent context is provided (parent_node_name/key); names should fit naturally under that parent.
- Provide name + description that are concise, clean, and human.
- If should_create is false, explain briefly in reason.`,
		Validators: []Validator{
			RequireNonEmpty("TaxonomyFacet", func(in Input) string { return in.TaxonomyFacet }),
			RequireNonEmpty("TaxonomyCandidatesJSON", func(in Input) string { return in.TaxonomyCandidatesJSON }),
			RequireNonEmpty("TaxonomyConstraintsJSON", func(in Input) string { return in.TaxonomyConstraintsJSON }),
		},
	})

	// ---------- Coherence + Planning ----------

	RegisterSpec(Spec{
		Name:       PromptPathCharter,
		Version:    1,
		SchemaName: "path_charter",
		Schema:     PathCharterSchema,
		System: `
You are establishing global coherence constraints for a personalized learning path.
The charter must keep terminology and diagram conventions consistent.
Return JSON only.`,
		User: `
USER_PROFILE_DOC:
{{.UserProfileDoc}}

MATERIAL_SET_SUMMARY (optional):
{{.BundleExcerpt}}

Task:
Output path_style with:
- tone, reading_level, verbosity, pace, analogy_style
- terminology_policy (must_use_terms, avoid_terms)
- diagram_conventions (preferred_formats, labeling, density)`,
		Validators: []Validator{
			RequireNonEmpty("UserProfileDoc", func(in Input) string { return in.UserProfileDoc }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptPathStructure,
		Version:    3,
		SchemaName: "path_structure",
		Schema:     PathStructureSchema,
		System: `
You design the path structure (nodes and activity slots) to cover all concepts coherently.
Respect prerequisite edges when ordering.
If a curriculum spec is provided, it is the source of truth for macro-coverage and sequencing intent.
Return JSON only.`,
		User: `
PATH_CHARTER_JSON:
{{.PathCharterJSON}}

MATERIAL_SET_SUMMARY_MD (optional):
{{.BundleExcerpt}}

CURRICULUM_SPEC_JSON (optional):
{{.CurriculumSpecJSON}}

CONCEPTS_JSON:
{{.ConceptsJSON}}

EDGES_JSON:
{{.EdgesJSON}}

Task:
Create a dynamic path outline that covers all concepts.

Guidance:
- If CURRICULUM_SPEC_JSON is present and coverage_target is "mastery", start with fundamentals and core semantics before specialized tooling.
- Use CURRICULUM_SPEC_JSON sections to decide high-level module ordering and ensure no major area is omitted.

You may include hierarchy:
- "module" nodes are grouping/overview nodes.
- "lesson" nodes are the main teaching units (usually children of a module).
- Optional: "review" nodes for spaced repetition, and a "capstone" node for integration.

Rules:
- Indices must be unique positive integers (start at 1, increase by 1).
- Use parent_index to nest nodes. For top-level nodes, parent_index must be null.
- Parents must come before children (parent_index < index). Avoid cycles. Keep depth <= 3.
- Every concept in CONCEPTS_JSON should appear in at least one node.concept_keys (prefer lesson/capstone nodes).

Each node must include:
- node_kind: module | lesson | review | capstone
- doc_template: overview | concept | practice | cheatsheet | project | review
- title, goal, concept_keys, prereq_concept_keys, difficulty
- activity_slots (reading/quiz/drill/case). For modules you can keep this empty if not needed.

Include coverage_check.uncovered_concept_keys.`,
		Validators: []Validator{
			RequireNonEmpty("PathCharterJSON", func(in Input) string { return in.PathCharterJSON }),
			RequireNonEmpty("ConceptsJSON", func(in Input) string { return in.ConceptsJSON }),
			RequireNonEmpty("EdgesJSON", func(in Input) string { return in.EdgesJSON }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptNodeRepresentationPlan,
		Version:    1, // schema version is inside NodeRepresentationPlanSchema() (currently const 2)
		SchemaName: "node_representation_plan",
		Schema:     NodeRepresentationPlanSchema,
		System: `
You decide the default representation envelope for a single path node.
It must stay coherent with the path charter and the user profile.
Include a study_cycle and a novelty policy (avoid dwelling on already-mastered similar content).
Return JSON only.`,
		User: `
PATH_CHARTER_JSON:
{{.PathCharterJSON}}

USER_PROFILE_DOC:
{{.UserProfileDoc}}

NODE_INDEX: {{.NodeIndex}}
NODE_TITLE: {{.NodeTitle}}
NODE_GOAL: {{.NodeGoal}}
NODE_CONCEPT_KEYS: {{.NodeConceptKeysCSV}}

COHORT_HINTS (optional):
{{.CohortHints}}

Task:
Return:
- default_representation (primary/secondary modality, variant, text_style, diagram_style, video_style, study_cycle)
- novelty_policy:
  - avoid_dwelling_when_similarity_gte
  - use_diagnostic_when_similarity_gte
  - target_mastery_threshold`,
		Validators: []Validator{
			RequireNonEmpty("PathCharterJSON", func(in Input) string { return in.PathCharterJSON }),
			RequireNonEmpty("UserProfileDoc", func(in Input) string { return in.UserProfileDoc }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptChainRepresentationCandidates,
		Version:    1, // schema version is inside ChainRepresentationCandidatesSchema() (currently const 2)
		SchemaName: "chain_representation_candidate",
		Schema:     ChainRepresentationCandidatesSchema,
		System: `
You propose chain-level representation candidates that may override node defaults.
Include study_cycle and novelty considerations, and suggest reusable teaching patterns when appropriate.
Return JSON only.`,
		User: `
PATH_CHARTER_JSON:
{{.PathCharterJSON}}

USER_PROFILE_DOC:
{{.UserProfileDoc}}

NODE_INDEX: {{.NodeIndex}}
NODE_CONCEPT_KEYS: {{.NodeConceptKeysCSV}}

CHAINS (JSON list of {chain_key, concept_keys, edges(optional)}):
{{.TargetsJSON}}

RECENT_EVENTS_SUMMARY (optional):
{{.RecentEventsSummary}}

COHORT_HINTS (optional):
{{.CohortHints}}

Task:
Return candidates[] with:
- chain_key, concept_keys
- representation (includes study_cycle)
- learning_value (why, priority 1..5, novelty_hint 0..1)
- pattern_suggestion (pattern_key, pattern_query)
- signals (user_signals_used, population_signals_used, coherence_risk, cost_risk)
- notes`,
		Validators: []Validator{
			RequireNonEmpty("PathCharterJSON", func(in Input) string { return in.PathCharterJSON }),
			RequireNonEmpty("UserProfileDoc", func(in Input) string { return in.UserProfileDoc }),
			RequireNonEmpty("TargetsJSON", func(in Input) string { return in.TargetsJSON }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptOverrideResolution,
		Version:    1,
		SchemaName: "override_resolution",
		Schema:     OverrideResolutionSchema,
		System: `
You explain override decisions that were made by a deterministic policy.
Do not invent inputs; use only the provided JSON.
Return JSON only.`,
		User: `
DECISION_INPUTS_JSON:
{{.InputsJSON}}

CANDIDATES_JSON:
{{.CandidatesJSON}}

CHOSEN_JSON:
{{.ChosenJSON}}

Task:
For each chain_key, explain whether node or chain was chosen, and list risks/mitigations.`,
		Validators: []Validator{
			RequireNonEmpty("InputsJSON", func(in Input) string { return in.InputsJSON }),
			RequireNonEmpty("CandidatesJSON", func(in Input) string { return in.CandidatesJSON }),
			RequireNonEmpty("ChosenJSON", func(in Input) string { return in.ChosenJSON }),
		},
	})

	// ---------- Retrieval + Reuse ----------

	RegisterSpec(Spec{
		Name:       PromptRetrievalSpec,
		Version:    1, // schema version is inside RetrievalSpecSchema() (currently const 2)
		SchemaName: "retrieval_spec",
		Schema:     RetrievalSpecSchema,
		System: `
You generate retrieval specifications for reuse-vs-generate decisions.
Your output must be implementable by code: query_text, namespaces, filters, top_k, reuse_threshold.
Include teaching_patterns, chains, user_library, population_library when relevant.
Return JSON only.`,
		User: `
USER_PROFILE_DOC:
{{.UserProfileDoc}}

TARGETS (JSON list of {node_index, slot, chain_key, concept_keys, desired_modality, desired_variant}):
{{.TargetsJSON}}

PATH_CHARTER_JSON (optional):
{{.PathCharterJSON}}

Task:
Return queries[] for each target.
Use namespaces that can include: teaching_patterns, chains, user_library, population_library.
Filters should include concept_keys + modality + variant + representation_key (if you have one).`,
		Validators: []Validator{
			RequireNonEmpty("UserProfileDoc", func(in Input) string { return in.UserProfileDoc }),
			RequireNonEmpty("TargetsJSON", func(in Input) string { return in.TargetsJSON }),
		},
	})

	// ---------- Teaching patterns + Diagnostic gates ----------

	RegisterSpec(Spec{
		Name:       PromptTeachingPatterns,
		Version:    1,
		SchemaName: "teaching_patterns",
		Schema:     TeachingPatternsSchema,
		System: `
You author reusable teaching patterns that can be reused across similar chains and users.
Patterns must specify representation (including study_cycle) and constraints.
Return JSON only.`,
		User: `
USER_PROFILE_DOC:
{{.UserProfileDoc}}

CONCEPT_CLUSTERS_JSON (optional):
{{.ConceptsJSON}}

CHAINS_JSON (optional):
{{.TargetsJSON}}

Task:
Propose 5-20 patterns.
pattern_key must be stable snake_case.
Use representation + study_cycle that generalizes to a family of chains.`,
		Validators: []Validator{
			RequireNonEmpty("UserProfileDoc", func(in Input) string { return in.UserProfileDoc }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptDiagnosticGate,
		Version:    1,
		SchemaName: "diagnostic_gate",
		Schema:     DiagnosticGateSchema,
		System: `
You generate a small diagnostic gate to verify mastery for a concept chain.
Questions must be answerable from excerpts; do not invent facts.
Return JSON only.`,
		User: `
CONCEPT_KEYS: {{.ConceptKeysCSV}}

EXCERPTS (chunk_id lines):
{{.ActivityExcerpts}}

Task:
Generate 2-5 MCQs to quickly verify mastery.
Include citations per question.`,
		Validators: []Validator{
			RequireNonEmpty("ConceptKeysCSV", func(in Input) string { return in.ConceptKeysCSV }),
			RequireNonEmpty("ActivityExcerpts", func(in Input) string { return in.ActivityExcerpts }),
		},
	})

	// ---------- Realization (generation) ----------

	RegisterSpec(Spec{
		Name:       PromptActivityContent,
		Version:    1,
		SchemaName: "activity_content",
		Schema:     ActivityContentSchema,
		System: `
You generate canonical learning activity content in block-based JSON.
Write like a great tutor: engaging, coherent, and concrete — not terse lecture notes.
You MUST ground all factual claims in the provided excerpts (chunk_id lines).
Do not invent facts or sources.
Return JSON only.`,
		User: `
USER_PROFILE_DOC:
{{.UserProfileDoc}}

PATH_CHARTER_JSON (optional):
{{.PathCharterJSON}}

TEACHING_PATTERNS_JSON (optional; pick 1-2 and apply them; do not mention pattern_key values):
{{.TeachingPatternsJSON}}

ACTIVITY_KIND: {{.ActivityKind}}
ACTIVITY_TITLE: {{.ActivityTitle}}
CONCEPT_KEYS: {{.ConceptKeysCSV}}

EXCERPTS (each line includes chunk_id):
{{.ActivityExcerpts}}

AVAILABLE_MEDIA_ASSETS_JSON (optional):
{{.AssetsJSON}}

Rules:
- Use blocks: heading|paragraph|bullets|steps|callout|divider|image|video_embed|diagram
- Target word counts (approx; err on the side of longer): lesson-like ~1000–1600 words; drill ~450–800 words; quiz ~250–450 words.
- For lesson-like activities, aim for a narrative arc: why it matters → intuition/mental model → explanation → worked example → guided practice → recap.
- For drills, include: a clear prompt, guided steps, and at least one "hint ladder" style callout to support retries.
- For quizzes, include brief explanations for answers (why correct / why others are wrong) grounded in excerpts.
- Prefer paragraphs + callouts over wall-of-bullets. Bullets/steps should support, not replace, explanation.
- Include at least one worked example and at least one quick self-check prompt.
- Include 1-2 common misconceptions/common mistakes when it fits the concept and helps the learner avoid errors.
- If you use image or video_embed blocks, asset_refs MUST be a URL from AVAILABLE_MEDIA_ASSETS_JSON. Do not invent URLs.
- If AVAILABLE_MEDIA_ASSETS_JSON is empty, do not include image/video_embed blocks.
- If AVAILABLE_MEDIA_ASSETS_JSON includes images/videos, prefer including 1-2 relevant image blocks and at most 1 relevant video_embed block.
- citations must be chunk_id strings actually used.`,
		Validators: []Validator{
			RequireNonEmpty("UserProfileDoc", func(in Input) string { return in.UserProfileDoc }),
			RequireNonEmpty("ActivityKind", func(in Input) string { return in.ActivityKind }),
			RequireNonEmpty("ActivityTitle", func(in Input) string { return in.ActivityTitle }),
			RequireNonEmpty("ConceptKeysCSV", func(in Input) string { return in.ConceptKeysCSV }),
			RequireNonEmpty("ActivityExcerpts", func(in Input) string { return in.ActivityExcerpts }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptActivityVariant,
		Version:    1,
		SchemaName: "activity_variant",
		Schema:     ActivityVariantSchema,
		System: `
You produce a presentation variant of an existing activity.
You must preserve factual correctness and must not introduce new facts beyond the canonical activity.
Return JSON only.`,
		User: `
USER_PROFILE_DOC:
{{.UserProfileDoc}}

PATH_CHARTER_JSON (optional):
{{.PathCharterJSON}}

COHORT_HINTS (optional):
{{.CohortHints}}

VARIANT: {{.Variant}}

CANONICAL_ACTIVITY_JSON:
{{.CanonicalActivityJSON}}

Task:
Produce content_json blocks for this variant plus a render_spec describing diagram requests and layout hints.`,
		Validators: []Validator{
			RequireNonEmpty("UserProfileDoc", func(in Input) string { return in.UserProfileDoc }),
			RequireNonEmpty("Variant", func(in Input) string { return in.Variant }),
			RequireNonEmpty("CanonicalActivityJSON", func(in Input) string { return in.CanonicalActivityJSON }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptDiagramSpec,
		Version:    1,
		SchemaName: "diagram_spec",
		Schema:     DiagramSpecSchema,
		System: `
You produce a machine-renderable diagram spec (mermaid/dot/json) that supports a learning activity.
Keep it faithful to the grounded content; do not invent facts.
Return JSON only.`,
		User: `
DIAGRAM_REQUEST (free text):
{{.CohortHints}}

CONCEPT_KEYS: {{.ConceptKeysCSV}}

GROUNDING EXCERPTS (chunk_id lines):
{{.ActivityExcerpts}}

Task:
Return diagram_type, format, spec, alt_text, citations.`,
		Validators: []Validator{
			RequireNonEmpty("ConceptKeysCSV", func(in Input) string { return in.ConceptKeysCSV }),
			RequireNonEmpty("ActivityExcerpts", func(in Input) string { return in.ActivityExcerpts }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptVideoStoryboard,
		Version:    1,
		SchemaName: "video_storyboard",
		Schema:     VideoStoryboardSchema,
		System: `
You produce a video storyboard and script for a micro-lecture.
Do not invent facts; ground claims in excerpts.
Return JSON only.`,
		User: `
USER_PROFILE_DOC:
{{.UserProfileDoc}}

CONCEPT_KEYS: {{.ConceptKeysCSV}}

GROUNDING EXCERPTS (chunk_id lines):
{{.ActivityExcerpts}}

Task:
Return video_kind, length_minutes, script_md, beats, citations.`,
		Validators: []Validator{
			RequireNonEmpty("UserProfileDoc", func(in Input) string { return in.UserProfileDoc }),
			RequireNonEmpty("ConceptKeysCSV", func(in Input) string { return in.ConceptKeysCSV }),
			RequireNonEmpty("ActivityExcerpts", func(in Input) string { return in.ActivityExcerpts }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptQuizFromActivity,
		Version:    1,
		SchemaName: "quiz_from_activity",
		Schema:     QuizFromActivitySchema,
		System: `
You generate fair assessment questions grounded in the provided activity content.
Do not introduce new facts.
Return JSON only.`,
		User: `
ACTIVITY_CONTENT_MD:
{{.ActivityContentMD}}

KNOWN_CITATIONS (chunk_id strings, optional):
{{.CitationsCSV}}

Task:
Generate 4-12 MCQ questions with citations per question.`,
		Validators: []Validator{
			RequireNonEmpty("ActivityContentMD", func(in Input) string { return in.ActivityContentMD }),
		},
	})

	// ---------- Audits ----------

	RegisterSpec(Spec{
		Name:       PromptCoverageAndCoheranceAudit,
		Version:    2,
		SchemaName: "coverage_and_coherance_audit",
		Schema:     CoverageAndCoherenceAuditSchema,
		System: `
You audit a generated path for coverage and coherence.
Use only the provided JSON snapshots; do not invent missing facts.
If a curriculum spec is provided, audit for curriculum coverage as well.
Return JSON only.`,
		User: `
CURRICULUM_SPEC_JSON (optional):
{{.CurriculumSpecJSON}}

CONCEPTS_JSON:
{{.ConceptsJSON}}

PATH_NODES_JSON:
{{.PathNodesJSON}}

NODE_PLANS_JSON:
{{.NodePlansJSON}}

CHAIN_PLANS_JSON:
{{.ChainPlansJSON}}

ACTIVITIES_JSON:
{{.ActivitiesJSON}}

VARIANTS_JSON:
{{.VariantsJSON}}

Task:
Return uncovered concepts, uncovered curriculum sections (if spec provided), terminology conflicts, style inconsistencies, sequence issues, and recommended fixes.`,
		Validators: []Validator{
			RequireNonEmpty("ConceptsJSON", func(in Input) string { return in.ConceptsJSON }),
			RequireNonEmpty("PathNodesJSON", func(in Input) string { return in.PathNodesJSON }),
		},
	})

	RegisterSpec(Spec{
		Name:       PromptDecisionTraceExplanation,
		Version:    1,
		SchemaName: "decision_trace_explanation",
		Schema:     DecisionTraceExplanationSchema,
		System: `
You produce a concise explanation for a deterministic decision trace.
Use only the provided JSON; do not invent signals.
Return JSON only.`,
		User: `
DECISION_TYPE: {{.DecisionType}}

INPUTS_JSON:
{{.InputsJSON}}

CANDIDATES_JSON:
{{.CandidatesJSON}}

CHOSEN_JSON:
{{.ChosenJSON}}

Task:
Explain why the chosen option won, plus risks and mitigations.`,
		Validators: []Validator{
			RequireNonEmpty("DecisionType", func(in Input) string { return in.DecisionType }),
			RequireNonEmpty("InputsJSON", func(in Input) string { return in.InputsJSON }),
			RequireNonEmpty("ChosenJSON", func(in Input) string { return in.ChosenJSON }),
		},
	})
}
