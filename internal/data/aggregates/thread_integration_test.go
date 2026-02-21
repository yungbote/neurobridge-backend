package aggregates

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	chatrepos "github.com/yungbote/neurobridge-backend/internal/data/repos/chat"
	repotest "github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func TestThreadAggregateMarkTurnFailedHappyPath(t *testing.T) {
	db := repotest.DB(t)
	tx := repotest.Tx(t, db)
	ensureThreadTables(t, tx)

	log := repotest.Logger(t)
	threads := chatrepos.NewChatThreadRepo(tx, log)
	messages := chatrepos.NewChatMessageRepo(tx, log)
	turns := chatrepos.NewChatTurnRepo(tx, log)

	agg := NewThreadAggregate(ThreadAggregateDeps{
		Base: BaseDeps{
			DB:       tx,
			Runner:   NewGormTxRunner(tx),
			CASGuard: NewCASGuard(tx),
		},
		Threads:  threads,
		Messages: messages,
		Turns:    turns,
	})

	ctx := context.Background()
	userID, threadID, turnID, asstID := seedThreadTurn(t, ctx, threads, messages, turns)
	at := time.Now().UTC()

	res, err := agg.MarkTurnFailed(ctx, domainagg.MarkTurnFailedInput{
		UserID:       userID,
		ThreadID:     threadID,
		TurnID:       turnID,
		FailureCode:  "llm_timeout",
		FailureCause: "context deadline exceeded",
		FailedAt:     at,
		Metadata: map[string]any{
			"attempt": 2,
		},
	})
	if err != nil {
		t.Fatalf("MarkTurnFailed: %v", err)
	}
	if res.TurnStatus != threadTurnStatusError {
		t.Fatalf("turn status: want=%q got=%q", threadTurnStatusError, res.TurnStatus)
	}

	turn, err := turns.GetByID(dbctx.Context{Ctx: ctx}, userID, turnID)
	if err != nil {
		t.Fatalf("GetByID turn: %v", err)
	}
	if turn == nil || turn.Status != threadTurnStatusError {
		t.Fatalf("turn status after mark failed: got=%v", turn)
	}
	if turn.CompletedAt == nil || turn.CompletedAt.IsZero() {
		t.Fatalf("turn completed_at should be set")
	}

	var trace map[string]any
	if err := json.Unmarshal(turn.RetrievalTrace, &trace); err != nil {
		t.Fatalf("unmarshal retrieval_trace: %v", err)
	}
	if got := trace["failure_code"]; got != "llm_timeout" {
		t.Fatalf("failure_code: want=llm_timeout got=%v", got)
	}

	var msg types.ChatMessage
	if err := tx.WithContext(ctx).Model(&types.ChatMessage{}).Where("id = ?", asstID).First(&msg).Error; err != nil {
		t.Fatalf("load assistant msg: %v", err)
	}
	if msg.Status != threadMessageStatusError {
		t.Fatalf("assistant status: want=%q got=%q", threadMessageStatusError, msg.Status)
	}
	var meta map[string]any
	if err := json.Unmarshal(msg.Metadata, &meta); err != nil {
		t.Fatalf("unmarshal assistant metadata: %v", err)
	}
	if got := meta["failure_code"]; got != "llm_timeout" {
		t.Fatalf("assistant failure_code: want=llm_timeout got=%v", got)
	}
}

func TestThreadAggregateMarkTurnFailedInvariantViolation(t *testing.T) {
	db := repotest.DB(t)
	tx := repotest.Tx(t, db)
	ensureThreadTables(t, tx)

	log := repotest.Logger(t)
	threads := chatrepos.NewChatThreadRepo(tx, log)
	messages := chatrepos.NewChatMessageRepo(tx, log)
	turns := chatrepos.NewChatTurnRepo(tx, log)

	agg := NewThreadAggregate(ThreadAggregateDeps{
		Base: BaseDeps{
			DB:       tx,
			Runner:   NewGormTxRunner(tx),
			CASGuard: NewCASGuard(tx),
		},
		Threads:  threads,
		Messages: messages,
		Turns:    turns,
	})

	ctx := context.Background()
	userID, _, turnID, _ := seedThreadTurn(t, ctx, threads, messages, turns)
	otherThread := &types.ChatThread{
		ID:            uuid.New(),
		UserID:        userID,
		Title:         "other",
		Status:        "active",
		Metadata:      datatypes.JSON([]byte(`{}`)),
		NextSeq:       0,
		LastMessageAt: time.Now().UTC(),
		LastViewedAt:  time.Now().UTC(),
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if _, err := threads.Create(dbctx.Context{Ctx: ctx}, []*types.ChatThread{otherThread}); err != nil {
		t.Fatalf("create other thread: %v", err)
	}

	_, err := agg.MarkTurnFailed(ctx, domainagg.MarkTurnFailedInput{
		UserID:      userID,
		ThreadID:    otherThread.ID,
		TurnID:      turnID,
		FailureCode: "x",
	})
	if err == nil {
		t.Fatalf("expected invariant violation")
	}
	if !domainagg.IsCode(err, domainagg.CodeInvariantViolation) {
		t.Fatalf("expected invariant violation code, got=%v", err)
	}
}

func TestThreadAggregateMarkTurnFailedRollbackOnInjectedFailure(t *testing.T) {
	db := repotest.DB(t)
	tx := repotest.Tx(t, db)
	ensureThreadTables(t, tx)

	log := repotest.Logger(t)
	threads := chatrepos.NewChatThreadRepo(tx, log)
	messages := chatrepos.NewChatMessageRepo(tx, log)
	turns := chatrepos.NewChatTurnRepo(tx, log)

	agg := NewThreadAggregate(ThreadAggregateDeps{
		Base: BaseDeps{
			DB:       tx,
			Runner:   rollbackAfterBodyRunner{db: tx},
			CASGuard: NewCASGuard(tx),
		},
		Threads:  threads,
		Messages: messages,
		Turns:    turns,
	})

	ctx := context.Background()
	userID, _, turnID, asstID := seedThreadTurn(t, ctx, threads, messages, turns)

	_, err := agg.MarkTurnFailed(ctx, domainagg.MarkTurnFailedInput{
		UserID:      userID,
		ThreadID:    mustThreadID(t, turns, ctx, userID, turnID),
		TurnID:      turnID,
		FailureCode: "forced_rollback",
	})
	if err == nil {
		t.Fatalf("expected injected rollback error")
	}

	turn, getErr := turns.GetByID(dbctx.Context{Ctx: ctx}, userID, turnID)
	if getErr != nil {
		t.Fatalf("GetByID turn: %v", getErr)
	}
	if turn == nil || turn.Status != threadTurnStatusRunning {
		t.Fatalf("turn status should remain running after rollback, got=%v", turn)
	}

	var msg types.ChatMessage
	if err := tx.WithContext(ctx).Model(&types.ChatMessage{}).Where("id = ?", asstID).First(&msg).Error; err != nil {
		t.Fatalf("load assistant msg: %v", err)
	}
	if msg.Status != "streaming" {
		t.Fatalf("assistant status should remain streaming after rollback, got=%q", msg.Status)
	}
}

func TestThreadAggregateMarkTurnFailedConcurrentConflict(t *testing.T) {
	db := repotest.DB(t)
	ensureThreadTables(t, db)

	log := repotest.Logger(t)
	threads := chatrepos.NewChatThreadRepo(db, log)
	messages := chatrepos.NewChatMessageRepo(db, log)
	turns := chatrepos.NewChatTurnRepo(db, log)

	agg := NewThreadAggregate(ThreadAggregateDeps{
		Base: BaseDeps{
			DB:       db,
			Runner:   NewGormTxRunner(db),
			CASGuard: NewCASGuard(db),
		},
		Threads:  threads,
		Messages: messages,
		Turns:    turns,
	})

	ctx := context.Background()
	userID, threadID, turnID, _ := seedThreadTurn(t, ctx, threads, messages, turns)
	t.Cleanup(func() {
		_ = db.WithContext(ctx).Where("id = ?", turnID).Delete(&types.ChatTurn{}).Error
		_ = db.WithContext(ctx).Where("thread_id = ?", threadID).Delete(&types.ChatMessage{}).Error
		_ = db.WithContext(ctx).Where("id = ?", threadID).Delete(&types.ChatThread{}).Error
	})

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	mark := func(code string) {
		defer wg.Done()
		<-start
		_, err := agg.MarkTurnFailed(ctx, domainagg.MarkTurnFailedInput{
			UserID:      userID,
			ThreadID:    threadID,
			TurnID:      turnID,
			FailureCode: code,
		})
		errs <- err
	}
	go mark("first")
	go mark("second")

	close(start)
	wg.Wait()
	close(errs)

	var successCount, conflictCount int
	for err := range errs {
		if err == nil {
			successCount++
			continue
		}
		if domainagg.IsCode(err, domainagg.CodeConflict) {
			conflictCount++
			continue
		}
		t.Fatalf("unexpected error: %v", err)
	}
	if successCount != 1 {
		t.Fatalf("success count: want=1 got=%d", successCount)
	}
	if conflictCount != 1 {
		t.Fatalf("conflict count: want=1 got=%d", conflictCount)
	}
}

func ensureThreadTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&types.ChatThread{}, &types.ChatMessage{}, &types.ChatTurn{}); err != nil {
		t.Fatalf("AutoMigrate chat tables: %v", err)
	}
}

func seedThreadTurn(
	t *testing.T,
	ctx context.Context,
	threads chatrepos.ChatThreadRepo,
	messages chatrepos.ChatMessageRepo,
	turns chatrepos.ChatTurnRepo,
) (userID uuid.UUID, threadID uuid.UUID, turnID uuid.UUID, assistantMessageID uuid.UUID) {
	t.Helper()
	now := time.Now().UTC()
	userID = uuid.New()
	thread := &types.ChatThread{
		ID:            uuid.New(),
		UserID:        userID,
		Title:         "thread",
		Status:        "active",
		Metadata:      datatypes.JSON([]byte(`{}`)),
		NextSeq:       2,
		LastMessageAt: now,
		LastViewedAt:  now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if _, err := threads.Create(dbctx.Context{Ctx: ctx}, []*types.ChatThread{thread}); err != nil {
		t.Fatalf("seed thread: %v", err)
	}

	userMsg := &types.ChatMessage{
		ID:        uuid.New(),
		ThreadID:  thread.ID,
		UserID:    userID,
		Seq:       1,
		Role:      "user",
		Status:    "sent",
		Content:   "hi",
		Metadata:  datatypes.JSON([]byte(`{}`)),
		CreatedAt: now,
		UpdatedAt: now,
	}
	asstMsg := &types.ChatMessage{
		ID:        uuid.New(),
		ThreadID:  thread.ID,
		UserID:    userID,
		Seq:       2,
		Role:      "assistant",
		Status:    "streaming",
		Content:   "",
		Metadata:  datatypes.JSON([]byte(`{}`)),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := messages.Create(dbctx.Context{Ctx: ctx}, []*types.ChatMessage{userMsg, asstMsg}); err != nil {
		t.Fatalf("seed messages: %v", err)
	}

	turn := &types.ChatTurn{
		ID:                 uuid.New(),
		UserID:             userID,
		ThreadID:           thread.ID,
		UserMessageID:      userMsg.ID,
		AssistantMessageID: asstMsg.ID,
		Status:             threadTurnStatusRunning,
		Attempt:            1,
		RetrievalTrace:     datatypes.JSON([]byte(`{}`)),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := turns.Create(dbctx.Context{Ctx: ctx}, turn); err != nil {
		t.Fatalf("seed turn: %v", err)
	}

	return userID, thread.ID, turn.ID, asstMsg.ID
}

func mustThreadID(t *testing.T, turns chatrepos.ChatTurnRepo, ctx context.Context, userID uuid.UUID, turnID uuid.UUID) uuid.UUID {
	t.Helper()
	turn, err := turns.GetByID(dbctx.Context{Ctx: ctx}, userID, turnID)
	if err != nil {
		t.Fatalf("GetByID turn: %v", err)
	}
	if turn == nil {
		t.Fatalf("turn not found")
	}
	return turn.ThreadID
}
