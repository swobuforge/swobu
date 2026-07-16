// runner_machine.go — state-cell types for the provider-transmit pipeline.
//
// These types are dispatched by the unified engine in requestpath.go.
// State cells are forward-only: each stage writes one cell, later stages read it.
//
// Naming convention:
//
//	State cells = noun phrases (what data is here)
//	Events      = past participles (what just happened)
//	Commands    = imperative phrases (what to do next)
//
// Pipeline (unified with routing in a single engine):
//
//	pipelineStarted     → pipelineStartedReduce     → resolveCodecs
//	codecsResolved      → codecsResolvedReduce      → encodeProviderRequest
//	requestEncoded      → requestEncodedReduce      → resolveProviderIngress
//	ingressReceived     → ingressReceivedReduce     → decodeProviderEnvelope
//	envelopeDecoded     → envelopeDecodedReduce     → encodeClientOutput
//	pipelineCompleted   → pipelineCompletedReduce   → (terminal, no command)
package exchange

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/replay"
)

// ---- state cells (one per stage, forward-only) ----

// ExchangeInput (defined in runner.go) is the seed cell.

// replayState holds runner-allocated replay identity for one exchange run.
type replayState struct {
	ResponseID replay.ResponseID
}

// codecResolutionState holds the codec lookup result for one provider protocol.
type codecResolutionState struct {
	ClientCodec     ClientCodec
	RequestEncoder  ProviderRequestDocumentEncoder
	StreamDecoder   ProviderEnvelopeDecoder
	DocumentDecoder ProviderDocumentDecoder
	OK              bool
}

// EncodedRequestState holds the provider request after encoding and patching.
type EncodedRequestState struct {
	Raw     carrier.CarrierDocument
	Patched carrier.CarrierDocument
	Effects []effect.Effect
}

// ProviderResponseState holds the raw ingress from the provider backend.
type ProviderResponseState struct {
	Ingress ProviderIngress
	Effects []effect.Effect
}

// DecodedEnvelopeState holds the canonical event stream after decoding and wrapping.
type DecodedEnvelopeState struct {
	Events      canonical.EventReader
	Effects     []effect.Effect
	Progressive bool
}

// pipelineOutcomeState holds the final client-facing response or error.
type pipelineOutcomeState struct {
	Response TransportResponse
	Err      error
}

// ---- events (past participles: what just happened) ----

// PipelineStartedEvent: input has been seeded into the store; begin pipeline.
type PipelineStartedEvent struct{}

// CodecsResolvedEvent: codec lookup finished.
type CodecsResolvedEvent struct{}

// RequestEncodedEvent: provider request encoded and patched.
type RequestEncodedEvent struct{}

// IngressReceivedEvent: HTTP response received from provider.
type IngressReceivedEvent struct{}

// EnvelopeDecodedEvent: provider envelope decoded and wrapped.
type EnvelopeDecodedEvent struct{}

// PipelineCompletedEvent: client output encoded; result is in pipelineOutcome.
type PipelineCompletedEvent struct{}

// ---- commands (imperatives: what to do next) ----

// ResolveCodecsAction looks up client and provider codecs for the current protocol.
type ResolveCodecsAction struct{}

// EncodeProviderRequestAction translates canonical request to provider wire document.
type EncodeProviderRequestAction struct{}

// ResolveProviderIngressAction sends the encoded request to the provider backend.
type ResolveProviderIngressAction struct{}

// DecodeProviderEnvelopeAction translates provider response to canonical events.
type DecodeProviderEnvelopeAction struct{}

// encodeClientOutput translates canonical events to client-family wire format.
type EncodeClientOutputAction struct{}
