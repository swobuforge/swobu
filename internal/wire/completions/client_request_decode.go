package completions

import (
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
)

func (ClientRequestDecoder) DecodeClientRequest(doc carrier.WireDocument) (effect.Result[wire.ClientRequestResult], error) {
	var effects []effect.Effect
	request, delivery, err := (ClientRequestDecoder{}).decodeClientRequestWithEffects(doc, effect.AccumulatorSink{Effects: &effects}, "")
	return effect.NewResult(wire.ClientRequestResult{
		Request:  request,
		Delivery: delivery,
	}, effects...), err
}

func (ClientRequestDecoder) decodeClientRequestWithEffects(doc carrier.WireDocument, sink effect.Sink, exchangeID string) (canonical.CanonicalRequest, delivery.Delivery, error) {
	raw := doc.RawBytes()
	var dto completionsRequestDTO
	if err := sse.DecodePermissiveJSON(raw, &dto, "completions request", nil); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	if dto.Prompt == "" {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("completions request is missing required fields")
	}
	streamRequested, err := core.DecodeRequestStreamFlag(raw, "completions")
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	resolvedDelivery := delivery.BufferedDelivery()
	if streamRequested {
		resolvedDelivery = delivery.StreamingDelivery(delivery.FramingNone)
	}
	if err := rejectCompletionsStructuredOutput(dto.ResponseFormat); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	controls, err := decodeCompletionsGenerationControls(dto)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:     strings.TrimSpace(dto.Model), // swobu:io-string source=boundary
		InputText: dto.Prompt,
		Controls:  controls,
	}), resolvedDelivery, nil
}
