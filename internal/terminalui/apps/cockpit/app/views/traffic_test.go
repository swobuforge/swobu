package views

import (
	"testing"

	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state"
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

func intPtr(v int) *int { return &v }
