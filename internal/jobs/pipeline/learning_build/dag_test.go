package learning_build

import "testing"

func TestStageDepsForPremiumGrouping(t *testing.T) {
	if !containsStageDep(pipelineStageDeps(nil, "path_intake_pre"), "file_signature_build") {
		t.Fatalf("path_intake_pre should depend on file_signature_build")
	}
	if !containsStageDep(pipelineStageDeps(nil, "path_intake_waitpoint"), "path_intake_pre") {
		t.Fatalf("path_intake_waitpoint should depend on path_intake_pre")
	}
	if !containsStageDep(pipelineStageDeps(nil, "path_grouping_refine_pre"), "path_intake_waitpoint") {
		t.Fatalf("path_grouping_refine_pre should depend on path_intake_waitpoint")
	}
	if !containsStageDep(pipelineStageDeps(nil, "path_grouping_refine_waitpoint"), "path_grouping_refine_pre") {
		t.Fatalf("path_grouping_refine_waitpoint should depend on path_grouping_refine_pre")
	}
	if !containsStageDep(pipelineStageDeps(nil, "path_structure_dispatch"), "path_grouping_refine_waitpoint") {
		t.Fatalf("path_structure_dispatch should depend on path_grouping_refine_waitpoint")
	}
	if containsStageDep(pipelineStageDeps(nil, "node_doc_build"), "node_figures_render") {
		t.Fatalf("node_doc_build should not depend on node_figures_render")
	}
	if containsStageDep(pipelineStageDeps(nil, "node_doc_build"), "node_videos_render") {
		t.Fatalf("node_doc_build should not depend on node_videos_render")
	}
	if !containsStageDep(pipelineStageDeps(nil, "node_doc_media_patch"), "node_doc_build") {
		t.Fatalf("node_doc_media_patch should depend on node_doc_build")
	}
	if !containsStageDep(pipelineStageDeps(nil, "node_doc_media_patch"), "node_figures_render") {
		t.Fatalf("node_doc_media_patch should depend on node_figures_render")
	}
	if !containsStageDep(pipelineStageDeps(nil, "node_doc_media_patch"), "node_videos_render") {
		t.Fatalf("node_doc_media_patch should depend on node_videos_render")
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
