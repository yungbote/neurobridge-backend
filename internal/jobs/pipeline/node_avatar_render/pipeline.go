package node_avatar_render

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
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

	heartbeatSec := 20
	if v := strings.TrimSpace(os.Getenv("NODE_AVATAR_RENDER_HEARTBEAT_SECONDS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			heartbeatSec = n
		}
	}
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
				jc.Progress("node_avatar_render", 2, "Generating unit avatars")
			}
		}
	}()
	defer stopTicker()

	jc.Progress("node_avatar_render", 2, "Generating unit avatars")
	pathID, err := p.bootstrap.EnsurePath(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, jc.Job.OwnerUserID, setID)
	if err != nil {
		if p.log != nil {
			p.log.Warn("node_avatar_render bootstrap failed", "error", err, "material_set_id", setID.String())
		}
		jc.Succeed("done", map[string]any{
			"material_set_id": setID.String(),
			"skipped":         true,
		})
		return nil
	}

	out, err := learningmod.New(learningmod.UsecasesDeps{
		Log:       p.log,
		Path:      p.path,
		PathNodes: p.nodes,
		Avatar:    p.avatar,
	}).NodeAvatarRender(jc.Ctx, learningmod.NodeAvatarRenderInput{PathID: pathID})
	stopTicker()
	if err != nil {
		if p.log != nil {
			p.log.Warn("node_avatar_render failed", "error", err, "path_id", pathID.String())
		}
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"path_id":         pathID.String(),
		"generated":       out.Generated,
		"existing":        out.Existing,
		"failed":          out.Failed,
	})
	return nil
}
