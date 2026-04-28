// Package operatorclient provides a shared HTTP client for the daemon-owned
// operator control plane.
//
// # Daemon Ownership
//
// The daemon is the single runtime owner of endpoint-intent persistence.
// Operator clients (TUI, CLI, future WebUI) hold drafts only; committed
// truth flows through this client to the daemon's /_swobu/endpoints namespace.
//
// # Control-Plane Contract
//
//   - GET    /_swobu/endpoints       → list all endpoints
//   - GET    /_swobu/endpoints/<name> → get one endpoint
//   - PUT    /_swobu/endpoints/<name> → upsert endpoint
//   - DELETE /_swobu/endpoints/<name> → delete endpoint
//
// Responses use structured error bodies with machine-readable codes.
// Error mapping is stable across client versions.
package operatorclient
