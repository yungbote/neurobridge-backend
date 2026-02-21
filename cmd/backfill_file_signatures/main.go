package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/app"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type idList []string

func (l *idList) String() string { return strings.Join(*l, ",") }
func (l *idList) Set(v string) error {
	v = strings.TrimSpace(v)
	if v != "" {
		*l = append(*l, v)
	}
	return nil
}

func main() {
	var sets idList
	var dryRun bool
	var limit int
	flag.Var(&sets, "set", "material_set_id to backfill (repeatable)")
	flag.BoolVar(&dryRun, "dry-run", false, "print planned jobs without enqueueing")
	flag.IntVar(&limit, "limit", 0, "limit number of sets processed")
	flag.Parse()

	application, err := app.New()
	if err != nil {
		fmt.Printf("init app: %v\n", err)
		os.Exit(1)
	}
	defer application.Close()

	ctx := context.Background()
	dbc := dbctx.Context{Ctx: ctx}

	var rows []*types.MaterialSet
	if len(sets) > 0 {
		ids := make([]uuid.UUID, 0, len(sets))
		for _, s := range sets {
			id, err := uuid.Parse(strings.TrimSpace(s))
			if err == nil && id != uuid.Nil {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			fmt.Println("no valid material_set_id values provided")
			return
		}
		rows, err = application.Repos.Materials.MaterialSet.GetByIDs(dbc, ids)
	} else {
		err = application.DB.WithContext(ctx).Find(&rows).Error
	}
	if err != nil {
		fmt.Printf("load material sets: %v\n", err)
		os.Exit(1)
	}
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}

	enqueued := 0
	for _, set := range rows {
		if set == nil || set.ID == uuid.Nil {
			continue
		}
		files, _ := application.Repos.Materials.MaterialFile.GetByMaterialSetID(dbc, set.ID)
		if len(files) == 0 {
			continue
		}
		sigs, _ := application.Repos.Materials.MaterialFileSignature.GetByMaterialSetID(dbc, set.ID)
		if len(sigs) >= len(files) {
			continue
		}
		if dryRun {
			fmt.Printf("[dry-run] enqueue file_signature_build material_set_id=%s (missing %d signatures)\n", set.ID.String(), len(files)-len(sigs))
			continue
		}
		if application.Services.JobService == nil {
			fmt.Println("job service unavailable (TEMPORAL_ADDRESS missing)")
			os.Exit(1)
		}
		payload := map[string]any{
			"material_set_id": set.ID.String(),
		}
		_, err := application.Services.JobService.Enqueue(dbc, set.UserID, "file_signature_build", "material_set", &set.ID, payload)
		if err != nil {
			fmt.Printf("enqueue failed for set %s: %v\n", set.ID.String(), err)
			continue
		}
		enqueued++
		fmt.Printf("enqueued file_signature_build for material_set_id=%s\n", set.ID.String())
	}

	fmt.Printf("done; enqueued=%d\n", enqueued)
}
