package replay

import (
	"context"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// Prepare determines the request to send to the provider and whether a native
// replay pointer is available for the selected target.
//
// Rules:
//   - No previous ID in request.Turn: return request with turn cleared, nil native ref.
//   - Store nil with previous ID present: reject (cannot validate previous).
//   - Unknown previous ID: bad request.
//   - Previous ID present: use the supplied request items as the current-turn
//     input. If the target pointer is present and the previous record has a
//     matching NativeRef, return the current-turn delta with the previous
//     durable request bands inherited and the turn selector cleared.
//   - Otherwise: materialize full request from previous record + current items.
//
// One record deep. No prefix matching. No chain walking.
func Prepare(
	ctx context.Context,
	store Store,
	scope Scope,
	target *TargetKey,
	request canonical.CanonicalRequest,
) (canonical.CanonicalRequest, *NativeRef, error) {
	previousID, ok := previousReplayID(request)
	if !ok {
		return withoutTurn(request), nil, nil
	}

	if store == nil {
		return canonical.CanonicalRequest{}, nil, canonical.BadRequest("unknown previous_response_id")
	}
	if strings.TrimSpace(scope.Namespace) == "" {
		return canonical.CanonicalRequest{}, nil, canonical.BadRequest("replay scope namespace is empty")
	}
	if strings.TrimSpace(scope.CallerKey) == "" {
		return canonical.CanonicalRequest{}, nil, canonical.BadRequest("replay scope caller key is empty")
	}

	previous, found, err := store.Get(ctx, scope, previousID)
	if err != nil {
		return canonical.CanonicalRequest{}, nil, err
	}
	if !found {
		return canonical.CanonicalRequest{}, nil, canonical.BadRequest("unknown previous_response_id")
	}
	if previous.ExpiresAt != nil && !previous.ExpiresAt.After(time.Now().UTC()) {
		return canonical.CanonicalRequest{}, nil, canonical.BadRequest("unknown previous_response_id")
	}

	if target != nil && previous.Native != nil && previous.Native.Target.Equal(*target) {
		return inheritRequestBands(previous.Request, request), previous.Native, nil
	}

	return materialize(previous, request), nil, nil
}

func previousReplayID(request canonical.CanonicalRequest) (ID, bool) {
	if request.Turn().IsZero() {
		return "", false
	}
	prev, ok := request.Turn().PreviousID()
	if !ok {
		return "", false
	}
	return ID(prev.String()), true
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
	})
}

func inheritRequestBands(previous canonical.CanonicalRequest, current canonical.CanonicalRequest) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         inheritString(current.Model(), previous.Model()),
		Instructions:  inheritString(current.Instructions(), previous.Instructions()),
		Items:         cloneCanonicalItems(current.Items()),
		Tools:         inheritToolDecls(current.Tools(), previous.Tools()),
		Turn:          canonical.TurnRef{},
		ToolPolicy:    inheritCloneable(current.ToolPolicy(), previous.ToolPolicy()),
		ToolCallBatch: inheritCloneable(current.ToolCallBatch(), previous.ToolCallBatch()),
		Controls:      inheritControls(current.Controls(), previous.Controls()),
		OutputFormat:  inheritCloneable(current.OutputFormat(), previous.OutputFormat()),
	})
}

func materialize(previous Record, current canonical.CanonicalRequest) canonical.CanonicalRequest {
	items := cloneCanonicalItems(previous.Request.Items())
	items = append(items, cloneCanonicalItems(previous.Response.Items())...)
	items = append(items, cloneCanonicalItems(current.Items())...)

	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         inheritString(current.Model(), previous.Request.Model()),
		Instructions:  inheritString(current.Instructions(), previous.Request.Instructions()),
		Items:         items,
		Tools:         inheritToolDecls(current.Tools(), previous.Request.Tools()),
		Turn:          canonical.TurnRef{},
		ToolPolicy:    inheritCloneable(current.ToolPolicy(), previous.Request.ToolPolicy()),
		ToolCallBatch: inheritCloneable(current.ToolCallBatch(), previous.Request.ToolCallBatch()),
		Controls:      inheritControls(current.Controls(), previous.Request.Controls()),
		OutputFormat:  inheritCloneable(current.OutputFormat(), previous.Request.OutputFormat()),
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

func inheritToolDecls(current, previous []canonical.ToolDecl) []canonical.ToolDecl {
	if len(current) > 0 {
		return cloneToolDecls(current)
	}
	return cloneToolDecls(previous)
}

func inheritString(current, previous string) string {
	if current != "" {
		return current
	}
	return previous
}

// cloneabler is a constraint for types that have a Clone() T method.
type cloneabler[T any] interface {
	Clone() T
	IsZero() bool
}

func inheritCloneable[T cloneabler[T]](current, previous T) T {
	if !current.IsZero() {
		return current.Clone()
	}
	return previous.Clone()
}

func inheritControls(current, previous canonical.GenerationControls) canonical.GenerationControls {
	if current.Limits.IsZero() && current.Sampling.IsZero() {
		return previous.Clone()
	}
	out := current.Clone()
	if out.Limits.IsZero() {
		out.Limits = previous.Limits.Clone()
	} else {
		if out.Limits.MaxOutputTokens.IsZero() && !previous.Limits.MaxOutputTokens.IsZero() {
			out.Limits.MaxOutputTokens = previous.Limits.MaxOutputTokens.Clone()
		}
		if len(out.Limits.StopSequences) == 0 && len(previous.Limits.StopSequences) > 0 {
			out.Limits.StopSequences = append([]string(nil), previous.Limits.StopSequences...)
		}
	}
	if out.Sampling.IsZero() {
		out.Sampling = previous.Sampling.Clone()
	} else {
		if out.Sampling.Temperature.IsZero() && !previous.Sampling.Temperature.IsZero() {
			out.Sampling.Temperature = previous.Sampling.Temperature.Clone()
		}
		if out.Sampling.TopP.IsZero() && !previous.Sampling.TopP.IsZero() {
			out.Sampling.TopP = previous.Sampling.TopP.Clone()
		}
	}
	return out
}
