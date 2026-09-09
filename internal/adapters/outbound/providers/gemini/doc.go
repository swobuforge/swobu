// Package gemini implements the Gemini provider adapter.
//
// A configured credential reference selects API-key authentication; an absent
// reference selects Google Application Default Credentials. Neither path falls
// back to the other. MCP credential acquisition and tool execution belong to
// exchange orchestration, not this adapter.
package gemini
