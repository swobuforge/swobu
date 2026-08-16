// Package activity renders Cockpit request activity rows and expanded trace
// detail.
//
// It owns the mounted lifecycle of its recent-evidence snapshot: daemon queries
// run outside render, successful changes return through the go-tui app loop,
// and the last successful snapshot remains visible across transient failures.
// Rows compose the historical resolution path—requested model, selected route,
// and terminal provider/model—as the primary elastic identity. Client evidence
// yields first while attempt count, status, and duration retain independent
// outcome cells. Rows use the same five-cell child indent as other workspace
// sections. The package does not own elapsed clocks.
// It does not own request execution or inspection workflows.
package activity
