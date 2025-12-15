package pipelines

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/jobs"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

type buildContext struct {
	jobCtx				*jobs.Context
	ctx						context.Context
	userID				uuid.UUID
	materialSetID	uuid.UUID
	courseID			uuid.UUID
	course				*types.Course
	files					[]*types.MaterialFile
	fileIDs				[]uuid.UUID
	chunks				[]*types.MaterialChunk
	combined			string
}

func (p *CourseBuildPipeline) Type() string { return "course_build" }

func (p *CourseBuildPipeline) Run(jobContext *jobs.Context) error {
	if jobContext == nil || jobContext.Job == nil {
		return nil
	}
	buildCtx := &buildContext{
		jobCtx:				jobContext,
		ctx:					jobContext.Ctx,
		userID:				jobContext.Job.OwnerUserID,
	}
	// 0) Validate + Load (Payload, Course, Files)
	if err := p.loadAndValidate(buildCtx); err != nil {
		p.fail(buildCtx, "validate", err)
		return nil
	}
	// 1) Ingest: Ensure Chunks Exist
	if err := p.stageIngest(buildCtx); err != nil {
		p.fail(buildCtx, "ingest", err)
		return nil
	}
	// 2) Embed: Ensure Chunk Embeddings Exist
	if err := p.stageEmbed(buildCtx); err != nil {
		p.fail(buildCtx, "embed", err)
		return nil
	}
	// Prepare combined text for later stages
	buildCtx.combined = buildCombinedFromChunks(buildCtx.chunks, 20000)
	if buildCtx.combined == "" {
		p.fail(buildCtx, "metadata", fmt.Errorf("no combined materials text available"))
		return nil
	}
	// 3) Metadata: Fill Course if Placeholder
	if err := p.stageMetadata(buildCtx); err != nil {
		p.fail(buildCtx, "metadata", err)
		return nil
	}
	// 4) Blueprint: Ensure Modules/Lessons Exist
	if err := p.stageBlueprint(buildCtx); err != nil {
		p.fail(buildCtx, "blueprint", err)
		return nil
	}
	// 5) Lessons + Quizzes
	if err := p.stageLessonsAndQuizzes(buildCtx); err != nil {
		p.fail(buildCtx, "lessons", err)
		return nil
	}
	// 6) Finalize
	if err := p.stageFinalize(buildCtx); err != nil {
		p.fail(buildCtx, "done", err)
		return nil
	}
	// Mark Job Succeesed + Emit JobDone (already inside stageFinalize we do snapshot)
	jobContext.Succeed("done", map[string]any{
		"course_id":					buildCtx.courseID.String(),
		"material_set_id":		buildCtx.materialSetID.String(),
	})
	// Emit Course-Domain Done Event (incudes full course + job)
	if p.courseNotify != nil {
		p.courseNotify.CourseGenerationDone(buildCtx.userID, buildCtx.course, buildCtx.jobCtx.Job)
	}
	return nil
}

func (p *CourseBuildPipeline) progress(buildCtx *buildContext, stage string, pct int, msg string) {
	if buildCtx == nil || buildCtx.jobCtx == nil {
		return
	}
	buildCtx.jobCtx.Progress(stage, pct, msg)
	if p.courseNotify != nil {
		p.courseNotify.CourseGenerationProgress(buildCtx.userID, buildCtx.course, buildCtx.jobCtx.Job, stage, pct, msg)
	}
}

func (p *CourseBuildPipeline) fail(buildCtx *buildContext, stage string, err error) {
	if buildCtx == nil || buildCtx.jobCtx == nil {
		return
	}
	buildCtx.jobCtx.Fail(stage, err)
	if p.courseNotify != nil {
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		p.courseNotify.CourseGenerationFailed(buildCtx.userID, buildCtx.course, buildCtx.jobCtx.Job, stage, msg)
	}
}

func (p *CourseBuildPipeline) snapshot(buildCtx *buildContext) {
	if buildCtx == nil || p.courseNotify == nil {
		return
	}
	p.courseNotify.CourseCreated(buildCtx.userID, buildCtx.course, buildCtx.jobCtx.Job)
}

func (p *CourseBuildPipeline) downloadMaterialFile(ctx context.Context, key string) ([]byte, error) {
	rc, err := p.bucket.DownloadFile(ctx, services.BucketCategoryMaterial, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return readAll(rc)
}










