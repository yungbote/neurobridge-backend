package db

import (
	"fmt"
	"strings"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/gorm"
)

func AutoMigrateAll(db *gorm.DB) error {
	return db.AutoMigrate(

		// =========================
		// Core identity + auth
		// =========================
		&types.User{},
		&types.UserToken{},
		&types.UserSessionState{},
		&types.UserIdentity{},
		&types.OAuthNonce{},

		// =========================
		// Materials (uploads + extraction)
		// =========================
		&types.MaterialSet{},
		&types.MaterialFile{},
		&types.MaterialChunk{},
		&types.MaterialAsset{},
		&types.MaterialSetSummary{},
		&types.MaterialEntity{},
		&types.MaterialClaim{},
		&types.MaterialChunkEntity{},
		&types.MaterialChunkClaim{},
		&types.MaterialClaimEntity{},
		&types.MaterialClaimConcept{},

		// =========================
		// Graph-centric learning (new centerpiece: Path)
		// =========================
		// Concepts + Graph Products
		&types.Concept{},
		&types.ConceptEvidence{},
		&types.ConceptEdge{},
		&types.ConceptCluster{},
		&types.ConceptClusterMember{},
		// Library + Population Priors + Decision Traces
		&types.UserLibraryIndex{},
		&types.CohortPrior{},
		&types.DecisionTrace{},
		// Chain identity + Prios + Completion
		&types.ChainSignature{},
		&types.ChainPrior{},
		&types.UserCompletedUnit{},
		// Activities (+ variants + grounding)
		&types.Activity{},
		&types.ActivityVariant{},
		&types.ActivityVariantStat{},
		&types.ActivityConcept{},
		&types.ActivityCitation{},
		// Node docs + drills (canonical content + supplemental tools)
		&types.LearningNodeDoc{},
		&types.LearningNodeDocRevision{},
		&types.LearningNodeFigure{},
		&types.LearningNodeVideo{},
		&types.LearningDocGenerationRun{},
		&types.LearningDrillInstance{},
		// Library taxonomy (user-specific evolving DAG)
		&types.LibraryTaxonomyNode{},
		&types.LibraryTaxonomyEdge{},
		&types.LibraryTaxonomyMembership{},
		&types.LibraryTaxonomyState{},
		&types.LibraryTaxonomySnapshot{},
		&types.LibraryPathEmbedding{},
		// Path (the non-legacy top-level object)
		&types.Path{},
		&types.PathNode{},
		&types.PathNodeActivity{},
		// Assets (polymorphic ownership)
		&types.Asset{},

		// =========================
		// Personalization backbone
		// =========================
		&types.UserEvent{},
		&types.UserEventCursor{},
		&types.UserConceptState{},
		&types.UserStylePreference{},
		&types.UserProgressionEvent{},
		&types.UserProfileVector{},
		&types.UserPersonalizationPrefs{},
		&types.TeachingPattern{},

		// =========================
		// Legacy (keep for now)
		// =========================
		&types.LearningProfile{},
		&types.TopicMastery{},
		&types.TopicStylePreference{},

		// =========================
		// Jobs / worker
		// =========================
		&types.SagaRun{},
		&types.SagaAction{},
		&types.JobRun{},
		&types.JobRunEvent{},

		// =========================
		// Chat
		// =========================
		&types.ChatThread{},
		&types.ChatMessage{},
		&types.ChatThreadState{},
		&types.ChatSummaryNode{},
		&types.ChatMemoryItem{},
		&types.ChatEntity{},
		&types.ChatEdge{},
		&types.ChatClaim{},
		&types.ChatDoc{},
		&types.ChatTurn{},
	)
}

func EnsureAuthIndexes(db *gorm.DB) error {
	// uuid-ossp is already enabled in NewPostgresService, but safe to re-run
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`).Error; err != nil {
		return fmt.Errorf("enable uuid-ossp: %w", err)
	}
	// user_identity
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_user_identity_user_id ON user_identity(user_id);`).Error; err != nil {
		return fmt.Errorf("create idx_user_identity_user_id: %w", err)
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_user_identity_provider_sub
		ON user_identity(provider, provider_sub)
		WHERE deleted_at IS NULL;
	`).Error; err != nil {
		return fmt.Errorf("create idx_user_identity_provider_sub: %w", err)
	}
	// oauth_nonce
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_oauth_nonce_expires_at ON oauth_nonce(expires_at);`).Error; err != nil {
		return fmt.Errorf("create idx_oauth_nonce_expires_at: %w", err)
	}
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_oauth_nonce_provider_used ON oauth_nonce(provider, used_at);`).Error; err != nil {
		return fmt.Errorf("create idx_oauth_nonce_provider_used: %w", err)
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_nonce_provider_hash_active
		ON oauth_nonce(provider, nonce_hash)
		WHERE deleted_at IS NULL AND used_at IS NULL;
	`).Error; err != nil {
		return fmt.Errorf("create idx_oauth_nonce_provider_hash_active: %w", err)
	}
	return nil
}

func EnsureChatIndexes(db *gorm.DB) error {
	// Full-text search over contextual_text (lexical retrieval view).
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_chat_doc_fts
		ON chat_doc
		USING GIN (to_tsvector('english', contextual_text));
	`).Error; err != nil {
		return fmt.Errorf("create idx_chat_doc_fts: %w", err)
	}

	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_chat_doc_scope_type
		ON chat_doc (user_id, scope, scope_id, doc_type, created_at DESC);
	`).Error; err != nil {
		return fmt.Errorf("create idx_chat_doc_scope_type: %w", err)
	}

	// Fast message pagination per thread.
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_chat_message_thread_seq
		ON chat_message (thread_id, seq);
	`).Error; err != nil {
		return fmt.Errorf("create idx_chat_message_thread_seq: %w", err)
	}

	// SQL-only fallback retrieval: lexical search over canonical messages.
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_chat_message_fts
		ON chat_message
		USING GIN (to_tsvector('english', content))
		WHERE deleted_at IS NULL;
	`).Error; err != nil {
		return fmt.Errorf("create idx_chat_message_fts: %w", err)
	}

	// Dedupe client retries for user messages.
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_message_idempotency_user
		ON chat_message (thread_id, user_id, idempotency_key)
		WHERE deleted_at IS NULL AND role = 'user' AND idempotency_key <> '';
	`).Error; err != nil {
		return fmt.Errorf("create idx_chat_message_idempotency_user: %w", err)
	}

	// Fast thread listing per user.
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_chat_thread_user_status_last
		ON chat_thread (user_id, status, last_message_at DESC);
	`).Error; err != nil {
		return fmt.Errorf("create idx_chat_thread_user_status_last: %w", err)
	}

	// Graph helpers.
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_chat_entity_user_scope_name
		ON chat_entity (user_id, scope, scope_id, lower(name));
	`).Error; err != nil {
		return fmt.Errorf("create idx_chat_entity_user_scope_name: %w", err)
	}

	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_chat_edge_user_scope_src
		ON chat_edge (user_id, scope, scope_id, src_entity_id);
	`).Error; err != nil {
		return fmt.Errorf("create idx_chat_edge_user_scope_src: %w", err)
	}

	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_chat_edge_user_scope_dst
		ON chat_edge (user_id, scope, scope_id, dst_entity_id);
	`).Error; err != nil {
		return fmt.Errorf("create idx_chat_edge_user_scope_dst: %w", err)
	}

	// Memory helpers.
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_chat_memory_user_scope_kind
		ON chat_memory_item (user_id, scope, scope_id, kind);
	`).Error; err != nil {
		return fmt.Errorf("create idx_chat_memory_user_scope_kind: %w", err)
	}

	// Turn tracing.
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_turn_user_thread_user_message
		ON chat_turn (user_id, thread_id, user_message_id)
		WHERE deleted_at IS NULL;
	`).Error; err != nil {
		return fmt.Errorf("create idx_chat_turn_user_thread_user_message: %w", err)
	}

	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_chat_turn_thread_created_at
		ON chat_turn (thread_id, created_at DESC);
	`).Error; err != nil {
		return fmt.Errorf("create idx_chat_turn_thread_created_at: %w", err)
	}

	return nil
}

func EnsureLearningIndexes(db *gorm.DB) error {
	// Concepts: enforce uniqueness within a scope boundary (and ignore soft-deleted rows).
	//
	// Older installs may have an incorrect unique index that only covered `key`, which prevents
	// different paths from having overlapping concept keys.
	if err := db.Exec(`ALTER TABLE concept DROP CONSTRAINT IF EXISTS idx_concept_scope_key;`).Error; err != nil {
		return fmt.Errorf("drop concept constraint idx_concept_scope_key: %w", err)
	}
	if err := db.Exec(`DROP INDEX IF EXISTS idx_concept_scope_key;`).Error; err != nil {
		return fmt.Errorf("drop index idx_concept_scope_key: %w", err)
	}
	if err := db.Exec(`DROP INDEX IF EXISTS idx_concept_scope_key_null_scope_id;`).Error; err != nil {
		return fmt.Errorf("drop index idx_concept_scope_key_null_scope_id: %w", err)
	}

	// Treat NULL scope_id as a real scope boundary (mirror IS NOT DISTINCT FROM semantics used in repos).
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_concept_scope_key
		ON concept (scope, scope_id, key)
		WHERE deleted_at IS NULL AND scope_id IS NOT NULL;
	`).Error; err != nil {
		return fmt.Errorf("create idx_concept_scope_key: %w", err)
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_concept_scope_key_null_scope_id
		ON concept (scope, key)
		WHERE deleted_at IS NULL AND scope_id IS NULL;
	`).Error; err != nil {
		return fmt.Errorf("create idx_concept_scope_key_null_scope_id: %w", err)
	}

	// Material chunks: fast lexical retrieval for hybrid evidence selection.
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_material_chunk_fts
		ON material_chunk
		USING GIN (to_tsvector('english', text))
		WHERE deleted_at IS NULL;
	`).Error; err != nil {
		return fmt.Errorf("create idx_material_chunk_fts: %w", err)
	}

	// Node docs: canonical per node.
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_learning_node_doc_path_node_id
		ON learning_node_doc (path_node_id);
	`).Error; err != nil {
		return fmt.Errorf("create idx_learning_node_doc_path_node_id: %w", err)
	}
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_learning_node_doc_user_path_updated
		ON learning_node_doc (user_id, path_id, updated_at DESC);
	`).Error; err != nil {
		return fmt.Errorf("create idx_learning_node_doc_user_path_updated: %w", err)
	}

	// Node doc revisions: per-node history view.
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_learning_node_doc_revision_node_created
		ON learning_node_doc_revision (path_node_id, created_at DESC);
	`).Error; err != nil {
		return fmt.Errorf("create idx_learning_node_doc_revision_node_created: %w", err)
	}
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_learning_node_doc_revision_doc_created
		ON learning_node_doc_revision (doc_id, created_at DESC);
	`).Error; err != nil {
		return fmt.Errorf("create idx_learning_node_doc_revision_doc_created: %w", err)
	}

	// Drill cache: (user,node,kind,count,sources_hash) uniqueness.
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_learning_drill_instance_key
		ON learning_drill_instance (user_id, path_node_id, kind, count, sources_hash);
	`).Error; err != nil {
		return fmt.Errorf("create idx_learning_drill_instance_key: %w", err)
	}

	// Node figures: (user,node,slot) uniqueness.
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_learning_node_figure_key
		ON learning_node_figure (user_id, path_node_id, slot);
	`).Error; err != nil {
		return fmt.Errorf("create idx_learning_node_figure_key: %w", err)
	}
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_learning_node_figure_node_status_updated
		ON learning_node_figure (path_node_id, status, updated_at DESC);
	`).Error; err != nil {
		return fmt.Errorf("create idx_learning_node_figure_node_status_updated: %w", err)
	}

	// Generation runs: quick per-node debugging.
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_learning_doc_generation_run_node_created
		ON learning_doc_generation_run (path_node_id, created_at DESC);
	`).Error; err != nil {
		return fmt.Errorf("create idx_learning_doc_generation_run_node_created: %w", err)
	}

	// Library taxonomy nodes: ON CONFLICT ("user_id","facet","key") requires a matching unique index.
	//
	// Older installs may have an incorrect unique index that only covered `key`, which prevents
	// multi-user taxonomy and breaks upserts.
	type pgIndexDef struct {
		Indexdef string `gorm:"column:indexdef"`
	}
	var idx pgIndexDef
	if err := db.Raw(`
		SELECT indexdef
		FROM pg_indexes
		WHERE tablename = 'library_taxonomy_node' AND indexname = 'idx_library_taxonomy_node_user_facet_key'
		LIMIT 1;
	`).Scan(&idx).Error; err != nil {
		return fmt.Errorf("load idx_library_taxonomy_node_user_facet_key: %w", err)
	}
	if idx.Indexdef != "" {
		compact := strings.ReplaceAll(strings.ToLower(idx.Indexdef), " ", "")
		if !strings.Contains(compact, "(user_id,facet,key)") {
			if err := db.Exec(`ALTER TABLE library_taxonomy_node DROP CONSTRAINT IF EXISTS idx_library_taxonomy_node_user_facet_key;`).Error; err != nil {
				return fmt.Errorf("drop library_taxonomy_node constraint idx_library_taxonomy_node_user_facet_key: %w", err)
			}
			if err := db.Exec(`DROP INDEX IF EXISTS idx_library_taxonomy_node_user_facet_key;`).Error; err != nil {
				return fmt.Errorf("drop index idx_library_taxonomy_node_user_facet_key: %w", err)
			}
		}
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_library_taxonomy_node_user_facet_key
		ON library_taxonomy_node (user_id, facet, key);
	`).Error; err != nil {
		return fmt.Errorf("create idx_library_taxonomy_node_user_facet_key: %w", err)
	}

	return nil
}

func (s *PostgresService) AutoMigrateAll() error {
	s.log.Info("Auto migrating postgres tables...")
	if err := AutoMigrateAll(s.db); err != nil {
		s.log.Error("Auto migration failed", "error", err)
		return err
	}
	if err := EnsureAuthIndexes(s.db); err != nil {
		s.log.Error("Auth index migration failed", "error", err)
		return err
	}
	if err := EnsureChatIndexes(s.db); err != nil {
		s.log.Error("Chat index migration failed", "error", err)
		return err
	}
	if err := EnsureLearningIndexes(s.db); err != nil {
		s.log.Error("Learning index migration failed", "error", err)
		return err
	}

	return nil
}
