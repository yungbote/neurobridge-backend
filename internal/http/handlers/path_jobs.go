package handlers

import (
	"context"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type pathWithJob struct {
	*types.Path
	JobStatus   string `json:"job_status,omitempty"`
	JobStage    string `json:"job_stage,omitempty"`
	JobProgress int    `json:"job_progress,omitempty"`
	JobMessage  string `json:"job_message,omitempty"`
}

func (h *PathHandler) attachJobSnapshot(ctx context.Context, userID uuid.UUID, paths []*types.Path) []*pathWithJob {
	if h == nil {
		return nil
	}
	out := make([]*pathWithJob, 0, len(paths))
	if len(paths) == 0 {
		return out
	}

	materialSetByPathID := map[uuid.UUID]uuid.UUID{}
	if h.userLibraryIndex != nil {
		rows, err := h.userLibraryIndex.GetByUserIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{userID})
		if err == nil {
			for _, r := range rows {
				if r == nil || r.PathID == nil || *r.PathID == uuid.Nil || r.MaterialSetID == uuid.Nil {
					continue
				}
				materialSetByPathID[*r.PathID] = r.MaterialSetID
			}
		}
	}

	jobIDs := make([]uuid.UUID, 0, len(paths))
	for _, p := range paths {
		if p == nil || p.JobID == nil || *p.JobID == uuid.Nil {
			continue
		}
		jobIDs = append(jobIDs, *p.JobID)
	}

	jobByID := map[uuid.UUID]*types.JobRun{}
	if h.jobs != nil && len(jobIDs) > 0 {
		rows, err := h.jobs.GetByIDs(dbctx.Context{Ctx: ctx}, jobIDs)
		if err == nil {
			for _, j := range rows {
				if j == nil || j.ID == uuid.Nil || j.OwnerUserID != userID {
					continue
				}
				jobByID[j.ID] = j
			}
		}
	}

	for _, p := range paths {
		if p == nil {
			continue
		}
		dto := &pathWithJob{Path: p}

		// Back-compat: older installs derived material_set_id from user_library_index.
		// Newer installs store it directly on the path row.
		if (p.MaterialSetID == nil || *p.MaterialSetID == uuid.Nil) && materialSetByPathID[p.ID] != uuid.Nil {
			msid := materialSetByPathID[p.ID]
			p.MaterialSetID = &msid
		}

		if p.JobID != nil && *p.JobID != uuid.Nil {
			if j := jobByID[*p.JobID]; j != nil {
				dto.JobStatus = j.Status
				dto.JobStage = j.Stage
				dto.JobProgress = j.Progress
				dto.JobMessage = j.Message
			}
		}
		out = append(out, dto)
	}

	return out
}
