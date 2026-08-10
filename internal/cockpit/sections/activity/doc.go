// Package activity renders Cockpit request activity rows and expanded trace
// detail.
//
// It owns the mounted lifecycle of its recent-evidence snapshot: daemon queries
// run outside render, successful changes return through the go-tui app loop,
// and the last successful snapshot remains visible across transient failures.
// Rows form one declarative five-column grid: time, route, status, and duration
// are fixed facts while resolved-target/client evidence is the sole elastic
// cell. The package does not own terminal-width breakpoints or elapsed clocks.
// It does not own request execution or inspection workflows.
package activity
