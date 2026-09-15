// Package exchange orchestrates one request across configured route targets.
//
// It coordinates continuation, provider attempts, local tool execution, and
// response settlement. Checkpoints are immutable fork-safe response boundaries;
// execution affinity influences placement without selecting state. Recognized provider error detail remains typed through
// attempt classification and terminal settlement; opaque backend bodies do
// not enter stable logs. Provider authentication and wire encoding remain in
// adapters. Media fetching uses an exchange-scoped resolver; fetched bytes
// are execution artifacts rather than conversation checkpoints.
package exchange
