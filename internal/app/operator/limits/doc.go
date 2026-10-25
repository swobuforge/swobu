// Package limits owns Control's Workspace policy review/apply/clear workflow.
// WebUI and mounted Admin MCP adapt to these same commands. The existing review
// authority binds exact limit intent and accounting revision; it is ordinary
// command review, never independent human approval or a new MCP ledger.
// Accounting owns durable policy and repeats live admission at publication.
package limits
