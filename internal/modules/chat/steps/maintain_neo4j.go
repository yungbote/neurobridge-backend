package steps

import (
	"context"

	"github.com/google/uuid"

	graphstore "github.com/yungbote/neurobridge-backend/internal/data/graph"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func syncChatThreadToNeo4j(ctx context.Context, deps MaintainDeps, thread *types.ChatThread) error {
	if deps.Graph == nil || deps.Graph.Driver == nil {
		return nil
	}
	if deps.DB == nil || thread == nil || thread.ID == uuid.Nil || thread.UserID == uuid.Nil {
		return nil
	}

	q := deps.DB.WithContext(ctx)
	if q == nil {
		q = deps.DB
	}
	if q == nil {
		return nil
	}

	var entities []*types.ChatEntity
	_ = q.Model(&types.ChatEntity{}).
		Where("user_id = ? AND scope = ? AND thread_id = ?", thread.UserID, ScopeThread, thread.ID).
		Find(&entities).Error

	var edges []*types.ChatEdge
	_ = q.Model(&types.ChatEdge{}).
		Where("user_id = ? AND scope = ? AND scope_id = ?", thread.UserID, ScopeThread, thread.ID).
		Find(&edges).Error

	var claims []*types.ChatClaim
	_ = q.Model(&types.ChatClaim{}).
		Where("user_id = ? AND scope = ? AND thread_id = ?", thread.UserID, ScopeThread, thread.ID).
		Find(&claims).Error

	return graphstore.UpsertChatGraph(ctx, deps.Graph, deps.Log, thread, entities, edges, claims)
}
