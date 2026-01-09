package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type WebResourcesSeedDeps struct {
	DB  *gorm.DB
	Log *logger.Logger

	Files  repos.MaterialFileRepo
	Path   repos.PathRepo
	Bucket gcp.BucketService

	Threads  repos.ChatThreadRepo
	Messages repos.ChatMessageRepo
	Notify   services.ChatNotifier

	AI   openai.Client
	Saga services.SagaService

	Bootstrap services.LearningBuildBootstrapService
}

type WebResourcesSeedInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	Prompt        string
	ThreadID      uuid.UUID
	JobID         uuid.UUID

	// WaitForUser controls whether we are allowed to pause for user consent.
	WaitForUser bool
}

type WebResourcesSeedOutput struct {
	PathID uuid.UUID `json:"path_id"`

	Skipped bool `json:"skipped"`
	Status  string `json:"status"` // "succeeded" | "waiting_user"
	Meta    any    `json:"meta,omitempty"`

	FilesCreated     int `json:"files_created"`
	ResourcesPlanned int `json:"resources_planned"`
	ResourcesFetched int `json:"resources_fetched"`
}

type webResourcePlanV1 struct {
	Resources []webResourceItemV1 `json:"resources"`
}

type webResourceItemV1 struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	Kind   string `json:"type"`
	Reason string `json:"reason"`
}

type webResourcePlanV2 struct {
	Resources []webResourceItemV2   `json:"resources"`
	Coverage  webResourceCoverageV2 `json:"coverage"`
}

type webResourceItemV2 struct {
	Title             string   `json:"title"`
	URL               string   `json:"url"`
	Kind              string   `json:"type"`
	Reason            string   `json:"reason"`
	CoversSectionKeys []string `json:"covers_section_keys"`
}

type webResourceCoverageV2 struct {
	CoveredSectionKeys []string `json:"covered_section_keys"`
	MissingSectionKeys []string `json:"missing_section_keys"`
}

func WebResourcesSeed(ctx context.Context, deps WebResourcesSeedDeps, in WebResourcesSeedInput) (WebResourcesSeedOutput, error) {
	out := WebResourcesSeedOutput{Status: "succeeded"}
	if deps.DB == nil || deps.Log == nil || deps.Files == nil || deps.Path == nil || deps.Bucket == nil || deps.AI == nil || deps.Saga == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("web_resources_seed: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("web_resources_seed: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("web_resources_seed: missing material_set_id")
	}
	if in.SagaID == uuid.Nil {
		return out, fmt.Errorf("web_resources_seed: missing saga_id")
	}

	pathID, err := deps.Bootstrap.EnsurePath(dbctx.Context{Ctx: ctx}, in.OwnerUserID, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, in.MaterialSetID)
	if err != nil {
		return out, err
	}

	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		// Upload-only build: do not fail; downstream stages can operate on uploaded materials.
		out.Skipped = true
		return out, nil
	}

	// Always ensure a small seed file exists so downstream stages never see an empty material set.
	createdGoal, err := deps.ensureGoalFile(ctx, in, prompt, files)
	if err != nil {
		return out, err
	}
	out.FilesCreated += createdGoal

	enabled := envBool("WEB_RESOURCES_ENABLED", true)
	if !enabled {
		deps.Log.Info("WEB_RESOURCES_ENABLED=false; skipping web fetch")
		out.Skipped = true
		return out, nil
	}

	// If we already seeded web_* files for this set, treat as completed (idempotent).
	if hasAnyWebResourceFile(files) {
		out.Skipped = true
		return out, nil
	}

	hasUploads := hasUserUploadedFiles(files)
	if hasUploads && !shouldAugmentUploadsWithWeb(prompt) && !envBool("WEB_RESOURCES_AUGMENT_UPLOADS", false) {
		// Prompt exists but the user likely intended "use my uploads" (or the prompt isn't mastery); keep uploads-only.
		out.Skipped = true
		_ = deps.persistWebPlanV2(ctx, pathID, CurriculumSpecV1{
			SchemaVersion:  1,
			Goal:           prompt,
			Domain:         "",
			CoverageTarget: InferCoverageTargetFromPrompt(prompt),
			Sections:       nil,
		}, webResourcePlanV2{}, 0, "uploads_only")
		return out, nil
	}

	// Production polish: permissioned web enrichment. We never fetch external resources without explicit consent.
	// If we can't ask (no thread), default to uploads-only rather than surprising the user.
	if envBool("WEB_RESOURCES_REQUIRE_CONSENT", true) {
		consentAllowed, consentStatus, consentMeta, err := ensureWebResourcesConsent(ctx, deps, in, pathID, prompt, hasUploads)
		if err != nil {
			return out, err
		}
		if consentMeta != nil {
			out.Meta = consentMeta
		}
		if consentStatus == "waiting_user" {
			out.Status = "waiting_user"
			return out, nil
		}
		if !consentAllowed {
			out.Skipped = true
			_ = deps.persistWebPlanV2(ctx, pathID, CurriculumSpecV1{
				SchemaVersion:  1,
				Goal:           prompt,
				Domain:         "",
				CoverageTarget: InferCoverageTargetFromPrompt(prompt),
				Sections:       nil,
			}, webResourcePlanV2{}, 0, "consent_denied")
			return out, nil
		}
	}

	spec, sErr := BuildCurriculumSpecV1(ctx, deps.AI, prompt)
	if sErr != nil {
		deps.Log.Warn("web_resources_seed: curriculum spec generation failed; falling back to v1 planner", "error", sErr)
		planV1, err := buildWebResourcePlan(ctx, deps, prompt)
		if err != nil {
			// Non-fatal: the rest of the pipeline can still run from the prompt seed file.
			deps.Log.Warn("web_resources_seed: plan generation failed; continuing with prompt-only", "error", err)
			out.Skipped = true
			return out, nil
		}
		out.ResourcesPlanned = len(planV1.Resources)
		return deps.fetchAndPersistPlanV1(ctx, in, pathID, planV1, &out)
	}

	plan, err := buildWebResourcePlanV2(ctx, deps, spec)
	if err != nil {
		// Non-fatal: the rest of the pipeline can still run from the prompt seed file.
		deps.Log.Warn("web_resources_seed: v2 plan generation failed; continuing with prompt-only", "error", err)
		out.Skipped = true
		return out, nil
	}
	out.ResourcesPlanned = len(plan.Resources)
	if len(plan.Resources) == 0 {
		out.Skipped = true
		return out, nil
	}

	maxFetch := envInt("WEB_RESOURCES_MAX_FETCH", 10)
	if maxFetch < 1 {
		maxFetch = 1
	}
	if strings.EqualFold(strings.TrimSpace(spec.CoverageTarget), "mastery") && maxFetch < 14 {
		maxFetch = 14
	}
	maxBytes := int64(envInt("WEB_RESOURCES_MAX_BYTES", 2*1024*1024))
	if maxBytes < 64*1024 {
		maxBytes = 64 * 1024
	}

	client := newWebHTTPClient()

	// Fetch and persist resources (best-effort; we don't fail the stage if some URLs fail).
	fetched := 0
	requiredSections := make([]string, 0, len(spec.Sections))
	for _, s := range spec.Sections {
		if strings.TrimSpace(s.Key) != "" {
			requiredSections = append(requiredSections, strings.TrimSpace(s.Key))
		}
	}
	requiredSections = dedupeStrings(requiredSections)
	selected := selectWebResourcesForCoverage(plan.Resources, requiredSections, maxFetch)

	for _, r := range selected {
		if fetched >= maxFetch {
			break
		}
		u := strings.TrimSpace(r.URL)
		if u == "" {
			continue
		}
		if !isAllowedWebURL(ctx, u) {
			deps.Log.Warn("web_resources_seed: blocked url", "url", u)
			continue
		}

		body, ctype, finalURL, ferr := fetchURL(ctx, client, u, maxBytes)
		if ferr != nil {
			deps.Log.Warn("web_resources_seed: fetch failed", "url", u, "error", ferr)
			continue
		}
		if len(body) == 0 {
			continue
		}

		// Reuse the v1 naming logic (title/url/kind fields are identical).
		name, mimeType := normalizeFetchedNameAndMime(webResourceItemV1{
			Title:  r.Title,
			URL:    r.URL,
			Kind:   r.Kind,
			Reason: r.Reason,
		}, finalURL, ctype)
		payload := body
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "text/") {
			// Prefix content with provenance so the ingestion extractor preserves it even when stripping HTML tags.
			provenance := fmt.Sprintf(
				"SOURCE_URL: %s\nSOURCE_TITLE: %s\n\n",
				strings.TrimSpace(finalURL),
				strings.TrimSpace(r.Title),
			)
			payload = append([]byte(provenance), body...)
		}

		n, uErr := deps.createMaterialFileFromBytes(ctx, in, name, mimeType, payload)
		if uErr != nil {
			deps.Log.Warn("web_resources_seed: failed to persist resource", "url", u, "error", uErr)
			continue
		}
		out.FilesCreated += n
		fetched++
	}
	out.ResourcesFetched = fetched

	// Record the plan (debuggability). Best-effort; failure shouldn't fail the stage.
	_ = deps.persistWebPlanV2(ctx, pathID, spec, plan, fetched, stringsOr(spec.CoverageTarget, "unknown"))

	return out, nil
}

func shouldSkipWebSeed(files []*types.MaterialFile) bool {
	if len(files) == 0 {
		return false
	}
	// If we've already created any web_* material file, we consider this stage completed for this set.
	for _, f := range files {
		if f == nil {
			continue
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(f.OriginalName)), "web_") {
			return true
		}
	}
	// Otherwise, assume this material set came from user uploads.
	// The only exception is a single seed goal file from a previous partial run.
	if len(files) == 1 {
		name := strings.ToLower(strings.TrimSpace(files[0].OriginalName))
		if name == "learning_goal.txt" || name == "learning_goal.md" {
			return false
		}
	}
	return true
}

func hasAnyWebResourceFile(files []*types.MaterialFile) bool {
	for _, f := range files {
		if f == nil {
			continue
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(f.OriginalName)), "web_") {
			return true
		}
	}
	return false
}

func hasUserUploadedFiles(files []*types.MaterialFile) bool {
	for _, f := range files {
		if f == nil {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(f.OriginalName))
		if name == "" {
			continue
		}
		if strings.HasPrefix(name, "web_") {
			continue
		}
		if name == "learning_goal.txt" || name == "learning_goal.md" {
			continue
		}
		return true
	}
	return false
}

func shouldAugmentUploadsWithWeb(prompt string) bool {
	// Conservative heuristic: only auto-augment uploads when the user clearly asks for full coverage.
	return InferCoverageTargetFromPrompt(prompt) == "mastery"
}

func ensureWebResourcesConsent(
	ctx context.Context,
	deps WebResourcesSeedDeps,
	in WebResourcesSeedInput,
	pathID uuid.UUID,
	prompt string,
	hasUploads bool,
) (allowed bool, status string, meta any, err error) {
	status = "succeeded"

	allowedPtr, _ := loadWebResourcesConsentFromPathMeta(ctx, deps, pathID)
	if allowedPtr != nil {
		return *allowedPtr, status, nil, nil
	}

	// If we can't ask, default to "no" and record it (so we don't keep re-checking).
	if in.ThreadID == uuid.Nil || deps.Threads == nil || deps.Messages == nil || deps.DB == nil {
		_ = persistWebResourcesConsent(ctx, deps, pathID, false, "no_thread_or_chat_deps")
		return false, status, map[string]any{"reason": "no_thread_or_chat_deps"}, nil
	}

	msgs, _ := deps.Messages.ListByThread(dbctx.Context{Ctx: ctx}, in.ThreadID, 300)
	qMsg := latestWebResourcesConsentMessage(msgs)
	if qMsg != nil {
		answer := userAnswerAfter(msgs, qMsg.Seq)
		if strings.TrimSpace(answer) != "" {
			parsed, ok := parseYesNo(answer)
			if ok {
				_ = persistWebResourcesConsent(ctx, deps, pathID, parsed, "user")
				return parsed, status, nil, nil
			}
			if !in.WaitForUser {
				_ = persistWebResourcesConsent(ctx, deps, pathID, false, "ambiguous_user_answer")
				return false, status, map[string]any{"reason": "ambiguous_user_answer"}, nil
			}
		}
	}

	if !in.WaitForUser {
		_ = persistWebResourcesConsent(ctx, deps, pathID, false, "non_interactive_default")
		return false, status, map[string]any{"reason": "non_interactive_default"}, nil
	}

	question := buildWebResourcesConsentQuestion(prompt, hasUploads)
	asked, askErr := appendWebResourcesConsentMessage(ctx, deps, in.OwnerUserID, in.ThreadID, in.JobID, in.MaterialSetID, pathID, question)
	if askErr != nil {
		// If we failed to ask, do not block the build.
		return false, status, map[string]any{"reason": "failed_to_ask", "error": askErr.Error()}, nil
	}

	return false, "waiting_user", map[string]any{
		"reason":       "awaiting_user_consent",
		"question_id":  asked.ID.String(),
		"question_seq": asked.Seq,
	}, nil
}

func loadWebResourcesConsentFromPathMeta(ctx context.Context, deps WebResourcesSeedDeps, pathID uuid.UUID) (*bool, error) {
	if deps.Path == nil || pathID == uuid.Nil {
		return nil, nil
	}
	row, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID)
	if err != nil || row == nil {
		return nil, err
	}
	if len(row.Metadata) == 0 || strings.TrimSpace(string(row.Metadata)) == "" || strings.TrimSpace(string(row.Metadata)) == "null" {
		return nil, nil
	}
	var meta map[string]any
	if err := json.Unmarshal(row.Metadata, &meta); err != nil || meta == nil {
		return nil, nil
	}
	raw, ok := meta["web_resources_consent"]
	if !ok || raw == nil {
		return nil, nil
	}
	m, ok := raw.(map[string]any)
	if !ok || m == nil {
		return nil, nil
	}
	switch v := m["allowed"].(type) {
	case bool:
		return &v, nil
	default:
		return nil, nil
	}
}

func persistWebResourcesConsent(ctx context.Context, deps WebResourcesSeedDeps, pathID uuid.UUID, allowed bool, source string) error {
	if deps.Path == nil || pathID == uuid.Nil {
		return nil
	}
	row, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID)
	if err != nil || row == nil {
		return err
	}
	meta := map[string]any{}
	if len(row.Metadata) > 0 && strings.TrimSpace(string(row.Metadata)) != "" && strings.TrimSpace(string(row.Metadata)) != "null" {
		_ = json.Unmarshal(row.Metadata, &meta)
	}
	meta["web_resources_consent"] = map[string]any{
		"allowed":     allowed,
		"source":      strings.TrimSpace(source),
		"updated_at":  time.Now().UTC().Format(time.RFC3339Nano),
		"version":     1,
		"description": "Controls whether Neurobridge may fetch external web resources for this path.",
	}
	return deps.Path.UpdateFields(dbctx.Context{Ctx: ctx}, pathID, map[string]any{
		"metadata": mustJSON(meta),
	})
}

func latestWebResourcesConsentMessage(msgs []*types.ChatMessage) *types.ChatMessage {
	var best *types.ChatMessage
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if messageKind(m) != "web_resources_consent" {
			continue
		}
		if best == nil || m.Seq > best.Seq {
			best = m
		}
	}
	return best
}

func parseYesNo(text string) (bool, bool) {
	s := strings.ToLower(strings.TrimSpace(text))
	if s == "" {
		return false, false
	}
	if s == "y" || s == "yes" || s == "yeah" || s == "yep" || s == "sure" || s == "ok" || s == "okay" {
		return true, true
	}
	if s == "n" || s == "no" || s == "nope" {
		return false, true
	}
	if strings.Contains(s, "don't") || strings.Contains(s, "do not") || strings.Contains(s, "no ") || strings.Contains(s, "skip") {
		return false, true
	}
	if strings.Contains(s, "yes") || strings.Contains(s, "go ahead") || strings.Contains(s, "do it") || strings.Contains(s, "fetch") || strings.Contains(s, "sounds good") {
		return true, true
	}
	return false, false
}

func buildWebResourcesConsentQuestion(prompt string, hasUploads bool) string {
	goal := strings.TrimSpace(prompt)
	if goal == "" {
		goal = "(not provided)"
	}
	mode := "your prompt"
	if hasUploads {
		mode = "your uploads + prompt"
	}
	return strings.TrimSpace(strings.Join([]string{
		"I can optionally pull in a handful of high-quality web sources to fill gaps and cross-check while I build your learning path.",
		"",
		"**What this does**",
		"- Adds a few curated sources (articles / docs / open course pages) alongside " + mode + ".",
		"- Helps when your materials are sparse, incomplete, or you want mastery-level coverage.",
		"",
		"**Your goal**",
		goal,
		"",
		"Reply **yes** to allow web enrichment, or **no** to use only your provided materials.",
	}, "\n"))
}

func appendWebResourcesConsentMessage(
	ctx context.Context,
	deps WebResourcesSeedDeps,
	owner uuid.UUID,
	threadID uuid.UUID,
	jobID uuid.UUID,
	materialSetID uuid.UUID,
	pathID uuid.UUID,
	content string,
) (*types.ChatMessage, error) {
	if deps.DB == nil || deps.Threads == nil || deps.Messages == nil {
		return nil, fmt.Errorf("web_resources_seed: missing chat deps")
	}
	if owner == uuid.Nil || threadID == uuid.Nil || jobID == uuid.Nil {
		return nil, fmt.Errorf("web_resources_seed: missing ids")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("web_resources_seed: empty consent prompt")
	}

	var created *types.ChatMessage
	createdNew := false

	err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		inner := dbctx.Context{Ctx: ctx, Tx: tx}
		th, err := deps.Threads.LockByID(inner, threadID)
		if err != nil {
			return err
		}
		if th == nil || th.ID == uuid.Nil || th.UserID != owner {
			return fmt.Errorf("thread not found")
		}

		// Idempotency: one consent message per job.
		var existing types.ChatMessage
		e := tx.WithContext(ctx).
			Model(&types.ChatMessage{}).
			Where("thread_id = ? AND user_id = ? AND metadata->>'kind' = ? AND metadata->>'job_id' = ?", threadID, owner, "web_resources_consent", jobID.String()).
			First(&existing).Error
		if e == nil && existing.ID != uuid.Nil {
			created = &existing
			return nil
		}
		if e != nil && e != gorm.ErrRecordNotFound {
			return e
		}

		now := time.Now().UTC()
		meta := map[string]any{
			"kind":            "web_resources_consent",
			"job_id":          jobID.String(),
			"path_id":         pathID.String(),
			"material_set_id": materialSetID.String(),
		}
		metaJSON, _ := json.Marshal(meta)

		nextSeq := th.NextSeq + 1
		msg := &types.ChatMessage{
			ID:        uuid.New(),
			ThreadID:  threadID,
			UserID:    owner,
			Seq:       nextSeq,
			Role:      "assistant",
			Status:    "sent",
			Content:   content,
			Model:     "",
			Metadata:  datatypes.JSON(metaJSON),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if _, err := deps.Messages.Create(inner, []*types.ChatMessage{msg}); err != nil {
			return err
		}
		if err := deps.Threads.UpdateFields(inner, threadID, map[string]interface{}{
			"next_seq":        nextSeq,
			"last_message_at": now,
			"updated_at":      now,
		}); err != nil {
			return err
		}

		created = msg
		createdNew = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	if createdNew && created != nil && deps.Notify != nil {
		deps.Notify.MessageCreated(owner, threadID, created, nil)
	}
	return created, nil
}

func (deps WebResourcesSeedDeps) ensureGoalFile(ctx context.Context, in WebResourcesSeedInput, prompt string, files []*types.MaterialFile) (int, error) {
	for _, f := range files {
		if f == nil {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(f.OriginalName))
		if name == "learning_goal.txt" || name == "learning_goal.md" {
			return 0, nil
		}
	}
	goal := strings.TrimSpace(prompt)
	if goal == "" {
		return 0, nil
	}
	content := []byte("LEARNING_GOAL:\n" + goal + "\n")
	return deps.createMaterialFileFromBytes(ctx, in, "learning_goal.txt", "text/plain", content)
}

func (deps WebResourcesSeedDeps) createMaterialFileFromBytes(
	ctx context.Context,
	in WebResourcesSeedInput,
	originalName string,
	mimeType string,
	data []byte,
) (int, error) {
	if strings.TrimSpace(originalName) == "" {
		originalName = "web_resource.txt"
	}
	if strings.TrimSpace(mimeType) == "" {
		mimeType = "text/plain"
	}
	now := time.Now().UTC()
	fileID := uuid.New()
	storageKey := fmt.Sprintf("materials/%s/%s", in.MaterialSetID.String(), fileID.String())

	row := &types.MaterialFile{
		ID:            fileID,
		MaterialSetID: in.MaterialSetID,
		OriginalName:  originalName,
		MimeType:      mimeType,
		SizeBytes:     int64(len(data)),
		StorageKey:    storageKey,
		Status:        "pending_upload",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		if _, err := deps.Files.Create(dbc, []*types.MaterialFile{row}); err != nil {
			return err
		}

		if err := deps.Bucket.UploadFile(dbc, gcp.BucketCategoryMaterial, storageKey, bytes.NewReader(data)); err != nil {
			return err
		}

		if err := deps.Saga.AppendAction(dbc, in.SagaID, services.SagaActionKindGCSDeleteKey, map[string]any{
			"category": "material",
			"key":      storageKey,
		}); err != nil {
			return err
		}

		fileURL := deps.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, storageKey)
		if err := tx.WithContext(ctx).Model(&types.MaterialFile{}).
			Where("id = ?", fileID).
			Updates(map[string]any{
				"status":     "uploaded",
				"file_url":   fileURL,
				"updated_at": time.Now().UTC(),
			}).Error; err != nil {
			return err
		}
		row.Status = "uploaded"
		row.FileURL = fileURL
		row.UpdatedAt = time.Now().UTC()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return 1, nil
}

func (deps WebResourcesSeedDeps) persistWebPlanV2(ctx context.Context, pathID uuid.UUID, spec CurriculumSpecV1, plan webResourcePlanV2, fetched int, mode string) error {
	if deps.Path == nil || pathID == uuid.Nil {
		return nil
	}
	row, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID)
	if err != nil || row == nil {
		return err
	}
	meta := map[string]any{}
	if len(row.Metadata) > 0 && strings.TrimSpace(string(row.Metadata)) != "" && strings.TrimSpace(string(row.Metadata)) != "null" {
		_ = json.Unmarshal(row.Metadata, &meta)
	}
	meta["web_resources_seed"] = map[string]any{
		"spec":      spec,
		"planned":   plan,
		"fetched":   fetched,
		"updated":   time.Now().UTC().Format(time.RFC3339Nano),
		"version":   "v2",
		"mode":      strings.TrimSpace(mode),
		"max_fetch": envInt("WEB_RESOURCES_MAX_FETCH", 10),
	}
	return deps.Path.UpdateFields(dbctx.Context{Ctx: ctx}, pathID, map[string]any{
		"metadata": mustJSON(meta),
	})
}

func (deps WebResourcesSeedDeps) fetchAndPersistPlanV1(ctx context.Context, in WebResourcesSeedInput, pathID uuid.UUID, plan webResourcePlanV1, out *WebResourcesSeedOutput) (WebResourcesSeedOutput, error) {
	if out == nil {
		out = &WebResourcesSeedOutput{}
	}
	maxFetch := envInt("WEB_RESOURCES_MAX_FETCH", 10)
	if maxFetch < 1 {
		maxFetch = 1
	}
	maxBytes := int64(envInt("WEB_RESOURCES_MAX_BYTES", 2*1024*1024))
	if maxBytes < 64*1024 {
		maxBytes = 64 * 1024
	}

	client := newWebHTTPClient()
	fetched := 0
	for _, r := range plan.Resources {
		if fetched >= maxFetch {
			break
		}
		u := strings.TrimSpace(r.URL)
		if u == "" {
			continue
		}
		if !isAllowedWebURL(ctx, u) {
			deps.Log.Warn("web_resources_seed: blocked url", "url", u)
			continue
		}

		body, ctype, finalURL, ferr := fetchURL(ctx, client, u, maxBytes)
		if ferr != nil {
			deps.Log.Warn("web_resources_seed: fetch failed", "url", u, "error", ferr)
			continue
		}
		if len(body) == 0 {
			continue
		}

		name, mimeType := normalizeFetchedNameAndMime(r, finalURL, ctype)
		payload := body
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "text/") {
			provenance := fmt.Sprintf(
				"SOURCE_URL: %s\nSOURCE_TITLE: %s\n\n",
				strings.TrimSpace(finalURL),
				strings.TrimSpace(r.Title),
			)
			payload = append([]byte(provenance), body...)
		}

		n, uErr := deps.createMaterialFileFromBytes(ctx, in, name, mimeType, payload)
		if uErr != nil {
			deps.Log.Warn("web_resources_seed: failed to persist resource", "url", u, "error", uErr)
			continue
		}
		out.FilesCreated += n
		fetched++
	}
	out.ResourcesFetched = fetched

	// Record v1 plan for debuggability.
	planV2 := webResourcePlanV2{
		Resources: make([]webResourceItemV2, 0, len(plan.Resources)),
		Coverage:  webResourceCoverageV2{},
	}
	for _, r := range plan.Resources {
		planV2.Resources = append(planV2.Resources, webResourceItemV2{
			Title:             r.Title,
			URL:               r.URL,
			Kind:              r.Kind,
			Reason:            r.Reason,
			CoversSectionKeys: nil,
		})
	}
	_ = deps.persistWebPlanV2(ctx, pathID, CurriculumSpecV1{
		SchemaVersion:  1,
		Goal:           strings.TrimSpace(in.Prompt),
		Domain:         "",
		CoverageTarget: InferCoverageTargetFromPrompt(in.Prompt),
		Sections:       nil,
	}, planV2, fetched, "v1_fallback")

	return *out, nil
}

func buildWebResourcePlan(ctx context.Context, deps WebResourcesSeedDeps, prompt string) (webResourcePlanV1, error) {
	out := webResourcePlanV1{}
	system := strings.TrimSpace(`
You are an expert curriculum researcher.

Task: propose a set of high-quality, FREE, publicly accessible web resources for learning.
Return ONLY JSON matching the provided schema.

Rules:
- Use ONLY https URLs.
- Prefer authoritative sources (official docs/specs, reputable references, university notes).
- Prefer open/free resources; avoid paywalled content.
- Ensure broad coverage from fundamentals to intermediate practice.
- Include a mix of: reference docs, beginner tutorial, exercises/problems, tooling/build, debugging, and style/best practices.
- Avoid duplicates (same URL or near-identical mirrors).
`)

	user := fmt.Sprintf(`LEARNING_GOAL:
%s

Return 8–14 resources.`, strings.TrimSpace(prompt))

	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"resources": map[string]any{
				"type":     "array",
				"minItems": 0,
				"maxItems": 20,
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"title":  map[string]any{"type": "string"},
						"url":    map[string]any{"type": "string"},
						"type":   map[string]any{"type": "string"},
						"reason": map[string]any{"type": "string"},
					},
					"required": []string{"title", "url", "type", "reason"},
				},
			},
		},
		"required": []string{"resources"},
	}

	obj, err := deps.AI.GenerateJSON(ctx, system, user, "web_resources_seed_v1", schema)
	if err != nil {
		return out, err
	}
	raw, _ := json.Marshal(obj)
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	out.Resources = normalizeWebResourceList(out.Resources)
	return out, nil
}

func buildWebResourcePlanV2(ctx context.Context, deps WebResourcesSeedDeps, spec CurriculumSpecV1) (webResourcePlanV2, error) {
	out := webResourcePlanV2{}
	goal := strings.TrimSpace(spec.Goal)
	if goal == "" {
		return out, fmt.Errorf("buildWebResourcePlanV2: missing spec goal")
	}

	sectionItems := make([]map[string]any, 0, len(spec.Sections))
	for _, s := range spec.Sections {
		sectionItems = append(sectionItems, map[string]any{
			"key":         strings.TrimSpace(s.Key),
			"title":       strings.TrimSpace(s.Title),
			"description": strings.TrimSpace(s.Description),
		})
	}
	sectionsJSON, _ := json.Marshal(map[string]any{"sections": sectionItems})

	system := strings.TrimSpace(`
You are an expert curriculum researcher.

Task: propose a set of high-quality, FREE, publicly accessible web resources for learning.
Return ONLY JSON matching the provided schema.

Rules:
- Use ONLY https URLs.
- Prefer authoritative sources (official docs/specs, reputable references, university notes).
- Prefer open/free resources; avoid paywalled content.
- Avoid duplicates (same URL or near-identical mirrors).
- The plan MUST cover the curriculum sections; missing_section_keys should only be non-empty if it is truly impossible.
- If the domain appears to be a programming language, ensure you include at least:
  - a beginner-friendly tutorial/guide
  - a reference (language + standard library)
  - a exercises/practice source
  - and at least one advanced/edge-cases resource.
`)

	target := strings.TrimSpace(spec.CoverageTarget)
	if target == "" {
		target = InferCoverageTargetFromPrompt(goal)
	}
	minResources := 8
	maxResources := 14
	if strings.EqualFold(target, "mastery") {
		minResources = 12
		maxResources = 20
	}

	user := fmt.Sprintf(`LEARNING_GOAL:
%s

CURRICULUM_DOMAIN: %s
CURRICULUM_COVERAGE_TARGET: %s

CURRICULUM_SECTIONS_JSON:
%s

Return %d–%d resources.`, goal, strings.TrimSpace(spec.Domain), target, string(sectionsJSON), minResources, maxResources)

	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"resources": map[string]any{
				"type":     "array",
				"minItems": 0,
				"maxItems": 24,
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"title":               map[string]any{"type": "string"},
						"url":                 map[string]any{"type": "string"},
						"type":                map[string]any{"type": "string"},
						"reason":              map[string]any{"type": "string"},
						"covers_section_keys": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
					"required": []string{"title", "url", "type", "reason", "covers_section_keys"},
				},
			},
			"coverage": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"covered_section_keys": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"missing_section_keys": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
				"required": []string{"covered_section_keys", "missing_section_keys"},
			},
		},
		"required": []string{"resources", "coverage"},
	}

	obj, err := deps.AI.GenerateJSON(ctx, system, user, "web_resources_seed_v2", schema)
	if err != nil {
		return out, err
	}
	raw, _ := json.Marshal(obj)
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	out.Resources = normalizeWebResourceListV2(out.Resources)
	out.Coverage.CoveredSectionKeys = dedupeStrings(out.Coverage.CoveredSectionKeys)
	out.Coverage.MissingSectionKeys = dedupeStrings(out.Coverage.MissingSectionKeys)
	return out, nil
}

func normalizeWebResourceList(in []webResourceItemV1) []webResourceItemV1 {
	seen := map[string]bool{}
	out := make([]webResourceItemV1, 0, len(in))
	for _, r := range in {
		u := strings.TrimSpace(r.URL)
		if u == "" {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(u), "https://") {
			continue
		}
		if seen[u] {
			continue
		}
		seen[u] = true
		r.URL = u
		r.Title = strings.TrimSpace(r.Title)
		r.Reason = strings.TrimSpace(r.Reason)
		r.Kind = strings.TrimSpace(r.Kind)
		if r.Title == "" {
			r.Title = "Resource"
		}
		out = append(out, r)
	}
	return out
}

func normalizeWebResourceListV2(in []webResourceItemV2) []webResourceItemV2 {
	seen := map[string]bool{}
	out := make([]webResourceItemV2, 0, len(in))
	for _, r := range in {
		u := strings.TrimSpace(r.URL)
		if u == "" {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(u), "https://") {
			continue
		}
		if seen[u] {
			continue
		}
		seen[u] = true
		r.URL = u
		r.Title = strings.TrimSpace(r.Title)
		r.Reason = strings.TrimSpace(r.Reason)
		r.Kind = strings.TrimSpace(r.Kind)
		r.CoversSectionKeys = dedupeStrings(r.CoversSectionKeys)
		if r.Title == "" {
			r.Title = "Resource"
		}
		out = append(out, r)
	}
	return out
}

func selectWebResourcesForCoverage(resources []webResourceItemV2, requiredSectionKeys []string, maxFetch int) []webResourceItemV2 {
	if maxFetch <= 0 {
		return nil
	}
	if len(resources) == 0 {
		return nil
	}
	required := map[string]bool{}
	for _, k := range requiredSectionKeys {
		k = strings.TrimSpace(k)
		if k != "" {
			required[k] = true
		}
	}
	uncovered := map[string]bool{}
	for k := range required {
		uncovered[k] = true
	}

	selected := make([]webResourceItemV2, 0, min(maxFetch, len(resources)))
	used := make([]bool, len(resources))

	for len(uncovered) > 0 && len(selected) < maxFetch {
		bestIdx := -1
		bestGain := 0
		for i, r := range resources {
			if used[i] {
				continue
			}
			gain := 0
			for _, k := range r.CoversSectionKeys {
				k = strings.TrimSpace(k)
				if k == "" {
					continue
				}
				if uncovered[k] {
					gain++
				}
			}
			if gain > bestGain {
				bestGain = gain
				bestIdx = i
			}
		}
		if bestIdx < 0 || bestGain == 0 {
			break
		}
		used[bestIdx] = true
		selected = append(selected, resources[bestIdx])
		for _, k := range resources[bestIdx].CoversSectionKeys {
			delete(uncovered, strings.TrimSpace(k))
		}
	}

	// Fill remaining slots in the original plan order.
	if len(selected) < maxFetch {
		for i, r := range resources {
			if used[i] {
				continue
			}
			selected = append(selected, r)
			if len(selected) >= maxFetch {
				break
			}
		}
	}

	return selected
}

func newWebHTTPClient() *http.Client {
	timeout := 25 * time.Second
	if v := strings.TrimSpace(os.Getenv("WEB_RESOURCES_HTTP_TIMEOUT_SECONDS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = time.Duration(n) * time.Second
		}
	}
	c := &http.Client{Timeout: timeout}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 6 {
			return fmt.Errorf("too many redirects")
		}
		if req == nil || req.URL == nil {
			return fmt.Errorf("redirect missing url")
		}
		if !isAllowedWebURL(context.Background(), req.URL.String()) {
			return fmt.Errorf("redirect blocked: %s", req.URL.String())
		}
		return nil
	}
	return c
}

func fetchURL(ctx context.Context, client *http.Client, rawURL string, maxBytes int64) ([]byte, string, string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	u := strings.TrimSpace(rawURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("User-Agent", "NeurobridgeBot/1.0 (learning path builder)")
	req.Header.Set("Accept", "text/html, text/plain, application/pdf;q=0.9, */*;q=0.1")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", fmt.Errorf("http %d", resp.StatusCode)
	}

	ctype := strings.TrimSpace(resp.Header.Get("Content-Type"))
	mediaType := ""
	if ctype != "" {
		if mt, _, err := mime.ParseMediaType(ctype); err == nil {
			mediaType = mt
		}
	}

	limited := io.LimitReader(resp.Body, maxBytes+1)
	b, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", "", err
	}
	if int64(len(b)) > maxBytes {
		return nil, "", "", fmt.Errorf("response too large (%d > %d)", len(b), maxBytes)
	}
	if mediaType == "" && len(b) > 0 {
		mediaType = http.DetectContentType(b[:min(512, len(b))])
	}
	finalURL := u
	if resp.Request != nil && resp.Request.URL != nil && strings.TrimSpace(resp.Request.URL.String()) != "" {
		finalURL = strings.TrimSpace(resp.Request.URL.String())
	}
	return b, mediaType, finalURL, nil
}

func normalizeFetchedNameAndMime(r webResourceItemV1, finalURL string, contentType string) (string, string) {
	u := strings.TrimSpace(finalURL)
	title := strings.TrimSpace(r.Title)
	ct := strings.ToLower(strings.TrimSpace(contentType))

	ext := ""
	switch {
	case strings.Contains(ct, "application/pdf") || strings.HasSuffix(strings.ToLower(u), ".pdf"):
		ext = ".pdf"
		ct = "application/pdf"
	case strings.Contains(ct, "text/html"):
		ext = ".html"
		ct = "text/html"
	case strings.Contains(ct, "text/plain"):
		ext = ".txt"
		ct = "text/plain"
	default:
		// default to html-ish extraction
		ext = ".html"
		if ct == "" {
			ct = "text/html"
		}
	}

	slug := slugify(title)
	if slug == "" {
		slug = "resource"
	}
	host := safeHostForName(u)
	name := fmt.Sprintf("web_%s_%s%s", host, slug, ext)
	// Avoid pathological lengths / weird extensions.
	name = strings.TrimSpace(name)
	if len(name) > 120 {
		name = name[:120]
	}
	if filepath.Ext(name) == "" {
		name += ext
	}
	return name, ct
}

func safeHostForName(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil {
		return "site"
	}
	h := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if h == "" {
		return "site"
	}
	h = strings.TrimPrefix(h, "www.")
	h = strings.ReplaceAll(h, ".", "_")
	if len(h) > 40 {
		h = h[:40]
	}
	return h
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == ' ' || r == '-' || r == '_' || r == '/':
			return '_'
		default:
			return -1
		}
	}, s)
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	s = strings.Trim(s, "_")
	if len(s) > 48 {
		s = s[:48]
	}
	return s
}

func isAllowedWebURL(ctx context.Context, raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return false
	}
	if strings.ToLower(u.Scheme) != "https" {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !isPrivateIP(ip)
	}

	// Best-effort: resolve and block private IPs (SSRF hardening).
	resCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(resCtx, "ip", host)
	if err != nil || len(ips) == 0 {
		// If we can't resolve, treat as blocked (safer default).
		return false
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return false
		}
	}
	return true
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	ip = ip.To4()
	if ip == nil {
		// IPv6: conservatively treat as private unless explicitly global unicast.
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() {
		return true
	}
	// 10.0.0.0/8
	if ip[0] == 10 {
		return true
	}
	// 172.16.0.0/12
	if ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 {
		return true
	}
	// 192.168.0.0/16
	if ip[0] == 192 && ip[1] == 168 {
		return true
	}
	// 127.0.0.0/8
	if ip[0] == 127 {
		return true
	}
	// 169.254.0.0/16 (link local)
	if ip[0] == 169 && ip[1] == 254 {
		return true
	}
	return false
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	v = strings.ToLower(v)
	if v == "1" || v == "true" || v == "yes" || v == "y" || v == "on" {
		return true
	}
	if v == "0" || v == "false" || v == "no" || v == "n" || v == "off" {
		return false
	}
	return def
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Ensure deterministic ordering for any later uses (debuggability only).
func (p *webResourcePlanV1) sort() {
	if p == nil || len(p.Resources) == 0 {
		return
	}
	sort.Slice(p.Resources, func(i, j int) bool {
		return strings.ToLower(strings.TrimSpace(p.Resources[i].URL)) < strings.ToLower(strings.TrimSpace(p.Resources[j].URL))
	})
}
