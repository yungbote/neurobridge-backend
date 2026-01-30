package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type PathStructuralUnitBuildDeps struct {
	DB        *gorm.DB
	Log       *logger.Logger
	PathNodes repos.PathNodeRepo
	Concepts  repos.ConceptRepo
	PSUs      repos.PathStructuralUnitRepo
	Bootstrap services.LearningBuildBootstrapService
}

type PathStructuralUnitBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
}

type PathStructuralUnitBuildOutput struct {
	PathID uuid.UUID `json:"path_id"`
	Units  int       `json:"units"`
}

func PathStructuralUnitBuild(ctx context.Context, deps PathStructuralUnitBuildDeps, in PathStructuralUnitBuildInput) (PathStructuralUnitBuildOutput, error) {
	out := PathStructuralUnitBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.PathNodes == nil || deps.PSUs == nil || deps.Concepts == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("psu_build: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("psu_build: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("psu_build: missing material_set_id")
	}

	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	dbc := dbctx.Context{Ctx: ctx}
	nodes, err := deps.PathNodes.GetByPathIDs(dbc, []uuid.UUID{pathID})
	if err != nil {
		return out, err
	}
	if len(nodes) == 0 {
		return out, nil
	}

	concepts, err := deps.Concepts.GetByScope(dbc, "path", &pathID)
	if err != nil {
		return out, err
	}
	canonicalByKey := map[string]uuid.UUID{}
	for _, c := range concepts {
		if c == nil || c.ID == uuid.Nil {
			continue
		}
		key := normalizeConceptKey(c.Key)
		if key == "" {
			continue
		}
		cid := c.ID
		if c.CanonicalConceptID != nil && *c.CanonicalConceptID != uuid.Nil {
			cid = *c.CanonicalConceptID
		}
		if cid != uuid.Nil {
			canonicalByKey[key] = cid
		}
	}

	grouped := map[uuid.UUID][]*types.PathNode{}
	for _, n := range nodes {
		if n == nil || n.ID == uuid.Nil {
			continue
		}
		parent := uuid.Nil
		if n.ParentNodeID != nil && *n.ParentNodeID != uuid.Nil {
			parent = *n.ParentNodeID
		}
		grouped[parent] = append(grouped[parent], n)
	}

	units := 0
	if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tdbc := dbctx.Context{Ctx: ctx, Tx: tx}
		for _, group := range grouped {
			if len(group) < 2 {
				continue
			}
			sort.Slice(group, func(i, j int) bool { return group[i].Index < group[j].Index })

			memberIDs := make([]string, 0, len(group))
			derivedConceptIDs := map[uuid.UUID]bool{}
			for _, n := range group {
				memberIDs = append(memberIDs, n.ID.String())
				for _, k := range nodeConceptKeys(n) {
					if cid := canonicalByKey[normalizeConceptKey(k)]; cid != uuid.Nil {
						derivedConceptIDs[cid] = true
					}
				}
			}
			structureEnc := "sequence:" + strings.Join(memberIDs, ",")
			psuKey := deterministicKey(pathID.String() + "|sequence|" + structureEnc)

			derived := make([]string, 0, len(derivedConceptIDs))
			for cid := range derivedConceptIDs {
				derived = append(derived, cid.String())
			}
			sort.Strings(derived)

			row := &types.PathStructuralUnit{
				PathID:                     pathID,
				PatternKind:                "sequence",
				PsuKey:                     psuKey,
				MemberNodeIDs:              mustJSON(memberIDs),
				StructureEnc:               structureEnc,
				DerivedCanonicalConceptIDs: mustJSON(derived),
				UpdatedAt:                  time.Now().UTC(),
			}
			if err := deps.PSUs.Upsert(tdbc, row); err != nil {
				return err
			}
			units++
		}
		return nil
	}); err != nil {
		return out, err
	}

	out.Units = units
	return out, nil
}

func nodeConceptKeys(node *types.PathNode) []string {
	if node == nil || len(node.Metadata) == 0 || strings.TrimSpace(string(node.Metadata)) == "" || strings.TrimSpace(string(node.Metadata)) == "null" {
		return nil
	}
	var meta map[string]any
	if err := json.Unmarshal(node.Metadata, &meta); err != nil {
		return nil
	}
	keys := append(stringSliceFromAny(meta["concept_keys"]), stringSliceFromAny(meta["prereq_concept_keys"])...)
	return dedupeStrings(keys)
}

func deterministicKey(input string) string {
	return hashString(input)
}
