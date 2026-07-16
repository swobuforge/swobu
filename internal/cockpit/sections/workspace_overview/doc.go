// Package workspace_overview renders the Cockpit workspace overview section.
//
// It owns the workspace hero endpoint row, the shared slug edit row, delete
// confirmation, and draft workspace rows. Draft mode uses the shared slug row
// grammar plus the static "new workspace" header. Workspace edit and delete
// confirmation state stays inside keyed mounted feature workflows: this section
// supplies product props and command callbacks, but does not retain child
// feature object refs or cascade lifecycle methods into children. This section
// emits no persistence effects and imports no adapters.
package workspace_overview
