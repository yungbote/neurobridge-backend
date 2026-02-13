package concept_graph_patch_build

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/structuraltrace"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	setID, ok := jc.PayloadUUID("material_set_id")
	if !ok || setID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing material_set_id"))
		return nil
	}
	sagaID, ok := jc.PayloadUUID("saga_id")
	if !ok || sagaID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing saga_id"))
		return nil
	}
	pathID, _ := jc.PayloadUUID("path_id")

	heartbeatSec := getEnvInt("CONCEPT_GRAPH_HEARTBEAT_SECONDS", 20)
	if heartbeatSec < 1 {
		heartbeatSec = 1
	}
	if heartbeatSec > 60 {
		heartbeatSec = 60
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var stopOnce sync.Once
	stopTicker := func() {
		stopOnce.Do(func() {
			close(stop)
			wg.Wait()
		})
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(time.Duration(heartbeatSec) * time.Second)
		defer t.Stop()
		for {
			select {
			case <-jc.Ctx.Done():
				return
			case <-stop:
				return
			case <-t.C:
				jc.Progress("concept_graph_patch", 2, "Patching concept graph")
			}
		}
	}()
	defer stopTicker()

	jc.Progress("concept_graph_patch", 2, "Patching concept graph")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:               p.db,
		Log:              p.log,
		Files:            p.files,
		FileSigs:         p.fileSigs,
		Chunks:           p.chunks,
		Path:             p.path,
		Concepts:         p.concepts,
		ConceptReps:      p.reps,
		MappingOverrides: p.overrides,
		Evidence:         p.evidence,
		Edges:            p.edges,
		Graph:            p.graph,
		AI:               p.ai,
		Vec:              p.vec,
		Saga:             p.saga,
		Bootstrap:        p.bootstrap,
		Artifacts:        p.artifacts,
	}).ConceptGraphPatchBuild(jc.Ctx, learningmod.ConceptGraphPatchBuildInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
	})
	stopTicker()
	if err != nil {
		jc.Fail("concept_graph_patch", err)
		return nil
	}

	meta := map[string]any{
		"job_run_id":       jc.Job.ID.String(),
		"owner_user_id":    jc.Job.OwnerUserID.String(),
		"material_set_id":  setID.String(),
		"path_id":          out.PathID.String(),
		"concepts_made":    out.ConceptsMade,
		"edges_made":       out.EdgesMade,
		"pinecone_batches": out.PineconeBatches,
	}
	inputs := map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
	}
	chosen := map[string]any{
		"concepts_made":    out.ConceptsMade,
		"edges_made":       out.EdgesMade,
		"pinecone_batches": out.PineconeBatches,
	}
	userID := jc.Job.OwnerUserID
	traceRes, err := structuraltrace.Record(jc.Ctx, structuraltrace.Deps{
		DB:           p.db,
		Log:          p.log,
		GraphVersion: p.graphVersions,
		TraceWriter:  p.structuralTraces,
	}, structuraltrace.TraceInput{
		DecisionType:      p.Type(),
		DecisionPhase:     "build",
		DecisionMode:      "deterministic",
		UserID:            &userID,
		PathID:            &out.PathID,
		MaterialSetID:     &setID,
		SagaID:            &sagaID,
		Inputs:            inputs,
		Chosen:            chosen,
		Metadata:          meta,
		Payload:           jc.Payload(),
		Validate:          true,
		RequireTrace:      true,
		WriteGraphVersion: true,
	})
	if err != nil {
		jc.Fail("invariant_validation", err)
		return nil
	}
	graphVersion := traceRes.GraphVersion

	jc.Succeed("done", map[string]any{
		"material_set_id":  setID.String(),
		"saga_id":          sagaID.String(),
		"path_id":          out.PathID.String(),
		"concepts_made":    out.ConceptsMade,
		"edges_made":       out.EdgesMade,
		"pinecone_batches": out.PineconeBatches,
		"graph_version":    graphVersion,
	})
	return nil
}

func getEnvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}
