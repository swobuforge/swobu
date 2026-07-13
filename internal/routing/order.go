package routing

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
)

// OrderTargets groups targets by rank, performs weighted-shuffle without
// replacement within each rank using the exchangeID as deterministic seed,
// and concatenates ranks in ascending rank order.
func OrderTargets(exchangeID string, targets []Target) []Target {
	if len(targets) == 0 {
		return nil
	}

	// Seed generator from exchangeID for deterministic ordering.
	var seed int64
	for i, b := range []byte(exchangeID) {
		seed += int64(b) << uint(i%8)
	}
	rng := rand.New(rand.NewSource(seed))

	// Group by rank.
	byRank := make(map[int][]Target)
	for _, t := range targets {
		byRank[t.Rank] = append(byRank[t.Rank], t)
	}

	// Collect unique ranks and sort ascending.
	ranks := make([]int, 0, len(byRank))
	for r := range byRank {
		ranks = append(ranks, r)
	}
	sort.Ints(ranks)

	// Result: concatenate shuffled groups in rank order.
	var result []Target
	for _, r := range ranks {
		group := byRank[r]
		shuffled := weightedShuffle(rng, group)
		result = append(result, shuffled...)
	}
	return result
}

// weightedShuffle returns a permutation of targets weighted by their Weight
// field. Uses weighted reservoir sampling without replacement.
func weightedShuffle(rng *rand.Rand, targets []Target) []Target {
	if len(targets) == 0 {
		return nil
	}
	if len(targets) == 1 {
		return []Target{targets[0]}
	}

	// Build weighted pool.
	type item struct {
		Target Target
		Weight int
	}
	items := make([]item, 0, len(targets))
	for _, t := range targets {
		w := t.Weight
		if w <= 0 {
			w = 1
		}
		items = append(items, item{Target: t, Weight: w})
	}

	// Weighted random selection without replacement.
	var result []Target
	remaining := make([]item, len(items))
	copy(remaining, items)

	for len(remaining) > 0 {
		totalWeight := 0
		for _, it := range remaining {
			totalWeight += it.Weight
		}
		if totalWeight <= 0 {
			// Safety: all weights zero, pick first.
			result = append(result, remaining[0].Target)
			remaining = remaining[1:]
			continue
		}

		pick := rng.Intn(totalWeight)
		targetIdx := 0
		for i, it := range remaining {
			pick -= it.Weight
			if pick < 0 {
				targetIdx = i
				break
			}
		}

		result = append(result, remaining[targetIdx].Target)
		// Remove selected.
		remaining = append(remaining[:targetIdx], remaining[targetIdx+1:]...)
	}

	return result
}

// BuildPlan compiles an ordered list of Attempts from a route and request.
// It performs fit filtering, rank grouping, weighted shuffle, and concatenation.
// The trace records filtering events and the plan.
func BuildPlan(exchangeID string, workspaceSlug string, routeModel string, targets []Target, trace *Trace) []Attempt {
	fitTargets := filterFit(targets, trace)
	ordered := OrderTargets(exchangeID, fitTargets)

	var plan []Attempt
	for i, t := range ordered {
		plan = append(plan, Attempt{
			WorkspaceSlug: workspaceSlug,
			RouteModel:    routeModel,
			Target:        t,
			Index:         i,
		})
	}

	// Record plan summary.
	if trace != nil {
		var parts []string
		for _, a := range plan {
			parts = append(parts, fmt.Sprintf("r%d %s/%s", a.Target.Rank, a.Target.Provider, a.Target.Model))
		}
		if len(parts) == 0 {
			trace.RecordPlanBuilt("no targets")
		} else {
			trace.RecordPlanBuilt(strings.Join(parts, " → "))
		}
	}

	return plan
}

// filterFit returns only targets that are eligible for the attempt plan.
// In V0 this is a minimal filter. Future versions will inspect request facts.
// The trace records filtered targets with reasons.
func filterFit(targets []Target, trace *Trace) []Target {
	var fit []Target
	for _, t := range targets {
		if !t.Enabled {
			if trace != nil {
				trace.RecordTargetFiltered(t.ID, FilterDisabled, "")
			}
			continue
		}
		// V0: all enabled targets are considered fit.
		// Future: check auth, context size, cooldown, streaming, tools, schema, modalities.
		fit = append(fit, t)
	}
	return fit
}
