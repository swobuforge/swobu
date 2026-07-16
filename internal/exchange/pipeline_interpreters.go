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
	"github.com/swobuforge/swobu/internal/replay"
	"github.com/swobuforge/swobu/internal/wire"
)

// runRunnerInterpret is the single interpreter that executes all runner-machine
// commands in the unified engine. Each arm reads prior state cells, executes
// one side-effectful operation, writes the next cell, and emits the event that
// drives the next reducer.
func runRunnerInterpret(ctx context.Context, store *machine.Store, cmd machine.Command, runner Runner) ([]machine.Event, error) {
	switch c := cmd.(type) {
	case ResolveCodecsAction:
		_ = c
		return interpResolveCodecs(runner, store)
	case EncodeProviderRequestAction:
		_ = c
		return interpEncodeProviderRequest(ctx, runner, store)
	case ResolveProviderIngressAction:
		_ = c
		return interpResolveProviderIngress(ctx, runner, store)
	case DecodeProviderEnvelopeAction:
		_ = c
		return interpDecodeProviderEnvelope(ctx, runner, store)
	case EncodeClientOutputAction:
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
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}
	cr := codecResolutionState{
		ClientCodec:     runner.Runtime.ClientCodec(in.ClientFamily),
		RequestEncoder:  runner.Runtime.ProviderRequestDocumentEncoder(in.ProviderProtocol),
		StreamDecoder:   runner.Runtime.ProviderEnvelopeDecoder(in.ProviderProtocol, in.ProviderDelivery),
		DocumentDecoder: runner.Runtime.ProviderDocumentDecoder(in.ProviderProtocol, in.ProviderDelivery),
	}
	cr.OK = cr.ClientCodec != nil && cr.RequestEncoder != nil &&
		(in.ProviderDelivery.Mode != delivery.Streaming || cr.StreamDecoder != nil) &&
		(in.ProviderDelivery.Mode != delivery.Buffered || cr.DocumentDecoder != nil)
	store.Put(reflect.TypeOf(cr), reflect.ValueOf(cr))
	return []machine.Event{machine.Event(CodecsResolvedEvent{})}, nil
}

func interpEncodeProviderRequest(ctx context.Context, runner Runner, store *machine.Store) ([]machine.Event, error) {
	var in ExchangeInput
	if err := store.Get(&in); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}
	var codecs codecResolutionState
	if err := store.Get(&codecs); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}

	result, err := codecs.RequestEncoder.EncodeProviderRequestDocument(
		wire.ProviderEncodeInput{
			Request:      canonical.CloneCanonicalRequest(in.Request),
			NativeReplay: in.NativeReplay,
		},
		in.ProviderDelivery, in.ExchangeID,
	)
	effs := append([]effect.Effect(nil), result.Effects...)
	if err != nil {
		commitEffectsBestEffort(ctx, runner.EffectSink, in.ExchangeID, effs)
		storePutError(store, wrapStageErr("encodeProviderRequest", err))
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}

	patch, err := applyDocumentPatches(ctx, runner.StageMechanics, in.ExchangeID,
		stage.StageRequestDocumentOut, result.Value, in.ProviderDelivery)
	effs = append(effs, patch.Effects...)
	if err != nil {
		commitEffectsBestEffort(ctx, runner.EffectSink, in.ExchangeID, effs)
		storePutError(store, wrapStageErr("encodeProviderRequest-patch", err))
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}
	effs = append(effs, nativePayloadEffects(in, patch.Value)...)

	store.Put(reflect.TypeOf(EncodedRequestState{}), reflect.ValueOf(EncodedRequestState{
		Raw:     result.Value,
		Patched: patch.Value,
		Effects: effs,
	}))
	return []machine.Event{machine.Event(RequestEncodedEvent{})}, nil
}

func interpResolveProviderIngress(ctx context.Context, runner Runner, store *machine.Store) ([]machine.Event, error) {
	var in ExchangeInput
	if err := store.Get(&in); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}
	var enc EncodedRequestState
	if err := store.Get(&enc); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
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
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}
	if err := ValidateProviderIngress(ingress); err != nil {
		commitEffectsBestEffort(ctx, runner.EffectSink, in.ExchangeID, effs)
		storePutError(store, canonical.InternalError("provider ingress shape is invalid"))
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}

	store.Put(reflect.TypeOf(ProviderResponseState{}), reflect.ValueOf(ProviderResponseState{
		Ingress: ingress,
		Effects: effs,
	}))
	return []machine.Event{machine.Event(IngressReceivedEvent{})}, nil
}

func interpDecodeProviderEnvelope(ctx context.Context, runner Runner, store *machine.Store) ([]machine.Event, error) {
	var in ExchangeInput
	if err := store.Get(&in); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}
	var codecs codecResolutionState
	if err := store.Get(&codecs); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}
	var resp ProviderResponseState
	if err := store.Get(&resp); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}
	var replayInfo replayState
	if err := store.Get(&replayInfo); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}

	events, effs, progressive, err := decodeIngress(ctx, in, resp, codecs, runner)
	if err != nil {
		commitEffectsBestEffort(ctx, runner.EffectSink, in.ExchangeID, effs)
		storePutError(store, err)
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}

	config := replay.TerminalCommitConfig{
		Scope:          in.ReplayScope,
		ExchangeID:     in.ExchangeID,
		ResponseID:     replayInfo.ResponseID,
		Store:          runner.ReplayStore,
		NativeReplay:   in.NativeReplay,
		CaptureRequest: in.Request,
	}
	if nativeExtractor := replayNativeExtractor(in, resp, codecs); nativeExtractor != nil {
		config.NativeExtractor = nativeExtractor
	}
	events = replay.NewCommitReader(events, config)

	store.Put(reflect.TypeOf(DecodedEnvelopeState{}), reflect.ValueOf(DecodedEnvelopeState{
		Events:      events,
		Effects:     effs,
		Progressive: progressive,
	}))
	return []machine.Event{machine.Event(EnvelopeDecodedEvent{})}, nil
}

func interpEncodeClientOutput(ctx context.Context, runner Runner, store *machine.Store) ([]machine.Event, error) {
	var in ExchangeInput
	if err := store.Get(&in); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}
	var codecs codecResolutionState
	if err := store.Get(&codecs); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}
	var dec DecodedEnvelopeState
	if err := store.Get(&dec); err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}

	// Commit effects accumulated through all earlier stages before encoding output.
	commitEffectsBestEffort(ctx, runner.EffectSink, in.ExchangeID, dec.Effects)

	response, err := encodeClientOutput(ctx, in, codecs.ClientCodec, dec.Events, dec.Progressive, runner.EffectSink)
	if err != nil {
		storePutError(store, err)
		return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
	}

	store.Put(reflect.TypeOf(pipelineOutcomeState{}), reflect.ValueOf(pipelineOutcomeState{
		Response: response,
	}))
	return []machine.Event{machine.Event(PipelineCompletedEvent{})}, nil
}

// ---- pure helpers (no side effects beyond explicit params) ----

func storePutError(store *machine.Store, err error) {
	store.Put(reflect.TypeOf(pipelineOutcomeState{}), reflect.ValueOf(pipelineOutcomeState{Err: err}))
}

func wrapStageErr(stage string, cause error) error {
	return fmt.Errorf("runner stage %s: %w", stage, cause)
}

func decodeIngress(
	ctx context.Context,
	in ExchangeInput,
	resp ProviderResponseState,
	codecs codecResolutionState,
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
	case carrier.CarrierStream:
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
	case carrier.CarrierDocument:
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
	return wrapped, effs, progressive, nil
}

func replayNativeExtractor(in ExchangeInput, resp ProviderResponseState, codecs codecResolutionState) func(providerResultID string, replayID replay.ID) *replay.NativeRef {
	source := nativeReplaySourceForIngress(resp.Ingress, codecs)
	if source == nil {
		return nil
	}
	targetKey := replayTargetKey(in.Target, in.ProviderProtocol, in.Request.Model())
	if targetKey == nil {
		return nil
	}
	return func(providerResultID string, replayID replay.ID) *replay.NativeRef {
		return source.NativeReplayFromOutput(*targetKey, replayID, providerResultID)
	}
}

func nativeReplaySourceForIngress(ingress ProviderIngress, codecs codecResolutionState) wire.NativeReplaySource {
	switch ingress.(type) {
	case carrier.CarrierStream:
		if source, ok := codecs.StreamDecoder.(wire.NativeReplaySource); ok {
			return source
		}
	case carrier.CarrierDocument:
		if source, ok := codecs.DocumentDecoder.(wire.NativeReplaySource); ok {
			return source
		}
	}
	return nil
}
