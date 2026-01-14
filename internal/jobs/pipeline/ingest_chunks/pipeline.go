package ingest_chunks

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

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

	fileTimeoutMin := getEnvInt("INGEST_CHUNKS_FILE_TIMEOUT_MINUTES", 10)
	jobTimeoutMin := getEnvInt("INGEST_CHUNKS_JOB_TIMEOUT_MINUTES", 30)
	heartbeatSec := getEnvInt("INGEST_CHUNKS_HEARTBEAT_SECONDS", 3)
	if heartbeatSec < 1 {
		heartbeatSec = 1
	}
	if heartbeatSec > 10 {
		heartbeatSec = 10
	}

	jobCtx := jc.Ctx
	cancelJob := func() {}
	if jobTimeoutMin > 0 {
		jobCtx, cancelJob = context.WithTimeout(jc.Ctx, time.Duration(jobTimeoutMin)*time.Minute)
	}
	defer cancelJob()

	var (
		currentPct int32 = 2
		currentMsg atomic.Value
	)
	currentMsg.Store("Ensuring chunks exist")

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
			case <-jobCtx.Done():
				return
			case <-stop:
				return
			case <-t.C:
				msgAny := currentMsg.Load()
				msg, _ := msgAny.(string)
				if strings.TrimSpace(msg) == "" {
					msg = "Ingesting…"
				}
				jc.Progress("ingest", int(atomic.LoadInt32(&currentPct)), msg)
			}
		}
	}()
	defer stopTicker()

	jc.Progress("ingest", 2, "Ensuring chunks exist")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:        p.db,
		Log:       p.log,
		Files:     p.files,
		Chunks:    p.chunks,
		Extract:   p.extract,
		Saga:      p.saga,
		Bootstrap: p.bootstrap,
	}).IngestChunks(jobCtx, learningmod.IngestChunksInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
	}, learningmod.IngestChunksOptions{
		FileTimeout: time.Duration(fileTimeoutMin) * time.Minute,
		Report: func(stage string, pct int, message string) {
			_ = stage
			if pct >= 0 && pct <= 99 {
				atomic.StoreInt32(&currentPct, int32(pct))
			}
			if strings.TrimSpace(message) != "" {
				currentMsg.Store(message)
			}
		},
	})
	stopTicker()
	if err != nil {
		jc.Fail("ingest", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id":       setID.String(),
		"saga_id":               sagaID.String(),
		"path_id":               out.PathID.String(),
		"files_total":           out.FilesTotal,
		"files_processed":       out.FilesProcessed,
		"files_already_chunked": out.FilesAlreadyChunked,
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
