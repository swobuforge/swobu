package completions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

type ClientRequestDecoder struct{}
type ClientDocumentEncoder struct{}
type ClientStreamEncoder struct{}
type ProviderRequestEncoder struct{}
type ProviderDocumentDecoder struct{}
type ProviderStreamDecoder struct{}

func (ClientRequestDecoder) DecodeClientRequest(doc carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error) {
	var dto completionsRequestDTO
	if err := json.Unmarshal(doc.Raw, &dto); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("completions request body is invalid JSON")
	}
	if dto.Prompt == "" {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("completions request is missing required fields")
	}
	resolvedDelivery := delivery.BufferedDelivery()
	if dto.Stream {
		resolvedDelivery = delivery.StreamingDelivery(delivery.FramingNone)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:     strings.TrimSpace(dto.Model), // swobu:io-string source=boundary
		InputText: dto.Prompt,
	}), resolvedDelivery, nil
}

func (ClientDocumentEncoder) EncodeClientDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
	raw, err := json.Marshal(completionsResponseDTO{
		ID:     sse.FallbackID(output.ResultID(), "cmpl_swobu"),
		Object: "text_completion",
		Model:  output.Model(),
		Choices: []completionsChoiceDTO{{
			Index:        0,
			Text:         sse.OutputText(output.Items()),
			FinishReason: sse.DefaultFinishReason(output.FinishReason(), "stop"),
		}},
		Usage: completionsUsageFromCanonical(output.Usage()),
	})
	if err != nil {
		return carrier.WireDocument{}, err
	}
	return carrier.WireDocument{Family: protocolkind.Completions, Media: "application/json", Raw: raw}, nil
}

func (ClientStreamEncoder) newStreamState() sse.EnvelopeStreamEncoder {
	return &completionsEnvelopeStreamEncoder{adapter: sse.NewEnvelopeEventAdapter()}
}

func (e ClientStreamEncoder) EncodeClientStream(events canonical.EventReader) (carrier.WireStream, error) {
	state := e.newStreamState()
	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = events.Close(context.Background()) }()
		defer func() { _ = pw.Close() }()
		for {
			ev, err := events.Next(context.Background())
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				_ = pw.CloseWithError(err)
				return
			}
			frames, err := state.EncodeEnvelopeEvent(ev)
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			for _, frame := range frames {
				if _, err := pw.Write(frame); err != nil {
					_ = pw.CloseWithError(err)
					return
				}
			}
		}
	}()
	return carrier.WireStream{Family: protocolkind.Completions, Framing: carrier.FramingSSE, Frames: carrier.FrameReaderFromReadCloser(pr)}, nil
}

func (ProviderRequestEncoder) EncodeProviderRequest(request canonical.CanonicalRequest, delivery delivery.Delivery) (carrier.WireDocument, error) {
	return encodeRequestCarrier(request, delivery)
}

func (ProviderDocumentDecoder) DecodeProviderDocument(doc carrier.WireDocument, exchangeID string) (canonical.EventReader, error) {
	return decodeResponseBuffered(doc.Raw, exchangeID)
}

func (ProviderStreamDecoder) DecodeProviderStream(stream carrier.WireStream, exchangeID string) canonical.EventReader {
	if err := core.ValidateResponseSSECarrierStream(stream, protocolkind.Completions); err != nil {
		carrierErr := canonical.InternalError("completions stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_stream_invariant": err.Error()}
		return canonical.NewErrorEventReader(carrierErr)
	}
	return decodeResponseStream(stream, exchangeID)
}
