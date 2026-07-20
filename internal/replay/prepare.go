package replay

import (
	"fmt"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// Prepared contains both the complete semantic request and the inherited
// current delta. Exact backend resolution chooses which representation is safe
// to send; replay lookup never predicts backend compatibility.
type Prepared struct {
	Semantic      canonical.CanonicalRequest
	Delta         canonical.CanonicalRequest
	ResolvedMedia ResolvedMedia
}

// PreferredForTarget selects canonical replay content for one exact target
// generation. Replay owns only Semantic-versus-Delta choice; exchange owns the
// provider request and delivery construction.
func (p Prepared) PreferredForTarget(target provider.TargetSnapshot) canonical.CanonicalRequest {
	if previous, ok := p.Delta.PreviousResponse(); ok && previous.Responses != nil &&
		previous.Responses.AppliesTo(target.TargetID, target.TargetVersion) {
		return p.Delta.Clone()
	}
	return p.Semantic.Clone()
}

// PreviousSwobuResponseID returns the explicit workspace-local replay capability carried by
// the canonical request, if present. It performs no store access.
func PreviousSwobuResponseID(request canonical.CanonicalRequest) (canonical.SwobuResponseID, bool, error) {
	prev, ok := request.PreviousResponse()
	if !ok {
		return "", false, nil
	}
	if err := prev.ValidateReplaySelector(); err != nil {
		return "", false, canonical.BadRequest("previous_response_id is empty")
	}
	return prev.SwobuID, true, nil
}

// PrepareCurrent constructs target-independent replay state for a request that
// does not reference a prior replay record.
func PrepareCurrent(request canonical.CanonicalRequest) Prepared {
	complete := withoutPreviousResponse(request)
	return Prepared{Semantic: complete, Delta: requestWithoutPreviousResponse(request)}
}

// PrepareFromRecord materializes one immutable replay snapshot already loaded
// by exchange orchestration. It performs no store access.
func PrepareFromRecord(request canonical.CanonicalRequest, requestedSwobuResponseID canonical.SwobuResponseID, previous Record) (Prepared, error) {
	if previous.ExpiresAt != nil && !previous.ExpiresAt.After(time.Now().UTC()) {
		return Prepared{}, canonical.BadRequest("unknown previous_response_id")
	}
	response := previous.Response.Response()
	if err := response.ValidateCommittedResponse(); err != nil {
		return Prepared{}, fmt.Errorf("invalid replay record response reference: %w", err)
	}
	if response.SwobuID != requestedSwobuResponseID {
		return Prepared{}, canonical.BadRequest("unknown previous_response_id")
	}
	if err := previous.ResolvedMedia.ValidateForRequest(previous.Request); err != nil {
		return Prepared{}, fmt.Errorf("invalid replay record media: %w", err)
	}
	return Prepared{
		Semantic:      materialize(previous, request),
		Delta:         inheritRequestBands(previous.Request, request, &response),
		ResolvedMedia: previous.ResolvedMedia.Clone(),
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

func materialize(previous Record, current canonical.CanonicalRequest) canonical.CanonicalRequest {
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
		panic("replay received invalid canonical tool declarations: " + err.Error())
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
