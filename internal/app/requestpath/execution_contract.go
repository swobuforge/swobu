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
	mode := ports.ResponseModeFromStreaming(deliveryMode)
	return NewExecutionContractForModes(mode, mode)
}

func NewExecutionContractForModes(clientResponseMode ResponseMode, providerCallMode ResponseMode) ExecutionContract {
	contract := ExecutionContract{
		ClientResponseMode:     clientResponseMode,
		ProviderCallMode:       providerCallMode,
		AllowPreCommitFallback: false,
	}
	switch {
	case clientResponseMode == ports.ResponseModeBuffered && providerCallMode == ports.ResponseModeStreaming:
		contract.ConversionKind = ports.ConversionCollectStreamToBatch
	case clientResponseMode == ports.ResponseModeStreaming && providerCallMode == ports.ResponseModeBuffered:
		contract.ConversionKind = ports.ConversionSynthesizeBatchToStream
	default:
		contract.ConversionKind = ports.ConversionPassthrough
	}
	return contract
}

func NewExecutionContractWithPreCommitFallback(deliveryMode bool) ExecutionContract {
	return ports.NewExecutionContract(deliveryMode).WithPreCommitFallbackEnabled()
}
