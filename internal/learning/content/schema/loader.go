package schema

import (
	"encoding/json"
	"fmt"
	"sync"
)

var (
	nodeDocV1Once       sync.Once
	nodeDocV1Schema     map[string]any
	nodeDocV1SchemaErr  error
	nodeDocGenV1Once    sync.Once
	nodeDocGenV1Schema  map[string]any
	nodeDocGenV1Err     error
	nodeOutlineV1Once   sync.Once
	nodeOutlineV1Schema map[string]any
	nodeOutlineErr      error
	figurePlanV1Once    sync.Once
	figurePlanV1Schema  map[string]any
	figurePlanV1Err     error
	videoPlanV1Once     sync.Once
	videoPlanV1Schema   map[string]any
	videoPlanV1Err      error
	drillV1Once         sync.Once
	drillV1Schema       map[string]any
	drillV1SchemaErr    error
)

func NodeDocV1() (map[string]any, error) {
	nodeDocV1Once.Do(func() {
		nodeDocV1Schema, nodeDocV1SchemaErr = loadJSONSchema("node_doc_v1.json")
	})
	return nodeDocV1Schema, nodeDocV1SchemaErr
}

// NodeDocGenV1 is an OpenAI-structured-output-friendly generation schema.
// It avoids oneOf/anyOf unions and keeps payload size smaller than NodeDocV1.
func NodeDocGenV1() (map[string]any, error) {
	nodeDocGenV1Once.Do(func() {
		nodeDocGenV1Schema, nodeDocGenV1Err = loadJSONSchema("node_doc_gen_v1.json")
	})
	return nodeDocGenV1Schema, nodeDocGenV1Err
}

func NodeDocOutlineV1() (map[string]any, error) {
	nodeOutlineV1Once.Do(func() {
		nodeOutlineV1Schema, nodeOutlineErr = loadJSONSchema("node_doc_outline_v1.json")
	})
	return nodeOutlineV1Schema, nodeOutlineErr
}

func FigurePlanV1() (map[string]any, error) {
	figurePlanV1Once.Do(func() {
		figurePlanV1Schema, figurePlanV1Err = loadJSONSchema("figure_plan_v1.json")
	})
	return figurePlanV1Schema, figurePlanV1Err
}

func VideoPlanV1() (map[string]any, error) {
	videoPlanV1Once.Do(func() {
		videoPlanV1Schema, videoPlanV1Err = loadJSONSchema("video_plan_v1.json")
	})
	return videoPlanV1Schema, videoPlanV1Err
}

func DrillPayloadV1() (map[string]any, error) {
	drillV1Once.Do(func() {
		drillV1Schema, drillV1SchemaErr = loadJSONSchema("drill_payload_v1.json")
	})
	return drillV1Schema, drillV1SchemaErr
}

func loadJSONSchema(name string) (map[string]any, error) {
	b, err := FS.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read schema %s: %w", name, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse schema %s: %w", name, err)
	}
	if err := ValidateOpenAIJSONSchema(name, m); err != nil {
		return nil, fmt.Errorf("lint schema %s: %w", name, err)
	}
	return m, nil
}
