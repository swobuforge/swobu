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
	model    string
	items    []CanonicalItem
	lastTurn []CanonicalItem

	previousResponseID string
	toolMode           ToolMode
	cacheIntent        CacheIntent
}

// Compatibility aliases while codebase converges on one request representation.
type DialogCanonicalRequest = CanonicalRequest
type GenerationCanonicalRequest = CanonicalRequest
type PromptCanonicalRequest = CanonicalRequest

// RequestParams contains normalized semantic input for one request.
type RequestParams struct {
	Model              string
	Items              []CanonicalItem
	LastTurn           []CanonicalItem
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
		lastTurn:           cloneCanonicalItems(params.LastTurn),
		previousResponseID: strings.TrimSpace(params.PreviousResponseID), // swobu:io-string source=domain
		toolMode:           params.ToolMode,
		cacheIntent: NewCacheIntent(CacheIntentParams{
			Key:       params.CacheIntent.Key(),
			Retention: params.CacheIntent.Retention(),
		}),
	}
}

// Compatibility constructors kept as one-type shims.
func NewDialogRequest(model string, items []CanonicalItem) CanonicalRequest {
	return NewCanonicalRequest(RequestParams{Model: model, Items: items})
}

func NewGenerationRequest(params GenerationRequestParams) CanonicalRequest {
	items := cloneCanonicalItems(params.Thread)
	if len(items) == 0 {
		items = cloneCanonicalItems(params.LastTurn)
	}
	if len(items) == 0 {
		items = cloneCanonicalItems(params.Items)
	}
	return NewCanonicalRequest(RequestParams{
		Model:              params.Model,
		Items:              items,
		LastTurn:           params.LastTurn,
		InputText:          params.InputText,
		PreviousResponseID: params.PreviousResponseID,
		ToolMode:           params.ToolMode,
		CacheIntent:        params.CacheIntent,
	})
}

func NewPromptRequest(model string, prompt string) CanonicalRequest {
	return NewCanonicalRequest(RequestParams{Model: model, InputText: prompt})
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

// Thread is a compatibility accessor; canonical request now has one item stream.
func (r CanonicalRequest) Thread() []CanonicalItem {
	return cloneCanonicalItems(r.items)
}

// LastTurn is a compatibility accessor; canonical request now has one item stream.
func (r CanonicalRequest) LastTurn() []CanonicalItem {
	return cloneCanonicalItems(r.lastTurn)
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

// Prompt is a compatibility accessor for prompt-style providers.
func (r CanonicalRequest) Prompt() string {
	out := ""
	for _, item := range r.items {
		if item.Kind == ItemKindText {
			out += item.Text
		}
	}
	return out
}

func (r CanonicalRequest) HasThread() bool {
	return len(r.items) > 0
}

func (r CanonicalRequest) HasLastTurn() bool {
	return len(r.lastTurn) > 0
}

func (r CanonicalRequest) Clone() CanonicalRequest {
	return NewCanonicalRequest(RequestParams{
		Model:              r.model,
		Items:              r.items,
		LastTurn:           r.lastTurn,
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

// GenerationRequestParams remains as a compatibility decode surface while callers migrate.
type GenerationRequestParams struct {
	Model              string
	InputText          string
	Items              []CanonicalItem
	Thread             []CanonicalItem
	LastTurn           []CanonicalItem
	PreviousResponseID string
	ToolMode           ToolMode
	CacheIntent        CacheIntent
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
