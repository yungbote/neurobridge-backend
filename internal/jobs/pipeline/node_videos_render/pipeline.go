package node_videos_render

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/jobs/learning/steps"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
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
	sagaID, _ := jc.PayloadUUID("saga_id")

	heartbeatSec := getEnvInt("NODE_VIDEOS_RENDER_HEARTBEAT_SECONDS", 5)
	if heartbeatSec < 1 {
		heartbeatSec = 1
	}
	if heartbeatSec > 10 {
		heartbeatSec = 10
	}

	var (
		currentPct int32 = 2
		currentMsg atomic.Value
	)
	currentMsg.Store("Rendering videos")

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
				msgAny := currentMsg.Load()
				msg, _ := msgAny.(string)
				if strings.TrimSpace(msg) == "" {
					msg = "Rendering videos"
				}
				jc.Progress("videos_render", int(atomic.LoadInt32(&currentPct)), msg)
			}
		}
	}()
	defer stopTicker()

	jc.Progress("videos_render", 2, "Rendering videos")
	out, err := steps.NodeVideosRender(jc.Ctx, steps.NodeVideosRenderDeps{
		DB:        p.db,
		Log:       p.log,
		Path:      p.path,
		PathNodes: p.nodes,
		Videos:    p.videos,
		Assets:    p.assets,
		GenRuns:   p.genRuns,
		AI:        p.ai,
		Bucket:    p.bucket,
		Bootstrap: p.bootstrap,
	}, steps.NodeVideosRenderInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	stopTicker()
	if err != nil {
		jc.Fail("videos_render", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
		"videos_rendered": out.VideosRendered,
		"videos_existing": out.VideosExisting,
		"videos_failed":   out.VideosFailed,
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
