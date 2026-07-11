package canonical

import "strings"

type SemanticKind string

const (
	SemanticKindCanonical    SemanticKind = "canonical_request"
	SemanticKindConversation SemanticKind = "conversation"
	SemanticKindResponse     SemanticKind = "response_generation"
	SemanticKindPrompt       SemanticKind = "prompt_generation"
)

// CanonicalRequest is the single semantic request representation in core.
// Transport/profile/protocol specifics must stay outside this type.
// Tool declarations and policy belong here because they are request grammar,
// not wire shape.
type CanonicalRequest struct {
	model string
	items []CanonicalItem
	tools []ToolDecl

	turn        TurnRef
	toolPolicy  ToolPolicy
	cacheIntent CacheIntent
}

// RequestParams contains normalized semantic input for one request, including
// semantic tool declarations, tool policy, and the canonical turn reference.
type RequestParams struct {
	Model       string
	Items       []CanonicalItem
	Tools       []ToolDecl
	InputText   string
	Turn        TurnRef
	ToolPolicy  ToolPolicy
	CacheIntent CacheIntent
}

func NewCanonicalRequest(params RequestParams) CanonicalRequest {
	items := cloneCanonicalItems(params.Items)
	tools := cloneToolDecls(params.Tools)
	if params.InputText != "" {
		items = append(items, NewTextItem(ItemAuthorUser, params.InputText))
	}
	return CanonicalRequest{
		model:      strings.TrimSpace(params.Model), // swobu:io-string source=domain
		items:      items,
		tools:      tools,
		turn:       params.Turn.Clone(),
		toolPolicy: params.ToolPolicy.Clone(),
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

func (r CanonicalRequest) Tools() []ToolDecl {
	return cloneToolDecls(r.tools)
}

func (r CanonicalRequest) Turn() TurnRef {
	return r.turn.Clone()
}

func (r CanonicalRequest) ToolPolicy() ToolPolicy {
	return r.toolPolicy.Clone()
}

func (r CanonicalRequest) CacheIntent() CacheIntent {
	return r.cacheIntent
}

func (r CanonicalRequest) Clone() CanonicalRequest {
	return NewCanonicalRequest(RequestParams{
		Model:       r.model,
		Items:       r.items,
		Tools:       r.tools,
		Turn:        r.turn,
		ToolPolicy:  r.toolPolicy,
		CacheIntent: r.cacheIntent,
	})
}

// CloneCanonicalRequest protects the provider and app seams from accidental mutation
// of canonical inputs after a request has been accepted.
func CloneCanonicalRequest(req CanonicalRequest) CanonicalRequest {
	return req.Clone()
}
