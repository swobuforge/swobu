package exchange

import (
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/wire"
)

// Result carries the next exchange-boundary value plus any side effects the
// step wants committed outside the step itself.
type Result[T any] = effect.Result[T]

// ClientCodec translates client-family wire documents and client-facing responses.
type ClientCodec = wire.ClientCodec

// ProviderRequestDocumentEncoder translates canonical requests into provider wire documents.
type ProviderRequestDocumentEncoder = wire.ProviderRequestDocumentEncoder

// ProviderEnvelopeDecoder translates provider streams into canonical events.
type ProviderEnvelopeDecoder = wire.ProviderEnvelopeDecoder

// ProviderDocumentDecoder translates provider documents into canonical events.
type ProviderDocumentDecoder = wire.ProviderDocumentDecoder
