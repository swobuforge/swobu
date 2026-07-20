package exchange

import (
	"context"
	"fmt"
	"reflect"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/session"
)

// prepareProviderCall is a reducer-owned deterministic edge. It resolves one
// exact backend and lowers one final wire document without external I/O.
func prepareProviderCall(s exchangeState, selection providerCallSelection, runner runtimeBundle, preparedOverride *providerAttemptPrepared) (providerCall, provider.TargetSnapshot, exchangeEvidence, *prepareProviderAttemptCommand, error) {
	target, ok := s.route.at(selection.candidateIndex)
	if !ok {
		return providerCall{}, provider.TargetSnapshot{}, exchangeEvidence{}, nil, fmt.Errorf("exchange invariant: provider candidate index %d is outside route plan", selection.candidateIndex)
	}
	path, err := resolveProviderPath(target)
	if err != nil {
		return providerCall{}, provider.TargetSnapshot{}, exchangeEvidence{}, nil, err
	}
	backend, err := runner.Runtime.ResolveBackend(path.target)
	if err != nil {
		return providerCall{}, path.target, exchangeEvidence{}, nil, err
	}
	if err := backend.Validate(); err != nil {
		return providerCall{}, path.target, exchangeEvidence{}, nil, canonical.UnsupportedOperation("required provider backend is incomplete")
	}
	if !backend.Target.Equal(path.target) {
		return providerCall{}, path.target, exchangeEvidence{}, nil, canonical.UnsupportedOperation("resolved provider backend changed target execution projection")
	}
	clientCodec := runner.Runtime.ClientCodec(s.input.clientFamily)
	if clientCodec == nil {
		return providerCall{}, path.target, exchangeEvidence{}, nil, canonical.UnsupportedOperation("required client codec not resolved")
	}
	// Reject already-known checkpoint material before attempt preparation performs
	// URL fetch I/O or the provider is invoked.
	if err := session.ValidateResolvedRequestSize(s.prepared.Full, s.prepared.ResolvedMedia, runner.Policy.Limits.MaxCheckpointBytes); err != nil {
		return providerCall{}, path.target, exchangeEvidence{}, nil, fmt.Errorf("checkpoint preflight: %w", err)
	}
	var canonicalRequest canonical.CanonicalRequest
	switch selection.requestChoice {
	case providerRequestPreferred:
		canonicalRequest = s.prepared.ForTarget(path.target)
	case providerRequestFullHistory:
		canonicalRequest = s.prepared.Full.Clone()
	default:
		return providerCall{}, path.target, exchangeEvidence{}, nil, fmt.Errorf("exchange invariant: unsupported provider request choice %d", selection.requestChoice)
	}
	usedMedia := session.ResolvedMedia{}
	if preparedOverride != nil {
		canonicalRequest = preparedOverride.request.Clone()
		usedMedia = preparedOverride.usedMedia.Clone()
	} else if requestHasImages(canonicalRequest) {
		historicalMedia := historicalMediaForAttempt(canonicalRequest, s.prepared.ResolvedMedia)
		command := &prepareProviderAttemptCommand{
			selection: selection, request: canonicalRequest.Clone(), semantic: s.prepared.Full.Clone(), protocol: backend.Target.ProtocolKind,
			policy: runner.Policy.ImageFetch, limits: runner.Policy.Limits.Media,
			fetcher: runner.ImageFetcher, fetchCache: cloneMediaFetchCache(s.mediaFetchCache), historical: historicalMedia,
		}
		return providerCall{}, path.target, exchangeEvidence{}, command, nil
	}
	attemptRequest, toolProjection, projectionDecisions, err := provider.ProjectAttemptTools(canonicalRequest)
	if err != nil {
		return providerCall{}, path.target, exchangeEvidence{}, nil, fmt.Errorf("provider tool projection: %w", err)
	}
	providerRequest := provider.Request{ExchangeID: s.input.exchangeID, Canonical: bindRequestToTarget(attemptRequest, path.target.Model), Delivery: path.delivery, Compatibility: runner.Policy.Compatibility, ToolProjection: toolProjection}
	evidence := exchangeEvidence{}
	evidence.decisions = append(evidence.decisions, projectionDecisions...)
	workspaceSlug := s.input.workspace.Slug().String()
	if err := validateCheckpointInput(runner, workspaceSlug); err != nil {
		return providerCall{}, path.target, exchangeEvidence{}, nil, err
	}
	contract := NewExecutionContract(s.input.clientDelivery).WithProviderDelivery(path.delivery)
	if err := contract.Validate(); err != nil {
		return providerCall{}, path.target, exchangeEvidence{}, nil, canonical.BadRequest("execution contract is invalid: " + err.Error())
	}
	doc, decisions, err := backend.Codec.Encode(providerRequest)
	evidence.decisions = append(evidence.decisions, decisions...)
	if err != nil {
		return providerCall{}, path.target, evidence, nil, fmt.Errorf("provider request encoding: %w", err)
	}
	resolvedMedia, err := s.prepared.ResolvedMedia.Merge(usedMedia)
	if err != nil {
		return providerCall{}, path.target, evidence, nil, fmt.Errorf("resolved media provenance: %w", err)
	}
	if err := session.ValidateResolvedRequestSize(s.prepared.Full, resolvedMedia, runner.Policy.Limits.MaxCheckpointBytes); err != nil {
		return providerCall{}, path.target, evidence, nil, preparationError(PreparationRequest, "checkpoint prepared-request preflight: %w", err)
	}
	return providerCall{
		backend: backend, request: providerRequest, document: doc, clientCodec: clientCodec,
		clientDelivery: s.input.clientDelivery, exchangeID: s.input.exchangeID,
		workspaceSlug: workspaceSlug, fullRequest: s.prepared.Full.Clone(),
		resolvedMedia: resolvedMedia,
	}, path.target, evidence, nil, nil
}

// rebaseAttemptMedia converts positions in the attempted request into the
// durable semantic request coordinate space. An attempted native delta is
// accepted only when its items are exactly the semantic transcript suffix.
func rebaseAttemptMedia(prepared session.ResolvedRequest, attempt canonical.CanonicalRequest, used session.ResolvedMedia) (session.ResolvedMedia, error) {
	semanticItems := prepared.Full.Items()
	attemptItems := attempt.Items()
	if len(attemptItems) > len(semanticItems) {
		return session.ResolvedMedia{}, fmt.Errorf("attempt items exceed semantic history")
	}
	offset := len(semanticItems) - len(attemptItems)
	if !reflect.DeepEqual(semanticItems[offset:], attemptItems) {
		return session.ResolvedMedia{}, fmt.Errorf("attempt items are not the semantic history suffix")
	}
	return used.ShiftItems(uint32(offset))
}

func historicalMediaForAttempt(request canonical.CanonicalRequest, media session.ResolvedMedia) session.ResolvedMedia {
	if previous, ok := request.PreviousResponse(); ok && previous.Responses != nil {
		// Native resumption does not resend historical request positions;
		// its delta positions begin at zero and must not collide with the
		// full-history binding coordinate space.
		return session.ResolvedMedia{}
	}
	return media.Clone()
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
	var events canonical.ResponseStream = newCanonicalToolProjectionStream(decoded.Stream, call.request.ToolProjection)
	events = newTerminalCompatibilityStream(events, decoded.TerminalDecisions, runner.DecisionSink, call.exchangeID)
	binding := canonical.ResponseBinding{SwobuID: swobuResponseID, TargetID: call.backend.Target.TargetID, TargetVersion: call.backend.Target.TargetVersion}
	events = canonical.NewBoundResponseIdentityStream(events, binding)
	events = canonical.NewValidatedResponseStream(events)
	events = session.NewCheckpointStream(events, session.CheckpointConfig{
		WorkspaceSlug: call.workspaceSlug,
		ExchangeID:    call.exchangeID,
		Binding:       binding,
		Store:         runner.CheckpointStore,
		Request:       call.fullRequest.Clone(),
		ResolvedMedia: call.resolvedMedia,
		MaxBytes:      runner.Policy.Limits.MaxCheckpointBytes,
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
		decoded, err = backend.Codec.Decode(ctx, call.request, resolved)
	case provider.DocumentIngress:
		if call.request.Delivery.Mode != delivery.Buffered {
			return provider.DecodedResponse{}, false, canonical.InternalError("provider wire document requires buffered delivery")
		}
		decoded, err = backend.Codec.Decode(ctx, call.request, resolved)
	default:
		return provider.DecodedResponse{}, false, canonical.InternalError("provider ingress carrier is unsupported")
	}
	return decoded, deliveryIsIncremental(call.clientDelivery, call.request.Delivery), err
}

func recordExchangeEvidenceBestEffort(ctx context.Context, sink compat.Sink, exchangeID string, evidence exchangeEvidence) {
	commitDecisionsBestEffort(ctx, sink, exchangeID, evidence.decisions)
}
