package services

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/envutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type GazeHitInput struct {
	BlockID    string  `json:"block_id"`
	LineID     string  `json:"line_id,omitempty"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Confidence float64 `json:"confidence"`
	OccurredAt string  `json:"ts"`
	DtMs       float64 `json:"dt_ms,omitempty"`
	ReadCredit float64 `json:"read_credit,omitempty"`
	Source     string  `json:"source,omitempty"`
	ScreenW    int     `json:"screen_w,omitempty"`
	ScreenH    int     `json:"screen_h,omitempty"`
	LineIndex  *int    `json:"line_index,omitempty"`
	Extra      any     `json:"extra,omitempty"`
}

type GazeIngestRequest struct {
	PathID string         `json:"path_id,omitempty"`
	NodeID string         `json:"node_id,omitempty"`
	Hits   []GazeHitInput `json:"hits"`
}

type GazeService interface {
	Ingest(dbc dbctx.Context, userID uuid.UUID, sessionID uuid.UUID, req GazeIngestRequest) (int, error)
}

type gazeService struct {
	log        *logger.Logger
	gazeEvents repos.UserGazeEventRepo
	gazeStats  repos.UserGazeBlockStatRepo
	userPrefs  repos.UserPersonalizationPrefsRepo

	enabled         bool
	storeRaw        bool
	retentionDays   int
	minConfidence   float64
	maxBatch        int
	maxPointsPerSec int

	cacheMu    sync.Mutex
	allowCache map[uuid.UUID]struct {
		allowed bool
		at      time.Time
	}

	lastCleanup time.Time
}

func NewGazeService(log *logger.Logger, gazeEvents repos.UserGazeEventRepo, gazeStats repos.UserGazeBlockStatRepo, userPrefs repos.UserPersonalizationPrefsRepo) GazeService {
	svc := &gazeService{
		log:             log.With("service", "GazeService"),
		gazeEvents:      gazeEvents,
		gazeStats:       gazeStats,
		userPrefs:       userPrefs,
		enabled:         strings.TrimSpace(strings.ToLower(getEnv("GAZE_STREAM_ENABLED"))) != "false",
		storeRaw:        strings.TrimSpace(strings.ToLower(getEnv("GAZE_STREAM_STORE_RAW"))) == "true",
		retentionDays:   envutil.Int("GAZE_STREAM_RETENTION_DAYS", 30),
		minConfidence:   float64(envutil.Int("GAZE_STREAM_MIN_CONFIDENCE_PCT", 40)) / 100.0,
		maxBatch:        envutil.Int("GAZE_STREAM_MAX_BATCH", 400),
		maxPointsPerSec: envutil.Int("GAZE_STREAM_MAX_POINTS_PER_SEC", 30),
		allowCache: make(map[uuid.UUID]struct {
			allowed bool
			at      time.Time
		}),
	}
	return svc
}

func (s *gazeService) Ingest(dbc dbctx.Context, userID uuid.UUID, sessionID uuid.UUID, req GazeIngestRequest) (int, error) {
	if userID == uuid.Nil || sessionID == uuid.Nil {
		return 0, nil
	}
	if !s.enabled || len(req.Hits) == 0 {
		return 0, nil
	}
	if !s.isAllowed(dbc, userID) {
		return 0, nil
	}

	hits := req.Hits
	if s.maxBatch > 0 && len(hits) > s.maxBatch {
		hits = hits[:s.maxBatch]
	}

	pathID := parseUUID(req.PathID)
	nodeID := parseUUID(req.NodeID)
	now := time.Now().UTC()

	type agg struct {
		blockID    string
		fixMs      int
		count      int
		readCredit float64
		lastSeen   time.Time
		lineStats  map[string]int
	}
	aggs := map[string]*agg{}
	rawRows := make([]*types.UserGazeEvent, 0, len(hits))
	secCounts := map[int64]int{}

	for _, h := range hits {
		bid := strings.TrimSpace(h.BlockID)
		if bid == "" {
			continue
		}
		if h.Confidence < s.minConfidence {
			continue
		}
		occ := parseTime(h.OccurredAt)
		if occ.IsZero() {
			occ = now
		}
		if s.maxPointsPerSec > 0 {
			sec := occ.Unix()
			if secCounts[sec] >= s.maxPointsPerSec {
				continue
			}
			secCounts[sec] = secCounts[sec] + 1
		}
		dt := int(h.DtMs)
		if dt <= 0 || dt > 2000 {
			dt = 100
		}
		a := aggs[bid]
		if a == nil {
			a = &agg{blockID: bid, lineStats: map[string]int{}}
			aggs[bid] = a
		}
		a.fixMs += dt
		a.count += 1
		rc := h.ReadCredit
		if rc < 0 {
			rc = 0
		}
		if rc > 1 {
			rc = 1
		}
		if rc > a.readCredit {
			a.readCredit = rc
		}
		if occ.After(a.lastSeen) {
			a.lastSeen = occ
		}
		if lid := strings.TrimSpace(h.LineID); lid != "" {
			a.lineStats[lid] = a.lineStats[lid] + dt
		}

		if s.storeRaw && s.gazeEvents != nil {
			meta := map[string]any{
				"source":     h.Source,
				"screen_w":   h.ScreenW,
				"screen_h":   h.ScreenH,
				"line_index": h.LineIndex,
			}
			if h.Extra != nil {
				meta["extra"] = h.Extra
			}
			rawRows = append(rawRows, &types.UserGazeEvent{
				UserID:     userID,
				SessionID:  sessionID,
				PathID:     pathID,
				PathNodeID: nodeID,
				BlockID:    bid,
				LineID:     strings.TrimSpace(h.LineID),
				X:          h.X,
				Y:          h.Y,
				Confidence: h.Confidence,
				OccurredAt: occ,
				Metadata:   mustJSON(meta),
			})
		}
	}

	for _, a := range aggs {
		if a.fixMs <= 0 {
			continue
		}
		row, _ := s.gazeStats.GetByUserSessionBlock(dbc, userID, sessionID, a.blockID)
		if row == nil {
			row = &types.UserGazeBlockStat{
				UserID:        userID,
				SessionID:     sessionID,
				PathID:        pathID,
				PathNodeID:    nodeID,
				BlockID:       a.blockID,
				FixationMs:    0,
				FixationCount: 0,
				ReadCredit:    0,
				Metadata:      datatypesJSON(map[string]any{}),
			}
		}
		row.PathID = pathID
		row.PathNodeID = nodeID
		row.FixationMs += a.fixMs
		row.FixationCount += a.count
		if a.readCredit > row.ReadCredit {
			row.ReadCredit = a.readCredit
		}
		if !a.lastSeen.IsZero() {
			t := a.lastSeen
			row.LastSeenAt = &t
		}
		meta := decodeJSONMap(row.Metadata)
		if len(a.lineStats) > 0 {
			ls := mapFromAny(meta["line_stats"])
			if ls == nil {
				ls = map[string]any{}
			}
			for k, v := range a.lineStats {
				prev := intFromAny(ls[k], 0)
				ls[k] = prev + v
			}
			meta["line_stats"] = ls
		}
		row.Metadata = datatypesJSON(meta)
		if err := s.gazeStats.Upsert(dbc, row); err != nil && s.log != nil {
			s.log.Warn("gaze_stat_upsert_failed", "error", err)
		}
	}

	if s.storeRaw && s.gazeEvents != nil && len(rawRows) > 0 {
		if err := s.gazeEvents.CreateMany(dbc, rawRows); err != nil && s.log != nil {
			s.log.Warn("gaze_event_insert_failed", "error", err)
		}
		s.maybeCleanup(dbc, userID)
	}

	return len(hits), nil
}

func (s *gazeService) isAllowed(dbc dbctx.Context, userID uuid.UUID) bool {
	if s.userPrefs == nil {
		return true
	}
	s.cacheMu.Lock()
	cached, ok := s.allowCache[userID]
	if ok && time.Since(cached.at) < 2*time.Minute {
		s.cacheMu.Unlock()
		return cached.allowed
	}
	s.cacheMu.Unlock()

	row, err := s.userPrefs.GetByUserID(dbc, userID)
	if err != nil || row == nil {
		return false
	}
	meta := decodeJSONMap(row.PrefsJSON)
	allowed := boolFromAny(meta["allowEyeTracking"])

	s.cacheMu.Lock()
	s.allowCache[userID] = struct {
		allowed bool
		at      time.Time
	}{allowed: allowed, at: time.Now()}
	s.cacheMu.Unlock()

	return allowed
}

func (s *gazeService) maybeCleanup(dbc dbctx.Context, userID uuid.UUID) {
	if s.retentionDays <= 0 || s.gazeEvents == nil {
		return
	}
	if time.Since(s.lastCleanup) < time.Hour {
		return
	}
	cutoff := time.Now().UTC().Add(-time.Duration(s.retentionDays) * 24 * time.Hour).Format(time.RFC3339)
	_ = s.gazeEvents.DeleteOlderThan(dbc, userID, cutoff)
	s.lastCleanup = time.Now().UTC()
}

func parseUUID(raw string) *uuid.UUID {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if id, err := uuid.Parse(raw); err == nil && id != uuid.Nil {
		return &id
	}
	return nil
}

func parseTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t
	}
	if ms, err := strconv.ParseInt(raw, 10, 64); err == nil && ms > 0 {
		return time.UnixMilli(ms).UTC()
	}
	return time.Time{}
}

func getEnv(key string) string {
	return strings.TrimSpace(strings.ToLower(strings.TrimSpace(getRawEnv(key))))
}

func getRawEnv(key string) string {
	return strings.TrimSpace(strings.TrimSpace(os.Getenv(key)))
}

func mustJSON(v map[string]any) datatypes.JSON {
	if v == nil {
		return datatypes.JSON([]byte("{}"))
	}
	b, err := json.Marshal(v)
	if err != nil {
		return datatypes.JSON([]byte("{}"))
	}
	return datatypes.JSON(b)
}

func datatypesJSON(v map[string]any) datatypes.JSON {
	return mustJSON(v)
}

func decodeJSONMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func mapFromAny(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func intFromAny(v any, def int) int {
	if v == nil {
		return def
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i)
		}
	}
	return def
}

func boolFromAny(v any) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	if s, ok := v.(string); ok {
		s = strings.ToLower(strings.TrimSpace(s))
		return s == "true" || s == "1" || s == "yes"
	}
	return false
}
