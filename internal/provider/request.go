package provider

import (
	"context"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
)

// EncodeContext carries request-scoped capabilities that an exact provider
// codec may invoke while lowering canonical input. URL-native codecs leave
// ResolveImage untouched; byte-only codecs call it only for URL images.
type EncodeContext struct {
	Context      context.Context
	ResolveImage func(context.Context, canonical.URLImage) (InspectedImage, error)
}

// ResponsesPrevious authorizes one exact OpenAI Responses lowering to replace
// a contiguous complete-request history range with previous_response_id.
type ResponsesPrevious struct {
	ProviderResponseID canonical.ResponsesResponseID
	OmitStart          uint32
	OmitEnd            uint32
}

// Request contains only the provider-facing input for one provider call.
// Canonical is always the complete effective canonical request and is the only
// request-history authority. ResponsesPrevious is optional concrete lowering
// data; it never changes Canonical's meaning.
type Request struct {
	// ExchangeID correlates progressive response events for this invocation. It
	// is execution context, not part of canonical request semantics.
	ExchangeID        string
	Canonical         canonical.CanonicalRequest
	ResponsesPrevious *ResponsesPrevious
	EncodeContext     EncodeContext
	Delivery          delivery.Delivery
	// ToolNames is transient provider-attempt representation state.
	ToolNames AttemptToolNames
	// MCPAccess is request-private and may be consumed only by an exact native
	// MCP request projection. It never enters canonical history.
	MCPAccess mcp.Access
}
