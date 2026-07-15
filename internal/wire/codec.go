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

// ProviderRequestDocumentEncoder translates canonical requests into provider wire documents.
type ProviderRequestDocumentEncoder interface {
	EncodeProviderRequestDocument(request canonical.CanonicalRequest, d delivery.Delivery, exchangeID string) (effect.Result[carrier.CarrierDocument], error)
}

// ProviderEnvelopeDecoder translates provider streams into canonical events.
type ProviderEnvelopeDecoder interface {
	DecodeProviderEnvelope(stream carrier.CarrierStream, exchangeID string) (effect.Result[canonical.EventReader], error)
}

// ProviderDocumentDecoder translates provider documents into canonical events.
type ProviderDocumentDecoder interface {
	DecodeProviderDocument(ctx context.Context, doc carrier.CarrierDocument, exchangeID string) (effect.Result[canonical.EventReader], error)
}
