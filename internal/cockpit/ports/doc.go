// Package ports defines the narrow Cockpit command and query boundaries.
//
// UI packages depend on these interfaces, not concrete operator clients.
// Adapters implement the interfaces by translating between daemon/config/domain
// APIs and the Cockpit readmodel. Ports keep workspace, route/target, run, and
// activity concerns separate so feature components can own local workflow
// lifecycles without importing raw clients.
package ports
