// Package workspace_edit owns the workspace slug lifecycle row.
//
// The same row edits an unchanged existing slug, saves a changed existing slug,
// and creates a draft workspace. The feature owns draft text, validation,
// phase, command assembly, and submit lifecycle when it is mounted with stable
// keyed identity.
package workspace_edit
