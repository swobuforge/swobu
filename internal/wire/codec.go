// Package wire is the Swobu wire-protocol translation bounded context.
//
// It owns encode/decode between carrier-level wire formats and the canonical
// domain model. Sub-packages are organized by protocol family; shared helpers
// for OpenAI-shaped wire live under the openai/ sub-package.
//
// Dependency direction:
//
//	wire/* → effect (for Result[T])
//	wire/* → carrier (for CarrierDocument, CarrierStream)
//	wire/* → domain/canonical (for domain model)
//	exchange → wire (for codec interfaces)
//	wire does NOT import exchange.
package wire

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/replay"
)

// ClientCodec translates client-family wire documents into canonical requests
// and canonical outputs back into client-facing wire documents or streams.
type ClientCodec interface {
	DecodeClientRequest(doc carrier.CarrierDocument) (effect.Result[ClientRequestResult], error)
	EncodeResponseDocument(output canonical.CanonicalOutput) (effect.Result[carrier.CarrierDocument], error)
	EncodeResponseStream(events canonical.EventReader, d delivery.Delivery) (effect.Result[carrier.CarrierStream], error)
}

// ClientRequestResult is the payload returned by DecodeClientRequest.
type ClientRequestResult struct {
	Request  canonical.CanonicalRequest
	Delivery delivery.Delivery
}

// ProviderEncodeInput is the declarative input for provider request encoders.
// Encoders receive the canonical request plus an optional native replay pointer.
// The Responses encoder uses NativeReplay.Value for previous_response_id;
// stateless encoders ignore NativeReplay entirely.
type ProviderEncodeInput struct {
	Request      canonical.CanonicalRequest
	NativeReplay *replay.NativeRef
}

// ProviderRequestDocumentEncoder translates canonical requests into provider wire documents.
type ProviderRequestDocumentEncoder interface {
	EncodeProviderRequestDocument(input ProviderEncodeInput, d delivery.Delivery, exchangeID string) (effect.Result[carrier.CarrierDocument], error)
}

// ProviderEnvelopeDecoder translates provider streams into canonical events.
type ProviderEnvelopeDecoder interface {
	DecodeProviderEnvelope(stream carrier.CarrierStream, exchangeID string) (effect.Result[canonical.EventReader], error)
}

// ProviderDocumentDecoder translates provider documents into canonical events.
type ProviderDocumentDecoder interface {
	DecodeProviderDocument(ctx context.Context, doc carrier.CarrierDocument, exchangeID string) (effect.Result[canonical.EventReader], error)
}

// NativeReplaySource is an optional interface implemented by provider codecs
// whose provider-native response IDs are valid continuation tokens.
// The commit reader calls this on terminal success to capture the native
// replay pointer alongside the Swobu replay record.
type NativeReplaySource interface {
	// NativeReplayFromOutput returns a native replay pointer when the provider
	// produced a usable continuation token. The caller supplies the raw
	// provider result ID so the extractor does not need to read it from a
	// projected output that may have been rewritten to a Swobu ID.
	NativeReplayFromOutput(target replay.TargetKey, replayID replay.ID, providerResultID string) *replay.NativeRef
}
