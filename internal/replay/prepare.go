package replay

import (
	"context"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// Prepared contains both the complete semantic request and the inherited
// current delta. Exact backend resolution chooses which representation is safe
// to send; replay lookup never predicts backend compatibility.
type Prepared struct {
	Semantic canonical.CanonicalRequest
	Delta    canonical.CanonicalRequest
	Base     *Record
}

// ForBackend constructs one provider request. Matching native continuation
// sends the inherited current delta; absent or mismatched continuation sends
// the complete semantic request.
func (p Prepared) ForBackend(backend provider.Backend, providerDelivery delivery.Delivery) provider.Request {
	request := provider.Request{Canonical: p.Semantic.Clone(), Delivery: providerDelivery}
	if backend.CaptureContinuation == nil || p.Base == nil || p.Base.Native == nil ||
		p.Base.Native.TargetID != backend.Target.TargetID ||
		p.Base.Native.TargetVersion != backend.Target.TargetVersion {
		return request
	}
	continuation := *p.Base.Native
	request.Canonical = p.Delta.Clone()
	request.Continuation = &continuation
	return request
}

// Prepare loads at most one explicit replay record and computes complete
// semantic state plus the inherited current delta.
//
// Rules:
//   - No previous ID in request.Turn: semantic and delta are the complete request.
//   - Store nil with previous ID present: reject (cannot validate previous).
//   - Unknown previous ID: bad request.
//   - Previous ID present: materialize full semantic state and inherit durable
//     request bands into the current delta.
//
// One record deep. No prefix matching. No chain walking.
func Prepare(
	ctx context.Context,
	store Store,
	workspaceSlug string,
	request canonical.CanonicalRequest,
) (Prepared, error) {
	previousID, ok := PreviousID(request)
	if !ok {
		return PrepareCurrent(request), nil
	}

	if store == nil {
		return Prepared{}, canonical.BadRequest("unknown previous_response_id")
	}
	if strings.TrimSpace(workspaceSlug) == "" {
		return Prepared{}, canonical.BadRequest("replay workspace slug is empty")
	}

	previous, found, err := store.Get(ctx, workspaceSlug, previousID)
	if err != nil {
		return Prepared{}, err
	}
	if !found {
		return Prepared{}, canonical.BadRequest("unknown previous_response_id")
	}
	if previous.ExpiresAt != nil && !previous.ExpiresAt.After(time.Now().UTC()) {
		return Prepared{}, canonical.BadRequest("unknown previous_response_id")
	}
	return PrepareFromRecord(request, previous)
}

// PreviousID returns the explicit workspace-local replay capability carried by
// the canonical request, if present. It performs no store access.
func PreviousID(request canonical.CanonicalRequest) (ID, bool) {
	if request.Turn().IsZero() {
		return "", false
	}
	prev, ok := request.Turn().PreviousID()
	if !ok {
		return "", false
	}
	return ID(prev.String()), true
}

// PrepareCurrent constructs target-independent replay state for a request that
// does not reference a prior replay record.
func PrepareCurrent(request canonical.CanonicalRequest) Prepared {
	complete := withoutTurn(request)
	return Prepared{Semantic: complete, Delta: complete.Clone()}
}

// PrepareFromRecord materializes one immutable replay snapshot already loaded
// by exchange orchestration. It performs no store access.
func PrepareFromRecord(request canonical.CanonicalRequest, previous Record) (Prepared, error) {
	if previous.ExpiresAt != nil && !previous.ExpiresAt.After(time.Now().UTC()) {
		return Prepared{}, canonical.BadRequest("unknown previous_response_id")
	}
	base := previous.Clone()
	return Prepared{
		Semantic: materialize(previous, request),
		Delta:    inheritRequestBands(previous.Request, request),
		Base:     &base,
	}, nil
}

func withoutTurn(request canonical.CanonicalRequest) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         request.Model(),
		Instructions:  request.Instructions(),
		Items:         cloneCanonicalItems(request.Items()),
		Tools:         cloneToolDecls(request.Tools()),
		Turn:          canonical.TurnRef{},
		ToolPolicy:    request.ToolPolicy(),
		ToolCallBatch: request.ToolCallBatch(),
		Controls:      request.Controls(),
		OutputFormat:  request.OutputFormat(),
		Presence:      request.Presence(),
	})
}

func inheritRequestBands(previous canonical.CanonicalRequest, current canonical.CanonicalRequest) canonical.CanonicalRequest {
	presence := current.Presence()
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         inheritString(presence.Model, current.Model(), previous.Model()),
		Instructions:  inheritString(presence.Instructions, current.Instructions(), previous.Instructions()),
		Items:         cloneCanonicalItems(current.Items()),
		Tools:         inheritToolDecls(presence.Tools, current.Tools(), previous.Tools()),
		Turn:          canonical.TurnRef{},
		ToolPolicy:    inheritCloneable(presence.ToolPolicy, current.ToolPolicy(), previous.ToolPolicy()),
		ToolCallBatch: inheritCloneable(presence.ToolCallBatch, current.ToolCallBatch(), previous.ToolCallBatch()),
		Controls:      inheritControls(presence.Controls, current.Controls(), previous.Controls()),
		OutputFormat:  inheritCloneable(presence.OutputFormat, current.OutputFormat(), previous.OutputFormat()),
		Presence:      presence,
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
		Turn:          canonical.TurnRef{},
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
