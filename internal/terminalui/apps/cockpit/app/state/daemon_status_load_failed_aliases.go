package state

import stateeffect "github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state/effect"

// Minimal effect/result surface consumed outside app/state package.
type ScheduleDaemonRefreshEffect = stateeffect.ScheduleDaemonRefreshEffect
type ReplaceDaemonStatus = stateeffect.ReplaceDaemonStatus
type DaemonStatusLoadFailed = stateeffect.DaemonStatusLoadFailed
