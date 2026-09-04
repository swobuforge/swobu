package continuity

import (
	"errors"
	"fmt"
	"math"
	"reflect"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// ResolvedRequest contains one complete canonical request and the optional
// exact native previous-history relation inherited from a checkpoint.
type ResolvedRequest struct {
	request         canonical.CanonicalRequest
	previousHistory *previousHistory
}

type previousHistory struct {
	response  canonical.ResponseRef
	omitItems requestItemRange
}

// previousHistory proves that response's typed native continuation represents
// exactly omitItems. Items outside that provider-known prefix remain explicit
// provider input. A newly completed provider response replaces any inherited
// relation; local results appended afterward never enter its omit range.

type requestItemRange struct {
	start uint32
	end   uint32
}

type materializedRequest struct {
	request         canonical.CanonicalRequest
	previousHistory requestItemRange
}

// Draft is transient continuity composition state used only while request-scoped
// MCP preparation may still update the current prelude.
type Draft struct {
	current    canonical.CanonicalRequest
	checkpoint *Checkpoint
}

func (r ResolvedRequest) Request() canonical.CanonicalRequest { return r.request.Clone() }
func (r ResolvedRequest) HasPreviousHistory() bool            { return r.previousHistory != nil }

// ContinueAfterLocalResult makes an in-process local-effect continuation
// equivalent to checkpointing the just-completed provider response and
// immediately resuming with the local results. Canonical history retains the
// provider response and results. The fresh response may replace inherited
// continuation authority only for the prefix through its own items; results
// remain outside that prefix and therefore stay explicit provider input.
func (r ResolvedRequest) ContinueAfterLocalResult(response canonical.CanonicalResponse, results []canonical.CanonicalItem) (ResolvedRequest, error) {
	responseRef := response.Response()
	if err := responseRef.ValidateCommittedResponse(); err != nil {
		return ResolvedRequest{}, fmt.Errorf("local tool round response is invalid: %w", err)
	}
	items := r.request.Items()
	items = append(items, cloneCanonicalItems(response.Items())...)
	omitEnd, err := checkedRequestItemIndex(len(items))
	if err != nil {
		return ResolvedRequest{}, err
	}
	items = append(items, cloneCanonicalItems(results)...)
	request := r.request.WithItems(items)
	prelude, _, err := canonical.SplitRequestPrelude(request.Items())
	if err != nil {
		return ResolvedRequest{}, err
	}
	omitStart, err := checkedRequestItemIndex(len(prelude.Items()))
	if err != nil {
		return ResolvedRequest{}, err
	}
	return newResolvedRequest(request, newPreviousHistory(responseRef, requestItemRange{start: omitStart, end: omitEnd}))
}

// PreviousHistory returns one closed exact-target history relation. The
// provider codec selects its own typed continuation child from Response.
func (r ResolvedRequest) PreviousHistory(targetID string, targetVersion uint64) (provider.PreviousHistory, bool) {
	previous := r.previousHistory
	if previous == nil || !r.request.PersistenceEligible() || !previous.responseAppliesTo(targetID, targetVersion) {
		return provider.PreviousHistory{}, false
	}
	return provider.PreviousHistory{
		Response:  previous.response.Clone(),
		OmitStart: previous.omitItems.start,
		OmitEnd:   previous.omitItems.end,
	}, true
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
		return Draft{}, errors.New("thread begin request contains previous response")
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
		Store:         request.StoreField(),
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
		return Draft{}, fmt.Errorf("invalid thread checkpoint response reference: %w", err)
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
		return ResolvedRequest{}, fmt.Errorf("continuity draft current request is invalid: %w", err)
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
	return newResolvedRequest(materialized.request, newPreviousHistory(d.checkpoint.Response.Response(), materialized.previousHistory))
}

func newResolvedRequest(request canonical.CanonicalRequest, previous *previousHistory) (ResolvedRequest, error) {
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
	return ResolvedRequest{request: request.Clone(), previousHistory: clonePreviousHistory(previous)}, nil
}

// resolveTurnContinuation validates tool-result correlation and restores only
// unfinished-turn context. Current invocation controls remain current-request
// truth: a checkpoint restores conversation state, never request configuration.
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
	return current, nil
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
		Store: request.StoreField(),
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
		Store:         request.StoreField(),
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
		Store:         current.StoreField(),
	})
	return materializedRequest{request: request, previousHistory: requestItemRange{start: start, end: end}}, nil
}

func checkedRequestItemIndex(value int) (uint32, error) {
	if value < 0 || uint64(value) > math.MaxUint32 {
		return 0, errors.New("resolved request item count exceeds coordinate range")
	}
	return uint32(value), nil
}

func newPreviousHistory(response canonical.ResponseRef, replacedHistory requestItemRange) *previousHistory {
	if response.Responses == nil && response.Interactions == nil {
		return nil
	}
	return &previousHistory{response: response.Clone(), omitItems: replacedHistory}
}

func clonePreviousHistory(previous *previousHistory) *previousHistory {
	if previous == nil {
		return nil
	}
	return &previousHistory{response: previous.response.Clone(), omitItems: previous.omitItems}
}

func (p previousHistory) validateFor(request canonical.CanonicalRequest) error {
	if p.response.Responses == nil && p.response.Interactions == nil {
		return errors.New("previous history relation is missing provider continuation")
	}
	items := request.Items()
	if p.omitItems.start > p.omitItems.end || uint64(p.omitItems.end) > uint64(len(items)) {
		return errors.New("previous history range is invalid")
	}
	prelude, _, err := canonical.SplitRequestPrelude(items)
	if err != nil {
		return fmt.Errorf("resolved request prelude is invalid: %w", err)
	}
	if uint32(len(prelude.Items())) != p.omitItems.start {
		return errors.New("previous history range does not follow request prelude")
	}
	return nil
}

func (p previousHistory) responseAppliesTo(targetID string, targetVersion uint64) bool {
	return (p.response.Responses != nil && p.response.Responses.AppliesTo(targetID, targetVersion)) ||
		(p.response.Interactions != nil && p.response.Interactions.AppliesTo(targetID, targetVersion))
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
