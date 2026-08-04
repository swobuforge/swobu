package session

import (
	"errors"
	"fmt"
	"math"
	"reflect"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ResolvedRequest contains one complete canonical request and the optional
// exact Responses history projection inherited from a checkpoint.
type ResolvedRequest struct {
	request           canonical.CanonicalRequest
	responsesPrevious *responsesPrevious
}

type responsesPrevious struct {
	response  canonical.ResponseRef
	omitItems requestItemRange
}

type requestItemRange struct {
	start uint32
	end   uint32
}

type materializedRequest struct {
	request         canonical.CanonicalRequest
	previousHistory requestItemRange
}

// Draft is transient session composition state used only while request-scoped
// MCP preparation may still update the current prelude.
type Draft struct {
	current    canonical.CanonicalRequest
	checkpoint *Checkpoint
}

func (r ResolvedRequest) Request() canonical.CanonicalRequest { return r.request.Clone() }
func (r ResolvedRequest) HasResponsesPrevious() bool          { return r.responsesPrevious != nil }

// AppendLocalRound returns a new complete request after one Swobu-executed MCP
// round. The local round is not represented by any prior provider response, so
// reusable Responses continuation state is intentionally cleared.
func (r ResolvedRequest) AppendLocalRound(responseItems, resultItems []canonical.CanonicalItem) (ResolvedRequest, error) {
	items := r.request.Items()
	items = append(items, cloneCanonicalItems(responseItems)...)
	items = append(items, cloneCanonicalItems(resultItems)...)
	return newResolvedRequest(r.request.WithItems(items), nil)
}

// ResponsesPrevious returns exact provider lowering data only for the target
// generation that produced the reusable OpenAI Responses state.
func (r ResolvedRequest) ResponsesPrevious(targetID string, targetVersion uint64) (canonical.ResponsesResponseID, uint32, uint32, bool) {
	previous := r.responsesPrevious
	if previous == nil || previous.response.Responses == nil ||
		!previous.response.Responses.AppliesTo(targetID, targetVersion) {
		return "", 0, 0, false
	}
	return previous.response.Responses.ProviderResponseID, previous.omitItems.start, previous.omitItems.end, true
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
	draft, err := PrepareBegin(request)
	if err != nil {
		return ResolvedRequest{}, err
	}
	return draft.Finalize(draft.Current())
}

// Resume applies request to one already-loaded immutable checkpoint. The
// request and checkpoint response are the two identity sources; equality is
// proven here once and no store access occurs.
func Resume(request canonical.CanonicalRequest, checkpoint Checkpoint) (ResolvedRequest, error) {
	draft, err := PrepareResume(request, checkpoint)
	if err != nil {
		return ResolvedRequest{}, err
	}
	return draft.Finalize(draft.Current())
}

// PrepareBegin validates and exposes one current invocation before its
// request-scoped MCP context is finalized.
func PrepareBegin(request canonical.CanonicalRequest) (Draft, error) {
	if _, ok := request.PreviousResponse(); ok {
		return Draft{}, errors.New("session begin request contains previous response")
	}
	current, err := materializeBeginRequest(request)
	if err != nil {
		return Draft{}, err
	}
	if err := canonical.ValidateMaterializedRequest(current); err != nil {
		return Draft{}, err
	}
	return Draft{current: current.Clone()}, nil
}

func materializeBeginRequest(request canonical.CanonicalRequest) (canonical.CanonicalRequest, error) {
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
		Responses:     request.Responses(),
	}), nil
}

// PrepareResume validates checkpoint identity and restores unfinished-turn
// state before request-scoped MCP preparation.
func PrepareResume(request canonical.CanonicalRequest, checkpoint Checkpoint) (Draft, error) {
	requestedSwobuResponseID, hasExplicit, err := PreviousSwobuResponseID(request)
	if err != nil {
		return Draft{}, err
	}
	response := checkpoint.Response.Response()
	if err := response.ValidateCommittedResponse(); err != nil {
		return Draft{}, fmt.Errorf("invalid session checkpoint response reference: %w", err)
	}
	if hasExplicit && response.SwobuID != requestedSwobuResponseID {
		return Draft{}, canonical.BadRequest("unknown previous_response_id")
	}
	current, err := withoutPreviousResponse(request)
	if err != nil {
		return Draft{}, err
	}
	effective, err := resolveTurnContinuation(checkpoint, current)
	if err != nil {
		return Draft{}, err
	}
	cloned := checkpoint.Clone()
	return Draft{current: effective.Clone(), checkpoint: &cloned}, nil
}

// Current returns the invocation-local request that MCP may prepare.
func (d Draft) Current() canonical.CanonicalRequest { return d.current.Clone() }

// Finalize freezes one resolved request after proving that preparation changed
// only the request-scoped prelude and preserved the current history verbatim.
func (d Draft) Finalize(prepared canonical.CanonicalRequest) (ResolvedRequest, error) {
	if _, ok := prepared.PreviousResponse(); ok {
		return ResolvedRequest{}, errors.New("prepared current request contains previous response")
	}
	_, originalHistory, err := canonical.SplitRequestPrelude(d.current.Items())
	if err != nil {
		return ResolvedRequest{}, fmt.Errorf("session draft current request is invalid: %w", err)
	}
	_, preparedHistory, err := canonical.SplitRequestPrelude(prepared.Items())
	if err != nil {
		return ResolvedRequest{}, fmt.Errorf("prepared request-scoped context must precede history: %w", err)
	}
	if !reflect.DeepEqual(originalHistory, preparedHistory) {
		return ResolvedRequest{}, errors.New("request-scoped preparation rewrote current history")
	}
	if !reflect.DeepEqual(d.current.WithItems(prepared.Items()), prepared) {
		return ResolvedRequest{}, errors.New("request-scoped preparation changed non-item request state")
	}
	if d.checkpoint == nil {
		return newResolvedRequest(prepared, nil)
	}
	materialized, err := materialize(*d.checkpoint, prepared)
	if err != nil {
		return ResolvedRequest{}, err
	}
	return newResolvedRequest(materialized.request, newResponsesPrevious(d.checkpoint.Response.Response(), materialized.previousHistory))
}

func newResolvedRequest(request canonical.CanonicalRequest, previous *responsesPrevious) (ResolvedRequest, error) {
	if _, ok := request.PreviousResponse(); ok {
		return ResolvedRequest{}, errors.New("resolved complete request contains previous response")
	}
	if err := canonical.ValidateMaterializedRequest(request); err != nil {
		return ResolvedRequest{}, err
	}
	if previous != nil {
		if err := previous.validateFor(request); err != nil {
			return ResolvedRequest{}, err
		}
	}
	return ResolvedRequest{request: request.Clone(), responsesPrevious: cloneResponsesPrevious(previous)}, nil
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
		Responses: request.Responses(),
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
		OutputFormat: request.OutputFormatField(), Responses: request.Responses(),
	})
}

func withoutPreviousResponse(request canonical.CanonicalRequest) (canonical.CanonicalRequest, error) {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         request.ModelField(),
		Items:         cloneCanonicalItems(request.Items()),
		ToolPolicy:    request.ToolPolicyField(),
		ToolCallBatch: request.ToolCallBatchField(),
		Controls:      request.Controls(),
		Reasoning:     request.Reasoning(),
		OutputFormat:  request.OutputFormatField(),
		Responses:     request.Responses(),
	}), nil
}

func materialize(previous Checkpoint, current canonical.CanonicalRequest) (materializedRequest, error) {
	prelude, currentHistory, err := canonical.SplitRequestPrelude(current.Items())
	if err != nil {
		return materializedRequest{}, canonical.BadRequest("request-scoped context must precede history")
	}
	items := prelude.Items()
	start, err := checkedRequestItemIndex(len(items))
	if err != nil {
		return materializedRequest{}, err
	}
	items = append(items, canonical.RetainedHistory(previous.Request.Items())...)
	items = append(items, cloneCanonicalItems(previous.Response.Items())...)
	end, err := checkedRequestItemIndex(len(items))
	if err != nil {
		return materializedRequest{}, err
	}
	items = append(items, currentHistory...)

	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         canonical.Specify(inheritString(current.ModelSpecified(), current.Model(), previous.Request.Model())),
		Items:         items,
		ToolPolicy:    current.ToolPolicyField(),
		ToolCallBatch: current.ToolCallBatchField(),
		Controls:      current.Controls(),
		Reasoning:     current.Reasoning(),
		OutputFormat:  current.OutputFormatField(),
		Responses:     current.Responses(),
	})
	return materializedRequest{request: request, previousHistory: requestItemRange{start: start, end: end}}, nil
}

func checkedRequestItemIndex(value int) (uint32, error) {
	if value < 0 || uint64(value) > math.MaxUint32 {
		return 0, errors.New("resolved request item count exceeds coordinate range")
	}
	return uint32(value), nil
}

func newResponsesPrevious(response canonical.ResponseRef, replacedHistory requestItemRange) *responsesPrevious {
	if response.Responses == nil {
		return nil
	}
	return &responsesPrevious{response: response.Clone(), omitItems: replacedHistory}
}

func cloneResponsesPrevious(previous *responsesPrevious) *responsesPrevious {
	if previous == nil {
		return nil
	}
	return &responsesPrevious{response: previous.response.Clone(), omitItems: previous.omitItems}
}

func (p responsesPrevious) validateFor(request canonical.CanonicalRequest) error {
	if p.response.Responses == nil {
		return errors.New("responses previous relation is missing provider continuation")
	}
	items := request.Items()
	if p.omitItems.start > p.omitItems.end || uint64(p.omitItems.end) > uint64(len(items)) {
		return errors.New("responses previous history range is invalid")
	}
	prelude, _, err := canonical.SplitRequestPrelude(items)
	if err != nil {
		return fmt.Errorf("resolved request prelude is invalid: %w", err)
	}
	if uint32(len(prelude.Items())) != p.omitItems.start {
		return errors.New("responses previous history range does not follow request prelude")
	}
	return nil
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
