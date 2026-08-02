// Package protocolcodec privately composes reusable wire-family grammar into
// exact provider codecs. It owns no provider routing or transport behavior.
package protocolcodec

import (
	"context"
	"fmt"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
	"github.com/swobuforge/swobu/internal/wire/messages"
	"github.com/swobuforge/swobu/internal/wire/responses"
)

// Codec lowers canonical semantics through one standard protocol family.
type Codec struct {
	Protocol protocolkind.ProtocolKind
	// CaptureResponsesContinuation is positive exact-provider authority to turn
	// a provider response ID into reusable previous_response_id continuation.
	// The zero value keeps generic Responses stateless and self-contained.
	CaptureResponsesContinuation bool
}

// Encode implements provider.Codec.
func (c Codec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	var changes []compat.Change
	var err error
	if c.Protocol != protocolkind.ChatCompletions {
		err = ValidateEncodeRequest(req)
	}
	if err != nil {
		return carrier.Document{}, changes, err
	}
	input := wire.ProviderEncodeInput{Request: req.Canonical, ToolNames: req.ToolNames, MCPAccess: req.MCPAccess}
	var result wire.ProviderEncodeResult
	switch c.Protocol {
	case protocolkind.ChatCompletions:
		var document chatcompletions.ProviderRequestDocument
		document, result.Changes, err = LowerChatCompletionsRequest(req)
		if err == nil {
			result.Document, err = chatcompletions.EncodeProviderRequestDocument(document)
		}
	case protocolkind.Responses:
		result, err = (responses.ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(input, req.Delivery, "")
	case protocolkind.Messages:
		result, err = (messages.ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(input, req.Delivery, "")
	default:
		return carrier.Document{}, changes, provider.NewIncompatibleTarget("selected provider protocol has no request codec")
	}
	changes = append(changes, result.Changes...)
	return result.Document, changes, err
}

// LowerChatCompletionsRequest owns the single standard typed lowering sequence
// used by both the protocol codec and exact-provider decorators.
func LowerChatCompletionsRequest(req provider.Request) (chatcompletions.ProviderRequestDocument, []compat.Change, error) {
	if err := ValidateEncodeRequest(req); err != nil {
		return chatcompletions.ProviderRequestDocument{}, nil, err
	}
	var changes []compat.Change
	document, err := func(sink *[]compat.Change) (chatcompletions.ProviderRequestDocument, error) {
		document, err := chatcompletions.LowerProviderRequestDocument(
			req.Canonical,
			req.ToolNames,
			req.Delivery,
			sink,
			req.ExchangeID,
		)
		if err != nil {
			return chatcompletions.ProviderRequestDocument{}, err
		}
		if err := chatcompletions.ApplyStandardProviderRequestReasoning(&document, req.Canonical, sink, req.ExchangeID); err != nil {
			return chatcompletions.ProviderRequestDocument{}, err
		}
		return document, nil
	}(&changes)
	return document, changes, err
}

// ValidateEncodeRequest enforces transport-independent protocol codec input
// invariants for standard and exact-provider typed compositions.
func ValidateEncodeRequest(req provider.Request) error {
	if err := req.Delivery.Validate(); err != nil {
		return canonical.InternalError("provider delivery is invalid")
	}
	if req.Delivery.IsStreaming() && req.Delivery.Framing != delivery.FramingSSE {
		return provider.NewIncompatibleTarget("provider codec supports only SSE streaming delivery")
	}
	return nil
}

// Decode implements provider.Codec.
func (c Codec) Decode(ctx context.Context, request provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	var result wire.ProviderDecodeResult
	var err error
	switch in := ingress.(type) {
	case provider.DocumentIngress:
		result, err = c.decodeDocument(ctx, request, in.Document)
	case provider.StreamIngress:
		result, err = c.decodeStream(in.Stream, request)
	default:
		return provider.DecodedResponse{}, fmt.Errorf("provider ingress %T is unsupported", ingress)
	}
	decoded := provider.DecodedResponse{
		Stream: result.Stream, Changes: result.Changes,
		ProgressiveChanges: result.ProgressiveChanges,
	}
	return decoded, err
}

func (c Codec) decodeDocument(ctx context.Context, request provider.Request, doc carrier.Document) (wire.ProviderDecodeResult, error) {
	exchangeID := request.ExchangeID
	switch c.Protocol {
	case protocolkind.ChatCompletions:
		return (chatcompletions.ProviderDocumentDecoder{}).DecodeProviderDocumentWithOptions(ctx, request.Canonical, request.ToolNames, doc, exchangeID)
	case protocolkind.Responses:
		continuationEligible := c.CaptureResponsesContinuation && request.Canonical.Responses().PersistenceEligible()
		return (responses.ProviderDocumentDecoder{}).DecodeProviderDocumentWithCapture(ctx, request.Canonical, request.ToolNames, doc, exchangeID, continuationEligible)
	case protocolkind.Messages:
		return (messages.ProviderDocumentDecoder{}).DecodeProviderDocument(ctx, request.Canonical, request.ToolNames, doc, exchangeID)
	default:
		return wire.ProviderDecodeResult{}, canonical.InternalError("selected provider protocol has no document codec")
	}
}

func (c Codec) decodeStream(stream carrier.ByteStream, request provider.Request) (wire.ProviderDecodeResult, error) {
	exchangeID := request.ExchangeID
	switch c.Protocol {
	case protocolkind.ChatCompletions:
		return (chatcompletions.ProviderEnvelopeDecoder{}).DecodeProviderEnvelopeWithOptions(request.Canonical, request.ToolNames, stream, exchangeID)
	case protocolkind.Responses:
		continuationEligible := c.CaptureResponsesContinuation && request.Canonical.Responses().PersistenceEligible()
		return (responses.ProviderEnvelopeDecoder{}).DecodeProviderEnvelopeWithCapture(request.Canonical, request.ToolNames, stream, exchangeID, continuationEligible)
	case protocolkind.Messages:
		return (messages.ProviderEnvelopeDecoder{}).DecodeProviderEnvelope(request.Canonical, request.ToolNames, stream, exchangeID)
	default:
		return wire.ProviderDecodeResult{}, canonical.InternalError("selected provider protocol has no stream codec")
	}
}

var _ provider.Codec = Codec{}
