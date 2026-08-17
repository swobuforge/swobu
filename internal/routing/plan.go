package routing

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"sort"
)

// BuildPlan shuffles equal targets deterministically from an opaque placement
// key inside each tier and concatenates tiers in fallback order. Routing owns
// neither the key's semantics nor its lifecycle; callers do. The route value
// is the entire input, so targets from another route cannot leak into the plan.
func BuildPlan(placementKey string, route Route) []Target {
	plan := make([]Target, 0)
	for tierIndex, tier := range route.tiers {
		targets := tier.Targets()
		// Tier position carries no domain meaning, so normalize the candidate set
		// before applying the deterministic placement permutation.
		sort.Slice(targets, func(i, j int) bool { return targets[i].ID().String() < targets[j].ID().String() })
		seedBytes := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", placementKey, route.name.String(), tierIndex)))
		rng := rand.New(rand.NewSource(int64(binary.LittleEndian.Uint64(seedBytes[:8]))))
		rng.Shuffle(len(targets), func(i, j int) { targets[i], targets[j] = targets[j], targets[i] })
		for _, target := range targets {
			plan = append(plan, target)
		}
	}
	return plan
}
