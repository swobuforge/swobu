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
	complete := withoutPreviousResponse(request)
	return ResolvedRequest{Full: complete, Delta: requestWithoutPreviousResponse(request)}, nil
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
	effective, err := resolveToolContinuation(checkpoint, request)
	if err != nil {
		return ResolvedRequest{}, err
	}
	return ResolvedRequest{
		Full:          materialize(checkpoint, effective),
		Delta:         inheritRequestBands(checkpoint.Request, effective, &response),
		ResolvedMedia: checkpoint.ResolvedMedia.Clone(),
	}, nil
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
	effective, err := resolveToolContinuation(checkpoint, rebased)
	if err != nil {
		return ResolvedRequest{}, err
	}
	return ResolvedRequest{
		Full:          materialize(checkpoint, effective),
		Delta:         inheritRequestBands(checkpoint.Request, effective, &response),
		ResolvedMedia: checkpoint.ResolvedMedia.Clone(),
	}, nil
}

// resolveToolContinuation validates tool-result correlation and writes the
// ongoing assistant turn's compute and effort into the one effective request.
// The checkpoint request is the sole authority for omitted continuation values.
func resolveToolContinuation(checkpoint Checkpoint, current canonical.CanonicalRequest) (canonical.CanonicalRequest, error) {
	pendingSet := make(map[canonical.ToolCallID]struct{})
	for _, item := range checkpoint.Response.Items() {
		call, ok := item.ToolCall()
		if !ok {
			continue
		}
		pendingSet[call.CallID()] = struct{}{}
	}
	matched := make(map[canonical.ToolCallID]struct{}, len(pendingSet))
	for _, item := range current.Items() {
		result, ok := item.ToolResult()
		if !ok {
			continue
		}
		if _, expected := pendingSet[result.CallID()]; !expected {
			return canonical.CanonicalRequest{}, canonical.BadRequest("tool result does not belong to the unfinished assistant turn")
		}
		if _, duplicate := matched[result.CallID()]; duplicate {
			return canonical.CanonicalRequest{}, canonical.BadRequest("unfinished assistant turn contains a duplicate tool result")
		}
		matched[result.CallID()] = struct{}{}
	}
	if len(matched) == 0 {
		return current.Clone(), nil
	}
	compute := current.Reasoning().ComputeField()
	priorCompute := checkpoint.Request.Reasoning().ComputeField()
	if explicit, ok := compute.Get(); ok {
		prior, priorSet := priorCompute.Get()
		if !priorSet || !equalReasoningCompute(explicit, prior) {
			return canonical.CanonicalRequest{}, canonical.UnsupportedOperation("current reasoning compute conflicts with unfinished tool turn")
		}
	} else {
		compute = priorCompute
	}
	controls := current.Controls()
	priorEffort := checkpoint.Request.Controls().Effort
	if explicit, ok := controls.Effort.Get(); ok {
		prior, priorSet := priorEffort.Get()
		if !priorSet || explicit != prior {
			return canonical.CanonicalRequest{}, canonical.UnsupportedOperation("current inference effort conflicts with unfinished tool turn")
		}
	} else {
		controls.Effort = priorEffort
	}
	reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Compute: compute, Disclosure: current.Reasoning().DisclosureField(),
	})
	if err != nil {
		return canonical.CanonicalRequest{}, err
	}
	return replaceComputeControls(current, controls, reasoning), nil
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
		Model: request.ModelField(), Instructions: request.InstructionsField(), Items: request.Items(),
		Tools: request.ToolsField(), PreviousResponse: previousPointer, ToolPolicy: request.ToolPolicyField(),
		ToolCallBatch: request.ToolCallBatchField(), Controls: controls, Reasoning: reasoning,
		OutputFormat: request.OutputFormatField(),
	})
}

func withoutPreviousResponse(request canonical.CanonicalRequest) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         canonical.Specify(request.Model()),
		Instructions:  canonical.Specify(request.Instructions()),
		Items:         cloneCanonicalItems(request.Items()),
		Tools:         canonical.Specify(mustToolSet(request.Tools())),
		ToolPolicy:    canonical.Specify(request.EffectiveToolPolicy()),
		ToolCallBatch: canonical.Specify(request.ToolCallBatch()),
		Controls:      request.Controls(),
		Reasoning:     request.Reasoning(),
		OutputFormat:  canonical.Specify(request.OutputFormat()),
	})
}

func requestWithoutPreviousResponse(request canonical.CanonicalRequest) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         request.ModelField(),
		Instructions:  request.InstructionsField(),
		Items:         cloneCanonicalItems(request.Items()),
		Tools:         request.ToolsField(),
		ToolPolicy:    request.ToolPolicyField(),
		ToolCallBatch: request.ToolCallBatchField(),
		Controls:      request.Controls(),
		Reasoning:     request.Reasoning(),
		OutputFormat:  request.OutputFormatField(),
	})
}

func inheritRequestBands(previous canonical.CanonicalRequest, current canonical.CanonicalRequest, previousResponse *canonical.ResponseRef) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:            canonical.Specify(inheritString(current.ModelSpecified(), current.Model(), previous.Model())),
		Instructions:     canonical.Specify(inheritInstructions(current.InstructionsSpecified(), current.Instructions(), previous.Instructions())),
		Items:            cloneCanonicalItems(current.Items()),
		Tools:            canonical.Specify(mustToolSet(inheritToolDecls(current.ToolsSpecified(), current.Tools(), previous.Tools()))),
		PreviousResponse: previousResponse,
		ToolPolicy:       canonical.Specify(inheritCloneable(current.ToolPolicySpecified(), current.ToolPolicy(), previous.ToolPolicy())),
		ToolCallBatch:    canonical.Specify(inheritCloneable(current.ToolCallBatchSpecified(), current.ToolCallBatch(), previous.ToolCallBatch())),
		Controls:         inheritControls(current.Controls(), previous.Controls()),
		Reasoning:        current.Reasoning(),
		OutputFormat:     canonical.Specify(inheritCloneable(current.OutputFormatSpecified(), current.OutputFormat(), previous.OutputFormat())),
	})
}

func materialize(previous Checkpoint, current canonical.CanonicalRequest) canonical.CanonicalRequest {
	items := cloneCanonicalItems(previous.Request.Items())
	items = append(items, cloneCanonicalItems(previous.Response.Items())...)
	items = append(items, cloneCanonicalItems(current.Items())...)

	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         canonical.Specify(inheritString(current.ModelSpecified(), current.Model(), previous.Request.Model())),
		Instructions:  canonical.Specify(inheritInstructions(current.InstructionsSpecified(), current.Instructions(), previous.Request.Instructions())),
		Items:         items,
		Tools:         canonical.Specify(mustToolSet(inheritToolDecls(current.ToolsSpecified(), current.Tools(), previous.Request.Tools()))),
		ToolPolicy:    canonical.Specify(inheritCloneable(current.ToolPolicySpecified(), current.ToolPolicy(), previous.Request.ToolPolicy())),
		ToolCallBatch: canonical.Specify(inheritCloneable(current.ToolCallBatchSpecified(), current.ToolCallBatch(), previous.Request.ToolCallBatch())),
		Controls:      inheritControls(current.Controls(), previous.Request.Controls()),
		Reasoning:     current.Reasoning(),
		OutputFormat:  canonical.Specify(inheritCloneable(current.OutputFormatSpecified(), current.OutputFormat(), previous.Request.OutputFormat())),
	})
}

func mustToolSet(tools []canonical.ToolDeclaration) canonical.ToolSet {
	set, err := canonical.NewToolSet(tools)
	if err != nil {
		panic("session resolution received invalid canonical tool declarations: " + err.Error())
	}
	return set
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

func cloneToolDecls(tools []canonical.ToolDeclaration) []canonical.ToolDeclaration {
	if tools == nil {
		return nil
	}
	cloned := make([]canonical.ToolDeclaration, len(tools))
	for i := range tools {
		cloned[i] = tools[i].Clone()
	}
	return cloned
}

func inheritToolDecls(present bool, current, previous []canonical.ToolDeclaration) []canonical.ToolDeclaration {
	if present {
		return cloneToolDecls(current)
	}
	return cloneToolDecls(previous)
}

func inheritString(present bool, current, previous string) string {
	if present {
		return current
	}
	return previous
}

func inheritInstructions(present bool, current, previous canonical.InstructionSet) canonical.InstructionSet {
	if present {
		return current.Clone()
	}
	return previous.Clone()
}

// cloneabler is a constraint for types that have a Clone() T method.
type cloneabler[T any] interface {
	Clone() T
	IsZero() bool
}

func inheritCloneable[T cloneabler[T]](present bool, current, previous T) T {
	if present {
		return current.Clone()
	}
	return previous.Clone()
}

func inheritControls(current, previous canonical.GenerationControls) canonical.GenerationControls {
	out := previous.Clone()
	if !current.Limits.MaxOutputTokens.IsZero() {
		out.Limits.MaxOutputTokens = current.Limits.MaxOutputTokens.Clone()
	}
	if current.Limits.StopSequences != nil {
		out.Limits.StopSequences = append([]string(nil), current.Limits.StopSequences...)
	}
	if !current.Sampling.Temperature.IsZero() {
		out.Sampling.Temperature = current.Sampling.Temperature.Clone()
	}
	if !current.Sampling.TopP.IsZero() {
		out.Sampling.TopP = current.Sampling.TopP.Clone()
	}
	// Inference effort is a per-invocation reasoning control. Unlike ordinary
	// generation limits, omission must remain omission across session resume.
	out.Effort = current.Clone().Effort
	return out
}
