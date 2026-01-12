package steps

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	chatIndex "github.com/yungbote/neurobridge-backend/internal/modules/chat/index"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
)

type RebuildDeps struct {
	DB  *gorm.DB
	Log *logger.Logger
	Vec pc.VectorStore
}

type RebuildInput struct {
	UserID   uuid.UUID
	ThreadID uuid.UUID
}

// RebuildThreadProjections deletes all derived artifacts for the thread (docs/summaries/graph/memory extracts)
// and clears thread state so maintenance can rebuild from canonical SQL messages.
func RebuildThreadProjections(ctx context.Context, deps RebuildDeps, in RebuildInput) error {
	if deps.DB == nil || deps.Log == nil {
		return fmt.Errorf("chat rebuild: missing deps")
	}
	if in.UserID == uuid.Nil || in.ThreadID == uuid.Nil {
		return fmt.Errorf("chat rebuild: missing ids")
	}

	// Verify ownership (do not leak existence).
	var thread types.ChatThread
	if err := deps.DB.WithContext(ctx).
		Model(&types.ChatThread{}).
		Where("id = ? AND user_id = ?", in.ThreadID, in.UserID).
		First(&thread).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("thread not found")
		}
		return err
	}

	// Collect vector IDs before deleting the SQL projection.
	var vectorIDs []string
	_ = deps.DB.WithContext(ctx).
		Model(&types.ChatDoc{}).
		Where("user_id = ? AND thread_id = ?", in.UserID, in.ThreadID).
		Pluck("vector_id", &vectorIDs).Error

	// Delete derived SQL artifacts (hard deletes; they are rebuildable).
	if err := deps.DB.WithContext(ctx).Where("user_id = ? AND thread_id = ?", in.UserID, in.ThreadID).Delete(&types.ChatDoc{}).Error; err != nil {
		return err
	}
	if err := deps.DB.WithContext(ctx).Where("thread_id = ?", in.ThreadID).Delete(&types.ChatSummaryNode{}).Error; err != nil {
		return err
	}
	if err := deps.DB.WithContext(ctx).Where("user_id = ? AND scope = ? AND scope_id = ?", in.UserID, ScopeThread, in.ThreadID).Delete(&types.ChatEntity{}).Error; err != nil {
		return err
	}
	if err := deps.DB.WithContext(ctx).Where("user_id = ? AND scope = ? AND scope_id = ?", in.UserID, ScopeThread, in.ThreadID).Delete(&types.ChatEdge{}).Error; err != nil {
		return err
	}
	if err := deps.DB.WithContext(ctx).Where("user_id = ? AND scope = ? AND scope_id = ?", in.UserID, ScopeThread, in.ThreadID).Delete(&types.ChatClaim{}).Error; err != nil {
		return err
	}
	// Thread-scoped memory is derived from this thread; clear it so it can be re-extracted.
	_ = deps.DB.WithContext(ctx).Where("user_id = ? AND scope = ? AND scope_id = ?", in.UserID, ScopeThread, in.ThreadID).Delete(&types.ChatMemoryItem{}).Error

	// Clear thread state to reset cursors and (optionally) conversation ID.
	_ = deps.DB.WithContext(ctx).Where("thread_id = ?", in.ThreadID).Delete(&types.ChatThreadState{}).Error

	// Best-effort vector index deletion (cache).
	if deps.Vec != nil && len(vectorIDs) > 0 {
		delCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		_ = deps.Vec.DeleteIDs(delCtx, chatIndex.ChatUserNamespace(in.UserID), vectorIDs)
		cancel()
	}

	return nil
}

// PurgeThreadArtifacts removes all derived artifacts and turn traces for a thread. Intended to run after the
// canonical thread/messages are soft-deleted.
func PurgeThreadArtifacts(ctx context.Context, deps RebuildDeps, in RebuildInput) error {
	if deps.DB == nil || deps.Log == nil {
		return fmt.Errorf("chat purge: missing deps")
	}
	if in.UserID == uuid.Nil || in.ThreadID == uuid.Nil {
		return fmt.Errorf("chat purge: missing ids")
	}

	// Collect vector IDs (best-effort).
	var vectorIDs []string
	_ = deps.DB.WithContext(ctx).
		Model(&types.ChatDoc{}).
		Where("user_id = ? AND thread_id = ?", in.UserID, in.ThreadID).
		Pluck("vector_id", &vectorIDs).Error

	// Derived tables (hard deletes).
	_ = deps.DB.WithContext(ctx).Where("user_id = ? AND thread_id = ?", in.UserID, in.ThreadID).Delete(&types.ChatDoc{}).Error
	_ = deps.DB.WithContext(ctx).Where("thread_id = ?", in.ThreadID).Delete(&types.ChatSummaryNode{}).Error
	_ = deps.DB.WithContext(ctx).Where("user_id = ? AND scope = ? AND scope_id = ?", in.UserID, ScopeThread, in.ThreadID).Delete(&types.ChatEntity{}).Error
	_ = deps.DB.WithContext(ctx).Where("user_id = ? AND scope = ? AND scope_id = ?", in.UserID, ScopeThread, in.ThreadID).Delete(&types.ChatEdge{}).Error
	_ = deps.DB.WithContext(ctx).Where("user_id = ? AND scope = ? AND scope_id = ?", in.UserID, ScopeThread, in.ThreadID).Delete(&types.ChatClaim{}).Error
	_ = deps.DB.WithContext(ctx).Where("user_id = ? AND thread_id = ?", in.UserID, in.ThreadID).Delete(&types.ChatTurn{}).Error
	_ = deps.DB.WithContext(ctx).Where("thread_id = ?", in.ThreadID).Delete(&types.ChatThreadState{}).Error

	// Memory items that explicitly reference this thread.
	_ = deps.DB.WithContext(ctx).Where("user_id = ? AND thread_id = ?", in.UserID, in.ThreadID).Delete(&types.ChatMemoryItem{}).Error

	if deps.Vec != nil && len(vectorIDs) > 0 {
		delCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		_ = deps.Vec.DeleteIDs(delCtx, chatIndex.ChatUserNamespace(in.UserID), vectorIDs)
		cancel()
	}
	return nil
}
