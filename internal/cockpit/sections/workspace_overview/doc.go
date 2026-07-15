// Package workspace_overview renders the Cockpit workspace overview section.
//
// It owns static workspace rows such as client base URL, run-once affordance,
// edit entrypoint, and draft workspace rows. Workspace edit and delete
// confirmation state belongs to feature components; this section only mounts
// those features in their overview slots. It emits no persistence effects and
// imports no adapters.
package workspace_overview
