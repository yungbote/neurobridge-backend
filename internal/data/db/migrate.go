package db

import (
	"fmt"
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

		// =========================
		// Course (legacy centerpiece)
		// =========================
		&types.Course{},
		&types.CourseTag{},
		&types.CourseModule{},
		&types.Lesson{},
		&types.QuizQuestion{},
		&types.CourseBlueprint{},

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
		&types.TeachingPattern{},

		// =========================
		// Legacy (keep for now)
		// =========================
		&types.LearningProfile{},
		&types.TopicMastery{},
		&types.TopicStylePreference{},
		&types.QuizAttempt{},

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

	return nil
}
