package runtime_update

import (
	"testing"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning/runtime"
)

func TestProgressiveReadyFromNodeRun(t *testing.T) {
	tests := []struct {
		name string
		nr   *types.NodeRun
		want bool
	}{
		{
			name: "completed node run",
			nr: &types.NodeRun{
				State: runtime.NodeRunCompleted,
			},
			want: true,
		},
		{
			name: "progress signal eligible",
			nr: &types.NodeRun{
				State: runtime.NodeRunReading,
				Metadata: encodeJSONMap(map[string]any{
					"runtime": map[string]any{
						"last_progress_state":      "progressing",
						"last_progress_confidence": 0.95,
					},
				}),
			},
			want: true,
		},
		{
			name: "readiness ready",
			nr: &types.NodeRun{
				State: runtime.NodeRunReading,
				Metadata: encodeJSONMap(map[string]any{
					"runtime": map[string]any{
						"readiness": map[string]any{
							"status": "ready",
							"score":  0.82,
						},
					},
				}),
			},
			want: true,
		},
		{
			name: "interactive completion plus engagement thresholds",
			nr: &types.NodeRun{
				State: runtime.NodeRunReading,
				Metadata: encodeJSONMap(map[string]any{
					"runtime": map[string]any{
						"completed_blocks": []string{"qc-1"},
						"read_blocks":      []string{"p-1", "p-2"},
					},
				}),
			},
			want: true,
		},
		{
			name: "insufficient evidence",
			nr: &types.NodeRun{
				State: runtime.NodeRunReading,
				Metadata: encodeJSONMap(map[string]any{
					"runtime": map[string]any{
						"read_blocks": []string{"p-1"},
					},
				}),
			},
			want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Helper()
			got := progressiveReadyFromNodeRun(tc.nr)
			if got != tc.want {
				t.Fatalf("progressiveReadyFromNodeRun() = %v want %v", got, tc.want)
			}
		})
	}
}

