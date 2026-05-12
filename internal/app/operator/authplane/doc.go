// Package authplane owns daemon-side operator auth session lifecycle
// orchestration.
//
// It provides a provider-agnostic lifecycle contract (start/poll/cancel/retry)
// over provider-specific auth method drivers and credential persistence
// dependencies.
package authplane
