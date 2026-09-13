// Package workspace_overview renders the Cockpit workspace summary.
//
// Mounted features own edit and confirmation state. This section supplies
// read models and command callbacks, without performing persistence itself.
// Discarding a local draft never invokes persisted workspace deletion.
// Workspace Share owns its picker, issue, copy, and revoke behavior locally.
package workspace_overview
