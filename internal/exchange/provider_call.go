package exchange

import (
	"context"
	"fmt"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/replay"
	"github.com/swobuforge/swobu/internal/routing"
)

// prepareProviderCall is a reducer-owned deterministic edge. It resolves one
// exact backend and lowers one final wire document without external I/O.
func prepareProviderCall(s exchangeState, attempt routing.Attempt, runner runtimeBundle) (preparedProviderCall, provider.TargetSnapshot, exchangeEvidence, error) {
	path, err := buildPathRecord(attempt.Target, s.prepared.Semantic)
	if err != nil {
		return preparedProviderCall{}, provider.TargetSnapshot{}, exchangeEvidence{}, err
	}
	deltaPath, err := buildPathRecord(attempt.Target, s.prepared.Delta)
	if err != nil {
		return preparedProviderCall{}, path.Target, exchangeEvidence{}, err
	}
	backend, err := runner.Runtime.ResolveBackend(path.Target)
	if err != nil {
		return preparedProviderCall{}, path.Target, exchangeEvidence{}, err
	}
	if err := backend.Validate(); err != nil {
		return preparedProviderCall{}, path.Target, exchangeEvidence{}, canonical.UnsupportedOperation("required provider backend is incomplete")
	}
	clientCodec := runner.Runtime.ClientCodec(s.input.clientFamily)
	if clientCodec == nil {
		return preparedProviderCall{}, path.Target, exchangeEvidence{}, canonical.UnsupportedOperation("required client codec not resolved")
	}
	providerPrepared := *s.prepared
	providerPrepared.Semantic = path.Request
	providerPrepared.Delta = deltaPath.Request
	providerRequest := providerPrepared.ForBackend(backend, path.ProviderDelivery)
	workspaceSlug := s.input.workspace.Slug().String()
	if err := validateReplayInput(runner, workspaceSlug); err != nil {
		return preparedProviderCall{}, path.Target, exchangeEvidence{}, err
	}
	contract := NewExecutionContract(s.input.clientDelivery).WithProviderDelivery(path.ProviderDelivery)
	if err := contract.Validate(); err != nil {
		return preparedProviderCall{}, path.Target, exchangeEvidence{}, canonical.BadRequest("execution contract is invalid: " + err.Error())
	}
	doc, decisions, err := backend.Codec.Encode(providerRequest)
	evidence := exchangeEvidence{decisions: decisions}
	if err != nil {
		return preparedProviderCall{}, path.Target, evidence, fmt.Errorf("provider request encoding: %w", err)
	}
	return preparedProviderCall{
		backend: backend, request: providerRequest, document: doc, clientCodec: clientCodec,
		clientDelivery: s.input.clientDelivery, exchangeID: s.input.exchangeID,
		workspaceSlug: workspaceSlug, semanticRequest: s.prepared.Semantic.Clone(),
	}, path.Target, evidence, nil
}

// completeProviderCall is a reducer-owned response edge. It validates and
// decodes provider ingress before deciding the final client handoff.
func completeProviderCall(ctx context.Context, call preparedProviderCall, ingress provider.Ingress, responseID replay.ResponseID, runner runtimeBundle) (ClientResponse, []compat.Decision, error) {
	if err := provider.ValidateIngress(ingress); err != nil {
		return nil, nil, canonical.InternalError("provider ingress shape is invalid")
	}
	decoded, incremental, err := decodeProviderIngress(ctx, call, ingress, call.backend)
	if err != nil {
		return nil, decoded.Decisions, err
	}
	events := newTerminalCompatibilityStream(decoded.Stream, decoded.TerminalDecisions, runner.DecisionSink, call.exchangeID)
	events = replay.NewCommitReader(events, replay.TerminalCommitConfig{
		WorkspaceSlug:       call.workspaceSlug,
		ExchangeID:          call.exchangeID,
		ResponseID:          responseID,
		Store:               runner.ReplayStore,
		SemanticRequest:     call.semanticRequest.Clone(),
		ContinuationSource:  decoded.Continuation,
		CaptureContinuation: call.backend.CaptureContinuation,
	})
	response, err := encodeClientOutput(ctx, call, events, incremental, runner.DecisionSink)
	return response, decoded.Decisions, err
}

func decodeProviderIngress(ctx context.Context, call preparedProviderCall, ingress provider.Ingress, backend provider.Backend) (provider.DecodedResponse, bool, error) {
	var decoded provider.DecodedResponse
	var err error
	switch resolved := ingress.(type) {
	case provider.StreamIngress:
		if call.request.Delivery.Mode != delivery.Streaming {
			return provider.DecodedResponse{}, false, canonical.InternalError("provider wire stream requires streaming delivery")
		}
		decoded, err = backend.Codec.Decode(ctx, call.exchangeID, resolved)
	case provider.DocumentIngress:
		if call.request.Delivery.Mode != delivery.Buffered {
			return provider.DecodedResponse{}, false, canonical.InternalError("provider wire document requires buffered delivery")
		}
		decoded, err = backend.Codec.Decode(ctx, call.exchangeID, resolved)
	default:
		return provider.DecodedResponse{}, false, canonical.InternalError("provider ingress carrier is unsupported")
	}
	return decoded, deliveryIsIncremental(call.clientDelivery, call.request.Delivery), err
}

func recordExchangeEvidenceBestEffort(ctx context.Context, sink compat.Sink, exchangeID string, evidence exchangeEvidence) {
	commitDecisionsBestEffort(ctx, sink, exchangeID, evidence.decisions)
}
