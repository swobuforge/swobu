// Package search owns direct Workspace Search invocation, Connection-backed
// sources, bounded HTTP discovery, and content-free lifecycle evidence.
//
// Upstreams and credentials belong to the shared Node Connection registry;
// Workspace Search selects a Connection, not a second source registry. Exact
// Connection ID and revision survive into invocation evidence. Search does not
// fabricate an LLM Exchange or own Principal admission.
//
// Direct Search fallback attempts explicit Connections in order, using a new
// captured binding and accounting reservation for every actual upstream call.
// An empty successful result is final; cancelled or unaccounted work never
// initiates another attempt.
// The bounded direct Search head has asynchronous atomic snapshot retention in
// the Node runtime directory. Clean shutdown drains its latest facts; abrupt
// death may lose pending updates. Retention errors are explicit and never choose
// providers, change accounting, or turn successful calls into fallback.
package search
