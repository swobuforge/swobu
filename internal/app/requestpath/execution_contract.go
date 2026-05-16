package requestpath

import (
	"github.com/swobuforge/swobu/internal/ports"
)

// ExecutionContract carries app-level execution semantics for one requestpath
// invocation and maps directly to provider-port execution contract shape.
type ExecutionContract = ports.ExecutionContract

func NewExecutionContract(deliveryMode bool) ExecutionContract {
	return ports.NewExecutionContract(deliveryMode)
}

func NewExecutionContractWithPreCommitFallback(deliveryMode bool) ExecutionContract {
	return ports.NewExecutionContract(deliveryMode).WithPreCommitFallbackEnabled()
}
