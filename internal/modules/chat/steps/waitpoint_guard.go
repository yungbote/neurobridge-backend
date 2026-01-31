package steps

import (
    "context"
    "encoding/json"
    "strings"

    "github.com/google/uuid"
    "gorm.io/datatypes"

    types "github.com/yungbote/neurobridge-backend/internal/domain"
    "github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type orchestratorStateProbe struct {
    Stages map[string]orchestratorStageProbe `json:"stages,omitempty"`
}

type orchestratorStageProbe struct {
    ChildJobID   string `json:"child_job_id,omitempty"`
    ChildJobType string `json:"child_job_type,omitempty"`
}

func threadHasActiveWaitpoint(ctx context.Context, deps RespondDeps, thread *types.ChatThread, userID uuid.UUID) bool {
    if thread == nil || thread.JobID == nil || *thread.JobID == uuid.Nil || deps.JobRuns == nil {
        return false
    }
    dbc := dbctx.Context{Ctx: ctx, Tx: deps.DB}
    rows, err := deps.JobRuns.GetByIDs(dbc, []uuid.UUID{*thread.JobID})
    if err != nil || len(rows) == 0 || rows[0] == nil {
        return false
    }
    job := rows[0]
    if job.OwnerUserID != userID {
        return false
    }
    if strings.EqualFold(strings.TrimSpace(job.Status), "waiting_user") {
        return true
    }

    candidates := waitpointChildCandidates(job.Result)
    if len(candidates) == 0 {
        return false
    }
    ids := make([]uuid.UUID, 0, len(candidates))
    for id := range candidates {
        if id != uuid.Nil {
            ids = append(ids, id)
        }
    }
    if len(ids) == 0 {
        return false
    }
    childRows, err := deps.JobRuns.GetByIDs(dbc, ids)
    if err != nil {
        return false
    }
    for _, row := range childRows {
        if row == nil || row.ID == uuid.Nil || row.OwnerUserID != userID {
            continue
        }
        if strings.EqualFold(strings.TrimSpace(row.Status), "waiting_user") {
            return true
        }
    }
    return false
}

func waitpointChildCandidates(raw datatypes.JSON) map[uuid.UUID]string {
    out := map[uuid.UUID]string{}
    if len(raw) == 0 {
        return out
    }
    text := strings.TrimSpace(string(raw))
    if text == "" || text == "null" {
        return out
    }
    var probe orchestratorStateProbe
    if err := json.Unmarshal(raw, &probe); err != nil || probe.Stages == nil {
        return out
    }
    for stageName, ss := range probe.Stages {
        if !isWaitpointStageName(stageName, ss) {
            continue
        }
        id, err := uuid.Parse(strings.TrimSpace(ss.ChildJobID))
        if err != nil || id == uuid.Nil {
            continue
        }
        out[id] = stageName
    }
    return out
}

func isWaitpointStageName(stageName string, ss orchestratorStageProbe) bool {
    name := strings.ToLower(strings.TrimSpace(stageName))
    if strings.HasSuffix(name, "_waitpoint") || strings.Contains(name, "waitpoint") {
        return true
    }
    return strings.EqualFold(strings.TrimSpace(ss.ChildJobType), "waitpoint_stage")
}
