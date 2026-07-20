// Package workspace_edit owns the operator-facing workspace name lifecycle row.
//
// The same shared ui.EditableRow edits an unchanged existing slug, saves a
// changed existing slug, and advances the local workspace draft. The feature owns draft
// text, its live draft endpoint preview, validation taxonomy, phase, command
// assembly, and submit lifecycle.
// Product copy calls the field "name" while the command boundary retains the
// URL-safe workspace slug contract. Naming never persists a workspace; the
// first route and target do that atomically. Workspace persistence state never
// determines editor phase: only an unnamed draft opens automatically, while a
// named draft mounts in view mode. Escape collapses the mounted input back to
// viewing, and Enter re-enters editing.
package workspace_edit
