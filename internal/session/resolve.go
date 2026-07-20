package session

import (
	"errors"
	"fmt"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// ResolvedRequest contains the complete canonical request and the current delta
// with any exact-target native-resumption refinement inherited from a checkpoint.
type ResolvedRequest struct {
	Full          canonical.CanonicalRequest
	Delta         canonical.CanonicalRequest
	ResolvedMedia ResolvedMedia
}

// ForTarget returns the valid request representation for one exact target
// generation. A matching native refinement selects Delta; every other target
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
	return ResolvedRequest{
		Full:          materialize(checkpoint, request),
		Delta:         inheritRequestBands(checkpoint.Request, request, &response),
		ResolvedMedia: checkpoint.ResolvedMedia.Clone(),
	}, nil
}

// ResumeHistory preserves the complete supplied request as fallback authority
// and combines a codec-rebased current invocation with an exact checkpoint's
// native refinement. Protocol history partitioning never occurs in session.
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
	return ResolvedRequest{
		Full:          complete.Clone(),
		Delta:         inheritRequestBands(checkpoint.Request, rebased, &response),
		ResolvedMedia: checkpoint.ResolvedMedia.Clone(),
	}, nil
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
	return out
}
