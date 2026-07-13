package exchange

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	stage "github.com/swobuforge/swobu/internal/exchange/stage"
	"github.com/swobuforge/swobu/internal/machine"
	transportpkg "github.com/swobuforge/swobu/internal/transport"

	"reflect"
)

// Runner executes one exchange through a single event-first lifecycle.
// It owns runtime codec lookup and wrapper application for the exchange.
type Runner struct {
	Runtime           ExecutionRuntime
	StageMechanics    stage.StageMechanics
	EffectSink        effect.Sink
	ContinuationStore canonical.ContinuationStore
}

// ExchangeInput contains the factual inputs for one request/response exchange.
// Runtime lookup and stage mechanics live on Runner, not here.
type ExchangeInput struct {
	ExchangeID       string
	ClientFamily     canonical.ClientFamily
	ClientDelivery   delivery.Delivery
	Request          canonical.CanonicalRequest
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

// Run executes one exchange using the runner-owned runtime and stage mechanics
// through the unified event-driven pipeline.
func (r Runner) Run(ctx context.Context, in ExchangeInput) (TransportResponse, error) {
	if r.Runtime == nil {
		return TransportResponse{}, errors.New("exchange runner runtime resolver is required")
	}
	reg := machine.NewRegistry()
	reg.Register(pipelineStartedReduce)
	reg.Register(codecsResolvedReduce)
	reg.Register(requestEncodedReduce)
	reg.Register(ingressReceivedReduce)
	reg.Register(envelopeDecodedReduce)
	reg.Register(continuationCapturedReduce)
	reg.Register(pipelineCompletedReduce)

	eng := machine.NewEngine(reg)
	eng.RegisterInterpreter(func(c context.Context, store *machine.Store, cmd machine.Command) ([]machine.Event, error) {
		return runRunnerInterpret(c, store, cmd, r)
	})

	store := machine.NewStore(
		machine.StateCell{Value: reflect.ValueOf(in)},
		machine.StateCell{Value: reflect.ValueOf(codecResolution{})},
		machine.StateCell{Value: reflect.ValueOf(encodedRequest{})},
		machine.StateCell{Value: reflect.ValueOf(providerResponse{})},
		machine.StateCell{Value: reflect.ValueOf(decodedEnvelope{})},
		machine.StateCell{Value: reflect.ValueOf(continuationContext{})},
		machine.StateCell{Value: reflect.ValueOf(pipelineOutcome{})},
	)

	if _, err := eng.Run(ctx, store, pipelineStarted{}); err != nil {
		return TransportResponse{}, err
	}

	var out pipelineOutcome
	_ = store.Get(&out)
	return out.Response, out.Err
}

// ---- helpers used by interpreters ----

func encodeClientOutput(ctx context.Context, in ExchangeInput, clientCodec ClientCodec, envelope canonical.EventReader, progressive bool, sink effect.Sink) (TransportResponse, error) {
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
	bodyDocResult, err := clientCodec.EncodeResponseDocument(response.Value)
	commitEffectsBestEffort(ctx, sink, in.ExchangeID, bodyDocResult.Effects)
	if err != nil {
		return TransportResponse{}, err
	}
	return NewTransportResponseFromDocument(bodyDocResult.Value), nil
}
