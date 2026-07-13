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
)

// ---- state cells (one per stage, forward-only) ----

// ExchangeInput (defined in runner.go) is the seed cell.

// codecResolution holds the codec lookup result for one provider protocol.
type codecResolution struct {
	ClientCodec     ClientCodec
	RequestEncoder  ProviderRequestDocumentEncoder
	StreamDecoder   ProviderEnvelopeDecoder
	DocumentDecoder ProviderDocumentDecoder
	OK              bool
}

// encodedRequest holds the provider request after encoding and patching.
type encodedRequest struct {
	Raw     carrier.WireDocument
	Patched carrier.WireDocument
	Effects []effect.Effect
}

// providerResponse holds the raw ingress from the provider backend.
type providerResponse struct {
	Ingress ProviderIngress
	Effects []effect.Effect
}

// decodedEnvelope holds the canonical event stream after decoding and wrapping.
type decodedEnvelope struct {
	Events      canonical.EventReader
	Effects     []effect.Effect
	Progressive bool
}

// pipelineOutcome holds the final client-facing response or error.
type pipelineOutcome struct {
	Response TransportResponse
	Err      error
}

// ---- events (past participles: what just happened) ----

// pipelineStarted: input has been seeded into the store; begin pipeline.
type pipelineStarted struct{}

// codecsResolved: codec lookup finished.
type codecsResolved struct{}

// requestEncoded: provider request encoded and patched.
type requestEncoded struct{}

// ingressReceived: HTTP response received from provider.
type ingressReceived struct{}

// envelopeDecoded: provider envelope decoded and wrapped.
type envelopeDecoded struct{}

// pipelineCompleted: client output encoded; result is in pipelineOutcome.
type pipelineCompleted struct{}

// ---- commands (imperatives: what to do next) ----

// resolveCodecs looks up client and provider codecs for the current protocol.
type resolveCodecs struct{}

// encodeProviderRequest translates canonical request to provider wire document.
type encodeProviderRequest struct{}

// resolveProviderIngress sends the encoded request to the provider backend.
type resolveProviderIngress struct{}

// decodeProviderEnvelope translates provider response to canonical events.
type decodeProviderEnvelope struct{}

// encodeClientOutput translates canonical events to client-family wire format.
type encodeClientOutputCmd struct{}
