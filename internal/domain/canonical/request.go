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
	presence     RequestPresence
}

// RequestPresence records which durable request bands the client supplied.
// Values remain provider-neutral canonical semantics; presence exists only so
// continuation preparation can distinguish omission from an explicit clear.
// Every new durable CanonicalRequest field must add a corresponding presence
// fact here and define its replay merge rule in internal/replay.
type RequestPresence struct {
	Model         bool
	Instructions  bool
	Tools         bool
	ToolPolicy    bool
	ToolCallBatch bool
	OutputFormat  bool
	Controls      GenerationControlsPresence
}

// GenerationControlsPresence keeps independently inheritable generation
// controls separate. A caller can therefore clear one control without
// accidentally clearing or inheriting its siblings.
type GenerationControlsPresence struct {
	MaxOutputTokens bool
	StopSequences   bool
	Temperature     bool
	TopP            bool
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
	Presence      RequestPresence
}

func NewCanonicalRequest(params RequestParams) CanonicalRequest {
	items := cloneCanonicalItems(params.Items)
	tools := cloneToolDecls(params.Tools)
	if params.InputText != "" {
		items = append(items, NewTextItem(ItemAuthorUser, params.InputText))
	}
	presence := inferRequestPresence(params)
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
		presence:     presence,
	}
}

func inferRequestPresence(params RequestParams) RequestPresence {
	presence := params.Presence
	presence.Model = presence.Model || strings.TrimSpace(params.Model) != ""
	presence.Instructions = presence.Instructions || strings.TrimSpace(params.Instructions) != ""
	presence.Tools = presence.Tools || params.Tools != nil
	presence.ToolPolicy = presence.ToolPolicy || params.ToolPolicy.Mode != "" || params.ToolPolicy.Specific != nil || strings.TrimSpace(params.ToolPolicy.SpecificType) != ""
	presence.ToolCallBatch = presence.ToolCallBatch || !params.ToolCallBatch.IsZero()
	presence.OutputFormat = presence.OutputFormat || !params.OutputFormat.IsZero()
	presence.Controls.MaxOutputTokens = presence.Controls.MaxOutputTokens || !params.Controls.Limits.MaxOutputTokens.IsZero()
	presence.Controls.StopSequences = presence.Controls.StopSequences || params.Controls.Limits.StopSequences != nil
	presence.Controls.Temperature = presence.Controls.Temperature || !params.Controls.Sampling.Temperature.IsZero()
	presence.Controls.TopP = presence.Controls.TopP || !params.Controls.Sampling.TopP.IsZero()
	return presence
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

// Presence returns the supplied-field facts used by continuation preparation.
func (r CanonicalRequest) Presence() RequestPresence {
	return r.presence
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
		Presence:      r.presence,
	})
}

// CloneCanonicalRequest protects the provider and app seams from accidental mutation
// of canonical inputs after a request has been accepted.
func CloneCanonicalRequest(req CanonicalRequest) CanonicalRequest {
	return req.Clone()
}
