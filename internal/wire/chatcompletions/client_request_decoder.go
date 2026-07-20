package chatcompletions

import shared "github.com/swobuforge/swobu/internal/wire/shared"

type ClientRequestDecoder struct{ ImageLimits shared.ImageDecodeLimitPolicy }
type ResponseDocumentEncoder struct{}
type ResponseStreamEncoder struct{}
type ProviderRequestDocumentEncoder struct{}
type ProviderDocumentDecoder struct{}
type ProviderEnvelopeDecoder struct{}
