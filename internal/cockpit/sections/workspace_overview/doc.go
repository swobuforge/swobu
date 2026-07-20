// Package workspace_overview renders the Cockpit workspace overview section.
//
// It owns the workspace hero endpoint row, the shared workspace name edit row,
// persisted delete confirmation, and draft workspace rows. A named draft adds
// local discard; it never invokes the daemon workspace-delete port. The
// workspace-name feature owns its live draft endpoint preview so this section
// never queries a second feature instance. Draft mode uses the shared row
// grammar plus the static "new workspace" header. Workspace edit and delete
// confirmation state stays inside keyed mounted feature workflows: this section
// supplies product props and command callbacks, but does not retain child
// feature object refs or cascade lifecycle methods into children. This section
// emits no persistence effects and imports no adapters.
package workspace_overview
