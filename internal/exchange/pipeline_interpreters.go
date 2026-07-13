package exchange

import (
	"context"
	"fmt"
	"reflect"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
	stage "github.com/swobuforge/swobu/internal/exchange/stage"
	"github.com/swobuforge/swobu/internal/machine"
)

// runRunnerInterpret is the single interpreter that executes all runner-machine
// commands in the unified engine. Each arm reads prior state cells, executes
// one side-effectful operation, writes the next cell, and emits the event that
// drives the next reducer.
func runRunnerInterpret(ctx context.Context, store *machine.Store, cmd machine.Command, runner Runner) ([]machine.Event, error) {
	switch c := cmd.(type) {
	case resolveCodecs:
		_ = c
		return interpResolveCodecs(runner, store)
	case encodeProviderRequest:
		_ = c
		return interpEncodeProviderRequest(ctx, runner, store)
	case resolveProviderIngress:
		_ = c
		return interpResolveProviderIngress(ctx, runner, store)
	case decodeProviderEnvelope:
		_ = c
		return interpDecodeProviderEnvelope(ctx, runner, store)
	case captureContinuation:
		_ = c
		return interpCaptureContinuation(ctx, runner, store)
	case encodeClientOutputCmd:
		_ = c
		return interpEncodeClientOutput(ctx, runner, store)
	default:
		return nil, nil
	}
}

func interpResolveCodecs(runner Runner, store *machine.Store) ([]machine.Event, error) {
	var in ExchangeInput
	if err := store.Get(&in); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}
	cr := codecResolution{
		ClientCodec:     runner.Runtime.ClientCodec(in.ClientFamily),
		RequestEncoder:  runner.Runtime.ProviderRequestDocumentEncoder(in.ProviderProtocol),
		StreamDecoder:   runner.Runtime.ProviderEnvelopeDecoder(in.ProviderProtocol, in.ProviderDelivery),
		DocumentDecoder: runner.Runtime.ProviderDocumentDecoder(in.ProviderProtocol, in.ProviderDelivery),
	}
	cr.OK = cr.ClientCodec != nil && cr.RequestEncoder != nil &&
		(in.ProviderDelivery.Mode != delivery.Streaming || cr.StreamDecoder != nil) &&
		(in.ProviderDelivery.Mode != delivery.Buffered || cr.DocumentDecoder != nil)
	store.Put(reflect.TypeOf(cr), reflect.ValueOf(cr))
	return []machine.Event{machine.Event(codecsResolved{})}, nil
}

func interpEncodeProviderRequest(ctx context.Context, runner Runner, store *machine.Store) ([]machine.Event, error) {
	var in ExchangeInput
	if err := store.Get(&in); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}
	var codecs codecResolution
	if err := store.Get(&codecs); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}

	result, err := codecs.RequestEncoder.EncodeProviderRequestDocument(
		canonical.CloneCanonicalRequest(in.Request), in.ProviderDelivery, in.ExchangeID,
	)
	effs := append([]effect.Effect(nil), result.Effects...)
	if err != nil {
		commitEffectsBestEffort(ctx, runner.EffectSink, in.ExchangeID, effs)
		storePutError(store, wrapStageErr("encodeProviderRequest", err))
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}

	patch, err := applyDocumentPatches(ctx, runner.StageMechanics, in.ExchangeID,
		stage.StageRequestDocumentOut, result.Value, in.ProviderDelivery)
	effs = append(effs, patch.Effects...)
	if err != nil {
		commitEffectsBestEffort(ctx, runner.EffectSink, in.ExchangeID, effs)
		storePutError(store, wrapStageErr("encodeProviderRequest-patch", err))
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}
	effs = append(effs, nativePayloadEffects(in, patch.Value)...)

	store.Put(reflect.TypeOf(encodedRequest{}), reflect.ValueOf(encodedRequest{
		Raw:     result.Value,
		Patched: patch.Value,
		Effects: effs,
	}))
	return []machine.Event{machine.Event(requestEncoded{})}, nil
}

func interpResolveProviderIngress(ctx context.Context, runner Runner, store *machine.Store) ([]machine.Event, error) {
	var in ExchangeInput
	if err := store.Get(&in); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}
	var enc encodedRequest
	if err := store.Get(&enc); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}

	var effs []effect.Effect
	providerReq := NewProviderRequest(in.ExchangeID, in.ClientFamily, in.Request,
		enc.Patched, in.Contract, in.Target,
		effect.AccumulatorSink{Effects: &effs},
	)
	effs = append(effs, enc.Effects...)

	ingress, err := runner.Runtime.ResolveProviderIngress(ctx, providerReq)
	if err != nil {
		effs = append(effs, backendErrorShapeEffects(in, err)...)
		commitEffectsBestEffort(ctx, runner.EffectSink, in.ExchangeID, effs)
		storePutError(store, wrapStageErr("resolveProviderIngress", err))
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}
	if err := ValidateProviderIngress(ingress); err != nil {
		commitEffectsBestEffort(ctx, runner.EffectSink, in.ExchangeID, effs)
		storePutError(store, canonical.InternalError("provider ingress shape is invalid"))
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}

	store.Put(reflect.TypeOf(providerResponse{}), reflect.ValueOf(providerResponse{
		Ingress: ingress,
		Effects: effs,
	}))
	return []machine.Event{machine.Event(ingressReceived{})}, nil
}

func interpDecodeProviderEnvelope(ctx context.Context, runner Runner, store *machine.Store) ([]machine.Event, error) {
	var in ExchangeInput
	if err := store.Get(&in); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}
	var codecs codecResolution
	if err := store.Get(&codecs); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}
	var resp providerResponse
	if err := store.Get(&resp); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}

	events, effs, progressive, err := decodeIngress(ctx, in, resp, codecs, runner)
	if err != nil {
		commitEffectsBestEffort(ctx, runner.EffectSink, in.ExchangeID, effs)
		storePutError(store, err)
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}

	store.Put(reflect.TypeOf(decodedEnvelope{}), reflect.ValueOf(decodedEnvelope{
		Events:      events,
		Effects:     effs,
		Progressive: progressive,
	}))
	return []machine.Event{machine.Event(envelopeDecoded{})}, nil
}

func interpCaptureContinuation(ctx context.Context, runner Runner, store *machine.Store) ([]machine.Event, error) {
	if runner.ContinuationStore == nil {
		return []machine.Event{machine.Event(continuationCaptured{})}, nil
	}
	var in ExchangeInput
	if err := store.Get(&in); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}
	var dec decodedEnvelope
	if err := store.Get(&dec); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}
	var contCtx continuationContext
	if err := store.Get(&contCtx); err != nil {
		// No namespace seeded means this pipeline was not wired for continuation.
		return []machine.Event{machine.Event(continuationCaptured{})}, nil
	}
	if contCtx.Namespace.IsZero() || dec.Events == nil {
		return []machine.Event{machine.Event(continuationCaptured{})}, nil
	}
	runtime := canonical.NewContinuationRuntime(runner.ContinuationStore)
	wrapped, err := runtime.WrapResponseEnvelope(ctx, contCtx.Namespace, in.Request, dec.Events)
	if err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}
	store.Put(reflect.TypeOf(decodedEnvelope{}), reflect.ValueOf(decodedEnvelope{
		Events:      wrapped,
		Effects:     dec.Effects,
		Progressive: dec.Progressive,
	}))
	return []machine.Event{machine.Event(continuationCaptured{})}, nil
}

func interpEncodeClientOutput(ctx context.Context, runner Runner, store *machine.Store) ([]machine.Event, error) {
	var in ExchangeInput
	if err := store.Get(&in); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}
	var codecs codecResolution
	if err := store.Get(&codecs); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}
	var dec decodedEnvelope
	if err := store.Get(&dec); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}

	// Commit effects accumulated through all earlier stages before encoding output.
	commitEffectsBestEffort(ctx, runner.EffectSink, in.ExchangeID, dec.Effects)

	response, err := encodeClientOutput(ctx, in, codecs.ClientCodec, dec.Events, dec.Progressive, runner.EffectSink)
	if err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(pipelineCompleted{})}, nil
	}

	store.Put(reflect.TypeOf(pipelineOutcome{}), reflect.ValueOf(pipelineOutcome{
		Response: response,
	}))
	return []machine.Event{machine.Event(pipelineCompleted{})}, nil
}

// ---- pure helpers (no side effects beyond explicit params) ----

func storePutError(store *machine.Store, err error) {
	store.Put(reflect.TypeOf(pipelineOutcome{}), reflect.ValueOf(pipelineOutcome{Err: err}))
}

func wrapStageErr(stage string, cause error) error {
	return fmt.Errorf("runner stage %s: %w", stage, cause)
}

func decodeIngress(
	ctx context.Context,
	in ExchangeInput,
	resp providerResponse,
	codecs codecResolution,
	runner Runner,
) (canonical.EventReader, []effect.Effect, bool, error) {
	effs := resp.Effects
	var events canonical.EventReader
	switch resolved := resp.Ingress.(type) {
	case carrier.CanonicalEventStream:
		if resolved.Events == nil {
			return nil, effs, false, canonical.InternalError("provider ingress canonical event stream is required")
		}
		events = resolved.Events
	case carrier.WireStream:
		if resolved.Frames != nil {
			if in.ProviderDelivery.Mode != delivery.Streaming {
				return nil, effs, false, canonical.InternalError("provider wire stream requires streaming delivery")
			}
			if codecs.StreamDecoder == nil {
				return nil, effs, false, canonical.InternalError("provider wire stream decoder is required")
			}
			r, err := codecs.StreamDecoder.DecodeProviderEnvelope(resolved, in.ExchangeID)
			effs = append(effs, r.Effects...)
			if err != nil {
				return nil, effs, false, err
			}
			events = r.Value
		} else {
			return nil, effs, false, canonical.InternalError("provider wire stream is required")
		}
	case carrier.WireDocument:
		if resolved.IsEmpty() {
			return nil, effs, false, canonical.InternalError("provider wire document is required")
		}
		if in.ProviderDelivery.Mode != delivery.Buffered {
			return nil, effs, false, canonical.InternalError("provider wire document requires buffered delivery")
		}
		patched, err := applyDocumentPatches(ctx, runner.StageMechanics, in.ExchangeID,
			stage.StageRequestDocumentIn, resolved, in.ProviderDelivery)
		effs = append(effs, patched.Effects...)
		if err != nil {
			return nil, effs, false, err
		}
		if codecs.DocumentDecoder == nil {
			return nil, effs, false, canonical.InternalError("provider wire document decoder is required")
		}
		r, err := codecs.DocumentDecoder.DecodeProviderDocument(ctx, patched.Value, in.ExchangeID)
		effs = append(effs, r.Effects...)
		if err != nil {
			return nil, effs, false, err
		}
		events = r.Value
	default:
		return nil, effs, false, canonical.InternalError("provider ingress carrier is unsupported")
	}

	wrapped, applied, err := runner.StageMechanics.WrapEventStream(
		stage.StageSemanticEvents,
		stage.Context{
			ExchangeID: in.ExchangeID,
			Carrier:    carrier.KindCanonicalEventStream,
			Family:     in.ProviderProtocol,
			Delivery:   in.ProviderDelivery,
		},
		events,
	)
	if err != nil {
		for _, a := range applied {
			effs = append(effs, a.Effects...)
		}
		return nil, effs, false, canonical.InternalError("semantic event wrapper failed")
	}
	progressive := streamProgressive(in.ClientDelivery, in.ProviderDelivery, applied)
	for _, a := range applied {
		effs = append(effs, a.Effects...)
	}
	effs = append(effs, deliveryCompatibilityEffects(in, progressive)...)
	return wrapped, effs, progressive, nil
}
