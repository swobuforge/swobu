// Package workspace_delete owns the inline workspace deletion confirmation.
//
// Confirmation is a feature workflow, not root or page state. First activation
// arms the row, second activation confirms deletion, Esc cancels, and submit
// errors remain visible for recovery.
package workspace_delete
