// Package clientprofile defines operator-facing client profiles for cockpit
// handoff rendering.
//
// Each client profile is colocated in one file and provides a single Actions
// method that yields operator rows plus side-effect payloads derived from
// runtime context (for example, endpoint base URL). A thin registry exposes
// all available profiles.
//
// A single capability matrix owns both operator-visible action rows and
// interactive run wiring for supported clients.
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
//   - ../../terminalui/apps/cockpit/app/state/effect/foreground_client_runner.go
//     (launcher consumption path)
//   - ../../terminalui/apps/cockpit/app/state/effect/effect_run_client_test.go
//     (effect-level run contract proofs)
package clientprofile
