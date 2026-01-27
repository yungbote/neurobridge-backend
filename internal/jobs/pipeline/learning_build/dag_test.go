package learning_build

import "testing"

func TestStageDepsForPremiumGrouping(t *testing.T) {
	if !containsStageDep(pipelineStageDeps(nil, "path_intake"), "file_signature_build") {
		t.Fatalf("path_intake should depend on file_signature_build")
	}
	if !containsStageDep(pipelineStageDeps(nil, "path_grouping_refine"), "path_intake") {
		t.Fatalf("path_grouping_refine should depend on path_intake")
	}
	if !containsStageDep(pipelineStageDeps(nil, "path_structure_dispatch"), "path_grouping_refine") {
		t.Fatalf("path_structure_dispatch should depend on path_grouping_refine")
	}
}

func containsStageDep(deps []string, want string) bool {
	for _, dep := range deps {
		if dep == want {
			return true
		}
	}
	return false
}
