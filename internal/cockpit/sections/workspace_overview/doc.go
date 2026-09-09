// Package workspace_overview renders the Cockpit workspace summary.
//
// Mounted features own edit and confirmation state. This section supplies
// read models and command callbacks, without performing persistence itself.
// Discarding a local draft never invokes persisted workspace deletion.
package workspace_overview
