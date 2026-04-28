package requestpath

import (
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/ports"
)

// ExecutionContract carries app-level execution semantics for one requestpath
// invocation and maps directly to provider-port execution contract shape.
type ExecutionContract = ports.ExecutionContract

func NewExecutionContract(deliveryMode compatibility.DeliveryMode) ExecutionContract {
	return ports.NewExecutionContract(deliveryMode)
}
