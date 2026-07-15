// Package clientprofile owns supported operator client launch profiles,
// launch-command rendering, and run capability declarations.
//
// This package is the static profile lane extracted from `operatorclient`.
// It defines operator-facing launch truth consumed by cockpit/workspace
// handoff flows without making UI packages own executable client-launch
// semantics.
//
// Non-goals:
//   - daemon HTTP transport/auth/session behavior
//   - endpoint wire DTOs or control-plane reads/writes
//
// Run contract (hard law):
//   - Every ActionKindRun command is interactive only.
//   - Do not encode batch probes, scripted one-shot prompts, or auto-exit
//     semantics in run args.
//   - If we need non-interactive probes, they must use a separate capability and
//     naming surface, never "run".
package clientprofile
