package provider

import (
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
)

// Request contains only the provider-facing input for one provider call.
// Canonical is either complete semantic state or an exact-target delta whose
// previous-response handle was already validated by session resolution. It is
// the only provider request history authority.
type Request struct {
	// ExchangeID correlates progressive response events for this invocation. It
	// is execution context, not part of canonical request semantics.
	ExchangeID     string
	Canonical      canonical.CanonicalRequest
	Delivery       delivery.Delivery
	ToolProjection ToolProjectionTable
	// MCPAccess is request-private and may be consumed only by an exact native
	// MCP request projection. It never enters canonical history.
	MCPAccess mcp.Access
}
