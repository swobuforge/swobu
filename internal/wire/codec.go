// Package wire is the Swobu wire-protocol translation bounded context.
//
// It owns encode/decode between carrier-level wire formats and the canonical
// domain model. Sub-packages are organized by protocol family; shared helpers
// for OpenAI-shaped wire live under the openai/ sub-package.
//
// Dependency direction:
//
//	wire/* → compat (for descriptive decisions)
//	wire/* → carrier (for Document, ByteStream)
//	wire/* → domain/canonical (for domain model)
//	exchange → wire (for codec interfaces)
//	wire does NOT import exchange.
package wire

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// ClientCodec translates client-family wire documents into canonical requests
// and canonical outputs back into client-facing wire documents or byte streams.
type ClientCodec interface {
	DecodeClientRequest(doc carrier.Document) (ClientDecodeResult, error)
	EncodeResponseDocument(output canonical.CanonicalOutput) (ClientDocumentResult, error)
	EncodeResponseStream(context.Context, canonical.ResponseStream, delivery.Delivery) (ClientByteStreamResult, error)
	EncodeResponseMessages(context.Context, canonical.ResponseStream, delivery.Delivery) (ClientMessageResult, error)
}

type ClientDecodeResult struct {
	Request   ClientRequestResult
	Decisions []compat.Decision
}

type ClientDocumentResult struct {
	Document  carrier.Document
	Decisions []compat.Decision
}

type ClientByteStreamResult struct {
	Stream    carrier.ByteStream
	Decisions []compat.Decision
}

type ClientMessageResult struct {
	Response  carrier.MessageResponse
	Decisions []compat.Decision
}

// ClientRequestResult is the payload returned by DecodeClientRequest.
type ClientRequestResult struct {
	Request  canonical.CanonicalRequest
	Delivery delivery.Delivery
}

// ProviderEncodeInput is the declarative canonical input for provider encoders.
type ProviderEncodeInput struct {
	Request canonical.CanonicalRequest
}

// ProviderEncodeResult is the concrete provider-request lowering result.
type ProviderEncodeResult struct {
	Document  carrier.Document
	Decisions []compat.Decision
}

// ProviderDecodeResult makes all provider-owned decode outputs explicit.
// Progressive decision sources are returned deliberately by the codec that
// constructs the stream; durable fidelity lives in canonical events.
type ProviderDecodeResult struct {
	Stream            canonical.ResponseStream
	Decisions         []compat.Decision
	TerminalDecisions provider.DecisionSource
}
