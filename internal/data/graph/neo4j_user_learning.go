package graph

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
)

func UpsertUserConceptStates(ctx context.Context, client *neo4jdb.Client, log *logger.Logger, userID uuid.UUID, rows []*types.UserConceptState) error {
	if client == nil || client.Driver == nil {
		return nil
	}
	if userID == uuid.Nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	relRows := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		if r == nil || r.UserID == uuid.Nil || r.ConceptID == uuid.Nil {
			continue
		}
		if r.UserID != userID {
			continue
		}
		relRows = append(relRows, map[string]any{
			"user_id":        r.UserID.String(),
			"concept_id":     r.ConceptID.String(),
			"mastery":        r.Mastery,
			"confidence":     r.Confidence,
			"decay_rate":     r.DecayRate,
			"attempts":       int64(r.Attempts),
			"correct":        int64(r.Correct),
			"misconceptions": string(r.Misconceptions),
			"last_seen_at": func() string {
				if r.LastSeenAt == nil || r.LastSeenAt.IsZero() {
					return ""
				}
				return r.LastSeenAt.UTC().Format(time.RFC3339Nano)
			}(),
			"next_review_at": func() string {
				if r.NextReviewAt == nil || r.NextReviewAt.IsZero() {
					return ""
				}
				return r.NextReviewAt.UTC().Format(time.RFC3339Nano)
			}(),
			"updated_at": r.UpdatedAt.UTC().Format(time.RFC3339Nano),
			"synced_at":  now,
		})
	}

	session := client.Driver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode:   neo4j.AccessModeWrite,
		DatabaseName: client.Database,
	})
	defer session.Close(ctx)

	// Best-effort schema init.
	if res, err := session.Run(ctx, `CREATE CONSTRAINT user_id_unique IF NOT EXISTS FOR (u:User) REQUIRE u.id IS UNIQUE`, nil); err != nil {
		if log != nil {
			log.Warn("neo4j schema init failed (continuing)", "error", err)
		}
	} else {
		_, _ = res.Consume(ctx)
	}

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		if res, err := tx.Run(ctx, `
MERGE (u:User {id: $user_id})
SET u.synced_at = $synced_at
`, map[string]any{"user_id": userID.String(), "synced_at": now}); err != nil {
			return nil, err
		} else if _, err := res.Consume(ctx); err != nil {
			return nil, err
		}

		if len(relRows) == 0 {
			return nil, nil
		}

		res, err := tx.Run(ctx, `
UNWIND $rows AS r
MERGE (u:User {id: r.user_id})
MERGE (c:Concept {id: r.concept_id})
MERGE (u)-[s:CONCEPT_STATE]->(c)
SET s.mastery = r.mastery,
    s.confidence = r.confidence,
    s.decay_rate = r.decay_rate,
    s.attempts = r.attempts,
    s.correct = r.correct,
    s.misconceptions_json = r.misconceptions,
    s.last_seen_at = r.last_seen_at,
    s.next_review_at = r.next_review_at,
    s.updated_at = r.updated_at,
    s.synced_at = r.synced_at
`, map[string]any{"rows": relRows})
		if err != nil {
			return nil, err
		}
		if _, err := res.Consume(ctx); err != nil {
			return nil, err
		}
		return nil, nil
	})
	return err
}
