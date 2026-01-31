package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type OptionalUUID struct {
	Set   bool
	Value *uuid.UUID
}

func (o *OptionalUUID) UnmarshalJSON(data []byte) error {
	o.Set = true
	data = bytes.TrimSpace(data)
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		o.Value = nil
		return nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return err
	}
	o.Value = &id
	return nil
}

type OptionalString struct {
	Set   bool
	Value *string
}

func (o *OptionalString) UnmarshalJSON(data []byte) error {
	o.Set = true
	data = bytes.TrimSpace(data)
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		o.Value = nil
		return nil
	}
	o.Value = &s
	return nil
}

type OptionalFloat64 struct {
	Set   bool
	Value *float64
}

func (o *OptionalFloat64) UnmarshalJSON(data []byte) error {
	o.Set = true
	data = bytes.TrimSpace(data)
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var v float64
	if err := json.Unmarshal(data, &v); err == nil {
		o.Value = &v
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		o.Value = nil
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	o.Value = &f
	return nil
}

type OptionalJSON struct {
	Set   bool
	Value *json.RawMessage
}

func (o *OptionalJSON) UnmarshalJSON(data []byte) error {
	o.Set = true
	data = bytes.TrimSpace(data)
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	cp := make(json.RawMessage, len(raw))
	copy(cp, raw)
	o.Value = &cp
	return nil
}

func mergeJSONObjects(base, patch json.RawMessage) (json.RawMessage, error) {
	if len(patch) == 0 {
		if len(base) == 0 {
			return nil, nil
		}
		out := make(json.RawMessage, len(base))
		copy(out, base)
		return out, nil
	}

	var patchObj map[string]any
	if err := json.Unmarshal(patch, &patchObj); err != nil {
		return nil, err
	}
	if patchObj == nil {
		patchObj = map[string]any{}
	}

	var baseObj map[string]any
	if len(base) > 0 {
		if err := json.Unmarshal(base, &baseObj); err != nil {
			return nil, err
		}
	}
	if baseObj == nil {
		baseObj = map[string]any{}
	}

	for k, v := range patchObj {
		baseObj[k] = v
	}

	merged, err := json.Marshal(baseObj)
	if err != nil {
		return nil, err
	}

	return json.RawMessage(merged), nil
}

type SessionStatePatch struct {
	ActivePathID       OptionalUUID    `json:"active_path_id"`
	ActivePathNodeID   OptionalUUID    `json:"active_path_node_id"`
	ActiveActivityID   OptionalUUID    `json:"active_activity_id"`
	ActiveChatThreadID OptionalUUID    `json:"active_chat_thread_id"`
	ActiveJobID        OptionalUUID    `json:"active_job_id"`
	ActiveRoute        OptionalString  `json:"active_route"`
	ActiveView         OptionalString  `json:"active_view"`
	ActiveDocBlockID   OptionalString  `json:"active_doc_block_id"`
	ScrollPercent      OptionalFloat64 `json:"scroll_percent"`
	Metadata           OptionalJSON    `json:"metadata"`
}

type SessionStateService interface {
	Get(dbc dbctx.Context) (*types.UserSessionState, error)
	Patch(dbc dbctx.Context, patch SessionStatePatch) (*types.UserSessionState, error)
}

type sessionStateService struct {
	db   *gorm.DB
	log  *logger.Logger
	repo repos.UserSessionStateRepo
}

func NewSessionStateService(db *gorm.DB, baseLog *logger.Logger, repo repos.UserSessionStateRepo) SessionStateService {
	return &sessionStateService{
		db:   db,
		log:  baseLog.With("service", "SessionStateService"),
		repo: repo,
	}
}

func (s *sessionStateService) Get(dbc dbctx.Context) (*types.UserSessionState, error) {
	return s.Patch(dbc, SessionStatePatch{})
}

func (s *sessionStateService) Patch(dbc dbctx.Context, patch SessionStatePatch) (*types.UserSessionState, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil || rd.SessionID == uuid.Nil {
		return nil, fmt.Errorf("unauthorized")
	}

	run := func(inner dbctx.Context) (*types.UserSessionState, error) {
		if err := s.repo.Ensure(inner, rd.UserID, rd.SessionID); err != nil {
			return nil, err
		}

		prev, err := s.repo.GetBySessionID(inner, rd.SessionID)
		if err != nil {
			return nil, err
		}

		now := time.Now().UTC()
		updates := map[string]any{
			"last_seen_at": now,
			"updated_at":   now,
		}

		if patch.ActivePathID.Set {
			if patch.ActivePathID.Value == nil {
				updates["active_path_id"] = nil
				updates["active_path_node_id"] = nil
				updates["active_activity_id"] = nil
				updates["active_doc_block_id"] = nil
				updates["scroll_percent"] = nil
			} else {
				if prev == nil || prev.ActivePathID == nil || *prev.ActivePathID != *patch.ActivePathID.Value {
					updates["active_path_node_id"] = nil
					updates["active_activity_id"] = nil
					updates["active_doc_block_id"] = nil
					updates["scroll_percent"] = nil
				}
				updates["active_path_id"] = *patch.ActivePathID.Value
			}
		}
		if patch.ActivePathNodeID.Set {
			if patch.ActivePathNodeID.Value == nil {
				updates["active_path_node_id"] = nil
				updates["active_activity_id"] = nil
				updates["active_doc_block_id"] = nil
				updates["scroll_percent"] = nil
			} else {
				if prev == nil || prev.ActivePathNodeID == nil || *prev.ActivePathNodeID != *patch.ActivePathNodeID.Value {
					updates["active_activity_id"] = nil
					updates["active_doc_block_id"] = nil
					updates["scroll_percent"] = nil
				}
				updates["active_path_node_id"] = *patch.ActivePathNodeID.Value
			}
		}
		if patch.ActiveActivityID.Set {
			if patch.ActiveActivityID.Value == nil {
				updates["active_activity_id"] = nil
			} else {
				updates["active_activity_id"] = *patch.ActiveActivityID.Value
			}
		}
		if patch.ActiveChatThreadID.Set {
			if patch.ActiveChatThreadID.Value == nil {
				updates["active_chat_thread_id"] = nil
			} else {
				updates["active_chat_thread_id"] = *patch.ActiveChatThreadID.Value
			}
		}
		if patch.ActiveJobID.Set {
			if patch.ActiveJobID.Value == nil {
				updates["active_job_id"] = nil
			} else {
				updates["active_job_id"] = *patch.ActiveJobID.Value
			}
		}
		if patch.ActiveRoute.Set {
			if patch.ActiveRoute.Value == nil {
				updates["active_route"] = nil
			} else {
				updates["active_route"] = *patch.ActiveRoute.Value
			}
		}
		if patch.ActiveView.Set {
			if patch.ActiveView.Value == nil {
				updates["active_view"] = nil
			} else {
				updates["active_view"] = *patch.ActiveView.Value
			}
		}
		if patch.ActiveDocBlockID.Set {
			if patch.ActiveDocBlockID.Value == nil {
				updates["active_doc_block_id"] = nil
			} else {
				updates["active_doc_block_id"] = *patch.ActiveDocBlockID.Value
			}
		}
		if patch.ScrollPercent.Set {
			if patch.ScrollPercent.Value == nil {
				updates["scroll_percent"] = nil
			} else {
				v := *patch.ScrollPercent.Value
				if v < 0 || v > 100 {
					return nil, fmt.Errorf("scroll_percent out of range (0..100)")
				}
				updates["scroll_percent"] = v
			}
		}
		if patch.Metadata.Set {
			if patch.Metadata.Value == nil {
				updates["metadata"] = nil
			} else {
				merged, err := mergeJSONObjects(json.RawMessage(prev.Metadata), *patch.Metadata.Value)
				if err != nil {
					return nil, err
				}
				if len(merged) == 0 {
					updates["metadata"] = nil
				} else {
					updates["metadata"] = datatypes.JSON(merged)
				}
			}
		}

		if err := s.repo.UpdateFields(inner, rd.SessionID, updates); err != nil {
			return nil, err
		}
		state, err := s.repo.GetBySessionID(inner, rd.SessionID)
		if err != nil {
			return nil, err
		}
		if state == nil {
			return nil, fmt.Errorf("session state not found")
		}
		return state, nil
	}

	if dbc.Tx != nil {
		return run(dbc)
	}

	var out *types.UserSessionState
	if err := s.db.WithContext(dbc.Ctx).Transaction(func(tx *gorm.DB) error {
		state, err := run(dbctx.Context{Ctx: dbc.Ctx, Tx: tx})
		if err != nil {
			return err
		}
		out = state
		return nil
	}); err != nil {
		s.log.Warn("Patch transaction error", "error", err)
		return nil, err
	}
	return out, nil
}
