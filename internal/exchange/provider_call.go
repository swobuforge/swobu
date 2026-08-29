package exchange

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/provider"
)

type responseProcessingError struct {
	stage string
	err   error
}

func (e responseProcessingError) Error() string { return e.err.Error() }
func (e responseProcessingError) Unwrap() error { return e.err }

func responseFailure(stage string, err error) error {
	if err == nil {
		return nil
	}
	var staged responseProcessingError
	if errors.As(err, &staged) {
		return err
	}
	return responseProcessingError{stage: stage, err: err}
}

// prepareProviderCall is a reducer-owned deterministic edge. It resolves one
// exact backend and lowers one final wire document without external I/O.
func prepareProviderCall(ctx context.Context, s exchangeState, selection providerCallSelection, runner runtimeBundle) (providerCall, provider.TargetSnapshot, []compat.Change, mediaFetchCache, error) {
	target, ok := s.route.at(selection.candidateIndex)
	if !ok {
		return providerCall{}, provider.TargetSnapshot{}, nil, s.mediaFetchCache, fmt.Errorf("exchange invariant: provider candidate index %d is outside route plan", selection.candidateIndex)
	}
	path, err := resolveProviderPath(target)
	if err != nil {
		return providerCall{}, provider.TargetSnapshot{}, nil, s.mediaFetchCache, err
	}
	backend, err := runner.Runtime.ResolveBackend(path.target)
	if err != nil {
		return providerCall{}, path.target, nil, s.mediaFetchCache, err
	}
	if err := backend.Validate(); err != nil {
		return providerCall{}, path.target, nil, s.mediaFetchCache, canonical.InternalError("required provider backend is incomplete")
	}
	if !backend.Target.Equal(path.target) {
		return providerCall{}, path.target, nil, s.mediaFetchCache, canonical.InternalError("resolved provider backend changed target execution projection")
	}
	clientCodec := runner.Runtime.ClientCodec(s.input.clientFamily)
	if clientCodec == nil {
		return providerCall{}, path.target, nil, s.mediaFetchCache, canonical.InternalError("required client codec not resolved")
	}
	resolved := *s.prepared
	if selection.requestChoice != providerRequestPreferred && selection.requestChoice != providerRequestFullHistory && selection.requestChoice != providerRequestReprojected {
		return providerCall{}, path.target, nil, s.mediaFetchCache, fmt.Errorf("exchange invariant: unsupported provider request choice %d", selection.requestChoice)
	}
	fetchCache := cloneMediaFetchCache(s.mediaFetchCache)
	attemptRequest := resolved.Request()
	if s.mcp != nil {
		attemptRequest, err = s.mcp.AttemptRequest(attemptRequest)
		if err != nil {
			return providerCall{}, path.target, nil, s.mediaFetchCache, err
		}
	}
	projectedRequest, replayChanged, replayChanges, err := projectOpaqueReplayForTarget(attemptRequest, path.target.TargetID, path.target.TargetVersion)
	if err != nil {
		return providerCall{}, path.target, nil, s.mediaFetchCache, err
	}
	attemptRequest = projectedRequest

	projectionChanges := []compat.Change(nil)
	projectionChanges = append(projectionChanges, replayChanges...)
	toolNames, namingChanges, err := provider.BuildAttemptToolNames(attemptRequest)
	if err != nil {
		return providerCall{}, path.target, nil, s.mediaFetchCache, err
	}
	generation := targetExceptionGeneration{
		workspace: s.input.workspace.Slug().String(), targetID: path.target.TargetID, targetVersion: path.target.TargetVersion,
	}
	targetFacts := targetFactsForAttempt(runner.TargetExceptions, generation)
	providerRequest := provider.Request{
		ExchangeID:    s.input.exchangeID,
		CacheLocality: s.cacheLocality,
		Canonical:     bindRequestToTarget(attemptRequest, path.target.Model),
		TargetFacts:   targetFacts,
		Delivery:      path.delivery,
		ToolNames:     toolNames,
	}
	preparationCtx := ctx
	cancel := func() {}
	if timeout := runner.Policy.ImageFetch.TotalPreparationTimeout(); timeout > 0 {
		preparationCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	providerRequest.EncodeContext = provider.EncodeContext{
		Context:               preparationCtx,
		ResolveImage:          newImageResolver(runner.Policy.ImageFetch, runner.Policy.Limits.Media, runner.ImageFetcher, &fetchCache),
		HasNextRouteCandidate: hasNextRouteCandidate(s, selection),
	}
	// Native continuation is exact only while the target-bound replay sequence
	// still matches the canonical sequence represented by that handle. Semantic
	// loss evidence is deliberately irrelevant to this execution choice.
	if providerHistoryHandleEligible(selection.requestChoice, replayChanged) {
		if previous, ok := resolved.PreviousHistory(path.target.TargetID, path.target.TargetVersion); ok {
			providerRequest.PreviousHistory = &previous
		}
	}
	requestChanges := compat.CloneChanges(projectionChanges)
	requestChanges = append(requestChanges, namingChanges...)
	workspaceSlug := s.input.workspace.Slug().String()
	if err := validateCheckpointInput(runner, workspaceSlug); err != nil {
		return providerCall{}, path.target, nil, s.mediaFetchCache, err
	}
	contract := NewExecutionContract(s.input.clientDelivery).WithProviderDelivery(path.delivery)
	if err := contract.Validate(); err != nil {
		return providerCall{}, path.target, nil, s.mediaFetchCache, canonical.BadRequest("execution contract is invalid: " + err.Error())
	}
	doc, changes, err := backend.Codec.Encode(providerRequest)
	factReads := targetFacts.Reads()
	requestChanges = append(requestChanges, changes...)
	if err != nil {
		return providerCall{}, path.target, requestChanges, fetchCache, fmt.Errorf("provider request encoding: %w", err)
	}
	return providerCall{
		backend: backend, request: providerRequest, document: doc, clientCodec: clientCodec,
		decodeContext:  bindRequestToTarget(attemptRequest, path.target.Model),
		clientDelivery: s.input.clientDelivery, exchangeID: s.input.exchangeID,
		workspaceSlug: workspaceSlug, fullRequest: resolved.Request(),
		historyScheme:      s.input.requestFingerprint.Scheme(),
		targetGeneration:   generation,
		factReads:          factReads,
		advance:            s.advance,
		sessionID:          s.sessionID,
		expectedHead:       s.expectedHead,
		delayClientHandoff: delayClientHandoffFor(s.mcp),
		providerRound:      len(s.providerUsage),
	}, path.target, requestChanges, fetchCache, nil
}

// providerHistoryHandleEligible depends only on whether target-bound replay still
// has the structure represented by the provider handle. Compatibility evidence
// cannot recover or alter that structural fact.
func providerHistoryHandleEligible(choice providerRequestChoice, replayChanged bool) bool {
	return choice != providerRequestFullHistory && !replayChanged
}

// hasNextRouteCandidate exposes route order only as request-scoped encoding
// context. It intentionally does not enter the target snapshot or route state.
func hasNextRouteCandidate(s exchangeState, selection providerCallSelection) bool {
	_, exists := nextEligibleRouteCandidate(s, selection.candidateIndex+1)
	return exists
}

func delayClientHandoffFor(run *mcp.Run) bool {
	return run != nil && run.CanExecute()
}

// completeProviderCall is a reducer-owned response edge. It validates and
// decodes provider ingress before deciding the final client handoff.
type providerCompatibility struct {
	initial     []compat.Change
	progressive func() []compat.Change
}

func (c providerCompatibility) completedChanges() []compat.Change {
	changes := compat.CloneChanges(c.initial)
	if c.progressive != nil {
		changes = append(changes, c.progressive()...)
	}
	return changes
}

func completeProviderCall(ctx context.Context, call providerCall, ingress provider.Ingress, swobuResponseID canonical.SwobuResponseID, runner runtimeBundle) (ClientResponse, *canonical.CanonicalResponse, providerCompatibility, error) {
	if err := provider.ValidateIngress(ingress); err != nil {
		return nil, nil, providerCompatibility{}, responseFailure("provider_stream_decode", canonical.InternalError("provider ingress shape is invalid"))
	}
	decoded, incremental, err := decodeProviderIngress(ctx, call, ingress, call.backend)
	compatibility := providerCompatibility{
		initial: compat.CloneChanges(decoded.Changes), progressive: decoded.ProgressiveChanges,
	}
	if err != nil {
		return nil, nil, compatibility, responseFailure("provider_stream_decode", err)
	}
	var events canonical.ResponseStream = decoded.Stream
	binding := canonical.ResponseBinding{SwobuID: swobuResponseID, TargetID: call.backend.Target.TargetID, TargetVersion: call.backend.Target.TargetVersion}
	events = canonical.NewBoundResponseIdentityStream(events, binding)
	events = canonical.NewValidatedResponseStream(events)
	if call.delayClientHandoff {
		defer events.Close(ctx)
		envelope, err := canonical.ReadClosedEnvelope(ctx, events, canonical.EnvResponse)
		if err != nil {
			if err == io.EOF {
				err = canonical.InternalError("provider response ended before a complete tool round")
			}
			return nil, nil, compatibility, responseFailure("canonical_response_validation", err)
		}
		response, err := envelope.ProjectResponse()
		if err != nil {
			return nil, nil, compatibility, responseFailure("canonical_response_validation", err)
		}
		cloned := response.Clone()
		compatibility.initial = compatibility.completedChanges()
		compatibility.progressive = nil
		return nil, &cloned, compatibility, nil
	}
	events = newTerminalResponseStream(events)
	response, err := handoffResponseStream(ctx, call, events, binding, incremental, runner)
	return response, nil, compatibility, responseFailure("client_stream_encode", err)
}

func handoffCompletedProviderResponse(ctx context.Context, call providerCall, response canonical.CanonicalResponse, runner runtimeBundle) (ClientResponse, error) {
	events := canonical.SynthesizeResponseEnvelopeEvents(
		call.exchangeID, response.Response(), response.Model(), response.Items(),
		response.Completion(), response.Usage(),
	)
	binding := canonical.ResponseBinding{
		SwobuID: response.Response().SwobuID, TargetID: call.backend.Target.TargetID,
		TargetVersion: call.backend.Target.TargetVersion,
	}
	return handoffResponseStream(ctx, call, canonical.NewSliceEventReader(events), binding, false, runner)
}

func handoffResponseStream(ctx context.Context, call providerCall, stream canonical.ResponseStream, binding canonical.ResponseBinding, incremental bool, runner runtimeBundle) (ClientResponse, error) {
	capture := newCheckpointCaptureResponseStream(stream, binding)
	committer := &checkpointCommitter{
		exchangeID: call.exchangeID, workspaceSlug: call.workspaceSlug,
		store: runner.CheckpointStore, request: call.fullRequest.Clone(), historyScheme: call.historyScheme,
		advance: call.advance, sessionID: call.sessionID, expectedHead: call.expectedHead,
	}
	gated := newCheckpointTerminalGate(capture, call.clientCodec, call.fullRequest, committer)
	return encodeClientOutput(ctx, call, gated, incremental)
}

func cloneSwobuResponseID(id *canonical.SwobuResponseID) *canonical.SwobuResponseID {
	if id == nil {
		return nil
	}
	cloned := *id
	return &cloned
}

func decodeProviderIngress(ctx context.Context, call providerCall, ingress provider.Ingress, backend provider.Backend) (provider.DecodedResponse, bool, error) {
	var decoded provider.DecodedResponse
	var err error
	decodeRequest := call.request
	decodeRequest.Canonical = call.decodeContext.Clone()
	switch resolved := ingress.(type) {
	case provider.StreamIngress:
		if call.request.Delivery.Mode != delivery.Streaming {
			return provider.DecodedResponse{}, false, canonical.InternalError("provider wire stream requires streaming delivery")
		}
		decoded, err = backend.Codec.Decode(ctx, decodeRequest, resolved)
	case provider.DocumentIngress:
		if call.request.Delivery.Mode != delivery.Buffered {
			return provider.DecodedResponse{}, false, canonical.InternalError("provider wire document requires buffered delivery")
		}
		decoded, err = backend.Codec.Decode(ctx, decodeRequest, resolved)
	default:
		return provider.DecodedResponse{}, false, canonical.InternalError("provider ingress carrier is unsupported")
	}
	return decoded, deliveryIsIncremental(call.clientDelivery, call.request.Delivery), err
}
