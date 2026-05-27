// Package effect owns cockpit outbound effect commands and their result actions.
//
// Client run law:
//   - "run" means interactive client launch only.
//   - This package executes run commands resolved by app/operator/clientprofile
//     and must not add batch probe semantics on top.
//
// If run command behavior changes, update proofs in:
// - effect_run_client_test.go
// - ../../../../app/operator/clientprofile/run_command_test.go
package effect
