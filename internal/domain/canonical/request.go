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
// Tool declarations, tool policy, tool-call batch policy, generation controls,
// and output format belong here because they are request grammar, not wire
// shape.
type CanonicalRequest struct {
	model        string
	instructions string
	items        []CanonicalItem
	tools        []ToolDecl

	turn         TurnRef
	toolPolicy   ToolPolicy
	toolBatch    ToolCallBatchPolicy
	controls     GenerationControls
	outputFormat OutputFormat
}

// RequestParams contains normalized semantic input for one request, including
// semantic tool declarations, tool policy, tool-call batch policy, generation
// controls, output format, and the canonical turn reference.
type RequestParams struct {
	Model         string
	Instructions  string
	Items         []CanonicalItem
	Tools         []ToolDecl
	InputText     string
	Turn          TurnRef
	ToolPolicy    ToolPolicy
	ToolCallBatch ToolCallBatchPolicy
	Controls      GenerationControls
	OutputFormat  OutputFormat
}

func NewCanonicalRequest(params RequestParams) CanonicalRequest {
	items := cloneCanonicalItems(params.Items)
	tools := cloneToolDecls(params.Tools)
	if params.InputText != "" {
		items = append(items, NewTextItem(ItemAuthorUser, params.InputText))
	}
	return CanonicalRequest{
		model:        strings.TrimSpace(params.Model),        // swobu:io-string source=domain
		instructions: strings.TrimSpace(params.Instructions), // swobu:io-string source=domain
		items:        items,
		tools:        tools,
		turn:         params.Turn.Clone(),
		toolPolicy:   params.ToolPolicy.Clone(),
		toolBatch:    params.ToolCallBatch.Clone(),
		controls:     params.Controls.Clone(),
		outputFormat: params.OutputFormat.Clone(),
	}
}

func (r CanonicalRequest) Model() string {
	return r.model
}

func (r CanonicalRequest) SemanticKind() SemanticKind {
	return SemanticKindCanonical
}

func (r CanonicalRequest) Instructions() string {
	return r.instructions
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

func (r CanonicalRequest) ToolCallBatch() ToolCallBatchPolicy {
	return r.toolBatch.Clone()
}

func (r CanonicalRequest) Controls() GenerationControls {
	return r.controls.Clone()
}

func (r CanonicalRequest) OutputFormat() OutputFormat {
	return r.outputFormat.Clone()
}

func (r CanonicalRequest) Clone() CanonicalRequest {
	return NewCanonicalRequest(RequestParams{
		Model:         r.model,
		Instructions:  r.instructions,
		Items:         r.items,
		Tools:         r.tools,
		Turn:          r.turn,
		ToolPolicy:    r.toolPolicy,
		ToolCallBatch: r.toolBatch,
		Controls:      r.controls,
		OutputFormat:  r.outputFormat,
	})
}

// CloneCanonicalRequest protects the provider and app seams from accidental mutation
// of canonical inputs after a request has been accepted.
func CloneCanonicalRequest(req CanonicalRequest) CanonicalRequest {
	return req.Clone()
}
