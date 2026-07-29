package session

import (
	"errors"
	"fmt"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// ResolvedRequest contains the complete canonical request and the current delta
// with any exact-target native-resumption handle inherited from a checkpoint.
type ResolvedRequest struct {
	Full          canonical.CanonicalRequest
	Delta         canonical.CanonicalRequest
	ResolvedMedia ResolvedMedia
}

// ForTarget returns the valid request representation for one exact target
// generation. A matching native handle selects Delta; every other target
// receives Full for full-history execution.
func (p ResolvedRequest) ForTarget(target provider.TargetSnapshot) canonical.CanonicalRequest {
	if previous, ok := p.Delta.PreviousResponse(); ok && previous.Responses != nil &&
		previous.Responses.AppliesTo(target.TargetID, target.TargetVersion) {
		return p.Delta.Clone()
	}
	return p.Full.Clone()
}

// PreviousSwobuResponseID returns the explicit workspace-local checkpoint key
// carried by the canonical request, if present. It performs no store access.
func PreviousSwobuResponseID(request canonical.CanonicalRequest) (canonical.SwobuResponseID, bool, error) {
	prev, ok := request.PreviousResponse()
	if !ok {
		return "", false, nil
	}
	if err := prev.ValidatePreviousResponseSelector(); err != nil {
		return "", false, canonical.BadRequest("previous_response_id is empty")
	}
	return prev.SwobuID, true, nil
}

// Begin resolves a request that has no predecessor checkpoint. A request that
// names one belongs on the Resume path and is rejected as an orchestration bug.
func Begin(request canonical.CanonicalRequest) (ResolvedRequest, error) {
	if _, ok := request.PreviousResponse(); ok {
		return ResolvedRequest{}, errors.New("session begin request contains previous response")
	}
	complete, err := withoutPreviousResponse(request)
	if err != nil {
		return ResolvedRequest{}, err
	}
	current := requestWithoutPreviousResponse(request)
	return newResolvedRequest(complete, current, ResolvedMedia{})
}

// Resume applies request to one already-loaded immutable checkpoint. The
// request and checkpoint response are the two identity sources; equality is
// proven here once and no store access occurs.
func Resume(request canonical.CanonicalRequest, checkpoint Checkpoint) (ResolvedRequest, error) {
	requestedSwobuResponseID, found, err := PreviousSwobuResponseID(request)
	if err != nil {
		return ResolvedRequest{}, err
	}
	if !found {
		return ResolvedRequest{}, canonical.BadRequest("unknown previous_response_id")
	}
	response := checkpoint.Response.Response()
	if err := response.ValidateCommittedResponse(); err != nil {
		return ResolvedRequest{}, fmt.Errorf("invalid session checkpoint response reference: %w", err)
	}
	if response.SwobuID != requestedSwobuResponseID {
		return ResolvedRequest{}, canonical.BadRequest("unknown previous_response_id")
	}
	if err := checkpoint.ResolvedMedia.ValidateForRequest(checkpoint.Request); err != nil {
		return ResolvedRequest{}, fmt.Errorf("invalid session checkpoint media: %w", err)
	}
	effective, err := resolveTurnContinuation(checkpoint, request)
	if err != nil {
		return ResolvedRequest{}, err
	}
	full, err := materialize(checkpoint, effective)
	if err != nil {
		return ResolvedRequest{}, err
	}
	return newResolvedRequest(full, nativeDelta(checkpoint.Request, effective, &response), checkpoint.ResolvedMedia)
}

// ResumeHistory uses the complete supplied request only as the history value
// whose fingerprint selected checkpoint. After that identity proof, checkpoint
// canonical truth is authoritative: materialization restores hidden opaque thinking
// that no client projection was allowed to carry.
func ResumeHistory(complete canonical.CanonicalRequest, rebased canonical.CanonicalRequest, checkpoint Checkpoint) (ResolvedRequest, error) {
	if _, ok := complete.PreviousResponse(); ok {
		return ResolvedRequest{}, errors.New("implicit history request contains explicit previous response")
	}
	if _, ok := rebased.PreviousResponse(); ok {
		return ResolvedRequest{}, errors.New("rebased history request contains explicit previous response")
	}
	response := checkpoint.Response.Response()
	if err := response.ValidateCommittedResponse(); err != nil {
		return ResolvedRequest{}, fmt.Errorf("invalid history checkpoint response reference: %w", err)
	}
	if err := checkpoint.ResolvedMedia.ValidateForRequest(checkpoint.Request); err != nil {
		return ResolvedRequest{}, fmt.Errorf("invalid history checkpoint media: %w", err)
	}
	effective, err := resolveTurnContinuation(checkpoint, rebased)
	if err != nil {
		return ResolvedRequest{}, err
	}
	full, err := materialize(checkpoint, effective)
	if err != nil {
		return ResolvedRequest{}, err
	}
	return newResolvedRequest(full, nativeDelta(checkpoint.Request, effective, &response), checkpoint.ResolvedMedia)
}

// newResolvedRequest validates only the fully materialized request. Delta may
// intentionally omit state retained behind an exact provider continuation
// handle, so applying full-history invariants to it would reject valid native
// resumptions.
func newResolvedRequest(full, delta canonical.CanonicalRequest, media ResolvedMedia) (ResolvedRequest, error) {
	if err := canonical.ValidateMaterializedRequest(full); err != nil {
		return ResolvedRequest{}, err
	}
	return ResolvedRequest{Full: full, Delta: delta, ResolvedMedia: media.Clone()}, nil
}

// resolveTurnContinuation validates tool-result correlation and writes the
// ongoing assistant turn's compute and effort into the one effective request.
// The checkpoint request is the sole authority for omitted continuation values.
func resolveTurnContinuation(checkpoint Checkpoint, current canonical.CanonicalRequest) (canonical.CanonicalRequest, error) {
	pendingSet := make(map[canonical.ToolCallID]canonical.ToolKind)
	for _, item := range checkpoint.Response.Items() {
		call, ok := item.ToolCall()
		if !ok {
			continue
		}
		pendingSet[call.CallID()] = call.Tool().Kind()
	}
	matched := make(map[canonical.ToolCallID]struct{}, len(pendingSet))
	for _, item := range current.Items() {
		result, ok := item.ToolResult()
		if !ok {
			continue
		}
		kind, expected := pendingSet[result.CallID()]
		if !expected {
			return canonical.CanonicalRequest{}, canonical.BadRequest("tool result does not belong to the unfinished assistant turn")
		}
		_, searchResult := result.WebSearch()
		if kind == canonical.ToolKindWebSearch {
			return canonical.CanonicalRequest{}, canonical.BadRequest("client cannot resolve an exchange-resolved web-search call")
		}
		if searchResult {
			return canonical.CanonicalRequest{}, canonical.BadRequest("caller-resolved tool call requires a content result")
		}
		if _, duplicate := matched[result.CallID()]; duplicate {
			return canonical.CanonicalRequest{}, canonical.BadRequest("unfinished assistant turn contains a duplicate tool result")
		}
		matched[result.CallID()] = struct{}{}
	}
	if len(matched) == 0 {
		return current.Clone(), nil
	}
	current, err := repeatUnfinishedTurnContext(checkpoint.Request, current)
	if err != nil {
		return canonical.CanonicalRequest{}, err
	}
	compute := current.Reasoning().ComputeField()
	priorCompute := checkpoint.Request.Reasoning().ComputeField()
	if explicit, ok := compute.Get(); ok {
		prior, priorSet := priorCompute.Get()
		if !priorSet || !equalReasoningCompute(explicit, prior) {
			return canonical.CanonicalRequest{}, canonical.BadRequest("current reasoning compute conflicts with unfinished tool turn")
		}
	} else {
		compute = priorCompute
	}
	controls := current.Controls()
	priorEffort := checkpoint.Request.Controls().Effort
	if explicit, ok := controls.Effort.Get(); ok {
		prior, priorSet := priorEffort.Get()
		if !priorSet || explicit != prior {
			return canonical.CanonicalRequest{}, canonical.BadRequest("current inference effort conflicts with unfinished tool turn")
		}
	} else {
		controls.Effort = priorEffort
	}
	reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Compute: compute, Disclosure: current.Reasoning().DisclosureField(),
		ResponsesContext: current.Reasoning().ResponsesContextField(),
	})
	if err != nil {
		return canonical.CanonicalRequest{}, err
	}
	return replaceComputeControls(current, controls, reasoning), nil
}

func repeatUnfinishedTurnContext(previous, current canonical.CanonicalRequest) (canonical.CanonicalRequest, error) {
	previousPrelude, _, previousErr := canonical.SplitRequestPrelude(previous.Items())
	currentPrelude, currentHistory, currentErr := canonical.SplitRequestPrelude(current.Items())
	if previousErr != nil {
		return canonical.CanonicalRequest{}, fmt.Errorf("invalid checkpoint request context: %w", previousErr)
	}
	if currentErr != nil {
		return canonical.CanonicalRequest{}, canonical.BadRequest("request-scoped context must precede history")
	}
	previousDirectives := previousPrelude.Directives()
	previousTools := previousPrelude.Declarations()
	previousToolsFirst := previousPrelude.ToolsFirst()
	currentDirectives := currentPrelude.Directives()
	currentTools := currentPrelude.Declarations()
	currentToolsFirst := currentPrelude.ToolsFirst()
	directives := currentDirectives
	if len(directives) == 0 {
		directives = previousDirectives
	}
	tools := currentTools
	if len(tools) == 0 {
		tools = previousTools
	}
	toolsFirst := previousToolsFirst
	if len(currentDirectives) > 0 && len(currentTools) > 0 {
		toolsFirst = currentToolsFirst
	}
	items := make([]canonical.CanonicalItem, 0, len(directives)+len(tools)+len(currentHistory))
	if toolsFirst {
		items = append(items, tools...)
		items = append(items, directives...)
	} else {
		items = append(items, directives...)
		items = append(items, tools...)
	}
	items = append(items, currentHistory...)
	return replaceRequestItems(current, items), nil
}

func replaceRequestItems(request canonical.CanonicalRequest, items []canonical.CanonicalItem) canonical.CanonicalRequest {
	previous, hasPrevious := request.PreviousResponse()
	var previousPointer *canonical.ResponseRef
	if hasPrevious {
		previousPointer = &previous
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: request.ModelField(), Items: items, PreviousResponse: previousPointer,
		ToolPolicy: request.ToolPolicyField(), ToolCallBatch: request.ToolCallBatchField(),
		Controls: request.Controls(), Reasoning: request.Reasoning(), OutputFormat: request.OutputFormatField(),
	})
}

func equalReasoningCompute(left, right canonical.ReasoningCompute) bool {
	if left.Kind() != right.Kind() || left.Kind() == "" {
		return false
	}
	leftTokens, leftBudget := left.Tokens()
	rightTokens, rightBudget := right.Tokens()
	return leftBudget == rightBudget && (!leftBudget || leftTokens == rightTokens)
}

func replaceComputeControls(request canonical.CanonicalRequest, controls canonical.GenerationControls, reasoning canonical.ReasoningControls) canonical.CanonicalRequest {
	previous, _ := request.PreviousResponse()
	var previousPointer *canonical.ResponseRef
	if _, ok := request.PreviousResponse(); ok {
		previousPointer = &previous
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: request.ModelField(), Items: request.Items(),
		PreviousResponse: previousPointer, ToolPolicy: request.ToolPolicyField(),
		ToolCallBatch: request.ToolCallBatchField(), Controls: controls, Reasoning: reasoning,
		OutputFormat: request.OutputFormatField(),
	})
}

func withoutPreviousResponse(request canonical.CanonicalRequest) (canonical.CanonicalRequest, error) {
	toolPolicy, err := request.EffectiveToolPolicy()
	if err != nil {
		return canonical.CanonicalRequest{}, err
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         canonical.Specify(request.Model()),
		Items:         cloneCanonicalItems(request.Items()),
		ToolPolicy:    canonical.Specify(toolPolicy),
		ToolCallBatch: canonical.Specify(request.ToolCallBatch()),
		Controls:      request.Controls(),
		Reasoning:     request.Reasoning(),
		OutputFormat:  canonical.Specify(request.OutputFormat()),
	}), nil
}

func requestWithoutPreviousResponse(request canonical.CanonicalRequest) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         request.ModelField(),
		Items:         cloneCanonicalItems(request.Items()),
		ToolPolicy:    request.ToolPolicyField(),
		ToolCallBatch: request.ToolCallBatchField(),
		Controls:      request.Controls(),
		Reasoning:     request.Reasoning(),
		OutputFormat:  request.OutputFormatField(),
	})
}

func nativeDelta(previous canonical.CanonicalRequest, current canonical.CanonicalRequest, previousResponse *canonical.ResponseRef) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:            canonical.Specify(inheritString(current.ModelSpecified(), current.Model(), previous.Model())),
		Items:            cloneCanonicalItems(current.Items()),
		PreviousResponse: previousResponse,
		ToolPolicy:       current.ToolPolicyField(),
		ToolCallBatch:    current.ToolCallBatchField(),
		Controls:         current.Controls(),
		Reasoning:        current.Reasoning(),
		OutputFormat:     current.OutputFormatField(),
	})
}

func materialize(previous Checkpoint, current canonical.CanonicalRequest) (canonical.CanonicalRequest, error) {
	prelude, currentHistory, err := canonical.SplitRequestPrelude(current.Items())
	if err != nil {
		return canonical.CanonicalRequest{}, canonical.BadRequest("request-scoped context must precede history")
	}
	items := prelude.Items()
	items = append(items, canonical.RetainedHistory(previous.Request.Items())...)
	items = append(items, cloneCanonicalItems(previous.Response.Items())...)
	items = append(items, currentHistory...)

	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         canonical.Specify(inheritString(current.ModelSpecified(), current.Model(), previous.Request.Model())),
		Items:         items,
		ToolPolicy:    current.ToolPolicyField(),
		ToolCallBatch: current.ToolCallBatchField(),
		Controls:      current.Controls(),
		Reasoning:     current.Reasoning(),
		OutputFormat:  current.OutputFormatField(),
	}), nil
}

func cloneCanonicalItems(items []canonical.CanonicalItem) []canonical.CanonicalItem {
	if items == nil {
		return nil
	}
	cloned := make([]canonical.CanonicalItem, len(items))
	for i := range items {
		cloned[i] = items[i].Clone()
	}
	return cloned
}

func inheritString(present bool, current, previous string) string {
	if present {
		return current
	}
	return previous
}
