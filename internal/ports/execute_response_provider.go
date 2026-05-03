package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

// ExecuteRequest is the semantic provider-port input.
// It carries canonical meaning plus selected-target wiring, not raw client or backend DTOs.
type ExecuteRequest struct {
	Request  compatibility.CanonicalRequest
	Contract ExecutionContract
	Target   RoutableTarget
}

// ExecutionContract carries runtime delivery semantics for one execution attempt.
// It is intentionally separate from canonical request semantics.
type ExecutionContract struct {
	DeliveryMode compatibility.DeliveryMode
}

func NewExecutionContract(deliveryMode compatibility.DeliveryMode) ExecutionContract {
	return ExecutionContract{
		DeliveryMode: deliveryMode,
	}
}

func NewExecuteRequest(request compatibility.CanonicalRequest, contract ExecutionContract, target RoutableTarget) ExecuteRequest {
	return ExecuteRequest{
		Request:  compatibility.CloneCanonicalRequest(request),
		Contract: contract,
		Target:   target.Clone(),
	}
}

type ExecuteResponse struct {
	delivery compatibility.DeliveryMode
	output   compatibility.CanonicalOutput
	stream   compatibility.CanonicalOutputEventStream
	metadata ExecuteMetadata
}

type ExecuteMetadata struct {
	AttemptCount              int
	ContinuityRecovered       bool
	ContinuityRecoveryTrigger string
	ModelRequested            string
	ModelResolved             string
	ModelResolutionMode       string
}

// NewBufferedExecuteResponse returns a fully materialized canonical output from provider adaptation.
func NewBufferedExecuteResponse(output compatibility.CanonicalOutput) ExecuteResponse {
	return ExecuteResponse{
		delivery: compatibility.DeliveryModeBuffered,
		output:   compatibility.CloneCanonicalOutput(output),
	}
}

// NewStreamingExecuteResponse returns canonical output-assembly events from provider adaptation.
func NewStreamingExecuteResponse(stream compatibility.CanonicalOutputEventStream) ExecuteResponse {
	return ExecuteResponse{
		delivery: compatibility.DeliveryModeStreaming,
		stream:   stream,
	}
}

func (r ExecuteResponse) WithMetadata(metadata ExecuteMetadata) ExecuteResponse {
	r.metadata = metadata
	return r
}

func (r ExecuteResponse) Output() compatibility.CanonicalOutput {
	return compatibility.CloneCanonicalOutput(r.output)
}

func (r ExecuteResponse) Stream() compatibility.CanonicalOutputEventStream {
	return r.stream
}

func (r ExecuteResponse) DeliveryMode() compatibility.DeliveryMode {
	return r.delivery
}

func (r ExecuteResponse) Metadata() ExecuteMetadata {
	return r.metadata
}

func (r ExecuteResponse) Close() error {
	if r.stream != nil {
		return r.stream.Close()
	}
	return nil
}

type ProviderExecutor interface {
	// Execute maps one canonical request to the selected target and returns
	// canonical success semantics or an origin-preserving failure.
	Execute(ctx context.Context, req ExecuteRequest) (ExecuteResponse, error)
}
