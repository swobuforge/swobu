package requestpath

import (
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
)

// BackendModelEntity is the resolved backend model descriptor for one execution.
// It is carried in execution context so middleware can reason on backend truth
// without depending on protocol adapters or canonical request shape.
type BackendModelEntity struct {
	BackendRef     string
	ProviderSpec   string
	ProtocolKind   protocolsurface.Kind
	BackendModelID string
}

// CapabilitySnapshot carries execution-time capability truth for one selected backend model entity.
type CapabilitySnapshot struct {
	ToolChoice ToolChoiceCapability
}

// ToolChoiceCapability intentionally carries only currently consumed policy
// behavior flags. Extend only when request-path policy consumes new fields.
type ToolChoiceCapability struct {
	ImmediateDowngradeRetry bool
}
