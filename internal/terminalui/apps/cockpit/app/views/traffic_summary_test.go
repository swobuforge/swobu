package views

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func TestCollapsedTrafficSummary_UsesSpendSemanticsWhenUsagePresent(t *testing.T) {
	t.Parallel()
	model := state.Model{
		TrafficRows: []state.TrafficRow{
			{
				Result:          "success",
				StatusCode:      200,
				InputTokens:     intSummaryPtr(100),
				OutputTokens:    intSummaryPtr(20),
				CacheReadTokens: intSummaryPtr(40),
				DurMillis:       intSummaryPtr(10),
			},
			{
				Result:      "backend_error",
				StatusCode:  429,
				InputTokens: intSummaryPtr(30),
				DurMillis:   intSummaryPtr(20),
			},
		},
	}

	got := collapsedTrafficSummary(model)
	want := "2 req · ok 50% · p95 10 ms · cache 40% (coverage 50%) · in 100 / out 20"
	if got != want {
		t.Fatalf("collapsedTrafficSummary()=%q want %q", got, want)
	}
}

func TestCollapsedTrafficSummary_FallsBackWithoutUsage(t *testing.T) {
	t.Parallel()
	model := state.Model{
		TrafficRows: []state.TrafficRow{
			{RequestID: "req_1", Result: "success", StatusCode: 200, DurMillis: intSummaryPtr(10), ObservedAt: "10:00:00"},
			{RequestID: "req_2", Result: "backend_error", StatusCode: 429, DurMillis: intSummaryPtr(20), ObservedAt: "10:00:01"},
		},
	}
	got := collapsedTrafficSummary(model)
	want := "2 req · ok 50% · p95 10 ms · usage unknown"
	if got != want {
		t.Fatalf("collapsedTrafficSummary()=%q want %q", got, want)
	}
}

func TestCollapsedTrafficSummary_ShowsCoverageAndBurnWhenKnown(t *testing.T) {
	t.Parallel()
	model := state.Model{
		TrafficRows: []state.TrafficRow{
			{
				Result:          "success",
				StatusCode:      200,
				DurMillis:       intSummaryPtr(10),
				InputTokens:     intSummaryPtr(1000),
				OutputTokens:    intSummaryPtr(200),
				CacheReadTokens: intSummaryPtr(710),
			},
			{
				Result:          "success",
				StatusCode:      200,
				DurMillis:       intSummaryPtr(20),
				InputTokens:     intSummaryPtr(1000),
				OutputTokens:    intSummaryPtr(300),
				CacheReadTokens: intSummaryPtr(600),
			},
		},
	}
	got := collapsedTrafficSummary(model)
	want := "2 req · ok 100% · p95 10 ms · cache 65% (coverage 100%) · in 2k / out 500"
	if got != want {
		t.Fatalf("collapsedTrafficSummary()=%q want %q", got, want)
	}
}

func intSummaryPtr(v int) *int { return &v }
