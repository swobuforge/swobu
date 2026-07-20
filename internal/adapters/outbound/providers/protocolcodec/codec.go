// Package protocolcodec privately composes reusable wire-family grammar into
// exact provider codecs. It owns no provider routing or transport behavior.
package protocolcodec

import (
	"context"
	"errors"
	"fmt"

	providercompat "github.com/swobuforge/swobu/internal/adapters/outbound/providers/providercompat"
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

// Codec lowers canonical semantics through one configured protocol family.
// Provider-specific variants remain explicit through Options.
type Codec struct {
	ProviderID string
	Protocol   protocolkind.ProtocolKind
	Options    Options
}

// Options describes exact-backend wire behavior, not a global capability
// snapshot. Each option changes the concrete codec composition itself.
type Options struct {
	ForceStructuredInput      bool
	Store                     *bool
	ChatCompletionsTokenField chatcompletions.MaxOutputTokensField
}

// Encode implements provider.Codec.
func (c Codec) Encode(req provider.Request) (carrier.Document, []compat.Decision, error) {
	var decisions []compat.Decision
	var err error
	if err := req.Delivery.Validate(); err != nil {
		return carrier.Document{}, decisions, canonical.UnsupportedDelivery("provider delivery is invalid")
	}
	if req.Delivery.IsStreaming() && req.Delivery.Framing != delivery.FramingSSE {
		return carrier.Document{}, decisions, provider.UnsupportedByBackend(canonical.UnsupportedDelivery("provider codec supports only SSE streaming delivery"))
	}
	input := wire.ProviderEncodeInput{Request: req.Canonical}

	var result wire.ProviderEncodeResult
	switch c.Protocol {
	case protocolkind.ChatCompletions:
		result, err = (chatcompletions.ProviderRequestDocumentEncoder{}).EncodeProviderRequestWithOptions(input, req.Delivery, "", chatcompletions.EncodeOptions{
			MaxOutputTokensField: c.Options.ChatCompletionsTokenField,
			Compatibility:        req.Compatibility,
		})
	case protocolkind.Responses:
		result, err = (responses.ProviderRequestDocumentEncoder{}).EncodeProviderRequestWithOptions(input, req.Delivery, "", responses.EncodeOptions{
			ForceStructuredInput: c.Options.ForceStructuredInput,
			Store:                c.Options.Store,
			Compatibility:        req.Compatibility,
		})
	case protocolkind.Messages:
		result, err = (messages.ProviderRequestDocumentEncoder{}).EncodeProviderRequestWithOptions(input, req.Delivery, "", messages.EncodeOptions{Compatibility: req.Compatibility})
	default:
		return carrier.Document{}, decisions, provider.UnsupportedByBackend(canonical.BadEndpoint("selected provider protocol has no request codec"))
	}
	decisions = append(decisions, result.Decisions...)
	if err != nil {
		if req.Canonical.OutputFormat().Kind == canonical.OutputFormatJSONSchema {
			decisions = append(decisions, compat.Decision{
				Feature: compat.RequestOutputFormat,
				Outcome: compat.Reject,
				Subject: providercompat.RouteSubject(c.ProviderID, string(c.Protocol)),
			})
		}
		return result.Document, decisions, markUnsupportedByBackend(err)
	}
	decisions = append(decisions, providercompat.StructuredOutputDecisions(c.ProviderID, c.Protocol, req.Canonical.OutputFormat())...)
	if strictDecision, ok := providercompat.ToolSchemaStrictDecision(c.ProviderID, c.Protocol, req.Canonical.Tools(), c.Protocol != protocolkind.Messages); ok {
		decisions = append(decisions, strictDecision)
	}
	return result.Document, decisions, err
}

func markUnsupportedByBackend(err error) error {
	var canonicalErr canonical.Error
	if !errors.As(err, &canonicalErr) {
		return err
	}
	switch canonicalErr.Code {
	case canonical.ErrorCodeUnsupportedOperation, canonical.ErrorCodeUnsupportedDelivery, canonical.ErrorCodeUnsupportedEndpoint:
		return provider.UnsupportedByBackend(err)
	default:
		return err
	}
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
		Stream: result.Stream, Decisions: result.Decisions,
		TerminalDecisions: result.TerminalDecisions,
	}
	return decoded, err
}

func (c Codec) decodeDocument(ctx context.Context, request provider.Request, doc carrier.Document) (wire.ProviderDecodeResult, error) {
	exchangeID := request.ExchangeID
	switch c.Protocol {
	case protocolkind.ChatCompletions:
		return (chatcompletions.ProviderDocumentDecoder{}).DecodeProviderDocument(ctx, request.Canonical, doc, exchangeID)
	case protocolkind.Responses:
		return (responses.ProviderDocumentDecoder{}).DecodeProviderDocument(ctx, request.Canonical, doc, exchangeID)
	case protocolkind.Messages:
		return (messages.ProviderDocumentDecoder{}).DecodeProviderDocument(ctx, request.Canonical, doc, exchangeID)
	default:
		return wire.ProviderDecodeResult{}, canonical.BadEndpoint("selected provider protocol has no document codec")
	}
}

func (c Codec) decodeStream(stream carrier.ByteStream, request provider.Request) (wire.ProviderDecodeResult, error) {
	exchangeID := request.ExchangeID
	switch c.Protocol {
	case protocolkind.ChatCompletions:
		return (chatcompletions.ProviderEnvelopeDecoder{}).DecodeProviderEnvelope(request.Canonical, stream, exchangeID)
	case protocolkind.Responses:
		return (responses.ProviderEnvelopeDecoder{}).DecodeProviderEnvelope(request.Canonical, stream, exchangeID)
	case protocolkind.Messages:
		return (messages.ProviderEnvelopeDecoder{}).DecodeProviderEnvelope(request.Canonical, stream, exchangeID)
	default:
		return wire.ProviderDecodeResult{}, canonical.BadEndpoint("selected provider protocol has no stream codec")
	}
}

var _ provider.Codec = Codec{}
