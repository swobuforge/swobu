// Package workspace_delete owns the inline workspace deletion confirmation.
//
// Confirmation is a feature workflow, not root or page state. First activation
// arms the row by storing the pending delete workspace ID, second activation
// confirms deletion, Esc cancels, and submit errors remain visible for
// recovery when it is mounted with stable keyed identity. This package accepts
// persisted workspaces only; named drafts use Cockpit-local discard instead.
package workspace_delete
