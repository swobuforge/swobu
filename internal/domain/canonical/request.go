package canonical

import "strings"

type SemanticKind string

const (
	SemanticKindCanonical    SemanticKind = "canonical_request"
	SemanticKindConversation SemanticKind = "conversation"
	SemanticKindResponse     SemanticKind = "response_generation"
	SemanticKindPrompt       SemanticKind = "prompt_generation"
)

type ToolMode string

const (
	ToolModeDefault  ToolMode = ""
	ToolModeAuto     ToolMode = "auto"
	ToolModeRequired ToolMode = "required"
)

// CanonicalRequest is the single semantic request representation in core.
// Transport/profile/protocol specifics must stay outside this type.
type CanonicalRequest struct {
	model string
	items []CanonicalItem

	previousResponseID string
	toolMode           ToolMode
	cacheIntent        CacheIntent
}

// RequestParams contains normalized semantic input for one request.
type RequestParams struct {
	Model              string
	Items              []CanonicalItem
	InputText          string
	PreviousResponseID string
	ToolMode           ToolMode
	CacheIntent        CacheIntent
}

func NewCanonicalRequest(params RequestParams) CanonicalRequest {
	items := cloneCanonicalItems(params.Items)
	if params.InputText != "" {
		items = append(items, NewTextItem(ItemAuthorUser, params.InputText))
	}
	return CanonicalRequest{
		model:              strings.TrimSpace(params.Model), // swobu:io-string source=domain
		items:              items,
		previousResponseID: strings.TrimSpace(params.PreviousResponseID), // swobu:io-string source=domain
		toolMode:           params.ToolMode,
		cacheIntent: NewCacheIntent(CacheIntentParams{
			Key:       params.CacheIntent.Key(),
			Retention: params.CacheIntent.Retention(),
		}),
	}
}

func (r CanonicalRequest) Model() string {
	return r.model
}

func (r CanonicalRequest) SemanticKind() SemanticKind {
	return SemanticKindCanonical
}

func (r CanonicalRequest) Items() []CanonicalItem {
	return cloneCanonicalItems(r.items)
}

func (r CanonicalRequest) PreviousResponseID() string {
	return r.previousResponseID
}

func (r CanonicalRequest) ToolMode() ToolMode {
	return r.toolMode
}

func (r CanonicalRequest) CacheIntent() CacheIntent {
	return r.cacheIntent
}

func (r CanonicalRequest) Clone() CanonicalRequest {
	return NewCanonicalRequest(RequestParams{
		Model:              r.model,
		Items:              r.items,
		PreviousResponseID: r.previousResponseID,
		ToolMode:           r.toolMode,
		CacheIntent:        r.cacheIntent,
	})
}

// CloneCanonicalRequest protects the provider and app seams from accidental mutation
// of canonical inputs after a request has been accepted.
func CloneCanonicalRequest(req CanonicalRequest) CanonicalRequest {
	return req.Clone()
}

func cloneStringAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneStringAnyMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = cloneAny(typed[i])
		}
		return out
	default:
		return typed
	}
}
