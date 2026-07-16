package responses

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/replay"
	"github.com/swobuforge/swobu/internal/wire"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

func (ProviderRequestDocumentEncoder) EncodeProviderRequestDocument(input wire.ProviderEncodeInput, d delivery.Delivery, exchangeID string) (effect.Result[carrier.CarrierDocument], error) {
	return shared.WithAccumulatedEffects(func(sink effect.Sink) (carrier.CarrierDocument, error) {
		return EncodeCarrierWithEffects(EncodeInput{Request: input.Request, NativeReplay: input.NativeReplay}, d, sink, exchangeID, EncodeOptions{})
	})
}

func (ProviderRequestDocumentEncoder) EncodeProviderRequestWithOptions(input wire.ProviderEncodeInput, d delivery.Delivery, exchangeID string, options EncodeOptions) (effect.Result[carrier.CarrierDocument], error) {
	return shared.WithAccumulatedEffects(func(sink effect.Sink) (carrier.CarrierDocument, error) {
		return EncodeCarrierWithEffects(EncodeInput{Request: input.Request, NativeReplay: input.NativeReplay}, d, sink, exchangeID, options)
	})
}

func (ProviderDocumentDecoder) DecodeProviderDocument(ctx context.Context, doc carrier.CarrierDocument, exchangeID string) (effect.Result[canonical.EventReader], error) {
	if err := core.ValidateResponseCarrierDocument(doc, protocolkind.Responses); err != nil {
		carrierErr := canonical.InternalError("responses response wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_document_invariant": err.Error()}
		return effect.Result[canonical.EventReader]{Value: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	return shared.WithAccumulatedEffects(func(sink effect.Sink) (canonical.EventReader, error) {
		return decodeResponseBuffered(ctx, doc.RawBytes(), exchangeID, sink)
	})
}

// NativeReplaySource implements wire.NativeReplaySource for the Responses protocol.
// The Responses provider response ID is usable as a native replay continuation
// token when it is present and the target key verifies equality.
func (ProviderDocumentDecoder) NativeReplayFromOutput(target replay.TargetKey, replayID replay.ID, providerResultID string) *replay.NativeRef {
	if providerResultID == "" {
		return nil
	}
	return &replay.NativeRef{
		ReplayID: replayID,
		Target:   target,
		Kind:     replay.NativeRefProviderResponseID,
		Value:    providerResultID,
	}
}

func (ProviderEnvelopeDecoder) DecodeProviderEnvelope(stream carrier.CarrierStream, exchangeID string) (effect.Result[canonical.EventReader], error) {
	if err := core.ValidateResponseSSECarrierStream(stream, protocolkind.Responses); err != nil {
		carrierErr := canonical.InternalError("responses stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_stream_invariant": err.Error()}
		return effect.Result[canonical.EventReader]{Value: canonical.NewErrorEventReader(carrierErr)}, carrierErr
	}
	return shared.WithAccumulatedEffects(func(sink effect.Sink) (canonical.EventReader, error) {
		return decodeResponseStream(stream, exchangeID, sink), nil
	})
}

// NativeReplayFromOutput implements wire.NativeReplaySource for the streaming
// Responses protocol decoder. The streaming event reader surfaces the provider
// response ID through the same native replay contract as the buffered decoder.
func (ProviderEnvelopeDecoder) NativeReplayFromOutput(target replay.TargetKey, replayID replay.ID, providerResultID string) *replay.NativeRef {
	if providerResultID == "" {
		return nil
	}
	return &replay.NativeRef{
		ReplayID: replayID,
		Target:   target,
		Kind:     replay.NativeRefProviderResponseID,
		Value:    providerResultID,
	}
}
