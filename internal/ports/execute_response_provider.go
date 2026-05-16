package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ProviderRequest is the semantic provider-port input.
// It carries canonical meaning plus selected-target wiring, not raw client or backend DTOs.
type ProviderRequest struct {
	Request  canonical.CanonicalRequest
	Contract ExecutionContract
	Target   RoutableTarget
}

// ExecutionContract carries runtime delivery semantics for one execution attempt.
// It is intentionally separate from canonical request semantics.
type ExecutionContract struct {
	Streaming              bool
	AllowPreCommitFallback bool
}

func NewExecutionContract(streaming bool) ExecutionContract {
	return ExecutionContract{
		Streaming:              streaming,
		AllowPreCommitFallback: false,
	}
}

func (c ExecutionContract) WithPreCommitFallbackEnabled() ExecutionContract {
	c.AllowPreCommitFallback = true
	return c
}

func NewProviderRequest(request canonical.CanonicalRequest, contract ExecutionContract, target RoutableTarget) ProviderRequest {
	return ProviderRequest{
		Request:  canonical.CloneCanonicalRequest(request),
		Contract: contract,
		Target:   target.Clone(),
	}
}

type ProviderResponse struct {
	envelope canonical.EventReader
	metadata ProviderResponseMetadata
}

type ProviderResponseMetadata struct {
	AttemptCount              int
	ContinuityRecovered       bool
	ContinuityRecoveryTrigger string
	ModelRequested            string
	ModelResolved             string
	ModelResolutionMode       string
}

// NewBufferedProviderResponse returns a fully materialized canonical output from provider adaptation.
func NewBufferedProviderResponse(output canonical.CanonicalOutput) ProviderResponse {
	envelope, _ := canonical.EventReaderFromCanonicalOutput("buffered_exchange", output)
	return ProviderResponse{
		envelope: envelope,
	}
}

// NewEnvelopeStreamingProviderResponse returns a streaming response whose source
// of truth is a canonical envelope event stream.
func NewEnvelopeStreamingProviderResponse(envelope canonical.EventReader) ProviderResponse {
	return ProviderResponse{
		envelope: envelope,
	}
}

func (r ProviderResponse) WithMetadata(metadata ProviderResponseMetadata) ProviderResponse {
	r.metadata = metadata
	return r
}

// EnvelopeStream returns the canonical envelope stream for this response.
func (r ProviderResponse) EnvelopeStream() canonical.EventReader {
	return r.envelope
}

func (r ProviderResponse) Metadata() ProviderResponseMetadata {
	return r.metadata
}

func (r ProviderResponse) Close() error {
	if r.envelope != nil {
		return r.envelope.Close(context.Background())
	}
	return nil
}

type ProviderExecutor interface {
	// Execute maps one canonical request to the selected target and returns
	// canonical success semantics or an origin-preserving failure.
	Execute(ctx context.Context, req ProviderRequest) (ProviderResponse, error)
}
