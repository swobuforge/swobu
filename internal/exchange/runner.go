package exchange

import (
	"context"
	"errors"
	"strings"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	stage "github.com/swobuforge/swobu/internal/exchange/stage"
	"github.com/swobuforge/swobu/internal/machine"
	"github.com/swobuforge/swobu/internal/replay"
	transportpkg "github.com/swobuforge/swobu/internal/transport"

	"reflect"
)

// Runner executes one exchange through a single event-first lifecycle.
// It owns runtime codec lookup and wrapper application for the exchange.
type Runner struct {
	Runtime        ExecutionRuntime
	StageMechanics stage.StageMechanics
	EffectSink     effect.Sink
	ReplayStore    replay.Store
	ResponseIDs    replay.ResponseIDGenerator
}

// ExchangeInput contains the factual inputs for one request/response exchange.
// Runtime lookup and stage mechanics live on Runner, not here.
type ExchangeInput struct {
	ExchangeID       string
	ClientFamily     canonical.ClientFamily
	ClientDelivery   delivery.Delivery
	Request          canonical.CanonicalRequest
	ReplayScope      replay.Scope
	NativeReplay     *replay.NativeRef
	Target           RoutableTarget
	Contract         ExecutionContract
	ProviderProtocol protocolkind.ProtocolKind
	ProviderDelivery delivery.Delivery
}

// TransportResponse contains one client-facing wire result for one exchange run.
type TransportResponse struct {
	Transport transportpkg.TransportResponse
	// Progressive reports whether a streaming response stayed source-incremental
	// after exchange routing and wrapper application.
	Progressive bool
}

func allocateReplayState(ctx context.Context, exchangeID string, gen replay.ResponseIDGenerator) (replayState, error) {
	if gen == nil {
		return replayState{}, errors.New("exchange response id generator is required")
	}
	responseID, err := gen.NewResponseID(ctx, exchangeID)
	if err != nil {
		return replayState{}, err
	}
	return replayState{ResponseID: responseID}, nil
}

func validateReplayRuntime(r Runner) error {
	if r.ReplayStore == nil {
		return errors.New("exchange replay store is required")
	}
	if r.ResponseIDs == nil {
		return errors.New("exchange response id generator is required")
	}
	return nil
}

func validateReplayInput(r Runner, in ExchangeInput) error {
	if err := validateReplayRuntime(r); err != nil {
		return err
	}
	if strings.TrimSpace(in.ReplayScope.Namespace) == "" {
		return errors.New("exchange replay scope namespace is required")
	}
	if strings.TrimSpace(in.ReplayScope.CallerKey) == "" {
		return errors.New("exchange replay scope caller key is required")
	}
	return nil
}

// Run executes one exchange using the runner-owned runtime and stage mechanics
// through the unified event-driven pipeline.
func (r Runner) Run(ctx context.Context, in ExchangeInput) (TransportResponse, error) {
	if r.Runtime == nil {
		return TransportResponse{}, errors.New("exchange runner runtime resolver is required")
	}
	if err := validateReplayInput(r, in); err != nil {
		return TransportResponse{}, err
	}
	replayState, err := allocateReplayState(ctx, in.ExchangeID, r.ResponseIDs)
	if err != nil {
		return TransportResponse{}, err
	}
	reg := machine.NewRegistry()
	reg.Register(pipelineStartedReduce)
	reg.Register(codecsResolvedReduce)
	reg.Register(requestEncodedReduce)
	reg.Register(ingressReceivedReduce)
	reg.Register(envelopeDecodedReduce)
	reg.Register(pipelineCompletedReduce)

	eng := machine.NewEngine(reg)
	eng.RegisterInterpreter(func(c context.Context, store *machine.Store, cmd machine.Command) ([]machine.Event, error) {
		return runRunnerInterpret(c, store, cmd, r)
	})

	store := machine.NewStore(
		machine.StateCell{Value: reflect.ValueOf(in)},
		machine.StateCell{Value: reflect.ValueOf(replayState)},
		machine.StateCell{Value: reflect.ValueOf(codecResolutionState{})},
		machine.StateCell{Value: reflect.ValueOf(EncodedRequestState{})},
		machine.StateCell{Value: reflect.ValueOf(ProviderResponseState{})},
		machine.StateCell{Value: reflect.ValueOf(DecodedEnvelopeState{})},
		machine.StateCell{Value: reflect.ValueOf(pipelineOutcomeState{})},
	)

	if _, err := eng.Run(ctx, store, PipelineStartedEvent{}); err != nil {
		return TransportResponse{}, err
	}

	var out pipelineOutcomeState
	_ = store.Get(&out)
	return out.Response, out.Err
}

// ---- helpers used by interpreters ----

func encodeClientOutput(ctx context.Context, in ExchangeInput, clientCodec ClientCodec, envelope canonical.EventReader, progressive bool, sink effect.Sink) (TransportResponse, error) {
	// Emit delivery compatibility effects for streaming decisions.
	compatEffs := deliveryCompatibilityEffects(in, progressive)
	commitEffectsBestEffort(ctx, sink, in.ExchangeID, compatEffs)

	if in.ClientDelivery.Mode == delivery.Streaming {
		streamResult, err := clientCodec.EncodeResponseStream(envelope, in.ClientDelivery)
		streamResult.Effects = append(streamResult.Effects, collectReaderEffects(envelope)...)
		commitEffectsBestEffort(ctx, sink, in.ExchangeID, streamResult.Effects)
		if err != nil {
			return TransportResponse{}, err
		}
		stream := streamResult.Value
		stream.Framing = toCarrierFraming(in.ClientDelivery.Framing)
		return NewTransportResponseFromStream(stream, progressive), nil
	}
	response, err := projectClientDocument(ctx, envelope)
	commitEffectsBestEffort(ctx, sink, in.ExchangeID, response.Effects)
	if err != nil {
		return TransportResponse{}, err
	}
	if commitAware, ok := envelope.(interface{ CommitError() error }); ok {
		if commitErr := commitAware.CommitError(); commitErr != nil {
			return TransportResponse{}, commitErr
		}
	}
	bodyDocResult, err := clientCodec.EncodeResponseDocument(response.Value)
	commitEffectsBestEffort(ctx, sink, in.ExchangeID, bodyDocResult.Effects)
	if err != nil {
		return TransportResponse{}, err
	}
	return NewTransportResponseFromDocument(bodyDocResult.Value), nil
}
