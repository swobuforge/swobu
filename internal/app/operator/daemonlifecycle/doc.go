// Package daemonlifecycle owns operator-facing local daemon lifecycle
// facts shared across CLI and Web Operator adapters.
//
// It centralizes attach-or-start readiness behavior, machine status probing,
// graceful shutdown, startup event emission, child-exit observation, and
// explicit restart orchestration so adapters do not duplicate process/shell
// logic. Automatic startup passes the resolved address to the child explicitly;
// process creation alone is never treated as readiness.
// Status, readiness and protocol admission use canonical /api/v1/status only;
// a healthy legacy endpoint cannot mask a missing canonical Control contract.
package daemonlifecycle
