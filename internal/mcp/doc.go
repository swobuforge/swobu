// Package mcp resolves and executes request-scoped remote MCP tools.
//
// Access credentials are transient, confined to the exact source origin, and
// excluded from canonical history. Each Run owns its SDK sessions and cleanup.
// Tool classification does not execute effects; batch admission reserves effects
// at the exchange boundary. Approval requirements and caller restrictions must
// not be weakened into unrestricted local tool execution.
package mcp
