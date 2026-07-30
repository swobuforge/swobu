package exchange

import (
	"context"
	"fmt"
	"io"
	"reflect"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/session"
)

// prepareProviderCall is a reducer-owned deterministic edge. It resolves one
// exact backend and lowers one final wire document without external I/O.
func prepareProviderCall(s exchangeState, selection providerCallSelection, runner runtimeBundle, preparedOverride *attemptImagesMaterialized) (providerCall, provider.TargetSnapshot, []compat.Change, command, error) {
	target, ok := s.route.at(selection.candidateIndex)
	if !ok {
		return providerCall{}, provider.TargetSnapshot{}, nil, nil, fmt.Errorf("exchange invariant: provider candidate index %d is outside route plan", selection.candidateIndex)
	}
	path, err := resolveProviderPath(target)
	if err != nil {
		return providerCall{}, provider.TargetSnapshot{}, nil, nil, err
	}
	backend, err := runner.Runtime.ResolveBackend(path.target)
	if err != nil {
		return providerCall{}, path.target, nil, nil, err
	}
	if err := backend.Validate(); err != nil {
		return providerCall{}, path.target, nil, nil, canonical.InternalError("required provider backend is incomplete")
	}
	if !backend.Target.Equal(path.target) {
		return providerCall{}, path.target, nil, nil, canonical.InternalError("resolved provider backend changed target execution projection")
	}
	clientCodec := runner.Runtime.ClientCodec(s.input.clientFamily)
	if clientCodec == nil {
		return providerCall{}, path.target, nil, nil, canonical.InternalError("required client codec not resolved")
	}
	resolved := *s.prepared
	var canonicalRequest canonical.CanonicalRequest
	switch selection.requestChoice {
	case providerRequestPreferred:
		canonicalRequest = resolved.ForTarget(path.target)
	case providerRequestFullHistory:
		canonicalRequest = resolved.Full.Clone()
	default:
		return providerCall{}, path.target, nil, nil, fmt.Errorf("exchange invariant: unsupported provider request choice %d", selection.requestChoice)
	}
	usedMedia := session.ResolvedMedia{}
	if preparedOverride != nil {
		canonicalRequest = preparedOverride.request.Clone()
		usedMedia = preparedOverride.usedMedia.Clone()
	} else if requestHasImages(canonicalRequest) {
		historicalMedia := historicalMediaForAttempt(canonicalRequest, s.prepared.ResolvedMedia)
		command := materializeAttemptImagesCommand{
			selection: selection, request: canonicalRequest.Clone(), semantic: s.prepared.Full.Clone(),
			policy: runner.Policy.ImageFetch, limits: runner.Policy.Limits.Media,
			fetcher: runner.ImageFetcher, fetchCache: cloneMediaFetchCache(s.mediaFetchCache), historical: historicalMedia,
		}
		return providerCall{}, path.target, nil, command, nil
	}
	projectionFull := resolved.Full.Clone()
	projectionRequest := canonicalRequest.Clone()
	if s.mcp != nil {
		projectionFull, err = s.mcp.AttemptRequest(projectionFull)
		if err != nil {
			return providerCall{}, path.target, nil, nil, fmt.Errorf("MCP provider full attempt view: %w", err)
		}
		projectionRequest, err = s.mcp.AttemptRequest(projectionRequest)
		if err != nil {
			return providerCall{}, path.target, nil, nil, fmt.Errorf("MCP provider attempt view: %w", err)
		}
	}
	projection, err := provider.BuildToolProjection(projectionFull)
	if err != nil {
		return providerCall{}, path.target, nil, nil, fmt.Errorf("provider tool projection: %w", err)
	}
	attemptRequest, projectionDecisions, err := projection.Rewrite(projectionRequest)
	if err != nil {
		return providerCall{}, path.target, nil, nil, fmt.Errorf("provider attempt tool projection: %w", err)
	}
	projectedDecodeContext, _, err := projection.Rewrite(projectionFull)
	if err != nil {
		return providerCall{}, path.target, nil, nil, fmt.Errorf("provider decode-context tool projection: %w", err)
	}
	toolProjection := projection.Table()
	replaySafety, err := providerReplaySafetyFor(attemptRequest)
	if err != nil {
		return providerCall{}, path.target, nil, nil, fmt.Errorf("provider attempt replay safety: %w", err)
	}
	providerRequest := provider.Request{
		ExchangeID:     s.input.exchangeID,
		Canonical:      bindRequestToTarget(attemptRequest, path.target.Model),
		Delivery:       path.delivery,
		ToolProjection: toolProjection,
		MCPAccess:      s.input.mcpAccess,
	}
	requestChanges := compat.CloneChanges(projectionDecisions)
	workspaceSlug := s.input.workspace.Slug().String()
	if err := validateCheckpointInput(runner, workspaceSlug); err != nil {
		return providerCall{}, path.target, nil, nil, err
	}
	contract := NewExecutionContract(s.input.clientDelivery).WithProviderDelivery(path.delivery)
	if err := contract.Validate(); err != nil {
		return providerCall{}, path.target, nil, nil, canonical.BadRequest("execution contract is invalid: " + err.Error())
	}
	doc, changes, err := backend.Codec.Encode(providerRequest)
	requestChanges = append(requestChanges, changes...)
	if err != nil {
		return providerCall{}, path.target, requestChanges, nil, fmt.Errorf("provider request encoding: %w", err)
	}
	resolvedMedia, err := s.prepared.ResolvedMedia.Merge(usedMedia)
	if err != nil {
		return providerCall{}, path.target, requestChanges, nil, fmt.Errorf("resolved media provenance: %w", err)
	}
	return providerCall{
		backend: backend, request: providerRequest, document: doc, clientCodec: clientCodec,
		projectedDecodeContext: bindRequestToTarget(projectedDecodeContext, path.target.Model),
		clientDelivery:         s.input.clientDelivery, exchangeID: s.input.exchangeID,
		workspaceSlug: workspaceSlug, fullRequest: s.prepared.Full.Clone(),
		resolvedMedia:      resolvedMedia,
		advance:            s.advance,
		delayClientHandoff: delayClientHandoffFor(s.mcp),
		providerRound:      len(s.providerUsage),
		replaySafety:       replaySafety,
	}, path.target, requestChanges, nil, nil
}

func delayClientHandoffFor(run *mcp.Run) bool {
	return run != nil && run.CanExecute()
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
		return nil, nil, providerCompatibility{}, canonical.InternalError("provider ingress shape is invalid")
	}
	decoded, incremental, err := decodeProviderIngress(ctx, call, ingress, call.backend)
	compatibility := providerCompatibility{
		initial: compat.CloneChanges(decoded.Changes), progressive: decoded.ProgressiveChanges,
	}
	if err != nil {
		return nil, nil, compatibility, err
	}
	var events canonical.ResponseStream = newCanonicalToolProjectionStream(decoded.Stream, call.request.ToolProjection)
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
			return nil, nil, compatibility, err
		}
		response, err := envelope.ProjectResponse()
		if err != nil {
			return nil, nil, compatibility, err
		}
		cloned := response.Clone()
		compatibility.initial = compatibility.completedChanges()
		compatibility.progressive = nil
		return nil, &cloned, compatibility, nil
	}
	events = newTerminalResponseStream(events)
	response, err := handoffResponseStream(ctx, call, events, binding, incremental, runner)
	return response, nil, compatibility, err
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
		store: runner.CheckpointStore, request: call.fullRequest.Clone(),
		resolvedMedia: call.resolvedMedia.Clone(), advance: call.advance,
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
	decodeRequest.Canonical = call.projectedDecodeContext.Clone()
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
