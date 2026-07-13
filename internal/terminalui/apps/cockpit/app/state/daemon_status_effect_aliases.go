// swobu:lint ignore test-only-dead-cluster because=alias types bridge two internal cockpit packages; alive from effect handlers.
package state

import stateeffect "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/effect"

// Minimal effect/result surface consumed outside app/state package.
type ScheduleDaemonRefreshEffect = stateeffect.ScheduleDaemonRefreshEffect
type DaemonStatusLoadFailed = stateeffect.DaemonStatusLoadFailed
