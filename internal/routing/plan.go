package routing

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"strings"
)

// Attempt is one immutable target try. Tier and Index are runtime positions,
// never durable identifiers.
type Attempt struct {
	Workspace WorkspaceSlug
	Route     RouteName
	Tier      int
	Index     int
	Target    Target
}

// BuildPlan shuffles equal targets deterministically inside each tier and
// concatenates tiers in fallback order. The route value is the entire input, so
// targets from another route cannot leak into the plan.
func BuildPlan(exchangeID string, workspace WorkspaceSlug, route Route, trace *Trace) []Attempt {
	plan := make([]Attempt, 0)
	for tierIndex, tier := range route.tiers {
		targets := tier.Targets()
		seedBytes := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", exchangeID, route.name.String(), tierIndex)))
		rng := rand.New(rand.NewSource(int64(binary.LittleEndian.Uint64(seedBytes[:8]))))
		rng.Shuffle(len(targets), func(i, j int) { targets[i], targets[j] = targets[j], targets[i] })
		for _, target := range targets {
			plan = append(plan, Attempt{Workspace: workspace, Route: route.name, Tier: tierIndex, Index: len(plan), Target: target})
		}
	}
	if trace != nil {
		parts := make([]string, 0, len(plan))
		for _, attempt := range plan {
			parts = append(parts, fmt.Sprintf("tier%d %s/%s", attempt.Tier, attempt.Target.Provider(), attempt.Target.Model().String()))
		}
		if len(parts) == 0 {
			trace.RecordPlanBuilt("no targets")
		} else {
			trace.RecordPlanBuilt(strings.Join(parts, " → "))
		}
	}
	return plan
}
