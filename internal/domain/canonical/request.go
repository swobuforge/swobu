package canonical

import "strings"

// CanonicalRequest is the single semantic request representation in core.
// Omission lives with each independently inheritable request band rather than
// in a parallel presence schema.
type CanonicalRequest struct {
	model        Specified[string]
	instructions Specified[InstructionSet]
	items        []CanonicalItem
	tools        Specified[ToolSet]

	previousResponse *ResponseRef
	toolPolicy       Specified[ToolPolicy]
	toolBatch        Specified[ToolCallBatchPolicy]
	controls         GenerationControls
	reasoning        ReasoningControls
	outputFormat     Specified[OutputFormat]
}

// RequestParams carries already-decoded request bands into canonical
// construction. Specified records omission, including explicit empty values.
type RequestParams struct {
	Model            Specified[string]
	Instructions     Specified[InstructionSet]
	Items            []CanonicalItem
	Tools            Specified[ToolSet]
	PreviousResponse *ResponseRef
	ToolPolicy       Specified[ToolPolicy]
	ToolCallBatch    Specified[ToolCallBatchPolicy]
	Controls         GenerationControls
	Reasoning        ReasoningControls
	OutputFormat     Specified[OutputFormat]
}

func NewCanonicalRequest(params RequestParams) CanonicalRequest {
	return CanonicalRequest{
		model:            cloneSpecified(params.Model, func(value string) string { return strings.TrimSpace(value) }), // swobu:io-string source=domain
		instructions:     cloneSpecified(params.Instructions, InstructionSet.Clone),
		items:            cloneCanonicalItems(params.Items),
		tools:            cloneSpecified(params.Tools, ToolSet.Clone),
		previousResponse: cloneResponseRefPointer(params.PreviousResponse),
		toolPolicy:       cloneSpecified(params.ToolPolicy, ToolPolicy.Clone),
		toolBatch:        cloneSpecified(params.ToolCallBatch, ToolCallBatchPolicy.Clone),
		controls:         params.Controls.Clone(),
		reasoning:        params.Reasoning.Clone(),
		outputFormat:     cloneSpecified(params.OutputFormat, OutputFormat.Clone),
	}
}

func cloneSpecified[T any](field Specified[T], clone func(T) T) Specified[T] {
	value, ok := field.Get()
	if !ok {
		return Unspecified[T]()
	}
	return Specify(clone(value))
}

func specifiedValue[T any](field Specified[T]) T {
	value, _ := field.Get()
	return value
}

func (r CanonicalRequest) Model() string        { return specifiedValue(r.model) }
func (r CanonicalRequest) ModelSpecified() bool { return r.model.IsSpecified() }
func (r CanonicalRequest) ModelField() Specified[string] {
	return cloneSpecified(r.model, func(value string) string { return value })
}
func (r CanonicalRequest) Instructions() InstructionSet {
	return specifiedValue(r.instructions).Clone()
}
func (r CanonicalRequest) InstructionsSpecified() bool { return r.instructions.IsSpecified() }
func (r CanonicalRequest) InstructionsField() Specified[InstructionSet] {
	return cloneSpecified(r.instructions, InstructionSet.Clone)
}
func (r CanonicalRequest) Items() []CanonicalItem   { return cloneCanonicalItems(r.items) }
func (r CanonicalRequest) Tools() []ToolDeclaration { return specifiedValue(r.tools).Declarations() }
func (r CanonicalRequest) ToolsSpecified() bool     { return r.tools.IsSpecified() }
func (r CanonicalRequest) ToolsField() Specified[ToolSet] {
	return cloneSpecified(r.tools, ToolSet.Clone)
}
func (r CanonicalRequest) ToolPolicy() ToolPolicy {
	return specifiedValue(r.toolPolicy).Clone()
}

// EffectiveToolPolicy resolves the protocol-default policy without changing
// the stored source fact. Session resolution writes this value explicitly
// before a successful attempt is committed.
func (r CanonicalRequest) EffectiveToolPolicy() ToolPolicy {
	if r.ToolPolicySpecified() {
		return r.ToolPolicy()
	}
	if len(r.Tools()) > 0 {
		return NewToolPolicy(ToolPolicyAuto, nil)
	}
	return NewToolPolicy(ToolPolicyNone, nil)
}
func (r CanonicalRequest) ToolPolicySpecified() bool { return r.toolPolicy.IsSpecified() }
func (r CanonicalRequest) ToolPolicyField() Specified[ToolPolicy] {
	return cloneSpecified(r.toolPolicy, ToolPolicy.Clone)
}
func (r CanonicalRequest) ToolCallBatch() ToolCallBatchPolicy {
	return specifiedValue(r.toolBatch).Clone()
}
func (r CanonicalRequest) ToolCallBatchSpecified() bool { return r.toolBatch.IsSpecified() }
func (r CanonicalRequest) ToolCallBatchField() Specified[ToolCallBatchPolicy] {
	return cloneSpecified(r.toolBatch, ToolCallBatchPolicy.Clone)
}
func (r CanonicalRequest) Controls() GenerationControls { return r.controls.Clone() }
func (r CanonicalRequest) Reasoning() ReasoningControls { return r.reasoning.Clone() }
func (r CanonicalRequest) OutputFormat() OutputFormat   { return specifiedValue(r.outputFormat).Clone() }
func (r CanonicalRequest) OutputFormatSpecified() bool  { return r.outputFormat.IsSpecified() }
func (r CanonicalRequest) OutputFormatField() Specified[OutputFormat] {
	return cloneSpecified(r.outputFormat, OutputFormat.Clone)
}

func (r CanonicalRequest) PreviousResponse() (ResponseRef, bool) {
	if r.previousResponse == nil {
		return ResponseRef{}, false
	}
	return r.previousResponse.Clone(), true
}

func (r CanonicalRequest) Clone() CanonicalRequest {
	return NewCanonicalRequest(RequestParams{
		Model:            cloneSpecified(r.model, func(value string) string { return value }),
		Instructions:     cloneSpecified(r.instructions, InstructionSet.Clone),
		Items:            r.items,
		Tools:            cloneSpecified(r.tools, ToolSet.Clone),
		PreviousResponse: r.previousResponse,
		ToolPolicy:       cloneSpecified(r.toolPolicy, ToolPolicy.Clone),
		ToolCallBatch:    cloneSpecified(r.toolBatch, ToolCallBatchPolicy.Clone),
		Controls:         r.controls,
		Reasoning:        r.reasoning,
		OutputFormat:     cloneSpecified(r.outputFormat, OutputFormat.Clone),
	})
}

func cloneResponseRefPointer(ref *ResponseRef) *ResponseRef {
	if ref == nil {
		return nil
	}
	cloned := ref.Clone()
	return &cloned
}

// CloneCanonicalRequest protects provider and app seams from mutation after a
// request has been accepted.
func CloneCanonicalRequest(req CanonicalRequest) CanonicalRequest { return req.Clone() }
