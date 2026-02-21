package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type repoField struct {
	Name     string `json:"name"`
	RepoType string `json:"repo_type"`
	Domain   string `json:"domain"`
	Targeted bool   `json:"targeted"`
}

type methodStats struct {
	StructName                    string   `json:"struct_name"`
	Method                        string   `json:"method"`
	File                          string   `json:"file"`
	Line                          int      `json:"line"`
	TargetedRepoWriteCalls        int      `json:"targeted_repo_write_calls"`
	TargetedRepoFieldsWritten     []string `json:"targeted_repo_fields_written"`
	AggregateWriteCalls           int      `json:"aggregate_write_calls"`
	AggregateWriteMethodsObserved []string `json:"aggregate_write_methods_observed"`
}

type metricsReport struct {
	ServiceLayerTargetedRepoWriteCallsites    int           `json:"service_layer_targeted_repo_write_callsites"`
	ServiceMethodsCoordinating2PlusTargeted   int           `json:"service_methods_coordinating_2plus_targeted_repos"`
	InvariantFlowsAggregateOwnedTxCallsites   int           `json:"invariant_write_flows_aggregate_owned_tx_callsites"`
	Methods                                   []methodStats `json:"methods"`
	TargetedResidualMethods                   []methodStats `json:"targeted_residual_methods"`
	TargetedAggregateAdoptionMethods          []methodStats `json:"targeted_aggregate_adoption_methods"`
	TargetedRepoFieldInventory                []repoField   `json:"targeted_repo_field_inventory"`
	TargetedServiceStructsWithRepoFields      []string      `json:"targeted_service_structs_with_repo_fields"`
	TargetedServiceMethodsWithAnyRepoWrites   int           `json:"targeted_service_methods_with_any_repo_writes"`
	TargetedServiceMethodsWithAggregateWrites int           `json:"targeted_service_methods_with_aggregate_writes"`
}

type structFields struct {
	RepoFields      map[string]repoField
	AggregateFields map[string]string
}

var repoWriteMethods = map[string]bool{
	"Create":        true,
	"UpdateFields":  true,
	"UpdateByID":    true,
	"Upsert":        true,
	"Delete":        true,
	"DeleteByID":    true,
	"DeleteByUser":  true,
	"DeleteByIDs":   true,
	"PurgeByUserID": true,
	"Increment":     true,
	"Replace":       true,
	"LockByID":      true,
	"GetMaxSeq":     true,
}

var aggregateWriteMethods = map[string]bool{
	"ApplyEvidence":           true,
	"ResolveMisconception":    true,
	"StartPathRun":            true,
	"AdvancePathRun":          true,
	"ApplyTaxonomyRefinement": true,
	"CommitTaxonomySnapshot":  true,
	"CommitTurn":              true,
	"MarkTurnFailed":          true,
	"CommitRevision":          true,
	"RecordVariantOutcome":    true,
	"AppendAction":            true,
	"TransitionStatus":        true,
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	servicesDir := filepath.Join(root, "internal", "services")
	fset := token.NewFileSet()

	pkgs, err := parser.ParseDir(fset, servicesDir, func(fi os.FileInfo) bool {
		name := fi.Name()
		return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
	}, 0)
	if err != nil {
		exitf("parse dir: %v", err)
	}

	pkg, ok := pkgs["services"]
	if !ok {
		exitf("services package not found in %s", servicesDir)
	}

	fieldsByStruct := map[string]structFields{}
	for filePath, f := range pkg.Files {
		_ = filePath
		collectStructFields(f, fieldsByStruct)
	}

	var methods []methodStats
	for filePath, f := range pkg.Files {
		rel, err := filepath.Rel(root, filePath)
		if err != nil {
			rel = filePath
		}
		collectMethodStats(fset, f, rel, fieldsByStruct, &methods)
	}

	report := buildReport(fieldsByStruct, methods)
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		exitf("marshal report: %v", err)
	}
	fmt.Println(string(out))
}

func collectStructFields(file *ast.File, out map[string]structFields) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				continue
			}
			sf := structFields{
				RepoFields:      map[string]repoField{},
				AggregateFields: map[string]string{},
			}
			for _, field := range st.Fields.List {
				if len(field.Names) == 0 {
					continue
				}
				fieldName := field.Names[0].Name
				sel, ok := field.Type.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				pkgIdent, ok := sel.X.(*ast.Ident)
				if !ok {
					continue
				}
				switch pkgIdent.Name {
				case "repos":
					repoType := strings.TrimSpace(sel.Sel.Name)
					if !strings.HasSuffix(repoType, "Repo") {
						continue
					}
					domain, targeted := domainForRepoType(repoType)
					sf.RepoFields[fieldName] = repoField{
						Name:     fieldName,
						RepoType: repoType,
						Domain:   domain,
						Targeted: targeted,
					}
				case "domainagg":
					aggType := strings.TrimSpace(sel.Sel.Name)
					if !strings.HasSuffix(aggType, "Aggregate") {
						continue
					}
					sf.AggregateFields[fieldName] = aggType
				}
			}
			if len(sf.RepoFields) > 0 || len(sf.AggregateFields) > 0 {
				out[ts.Name.Name] = sf
			}
		}
	}
}

func collectMethodStats(
	fset *token.FileSet,
	file *ast.File,
	relFile string,
	fieldsByStruct map[string]structFields,
	out *[]methodStats,
) {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || fd.Body == nil || len(fd.Recv.List) == 0 {
			continue
		}

		recvName, recvType := recvInfo(fd.Recv.List[0])
		if recvType == "" || recvName == "" {
			continue
		}
		sf, ok := fieldsByStruct[recvType]
		if !ok {
			continue
		}

		targetedCalls := 0
		targetedFields := map[string]bool{}
		aggCalls := 0
		aggMethods := map[string]bool{}

		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fnSel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			rcvSel, ok := fnSel.X.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			baseIdent, ok := rcvSel.X.(*ast.Ident)
			if !ok || baseIdent.Name != recvName {
				return true
			}

			field := strings.TrimSpace(rcvSel.Sel.Name)
			method := strings.TrimSpace(fnSel.Sel.Name)

			if rf, ok := sf.RepoFields[field]; ok && rf.Targeted && repoWriteMethods[method] {
				targetedCalls++
				targetedFields[field] = true
				return true
			}

			if _, ok := sf.AggregateFields[field]; ok && aggregateWriteMethods[method] {
				aggCalls++
				aggMethods[method] = true
			}
			return true
		})

		line := fset.Position(fd.Pos()).Line
		method := methodStats{
			StructName:                    recvType,
			Method:                        fd.Name.Name,
			File:                          filepath.ToSlash(relFile),
			Line:                          line,
			TargetedRepoWriteCalls:        targetedCalls,
			TargetedRepoFieldsWritten:     sortedKeys(targetedFields),
			AggregateWriteCalls:           aggCalls,
			AggregateWriteMethodsObserved: sortedKeys(aggMethods),
		}
		*out = append(*out, method)
	}
}

func buildReport(fieldsByStruct map[string]structFields, methods []methodStats) metricsReport {
	sort.Slice(methods, func(i, j int) bool {
		if methods[i].File == methods[j].File {
			return methods[i].Line < methods[j].Line
		}
		return methods[i].File < methods[j].File
	})

	var report metricsReport
	report.Methods = methods

	targetedStructs := map[string]bool{}
	targetedRepoFields := map[string]repoField{}
	for structName, sf := range fieldsByStruct {
		for _, rf := range sf.RepoFields {
			if !rf.Targeted {
				continue
			}
			targetedStructs[structName] = true
			key := structName + "." + rf.Name
			targetedRepoFields[key] = rf
		}
	}

	for _, m := range methods {
		if m.TargetedRepoWriteCalls > 0 {
			report.ServiceLayerTargetedRepoWriteCallsites += m.TargetedRepoWriteCalls
			report.TargetedServiceMethodsWithAnyRepoWrites++
			report.TargetedResidualMethods = append(report.TargetedResidualMethods, m)
		}
		if len(m.TargetedRepoFieldsWritten) >= 2 {
			report.ServiceMethodsCoordinating2PlusTargeted++
		}
		if m.AggregateWriteCalls > 0 {
			report.InvariantFlowsAggregateOwnedTxCallsites += m.AggregateWriteCalls
			report.TargetedServiceMethodsWithAggregateWrites++
			report.TargetedAggregateAdoptionMethods = append(report.TargetedAggregateAdoptionMethods, m)
		}
	}

	report.TargetedServiceStructsWithRepoFields = sortedKeys(targetedStructs)

	fieldKeys := make([]string, 0, len(targetedRepoFields))
	for k := range targetedRepoFields {
		fieldKeys = append(fieldKeys, k)
	}
	sort.Strings(fieldKeys)
	for _, k := range fieldKeys {
		report.TargetedRepoFieldInventory = append(report.TargetedRepoFieldInventory, targetedRepoFields[k])
	}
	return report
}

func recvInfo(field *ast.Field) (string, string) {
	if field == nil || len(field.Names) == 0 {
		return "", ""
	}
	recvName := field.Names[0].Name
	switch t := field.Type.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return recvName, id.Name
		}
	case *ast.Ident:
		return recvName, t.Name
	}
	return "", ""
}

func domainForRepoType(repoType string) (string, bool) {
	rt := strings.TrimSpace(repoType)
	if rt == "" {
		return "Unknown", false
	}

	switch {
	case strings.HasPrefix(rt, "Chat"):
		return "Chat", true
	case strings.HasPrefix(rt, "Saga"), strings.HasPrefix(rt, "Job"):
		return "Jobs", true
	case strings.HasPrefix(rt, "Path"), strings.HasPrefix(rt, "NodeRun"), strings.HasPrefix(rt, "ActivityRun"):
		return "Paths", true
	case strings.HasPrefix(rt, "Library"), strings.HasPrefix(rt, "UserLibrary"):
		return "Library", true
	case strings.HasPrefix(rt, "LearningNodeDoc"), strings.HasPrefix(rt, "Doc"):
		return "DocGen", true
	case strings.HasPrefix(rt, "UserConcept"),
		strings.HasPrefix(rt, "ConceptReadiness"),
		strings.HasPrefix(rt, "PrereqGate"),
		strings.HasPrefix(rt, "Topic"),
		strings.HasPrefix(rt, "ItemCalibration"),
		strings.HasPrefix(rt, "UserModel"),
		strings.HasPrefix(rt, "UserMisconception"),
		strings.HasPrefix(rt, "Intervention"),
		strings.HasPrefix(rt, "UserSkill"),
		strings.HasPrefix(rt, "UserBelief"),
		strings.HasPrefix(rt, "Misconception"):
		return "Learning", true
	default:
		return "Other", false
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
