package completions

import (
	"strings"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func (ClientRequestDecoder) DecodeClientRequest(doc carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error) {
	raw := doc.RawBytes()
	var dto completionsRequestDTO
	if err := sse.DecodeStrictJSON(raw, &dto, "completions request"); err != nil {
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
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:     strings.TrimSpace(dto.Model), // swobu:io-string source=boundary
		InputText: dto.Prompt,
	}), resolvedDelivery, nil
}
