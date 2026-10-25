// Package authplane owns daemon-side operator auth session lifecycle
// orchestration.
//
// It provides a provider-agnostic lifecycle contract (start/poll/cancel)
// over provider-specific auth method drivers. Drivers own credential establishment
// and return opaque references. Polling never installs a credential into Routing;
// the trusted authoring consumer must prepare and confirm retained replacement
// against its original Workspace revision. Repeated polling is lifecycle observation,
// not repeated routing mutation.
// Session IDs are process-local; they do not establish durable recovery of
// provider sessions across Node restart. A subsequent user start is a
// fresh canonical retained POST intent, never a separate retry operation.
// StartAttempt separately retains exact normalized start intent before dispatch.
// Reopening that retained marker returns typed indeterminate/conflict outcomes,
// never redispatch authority or fabricated provider success. Its digest excludes
// authentication codes/credentials; marker lifetime cannot be automatically
// expired without removing replay protection. Canonical POST uses this checked
// operation with fresh trusted admission.
package authplane
