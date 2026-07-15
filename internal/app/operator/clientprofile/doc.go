// Package clientprofile defines operator-facing client profiles for cockpit
// handoff rendering.
//
// Each supported client profile is colocated in one file and provides a single
// Actions method that yields the run row plus its side-effect payload derived
// from runtime context (for example, endpoint base URL). A thin registry
// exposes all available supported profiles.
//
// A single capability matrix owns both operator-visible run rows and
// interactive run wiring for supported clients. Non-run affordances are not
// part of this package's supported surface.
//
// Run contract (hard law):
//   - Every ActionKindRun command is interactive only.
//   - Do not encode batch probes, scripted one-shot prompts, or auto-exit
//     semantics in run args.
//   - If we need non-interactive probes, they must use a separate capability and
//     naming surface, never "run".
//
// When adding/updating any run command, update and prove all of:
//   - action_kind_catalog.go (run capability source of truth)
//   - run_command_spec.go (template expansion + display payload rendering)
//   - run_command_test.go (profile command contract proofs)
//   - internal/cockpit/features/run_once and internal/cockpit/adapters once
//     the active cockpit exists (launcher consumption path and effect-level
//     run contract proofs)
package clientprofile
