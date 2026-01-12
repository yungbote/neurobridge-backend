package handlers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
	librarymod "github.com/yungbote/neurobridge-backend/internal/modules/library"
	"github.com/yungbote/neurobridge-backend/internal/platform/apierr"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type LibraryHandler struct {
	log *logger.Logger
	db  *gorm.DB

	library librarymod.Usecases

	nodes      repos.LibraryTaxonomyNodeRepo
	edges      repos.LibraryTaxonomyEdgeRepo
	membership repos.LibraryTaxonomyMembershipRepo
	state      repos.LibraryTaxonomyStateRepo
	snapshots  repos.LibraryTaxonomySnapshotRepo
}

func NewLibraryHandler(
	log *logger.Logger,
	db *gorm.DB,
	library librarymod.Usecases,
	nodes repos.LibraryTaxonomyNodeRepo,
	edges repos.LibraryTaxonomyEdgeRepo,
	membership repos.LibraryTaxonomyMembershipRepo,
	state repos.LibraryTaxonomyStateRepo,
	snapshots repos.LibraryTaxonomySnapshotRepo,
) *LibraryHandler {
	return &LibraryHandler{
		log:        log.With("handler", "LibraryHandler"),
		db:         db,
		library:    library,
		nodes:      nodes,
		edges:      edges,
		membership: membership,
		state:      state,
		snapshots:  snapshots,
	}
}

// GET /api/library/taxonomy
func (h *LibraryHandler) GetTaxonomySnapshot(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	userID := rd.UserID

	snapshotAny, enqueuedRefine, err := h.library.GetTaxonomySnapshot(c.Request.Context(), userID)
	if err != nil {
		var ae *apierr.Error
		if errors.As(err, &ae) {
			response.RespondError(c, ae.Status, ae.Code, ae.Err)
			return
		}
		response.RespondError(c, http.StatusInternalServerError, "load_snapshot_failed", err)
		return
	}

	response.RespondOK(c, gin.H{
		"snapshot":        snapshotAny,
		"enqueued_refine": enqueuedRefine,
	})
}

var errTaxonomyNodeNotFound = errors.New("taxonomy_node_not_found")

type homeTaxonomyItemsCursorV1 struct {
	T  string `json:"t"`
	K  string `json:"k"`
	ID string `json:"id"`
}

func decodeHomeTaxonomyItemsCursor(raw string) (*time.Time, int, uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, 0, uuid.Nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, 0, uuid.Nil, err
	}
	var cur homeTaxonomyItemsCursorV1
	if err := json.Unmarshal(decoded, &cur); err != nil {
		return nil, 0, uuid.Nil, err
	}

	tRaw := strings.TrimSpace(cur.T)
	kRaw := strings.TrimSpace(cur.K)
	idRaw := strings.TrimSpace(cur.ID)
	if tRaw == "" || kRaw == "" || idRaw == "" {
		return nil, 0, uuid.Nil, fmt.Errorf("missing cursor fields")
	}

	t, err := time.Parse(time.RFC3339Nano, tRaw)
	if err != nil {
		t, err = time.Parse(time.RFC3339, tRaw)
		if err != nil {
			return nil, 0, uuid.Nil, err
		}
	}

	id, err := uuid.Parse(idRaw)
	if err != nil || id == uuid.Nil {
		return nil, 0, uuid.Nil, fmt.Errorf("invalid cursor id")
	}

	kind := strings.ToLower(kRaw)
	kindRank := 0
	switch kind {
	case "path":
		kindRank = 0
	case "material":
		kindRank = 1
	default:
		return nil, 0, uuid.Nil, fmt.Errorf("invalid cursor kind")
	}

	return &t, kindRank, id, nil
}

func encodeHomeTaxonomyItemsCursor(t time.Time, kindRank int, id uuid.UUID) string {
	kind := "path"
	if kindRank == 1 {
		kind = "material"
	}
	payload, _ := json.Marshal(homeTaxonomyItemsCursorV1{
		T:  t.UTC().Format(time.RFC3339Nano),
		K:  kind,
		ID: id.String(),
	})
	return base64.RawURLEncoding.EncodeToString(payload)
}

type homeTaxonomyPathRow struct {
	types.Path
	MaterialSetID *uuid.UUID `gorm:"column:material_set_id" json:"material_set_id,omitempty"`
}

type homeTaxonomyNodeItem struct {
	Kind string               `json:"kind"`
	Path *homeTaxonomyPathRow `json:"path,omitempty"`
	File *types.MaterialFile  `json:"file,omitempty"`
}

func kindRank(kind string) int {
	if strings.EqualFold(strings.TrimSpace(kind), "material") {
		return 1
	}
	return 0
}

func (h *LibraryHandler) validateTaxonomyNode(dbc dbctx.Context, userID uuid.UUID, facet string, nodeID uuid.UUID) error {
	if h == nil || h.nodes == nil {
		return fmt.Errorf("library_not_configured")
	}
	if userID == uuid.Nil || nodeID == uuid.Nil {
		return fmt.Errorf("invalid_request")
	}
	facet = strings.TrimSpace(facet)
	if facet == "" {
		return fmt.Errorf("invalid_request")
	}
	row, err := h.nodes.GetByID(dbc, nodeID)
	if err != nil {
		return err
	}
	if row == nil || row.ID == uuid.Nil || row.UserID != userID || !strings.EqualFold(strings.TrimSpace(row.Facet), facet) {
		return errTaxonomyNodeNotFound
	}
	return nil
}

// GET /api/library/taxonomy/nodes/:id/items?facet=topic&filter=all&limit=30&cursor=...
func (h *LibraryHandler) ListTaxonomyNodeItems(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h == nil || h.db == nil || h.nodes == nil {
		response.RespondError(c, http.StatusInternalServerError, "library_not_configured", nil)
		return
	}

	userID := rd.UserID
	nodeID, err := uuid.Parse(c.Param("id"))
	if err != nil || nodeID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_node_id", err)
		return
	}

	facet := strings.TrimSpace(c.DefaultQuery("facet", "topic"))
	if facet == "" {
		facet = "topic"
	}

	filter := strings.ToLower(strings.TrimSpace(c.DefaultQuery("filter", "all")))
	if filter == "" {
		filter = "all"
	}
	if filter != "all" && filter != "paths" && filter != "files" {
		response.RespondError(c, http.StatusBadRequest, "invalid_filter", nil)
		return
	}

	limit := 30
	if v := strings.TrimSpace(c.Query("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 120 {
		limit = 120
	}

	cursorTime, cursorKindRank, cursorID, err := decodeHomeTaxonomyItemsCursor(c.Query("cursor"))
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_cursor", err)
		return
	}

	ctx := c.Request.Context()
	if err := h.validateTaxonomyNode(dbctx.Context{Ctx: ctx}, userID, facet, nodeID); err != nil {
		if errors.Is(err, errTaxonomyNodeNotFound) {
			response.RespondError(c, http.StatusNotFound, "node_not_found", nil)
			return
		}
		response.RespondError(c, http.StatusBadRequest, "invalid_node", err)
		return
	}

	fetchN := limit + 1

	paths := make([]homeTaxonomyPathRow, 0, fetchN)
	files := make([]types.MaterialFile, 0, fetchN)

	if filter == "all" || filter == "paths" {
		whereCursor := ""
		args := []any{userID, facet, userID, nodeID, userID}
		if cursorTime != nil {
			if cursorKindRank == 0 {
				whereCursor = "AND (p.updated_at < ? OR (p.updated_at = ? AND p.id > ?))"
				args = append(args, *cursorTime, *cursorTime, cursorID)
			} else {
				whereCursor = "AND p.updated_at < ?"
				args = append(args, *cursorTime)
			}
		}
		args = append(args, fetchN)

		q := fmt.Sprintf(`
WITH primary_anchor AS (
  SELECT DISTINCT ON (m.path_id) m.path_id, m.node_id
  FROM library_taxonomy_membership m
  JOIN library_taxonomy_node n
    ON n.id = m.node_id
    AND n.user_id = m.user_id
    AND n.facet = m.facet
    AND lower(n.kind) = 'anchor'
    AND n.deleted_at IS NULL
  WHERE m.user_id = ? AND m.facet = ? AND m.deleted_at IS NULL
  ORDER BY m.path_id, m.weight DESC, m.updated_at DESC, m.node_id ASC
)
SELECT p.*, uli.material_set_id
FROM path p
JOIN primary_anchor pa ON pa.path_id = p.id
LEFT JOIN user_library_index uli ON uli.user_id = ? AND uli.path_id = p.id AND uli.deleted_at IS NULL
WHERE pa.node_id = ?
  AND p.user_id = ?
  AND lower(p.status) = 'ready'
  AND p.deleted_at IS NULL
  %s
ORDER BY p.updated_at DESC, p.id ASC
LIMIT ?
`, whereCursor)

		if err := h.db.WithContext(ctx).Raw(q, args...).Scan(&paths).Error; err != nil {
			h.log.Error("ListTaxonomyNodeItems failed (paths)", "error", err, "user_id", userID.String(), "node_id", nodeID.String())
			response.RespondError(c, http.StatusInternalServerError, "load_paths_failed", err)
			return
		}
	}

	if filter == "all" || filter == "files" {
		whereCursor := ""
		args := []any{userID, facet, nodeID, userID, userID}
		if cursorTime != nil {
			if cursorKindRank == 0 {
				whereCursor = "AND mf.updated_at <= ?"
				args = append(args, *cursorTime)
			} else {
				whereCursor = "AND (mf.updated_at < ? OR (mf.updated_at = ? AND mf.id > ?))"
				args = append(args, *cursorTime, *cursorTime, cursorID)
			}
		}
		args = append(args, fetchN)

		q := fmt.Sprintf(`
WITH primary_anchor AS (
  SELECT DISTINCT ON (m.path_id) m.path_id, m.node_id
  FROM library_taxonomy_membership m
  JOIN library_taxonomy_node n
    ON n.id = m.node_id
    AND n.user_id = m.user_id
    AND n.facet = m.facet
    AND lower(n.kind) = 'anchor'
    AND n.deleted_at IS NULL
  WHERE m.user_id = ? AND m.facet = ? AND m.deleted_at IS NULL
  ORDER BY m.path_id, m.weight DESC, m.updated_at DESC, m.node_id ASC
)
SELECT mf.*
FROM material_file mf
JOIN user_library_index uli ON uli.material_set_id = mf.material_set_id AND uli.deleted_at IS NULL
JOIN path p ON p.id = uli.path_id AND p.deleted_at IS NULL
JOIN primary_anchor pa ON pa.path_id = p.id AND pa.node_id = ?
WHERE uli.user_id = ?
  AND p.user_id = ?
  AND lower(p.status) = 'ready'
  AND mf.deleted_at IS NULL
  %s
ORDER BY mf.updated_at DESC, mf.id ASC
LIMIT ?
`, whereCursor)

		if err := h.db.WithContext(ctx).Raw(q, args...).Scan(&files).Error; err != nil {
			h.log.Error("ListTaxonomyNodeItems failed (files)", "error", err, "user_id", userID.String(), "node_id", nodeID.String())
			response.RespondError(c, http.StatusInternalServerError, "load_files_failed", err)
			return
		}
	}

	items := make([]homeTaxonomyNodeItem, 0, len(paths)+len(files))
	for i := range paths {
		items = append(items, homeTaxonomyNodeItem{Kind: "path", Path: &paths[i]})
	}
	for i := range files {
		items = append(items, homeTaxonomyNodeItem{Kind: "material", File: &files[i]})
	}

	sort.Slice(items, func(i, j int) bool {
		var ti, tj time.Time
		if items[i].Kind == "material" && items[i].File != nil {
			ti = items[i].File.UpdatedAt
		} else if items[i].Path != nil {
			ti = items[i].Path.UpdatedAt
		}
		if items[j].Kind == "material" && items[j].File != nil {
			tj = items[j].File.UpdatedAt
		} else if items[j].Path != nil {
			tj = items[j].Path.UpdatedAt
		}

		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		ri := kindRank(items[i].Kind)
		rj := kindRank(items[j].Kind)
		if ri != rj {
			return ri < rj
		}

		var idi, idj string
		if items[i].Kind == "material" && items[i].File != nil {
			idi = items[i].File.ID.String()
		} else if items[i].Path != nil {
			idi = items[i].Path.ID.String()
		}
		if items[j].Kind == "material" && items[j].File != nil {
			idj = items[j].File.ID.String()
		} else if items[j].Path != nil {
			idj = items[j].Path.ID.String()
		}
		return idi < idj
	})

	nextCursor := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		var t time.Time
		var id uuid.UUID
		kr := kindRank(last.Kind)
		if last.Kind == "material" && last.File != nil {
			t = last.File.UpdatedAt
			id = last.File.ID
		} else if last.Path != nil {
			t = last.Path.UpdatedAt
			id = last.Path.ID
		}
		if !t.IsZero() && id != uuid.Nil {
			nextCursor = encodeHomeTaxonomyItemsCursor(t, kr, id)
		}
	}

	response.RespondOK(c, gin.H{
		"items":       items,
		"next_cursor": nextCursor,
	})
}
