// Package e2e owns high-signal end-to-end proof over shipped Swobu entrypoints.
//
// E2E honesty law for this package:
//   - every test drives user-reachable shipped entrypoints and real wire paths
//   - no in-process runtime shortcuts, direct reducer mutation, or fake control
//     paths that bypass operator/client behavior
//   - operator journeys use PTY interaction and terminal-observable assertions
//
// Package map:
//   - e2e tests in this package own behavior assertions
//   - test/e2e/harness owns daemon and PTY mechanics only
//
// This lane proves behavior that cheaper contract/integration layers cannot.
package e2e
