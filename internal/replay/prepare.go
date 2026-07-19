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
	Semantic canonical.CanonicalRequest
	Delta    canonical.CanonicalRequest
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
	return Prepared{Semantic: complete, Delta: complete.Clone()}
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
	return Prepared{
		Semantic: materialize(previous, request),
		Delta:    inheritRequestBands(previous.Request, request, &response),
	}, nil
}

func withoutPreviousResponse(request canonical.CanonicalRequest) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         request.Model(),
		Instructions:  request.Instructions(),
		Items:         cloneCanonicalItems(request.Items()),
		Tools:         cloneToolDecls(request.Tools()),
		ToolPolicy:    request.ToolPolicy(),
		ToolCallBatch: request.ToolCallBatch(),
		Controls:      request.Controls(),
		OutputFormat:  request.OutputFormat(),
		Presence:      request.Presence(),
	})
}

func inheritRequestBands(previous canonical.CanonicalRequest, current canonical.CanonicalRequest, previousResponse *canonical.ResponseRef) canonical.CanonicalRequest {
	presence := current.Presence()
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:            inheritString(presence.Model, current.Model(), previous.Model()),
		Instructions:     inheritString(presence.Instructions, current.Instructions(), previous.Instructions()),
		Items:            cloneCanonicalItems(current.Items()),
		Tools:            inheritToolDecls(presence.Tools, current.Tools(), previous.Tools()),
		PreviousResponse: previousResponse,
		ToolPolicy:       inheritCloneable(presence.ToolPolicy, current.ToolPolicy(), previous.ToolPolicy()),
		ToolCallBatch:    inheritCloneable(presence.ToolCallBatch, current.ToolCallBatch(), previous.ToolCallBatch()),
		Controls:         inheritControls(presence.Controls, current.Controls(), previous.Controls()),
		OutputFormat:     inheritCloneable(presence.OutputFormat, current.OutputFormat(), previous.OutputFormat()),
		Presence:         presence,
	})
}

func materialize(previous Record, current canonical.CanonicalRequest) canonical.CanonicalRequest {
	presence := current.Presence()
	items := cloneCanonicalItems(previous.Request.Items())
	items = append(items, cloneCanonicalItems(previous.Response.Items())...)
	items = append(items, cloneCanonicalItems(current.Items())...)

	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         inheritString(presence.Model, current.Model(), previous.Request.Model()),
		Instructions:  inheritString(presence.Instructions, current.Instructions(), previous.Request.Instructions()),
		Items:         items,
		Tools:         inheritToolDecls(presence.Tools, current.Tools(), previous.Request.Tools()),
		ToolPolicy:    inheritCloneable(presence.ToolPolicy, current.ToolPolicy(), previous.Request.ToolPolicy()),
		ToolCallBatch: inheritCloneable(presence.ToolCallBatch, current.ToolCallBatch(), previous.Request.ToolCallBatch()),
		Controls:      inheritControls(presence.Controls, current.Controls(), previous.Request.Controls()),
		OutputFormat:  inheritCloneable(presence.OutputFormat, current.OutputFormat(), previous.Request.OutputFormat()),
		Presence:      presence,
	})
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

func cloneToolDecls(tools []canonical.ToolDecl) []canonical.ToolDecl {
	if tools == nil {
		return nil
	}
	cloned := make([]canonical.ToolDecl, len(tools))
	for i := range tools {
		if tools[i] == nil {
			continue
		}
		cloned[i] = tools[i].Clone()
	}
	return cloned
}

func inheritToolDecls(present bool, current, previous []canonical.ToolDecl) []canonical.ToolDecl {
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

func inheritControls(presence canonical.GenerationControlsPresence, current, previous canonical.GenerationControls) canonical.GenerationControls {
	out := previous.Clone()
	if presence.MaxOutputTokens {
		out.Limits.MaxOutputTokens = current.Limits.MaxOutputTokens.Clone()
	}
	if presence.StopSequences {
		out.Limits.StopSequences = append([]string(nil), current.Limits.StopSequences...)
	}
	if presence.Temperature {
		out.Sampling.Temperature = current.Sampling.Temperature.Clone()
	}
	if presence.TopP {
		out.Sampling.TopP = current.Sampling.TopP.Clone()
	}
	return out
}
