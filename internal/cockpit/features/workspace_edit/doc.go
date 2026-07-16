// Package workspace_edit owns the workspace slug lifecycle row.
//
// The same shared ui.EditableRow edits an unchanged existing slug, saves a
// changed existing slug, and creates a draft workspace. The feature owns draft
// text, validation taxonomy, phase, command assembly, and submit lifecycle
// when it is mounted with stable keyed identity. Draft create uses the same
// row-local back contract: Escape collapses the mounted input back to viewing,
// and Enter re-enters editing.
package workspace_edit
