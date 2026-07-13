package routing

import (
	"testing"
)

func TestOrderTargets_Empty(t *testing.T) {
	result := OrderTargets("abc", nil)
	if len(result) != 0 {
		t.Fatalf("expected empty, got %d", len(result))
	}
}

func TestOrderTargets_Single(t *testing.T) {
	targets := []Target{{ID: "a", Provider: "p", Model: "m", Rank: 1, Weight: 1}}
	result := OrderTargets("abc", targets)
	if len(result) != 1 || result[0].ID != "a" {
		t.Fatalf("expected [a], got %v", result)
	}
}

func TestOrderTargets_Deterministic(t *testing.T) {
	// Same exchangeID must produce the same order deterministically.
	targets := []Target{
		{ID: "a", Provider: "p", Model: "m", Rank: 1, Weight: 1},
		{ID: "b", Provider: "p", Model: "m", Rank: 1, Weight: 1},
		{ID: "c", Provider: "p", Model: "m", Rank: 1, Weight: 1},
	}
	runs := 5
	var first []string
	for i := 0; i < runs; i++ {
		result := OrderTargets("seed-fixed", targets)
		ids := make([]string, len(result))
		for j, r := range result {
			ids[j] = r.ID
		}
		if i == 0 {
			first = ids
		} else if !slicesEqual(ids, first) {
			t.Fatalf("run %d: order %v != first %v", i, ids, first)
		}
	}
}

func TestOrderTargets_DifferentSeeds(t *testing.T) {
	// Different exchangeIDs should potentially produce different orders.
	targets := []Target{
		{ID: "a", Provider: "p", Model: "m", Rank: 1, Weight: 1},
		{ID: "b", Provider: "p", Model: "m", Rank: 1, Weight: 1},
		{ID: "c", Provider: "p", Model: "m", Rank: 1, Weight: 1},
	}
	var orders []string
	for i := 0; i < 10; i++ {
		result := OrderTargets(string(rune('A'+i)), targets)
		var ids []string
		for _, r := range result {
			ids = append(ids, r.ID)
		}
		orders = append(orders, joinIDs(ids))
	}
	// At least two different orders should appear among 10 seeds.
	seen := make(map[string]bool)
	for _, o := range orders {
		seen[o] = true
	}
	if len(seen) < 2 {
		t.Logf("orders: %v", orders)
		t.Fatal("expected at least 2 different orderings across seeds")
	}
}

func TestOrderTargets_RankOrdering(t *testing.T) {
	targets := []Target{
		{ID: "r2a", Provider: "p", Model: "m", Rank: 2, Weight: 1},
		{ID: "r1a", Provider: "p", Model: "m", Rank: 1, Weight: 1},
		{ID: "r1b", Provider: "p", Model: "m", Rank: 1, Weight: 1},
		{ID: "r3a", Provider: "p", Model: "m", Rank: 3, Weight: 1},
	}
	result := OrderTargets("abc", targets)
	if len(result) != 4 {
		t.Fatalf("expected 4, got %d", len(result))
	}
	// All rank 1 targets come first, then rank 2, then rank 3.
	if result[0].Rank != 1 || result[1].Rank != 1 {
		t.Fatalf("expected ranks [1,1,...], got [%d,%d,...]", result[0].Rank, result[1].Rank)
	}
	if result[2].Rank != 2 {
		t.Fatalf("expected rank 2 at index 2, got %d", result[2].Rank)
	}
	if result[3].Rank != 3 {
		t.Fatalf("expected rank 3 at index 3, got %d", result[3].Rank)
	}
}

func TestOrderTargets_WeightedSkew(t *testing.T) {
	// One heavy weight should appear first more often.
	targets := []Target{
		{ID: "light", Provider: "p", Model: "m", Rank: 1, Weight: 1},
		{ID: "heavy", Provider: "p", Model: "m", Rank: 1, Weight: 100},
	}
	heavyFirst := 0
	runs := 100
	for i := 0; i < runs; i++ {
		result := OrderTargets(string(rune('A'+i%26)), targets)
		if result[0].ID == "heavy" {
			heavyFirst++
		}
	}
	// With weight 100 vs 1, heavy should be first the vast majority of runs.
	if heavyFirst < runs*3/4 {
		t.Fatalf("heavy first %d/%d times, expected > 75%%", heavyFirst, runs)
	}
}

func TestBuildPlan_FilterDisabled(t *testing.T) {
	targets := []Target{
		{ID: "enabled", Provider: "p", Model: "m", Rank: 1, Weight: 1, Enabled: true},
		{ID: "disabled", Provider: "p", Model: "m", Rank: 1, Weight: 1, Enabled: false},
	}
	trace := &Trace{ExchangeID: "abc", Workspace: "dev", RouteModel: "gpt"}
	plan := BuildPlan("abc", "dev", "gpt", targets, trace)
	if len(plan) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(plan))
	}
	if plan[0].Target.ID != "enabled" {
		t.Errorf("expected enabled target, got %s", plan[0].Target.ID)
	}

	// Trace should record the disabled filter.
	if len(trace.Events) != 2 { // filtered + plan built
		t.Fatalf("expected 2 trace events, got %d", len(trace.Events))
	}
	if trace.Events[0].Kind != TraceTargetFiltered {
		t.Errorf("first event = %v, want target_filtered", trace.Events[0].Kind)
	}
}

func TestBuildPlan_NoTargets(t *testing.T) {
	trace := &Trace{ExchangeID: "abc", Workspace: "dev", RouteModel: "gpt"}
	plan := BuildPlan("abc", "dev", "gpt", []Target{}, trace)
	if len(plan) != 0 {
		t.Fatalf("expected empty plan, got %d", len(plan))
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func joinIDs(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	result := ids[0]
	for _, id := range ids[1:] {
		result += "," + id
	}
	return result
}
