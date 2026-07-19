package exchange

import (
	"context"
	"fmt"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/replay"
)

// prepareProviderCall is a reducer-owned deterministic edge. It resolves one
// exact backend and lowers one final wire document without external I/O.
func prepareProviderCall(s exchangeState, selection providerCallSelection, runner runtimeBundle) (providerCall, provider.TargetSnapshot, exchangeEvidence, error) {
	target, ok := s.route.at(selection.candidateIndex)
	if !ok {
		return providerCall{}, provider.TargetSnapshot{}, exchangeEvidence{}, fmt.Errorf("exchange invariant: provider candidate index %d is outside route plan", selection.candidateIndex)
	}
	path, err := resolveProviderPath(target)
	if err != nil {
		return providerCall{}, provider.TargetSnapshot{}, exchangeEvidence{}, err
	}
	backend, err := runner.Runtime.ResolveBackend(path.target)
	if err != nil {
		return providerCall{}, path.target, exchangeEvidence{}, err
	}
	if err := backend.Validate(); err != nil {
		return providerCall{}, path.target, exchangeEvidence{}, canonical.UnsupportedOperation("required provider backend is incomplete")
	}
	if !backend.Target.Equal(path.target) {
		return providerCall{}, path.target, exchangeEvidence{}, canonical.UnsupportedOperation("resolved provider backend changed target execution projection")
	}
	clientCodec := runner.Runtime.ClientCodec(s.input.clientFamily)
	if clientCodec == nil {
		return providerCall{}, path.target, exchangeEvidence{}, canonical.UnsupportedOperation("required client codec not resolved")
	}
	var canonicalRequest canonical.CanonicalRequest
	switch selection.requestChoice {
	case providerRequestPreferred:
		canonicalRequest = s.prepared.PreferredForTarget(path.target)
	case providerRequestFullHistory:
		canonicalRequest = s.prepared.Semantic.Clone()
	default:
		return providerCall{}, path.target, exchangeEvidence{}, fmt.Errorf("exchange invariant: unsupported provider request choice %d", selection.requestChoice)
	}
	providerRequest := provider.Request{Canonical: bindRequestToTarget(canonicalRequest, path.target.Model), Delivery: path.delivery}
	evidence := exchangeEvidence{}
	workspaceSlug := s.input.workspace.Slug().String()
	if err := validateReplayInput(runner, workspaceSlug); err != nil {
		return providerCall{}, path.target, exchangeEvidence{}, err
	}
	contract := NewExecutionContract(s.input.clientDelivery).WithProviderDelivery(path.delivery)
	if err := contract.Validate(); err != nil {
		return providerCall{}, path.target, exchangeEvidence{}, canonical.BadRequest("execution contract is invalid: " + err.Error())
	}
	doc, decisions, err := backend.Codec.Encode(providerRequest)
	evidence.decisions = append(evidence.decisions, decisions...)
	if err != nil {
		return providerCall{}, path.target, evidence, fmt.Errorf("provider request encoding: %w", err)
	}
	return providerCall{
		backend: backend, request: providerRequest, document: doc, clientCodec: clientCodec,
		clientDelivery: s.input.clientDelivery, exchangeID: s.input.exchangeID,
		workspaceSlug: workspaceSlug, replayRequest: s.prepared.Semantic.Clone(),
	}, path.target, evidence, nil
}

// completeProviderCall is a reducer-owned response edge. It validates and
// decodes provider ingress before deciding the final client handoff.
func completeProviderCall(ctx context.Context, call providerCall, ingress provider.Ingress, swobuResponseID canonical.SwobuResponseID, runner runtimeBundle) (ClientResponse, []compat.Decision, error) {
	if err := provider.ValidateIngress(ingress); err != nil {
		return nil, nil, canonical.InternalError("provider ingress shape is invalid")
	}
	decoded, incremental, err := decodeProviderIngress(ctx, call, ingress, call.backend)
	if err != nil {
		return nil, decoded.Decisions, err
	}
	events := newTerminalCompatibilityStream(decoded.Stream, decoded.TerminalDecisions, runner.DecisionSink, call.exchangeID)
	events = replay.NewCommitReader(events, replay.TerminalCommitConfig{
		WorkspaceSlug:   call.workspaceSlug,
		ExchangeID:      call.exchangeID,
		SwobuResponseID: swobuResponseID,
		Store:           runner.ReplayStore,
		SemanticRequest: call.replayRequest.Clone(),
		TargetID:        call.backend.Target.TargetID,
		TargetVersion:   call.backend.Target.TargetVersion,
	})
	response, err := encodeClientOutput(ctx, call, events, incremental, runner.DecisionSink)
	return response, decoded.Decisions, err
}

func decodeProviderIngress(ctx context.Context, call providerCall, ingress provider.Ingress, backend provider.Backend) (provider.DecodedResponse, bool, error) {
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
