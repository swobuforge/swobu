// Package harness provides reusable E2E mechanics for process and PTY control.
//
// Ownership boundary:
//   - owns daemon-process wiring (temp config, binary launch, status polling,
//     graceful shutdown)
//   - owns PTY key/wait/assertion helpers used by operator journeys
//   - owns terminal-emulated viewport assertions for visible text checks
//   - does not own product assertions, workflow semantics, client request shape,
//     or backend behavior truth
//
// This keeps E2E journeys honest: tests drive shipped entrypoints and real wire
// paths while this package stays in plumbing and terminal mechanics.
package harness
