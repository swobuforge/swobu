package views

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

func TestBuildTrafficRowKeys_DuplicateRequestIDGetsUniqueKeys(t *testing.T) {
	rows := []state.TrafficRow{
		{RequestID: "chatcmpl_1", ObservedAt: "10:00:01"},
		{RequestID: "chatcmpl_1", ObservedAt: "10:00:02"},
		{RequestID: "chatcmpl_1", ObservedAt: "10:00:03"},
	}

	got := buildTrafficRowKeys(rows)
	want := []string{"chatcmpl_1", "chatcmpl_1-2", "chatcmpl_1-3"}
	if len(got) != len(want) {
		t.Fatalf("len(keys)=%d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys[%d]=%q want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestTrafficRowKeyBase_FallsBackWhenRequestIDMissing(t *testing.T) {
	row := state.TrafficRow{ObservedAt: "2026-04-18T17:00:00Z"}
	if got := trafficRowKeyBase(row); got != "2026-04-18t17_00_00z" {
		t.Fatalf("trafficRowKeyBase()=%q want %q", got, "2026-04-18t17_00_00z")
	}
}

func TestTrafficTokenDetailLines_EmitsAllTokenRowsWhenPresent(t *testing.T) {
	input := 120
	output := 9
	cacheRead := 70
	cacheWrite := 5
	row := state.TrafficRow{
		InputTokens:      &input,
		OutputTokens:     &output,
		CacheReadTokens:  &cacheRead,
		CacheWriteTokens: &cacheWrite,
	}

	lines := trafficTokenDetailLines(row)
	if len(lines) != 3 {
		t.Fatalf("len(lines) = %d, want 3", len(lines))
	}
}

func TestTrafficCacheSummary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		row  state.TrafficRow
		want string
	}{
		{
			name: "coverage with cache ratio",
			row: state.TrafficRow{
				InputTokens:     intPtr(100),
				OutputTokens:    intPtr(10),
				CacheReadTokens: intPtr(71),
			},
			want: "c 71%",
		},
		{
			name: "usage unknown when missing cache",
			row: state.TrafficRow{
				InputTokens:  intPtr(20),
				OutputTokens: intPtr(40),
			},
			want: "c n/a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := trafficCacheSummary(tt.row)
			if got != tt.want {
				t.Fatalf("trafficCacheSummary()=%q want %q", got, tt.want)
			}
		})
	}
}

func TestTrafficTransformSummary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		row  state.TrafficRow
		want string
	}{
		{
			name: "no mutations",
			row:  state.TrafficRow{},
			want: "p n/a",
		},
		{
			name: "changed and noop mix",
			row: state.TrafficRow{
				Mutations: []stateModel.Mutation{
					{Stage: "encode", Transform: "A", Changed: true},
					{Stage: "decode", Transform: "B", Changed: false},
				},
			},
			want: "p 1/2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := trafficTransformSummary(tt.row)
			if got != tt.want {
				t.Fatalf("trafficTransformSummary()=%q want %q", got, tt.want)
			}
		})
	}
}

func TestTrafficTransformDetailLines(t *testing.T) {
	row := state.TrafficRow{
		Mutations: []stateModel.Mutation{
			{
				Stage:         "encode",
				Transform:     "openaifamily.CacheAffinityWireTransform",
				Changed:       true,
				ChangedFields: []string{"prompt_cache_key"},
			},
			{
				Stage:     "decode",
				Transform: "openaifamily.DecodeWireTransform",
				Changed:   false,
			},
		},
	}
	lines := trafficTransformDetailLines(row)
	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2", len(lines))
	}
}

func TestTrafficDiagnosticSummary(t *testing.T) {
	t.Parallel()
	if got := trafficDiagnosticSummary(state.TrafficRow{}); got != "d n/a" {
		t.Fatalf("trafficDiagnosticSummary()=%q want d n/a", got)
	}
	row := state.TrafficRow{ExchangeDiagnostics: []string{"high_transform_noop_ratio:4/5", "repeated_decode_mutation:x:2"}}
	if got := trafficDiagnosticSummary(row); got != "d 2" {
		t.Fatalf("trafficDiagnosticSummary()=%q want d 2", got)
	}
}

func TestTrafficDiagnosticDetailLines(t *testing.T) {
	row := state.TrafficRow{ExchangeDiagnostics: []string{"high_transform_noop_ratio:4/5"}}
	lines := trafficDiagnosticDetailLines(row)
	if len(lines) != 1 {
		t.Fatalf("len(lines) = %d, want 1", len(lines))
	}
}

func TestTrafficStageReportSummary(t *testing.T) {
	t.Parallel()
	if got := trafficStageReportSummary(state.TrafficRow{}); got != "s n/a" {
		t.Fatalf("trafficStageReportSummary()=%q want s n/a", got)
	}
	row := state.TrafficRow{
		StageReports: []stateModel.StageReport{
			{Stage: "provider.wire.out", Carrier: "wire_document", Applied: []string{"p.a"}, Mutated: true},
			{Stage: "provider.wire.in", Carrier: "wire_document", Applied: []string{"p.b"}, Mutated: false},
		},
	}
	if got := trafficStageReportSummary(row); got != "s 2" {
		t.Fatalf("trafficStageReportSummary()=%q want s 2", got)
	}
}

func TestTrafficStageReportDetailLines(t *testing.T) {
	row := state.TrafficRow{
		StageReports: []stateModel.StageReport{
			{Stage: "provider.wire.out", Carrier: "wire_document", Applied: []string{"openaifamily.CacheAffinityWireTransform"}, Mutated: true},
			{Stage: "provider.wire.in", Carrier: "wire_document", Mutated: false},
		},
	}
	lines := trafficStageReportDetailLines(row)
	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2", len(lines))
	}
}

func intPtr(v int) *int { return &v }
