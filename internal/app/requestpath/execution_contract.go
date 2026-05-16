package requestpath

import (
	"github.com/swobuforge/swobu/internal/ports"
)

// ExecutionContract carries app-level execution semantics for one requestpath
// invocation and maps directly to provider-port execution contract shape.
type ExecutionContract = ports.ExecutionContract
type ResponseMode = ports.ResponseMode

const (
	ResponseModeBuffered  = ports.ResponseModeBuffered
	ResponseModeStreaming = ports.ResponseModeStreaming
)

func NewExecutionContract(deliveryMode bool) ExecutionContract {
	return ports.NewExecutionContract(deliveryMode)
}

func NewExecutionContractForModes(clientResponseMode ResponseMode, providerCallMode ResponseMode) ExecutionContract {
	return ports.NewExecutionContractForModes(clientResponseMode, providerCallMode)
}

func NewExecutionContractWithPreCommitFallback(deliveryMode bool) ExecutionContract {
	return ports.NewExecutionContract(deliveryMode).WithPreCommitFallbackEnabled()
}
