// Package state defines the cockpit state-machine facade used by app/views.
//
// Subpackages own depth by concern:
//   - model: state value shapes and provider defaults
//   - intent: reducer input action nouns
//   - effect: outbound effect commands and result actions
//   - ports: narrow effect-facing runtime service interfaces
//
// This root package preserves ergonomic imports for userspace code by exposing
// a stable public surface and the Reduce entrypoint.
package state
