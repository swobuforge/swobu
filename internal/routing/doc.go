// Package routing owns validated workspace, route, tier, and target configuration.
//
// Construction receives provider validation facts; this package does not perform
// provider I/O, persist configuration, or track runtime target availability.
// Collection accessors return copies. Target versions advance when durable
// settings change, and retained generations prevent delete/re-add from reviving
// stale target state.
package routing
