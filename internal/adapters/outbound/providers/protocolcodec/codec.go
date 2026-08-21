// Package protocolcodec is the provider-facing target-aware compiler facade.
// It composes reusable wire-family grammar with narrow occurrence-local
// dialect rules, resolves dependent tool policy after lowering, and leaves one
// completed document for serialization. It owns no routing or transport.
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
	Protocol         protocolkind.ProtocolKind
	ChatDialect      ChatDialect
	ResponsesDialect ResponsesDialect
	MessagesDialect  MessagesDialect
}

// Encode implements provider.Codec.
func (c Codec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	var changes []compat.Change
	var err error
	if c.Protocol == protocolkind.Responses && c.ResponsesDialect.RequireStreamingSSE {
		if req.Delivery != delivery.StreamingDelivery(delivery.FramingSSE) {
			return carrier.Document{}, nil, provider.NewIncompatibleTarget("Responses target requires SSE streaming delivery")
		}
	}
	if c.Protocol != protocolkind.ChatCompletions {
		err = ValidateEncodeRequest(req)
	}
	if err != nil {
		return carrier.Document{}, changes, err
	}

	var decoration AttemptDecoration
	var result wire.ProviderEncodeResult
	switch c.Protocol {
	case protocolkind.ChatCompletions:
		var document chatcompletions.ProviderRequestDocument
		document, result.Changes, err = CompileChatRequest(req, c.ChatDialect)
		if err == nil {
			if c.ChatDialect.DecorateAttempt != nil {
				decoration, err = c.ChatDialect.DecorateAttempt(attemptContextFromRequest(req))
				if err == nil && len(decoration.Fields) > 0 {
					err = chatcompletions.ApplyAttemptDecoration(document.Payload, decoration.Fields)
				}
			}
			if err == nil {
				result.Document, err = chatcompletions.EncodeProviderRequestDocument(document)
			}
		}
	case protocolkind.Responses:
		var document responses.ProviderRequestDocument
		reqForLowering := req
		if !c.ResponsesDialect.CaptureResponsesContinuation {
			reqForLowering.PreviousHistory = nil
		}
		document, result.Changes, err = CompileResponsesRequest(reqForLowering, c.ResponsesDialect)
		if err == nil {
			if c.ResponsesDialect.DecorateAttempt != nil {
				decoration, err = c.ResponsesDialect.DecorateAttempt(attemptContextFromRequest(req))
				if err == nil && len(decoration.Fields) > 0 {
					err = responses.ApplyAttemptDecoration(document.Payload, decoration.Fields)
				}
			}
			if err == nil {
				result.Document, err = responses.EncodeProviderRequestDocument(document)
			}
		}
	case protocolkind.Messages:
		var document messages.ProviderRequestDocument
		document, result.Changes, err = CompileMessagesRequest(req, c.MessagesDialect)
		if err == nil {
			if c.MessagesDialect.DecorateAttempt != nil {
				decoration, err = c.MessagesDialect.DecorateAttempt(attemptContextFromRequest(req))
				if err == nil && len(decoration.Fields) > 0 {
					err = messages.ApplyAttemptDecoration(document.Payload, decoration.Fields)
				}
			}
			if err == nil {
				result.Document, err = messages.EncodeProviderRequestDocument(document)
			}
		}
	default:
		return carrier.Document{}, changes, provider.NewIncompatibleTarget("selected provider protocol has no request codec")
	}
	if err != nil {
		return carrier.Document{}, changes, err
	}
	if len(decoration.Meta.Opaque) > 0 {
		if result.Document.Meta.Opaque == nil {
			result.Document.Meta.Opaque = make(map[string]string)
		}
		for k, v := range decoration.Meta.Opaque {
			result.Document.Meta.Opaque[k] = v
		}
	}
	changes = append(changes, result.Changes...)
	return result.Document, changes, err
}

func attemptContextFromRequest(req provider.Request) AttemptContext {
	return AttemptContext{
		CacheLocality:         req.CacheLocality,
		HasNextRouteCandidate: req.EncodeContext.HasNextRouteCandidate,
	}
}

// CompileResponsesRequest owns the single standard typed lowering sequence used
// by both the protocol codec and exact-provider decorators.
func CompileResponsesRequest(req provider.Request, dialect ResponsesDialect) (responses.ProviderRequestDocument, []compat.Change, error) {
	if err := ValidateEncodeRequest(req); err != nil {
		return responses.ProviderRequestDocument{}, nil, err
	}
	var changes []compat.Change
	document, err := responses.CompileProviderRequestDocument(
		responses.EncodeInput{Request: req.Canonical, PreviousHistory: req.PreviousHistory, ToolNames: req.ToolNames},
		req.Delivery,
		&changes,
		req.ExchangeID,
		responses.EncodeOptions{},
		responses.CompileOptions{
			LowerTool:                  dialect.LowerTool,
			LowerToolPolicy:            dialect.LowerToolPolicy,
			PrependInstructionsToInput: dialect.PrependInstructionsToInput,
			OmitInclude:                dialect.OmitInclude,
			OmitStoreFalse:             dialect.OmitStoreFalse,
			ForceArrayInput:            dialect.ForceArrayInput,
			DefaultStore:               dialect.DefaultStore,
		},
	)
	return document, changes, err
}

// CompileChatRequest owns the single standard typed lowering sequence
// used by both the protocol codec and exact-provider decorators.
func CompileChatRequest(req provider.Request, dialect ChatDialect) (chatcompletions.ProviderRequestDocument, []compat.Change, error) {
	if err := ValidateEncodeRequest(req); err != nil {
		return chatcompletions.ProviderRequestDocument{}, nil, err
	}
	var changes []compat.Change
	document, err := chatcompletions.CompileProviderRequestDocument(
		req.Canonical,
		req.ToolNames,
		req.Delivery,
		&changes,
		req.ExchangeID,
		chatcompletions.CompileOptions{
			LowerTool:              dialect.LowerTool,
			LowerToolPolicy:        dialect.LowerToolPolicy,
			LowerReasoning:         dialect.LowerReasoning,
			LowerMessage:           dialect.LowerMessage,
			UseMaxCompletionTokens: dialect.UseMaxCompletionTokens,
		},
	)
	return document, changes, err
}

// CompileMessagesRequest owns the single standard typed lowering sequence used
// by both the protocol codec and exact-provider decorators.
func CompileMessagesRequest(req provider.Request, dialect MessagesDialect) (messages.ProviderRequestDocument, []compat.Change, error) {
	if err := ValidateEncodeRequest(req); err != nil {
		return messages.ProviderRequestDocument{}, nil, err
	}
	var changes []compat.Change
	document, err := messages.CompileProviderRequestDocument(
		req.Canonical,
		req.ToolNames,
		req.Delivery,
		&changes,
		req.ExchangeID,
		messages.CompileOptions{
			LowerTool:            dialect.LowerTool,
			LowerToolPolicy:      dialect.LowerToolPolicy,
			OmitAdaptiveThinking: dialect.OmitAdaptiveThinking,
		},
	)
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
	if c.Protocol == protocolkind.ChatCompletions {
		if c.ChatDialect.ResponseReasoning != nil {
			if extractor := c.ChatDialect.ResponseReasoning(); extractor != nil {
				return DecodeChatWithReasoningCarrier(ctx, c, request, ingress, extractor)
			}
		}
	}
	return c.decodeBase(ctx, request, ingress)
}

func (c Codec) decodeBase(ctx context.Context, request provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
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
		continuationEligible := c.ResponsesDialect.CaptureResponsesContinuation && request.Canonical.PersistenceEligible()
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
		continuationEligible := c.ResponsesDialect.CaptureResponsesContinuation && request.Canonical.PersistenceEligible()
		return (responses.ProviderEnvelopeDecoder{}).DecodeProviderEnvelopeWithCapture(request.Canonical, request.ToolNames, stream, exchangeID, continuationEligible)
	case protocolkind.Messages:
		return (messages.ProviderEnvelopeDecoder{}).DecodeProviderEnvelope(request.Canonical, request.ToolNames, stream, exchangeID)
	default:
		return wire.ProviderDecodeResult{}, canonical.InternalError("selected provider protocol has no stream codec")
	}
}

var _ provider.Codec = Codec{}
