package canonical

import (
	"encoding/json"
	"fmt"
	"time"

	"strings"
)

// GrammarValidator enforces the canonical envelope lifecycle rules observed by
// the request-path tests and stream projectors.
type GrammarValidator struct {
	lastSeq int64
	open    map[EnvelopeID]*grammarEnvelopeState
	closed  map[EnvelopeID]EnvelopeKind
}

type grammarEnvelopeState struct {
	kind         EnvelopeKind
	parent       EnvelopeID
	openChildren int
}

// NewGrammarValidator constructs an empty canonical envelope validator.
func NewGrammarValidator() *GrammarValidator {
	return &GrammarValidator{
		open:   map[EnvelopeID]*grammarEnvelopeState{},
		closed: map[EnvelopeID]EnvelopeKind{},
	}
}

// Observe validates one canonical envelope event against ordering and
// lifecycle rules.
func (v *GrammarValidator) Observe(ev Event) error {
	if v == nil {
		return nil
	}
	v.init()
	if ev.Seq <= v.lastSeq {
		return fmt.Errorf("event sequence regressed")
	}
	v.lastSeq = ev.Seq

	switch ev.Kind {
	case EventEnvelopeStart:
		return v.observeStart(ev)
	case EventEnvelopeEnd:
		return v.observeEnd(ev)
	case EventTextDelta:
		return v.requireOpen(ev, EnvMessage, EnvToolResult)
	case EventArgsDelta:
		return v.requireOpen(ev, EnvToolCall)
	case EventUsage, EventFinish, EventError, EventMetadata:
		return v.requireOpen(ev)
	default:
		return fmt.Errorf("event kind %q is unsupported", ev.Kind)
	}
}

func (v *GrammarValidator) init() {
	if v.open == nil {
		v.open = map[EnvelopeID]*grammarEnvelopeState{}
	}
	if v.closed == nil {
		v.closed = map[EnvelopeID]EnvelopeKind{}
	}
}

func (v *GrammarValidator) observeStart(ev Event) error {
	payload, ok := ev.Payload.(EnvelopeStartPayload)
	if !ok {
		return fmt.Errorf("envelope.start payload type %T is unsupported", ev.Payload)
	}
	if payload.Kind == "" {
		return fmt.Errorf("envelope.start kind is required")
	}
	if ev.EnvID == "" {
		return fmt.Errorf("envelope.start env id is required")
	}
	if _, ok := v.open[ev.EnvID]; ok {
		return fmt.Errorf("envelope %q is already open", ev.EnvID)
	}
	if _, ok := v.closed[ev.EnvID]; ok {
		return fmt.Errorf("envelope %q was already closed", ev.EnvID)
	}
	state := &grammarEnvelopeState{kind: payload.Kind, parent: ev.ParentID}
	if ev.ParentID != "" {
		parent, ok := v.open[ev.ParentID]
		if !ok {
			return fmt.Errorf("parent envelope %q is not open", ev.ParentID)
		}
		parent.openChildren++
	}
	v.open[ev.EnvID] = state
	return nil
}

func (v *GrammarValidator) observeEnd(ev Event) error {
	payload, ok := ev.Payload.(EnvelopeEndPayload)
	if !ok {
		return fmt.Errorf("envelope.end payload type %T is unsupported", ev.Payload)
	}
	state, ok := v.open[ev.EnvID]
	if !ok {
		return fmt.Errorf("close for unknown envelope %q", ev.EnvID)
	}
	if payload.Kind != "" && payload.Kind != state.kind {
		return fmt.Errorf("close for envelope %q has mismatched kind", ev.EnvID)
	}
	if state.openChildren > 0 {
		return fmt.Errorf("envelope %q cannot close before its children", ev.EnvID)
	}
	delete(v.open, ev.EnvID)
	v.closed[ev.EnvID] = state.kind
	if state.parent != "" {
		if parent, ok := v.open[state.parent]; ok && parent.openChildren > 0 {
			parent.openChildren--
		}
	}
	return nil
}

func (v *GrammarValidator) requireOpen(ev Event, allowedKinds ...EnvelopeKind) error {
	state, ok := v.open[ev.EnvID]
	if !ok {
		return fmt.Errorf("event %q requires an open envelope", ev.Kind)
	}
	if len(allowedKinds) == 0 {
		return nil
	}
	for _, kind := range allowedKinds {
		if state.kind == kind {
			return nil
		}
	}
	return fmt.Errorf("event %q is not valid for envelope kind %q", ev.Kind, state.kind)
}

// SynthesizeResponseFromOutput converts canonical output into a finite response
// envelope stream suitable for stream or batch adapters.
func SynthesizeResponseFromOutput(exchangeID string, output CanonicalOutput) ([]Event, error) {
	if output == nil {
		return nil, fmt.Errorf("output is nil")
	}
	return SynthesizeResponseEnvelopeEvents(exchangeID, output.Response(), output.Model(), output.Items(), output.FinishReason(), output.Usage()), nil
}

// SynthesizeRequestFromCanonicalRequest converts a canonical request into a
// finite request envelope stream suitable for round-trip projection tests.
func SynthesizeRequestFromCanonicalRequest(exchangeID string, request CanonicalRequest) ([]Event, error) {
	seq := int64(0)
	next := func() int64 {
		seq++
		return seq
	}
	requestID := EnvelopeID(fmt.Sprintf("%s:request:0", exchangeID))
	toolsRaw, err := encodeRequestToolDeclsMetadata(request.Tools())
	if err != nil {
		return nil, err
	}
	toolPolicyRaw, err := encodeToolPolicyMetadataForTest(request.ToolPolicy())
	if err != nil {
		return nil, err
	}
	toolCallBatchRaw, err := encodeToolCallBatchMetadata(request.ToolCallBatch())
	if err != nil {
		return nil, err
	}
	controlsRaw, err := encodeGenerationControlsMetadata(request.Controls())
	if err != nil {
		return nil, err
	}
	outputFormatRaw, err := encodeOutputFormatMetadata(request.OutputFormat())
	if err != nil {
		return nil, err
	}
	events := []Event{
		{
			ExchangeID: exchangeID,
			Seq:        next(),
			Time:       time.Now().UTC(),
			Kind:       EventEnvelopeStart,
			EnvID:      requestID,
			Payload: EnvelopeStartPayload{
				Kind: EnvRequest,
			},
		},
	}
	metadata := map[string]string{
		"model":               request.Model(),
		"tools":               toolsRaw,
		"tool_policy":         toolPolicyRaw,
		"tool_call_batch":     toolCallBatchRaw,
		"generation_controls": controlsRaw,
		"output_format":       outputFormatRaw,
	}
	events = append(events, Event{
		ExchangeID: exchangeID,
		Seq:        next(),
		Time:       time.Now().UTC(),
		Kind:       EventMetadata,
		EnvID:      requestID,
		Payload:    MetadataPayload{Values: metadata},
	})
	msgIdx := 0
	toolIdx := 0
	resultIdx := 0
	for _, item := range request.Items() {
		switch item.Kind() {
		case ItemKindText:
			text, ok := item.TextItem()
			if !ok {
				return nil, fmt.Errorf("text kind has payload %T", item.payload)
			}
			id := EnvelopeID(fmt.Sprintf("%s:message:%d", requestID, msgIdx))
			msgIdx++
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: requestID, Payload: EnvelopeStartPayload{Kind: EnvMessage, Role: item.Author()}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventTextDelta, EnvID: id, ParentID: requestID, Payload: TextDeltaPayload{Text: text.Text}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: requestID, Payload: EnvelopeEndPayload{Kind: EnvMessage, Status: EnvelopeStatusCompleted}},
			)
		case ItemKindToolUse:
			toolUse, ok := item.ToolUse()
			if !ok {
				return nil, fmt.Errorf("tool-use kind has payload %T", item.payload)
			}
			id := EnvelopeID(fmt.Sprintf("%s:tool_call:%d", requestID, toolIdx))
			toolIdx++
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: requestID, Payload: EnvelopeStartPayload{Kind: EnvToolCall, Name: toolUse.Name, ToolUseID: toolUse.UseID, Role: item.Author()}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventArgsDelta, EnvID: id, ParentID: requestID, Payload: ArgsDeltaPayload{Args: toolUse.Input.RawObject()}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: requestID, Payload: EnvelopeEndPayload{Kind: EnvToolCall, Status: EnvelopeStatusCompleted}},
			)
		case ItemKindToolResult:
			toolResult, ok := item.ToolResult()
			if !ok {
				return nil, fmt.Errorf("tool-result kind has payload %T", item.payload)
			}
			id := EnvelopeID(fmt.Sprintf("%s:tool_result:%d", requestID, resultIdx))
			resultIdx++
			events = append(events,
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeStart, EnvID: id, ParentID: requestID, Payload: EnvelopeStartPayload{Kind: EnvToolResult, ToolUseID: toolResult.UseID, Role: item.Author()}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventTextDelta, EnvID: id, ParentID: requestID, Payload: TextDeltaPayload{Text: toolResult.Text}},
				Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: id, ParentID: requestID, Payload: EnvelopeEndPayload{Kind: EnvToolResult, Status: EnvelopeStatusCompleted}},
			)
		default:
			// Ignore unsupported request item kinds during synthesis.
		}
	}
	events = append(events,
		Event{ExchangeID: exchangeID, Seq: next(), Time: time.Now().UTC(), Kind: EventEnvelopeEnd, EnvID: requestID, Payload: EnvelopeEndPayload{Kind: EnvRequest, Status: EnvelopeStatusCompleted}},
	)
	return events, nil
}

func encodeRequestToolDeclsMetadata(tools []ToolDecl) (string, error) {
	if len(tools) == 0 {
		return "", nil
	}
	type requestToolDeclMetadataDTO struct {
		Kind             string          `json:"kind,omitempty"`
		ID               string          `json:"id,omitempty"`
		Name             string          `json:"name,omitempty"`
		Description      string          `json:"description,omitempty"`
		InputSchema      json.RawMessage `json:"input_schema,omitempty"`
		Strict           *bool           `json:"strict,omitempty"`
		Format           json.RawMessage `json:"format,omitempty"`
		Capability       string          `json:"capability,omitempty"`
		CapabilityConfig json.RawMessage `json:"capability_config,omitempty"`
		Execution        string          `json:"execution,omitempty"`
	}
	var encodeDecls func([]ToolDecl) ([]requestToolDeclMetadataDTO, error)
	encodeDecls = func(tools []ToolDecl) ([]requestToolDeclMetadataDTO, error) {
		dto := make([]requestToolDeclMetadataDTO, 0, len(tools))
		for _, tool := range tools {
			if tool == nil {
				return nil, BadRequest("canonical request tool declarations are invalid")
			}
			execution := strings.TrimSpace(string(tool.Owner())) // swobu:io-string source=domain
			switch decl := tool.(type) {
			case FunctionToolDecl:
				schema, err := encodeToolSchemaMetadataForTest(decl.ToolInputSchema())
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(decl.ToolName()) == "" { // swobu:io-string source=domain
					return nil, BadRequest("canonical request tool declarations require a name")
				}
				dto = append(dto, requestToolDeclMetadataDTO{
					Kind:        "function",
					ID:          strings.TrimSpace(decl.ToolID().String()), // swobu:io-string source=domain
					Name:        strings.TrimSpace(decl.ToolName()),        // swobu:io-string source=domain
					Description: strings.TrimSpace(decl.ToolDescription()), // swobu:io-string source=domain
					InputSchema: schema,
					Strict:      cloneBoolPointer(decl.Strict),
					Execution:   execution,
				})
			case *FunctionToolDecl:
				schema, err := encodeToolSchemaMetadataForTest(decl.ToolInputSchema())
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(decl.ToolName()) == "" { // swobu:io-string source=domain
					return nil, BadRequest("canonical request tool declarations require a name")
				}
				dto = append(dto, requestToolDeclMetadataDTO{
					Kind:        "function",
					ID:          strings.TrimSpace(decl.ToolID().String()), // swobu:io-string source=domain
					Name:        strings.TrimSpace(decl.ToolName()),        // swobu:io-string source=domain
					Description: strings.TrimSpace(decl.ToolDescription()), // swobu:io-string source=domain
					InputSchema: schema,
					Strict:      cloneBoolPointer(decl.Strict),
					Execution:   execution,
				})
			case CustomToolDecl:
				format, err := encodeToolFormatMetadataForTest(decl.Format)
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(decl.ToolName()) == "" { // swobu:io-string source=domain
					return nil, BadRequest("canonical request tool declarations require a name")
				}
				dto = append(dto, requestToolDeclMetadataDTO{
					Kind:        "custom",
					ID:          strings.TrimSpace(decl.ToolID().String()), // swobu:io-string source=domain
					Name:        strings.TrimSpace(decl.ToolName()),        // swobu:io-string source=domain
					Description: strings.TrimSpace(decl.ToolDescription()), // swobu:io-string source=domain
					Format:      format,
					Execution:   execution,
				})
			case *CustomToolDecl:
				format, err := encodeToolFormatMetadataForTest(decl.Format)
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(decl.ToolName()) == "" { // swobu:io-string source=domain
					return nil, BadRequest("canonical request tool declarations require a name")
				}
				dto = append(dto, requestToolDeclMetadataDTO{
					Kind:        "custom",
					ID:          strings.TrimSpace(decl.ToolID().String()), // swobu:io-string source=domain
					Name:        strings.TrimSpace(decl.ToolName()),        // swobu:io-string source=domain
					Description: strings.TrimSpace(decl.ToolDescription()), // swobu:io-string source=domain
					Format:      format,
					Execution:   execution,
				})
			case CapabilityToolDecl:
				config, err := encodeToolCapabilityConfigMetadataForTest(decl.CapabilityConfig())
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(string(decl.ToolCapability())) == "" { // swobu:io-string source=domain
					return nil, BadRequest("canonical request tool declarations require a capability")
				}
				dto = append(dto, requestToolDeclMetadataDTO{
					Kind:             "capability",
					ID:               strings.TrimSpace(decl.ToolID().String()),        // swobu:io-string source=domain
					Capability:       strings.TrimSpace(string(decl.ToolCapability())), // swobu:io-string source=domain
					CapabilityConfig: config,
					Execution:        execution,
				})
			case *CapabilityToolDecl:
				config, err := encodeToolCapabilityConfigMetadataForTest(decl.CapabilityConfig())
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(string(decl.ToolCapability())) == "" { // swobu:io-string source=domain
					return nil, BadRequest("canonical request tool declarations require a capability")
				}
				dto = append(dto, requestToolDeclMetadataDTO{
					Kind:             "capability",
					ID:               strings.TrimSpace(decl.ToolID().String()),        // swobu:io-string source=domain
					Capability:       strings.TrimSpace(string(decl.ToolCapability())), // swobu:io-string source=domain
					CapabilityConfig: config,
					Execution:        execution,
				})
			default:
				return nil, InternalError("canonical request tool declarations contain an unsupported tool declaration type")
			}
		}
		return dto, nil
	}
	dto, err := encodeDecls(tools)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(dto)
	if err != nil {
		return "", InternalError("canonical request tool declarations could not be encoded")
	}
	return string(raw), nil
}

func encodeToolSchemaMetadataForTest(schema ToolSchema) (json.RawMessage, error) {
	raw := strings.TrimSpace(schema.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return nil, BadRequest("canonical request tool declarations require input_schema")
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil, BadRequest("canonical request tool declarations require a JSON object input_schema")
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return nil, InternalError("canonical request tool declarations could not be encoded")
	}
	return json.RawMessage(normalized), nil
}

func encodeToolFormatMetadataForTest(format ToolFormat) (json.RawMessage, error) {
	raw := strings.TrimSpace(format.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return nil, BadRequest("canonical request tool declarations require format")
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil, BadRequest("canonical request tool declarations require a JSON object format")
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return nil, InternalError("canonical request tool declarations could not be encoded")
	}
	return json.RawMessage(normalized), nil
}

func encodeToolCapabilityConfigMetadataForTest(config ToolCapabilityConfig) (json.RawMessage, error) {
	raw := strings.TrimSpace(config.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return nil, nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil, BadRequest("canonical request tool capability config must be a JSON object")
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return nil, InternalError("canonical request tool capability config could not be encoded")
	}
	return json.RawMessage(normalized), nil
}

func encodeToolPolicyMetadataForTest(policy ToolPolicy) (string, error) {
	if err := policy.Validate(); err != nil {
		return "", err
	}
	type requestToolPolicyMetadataDTO struct {
		Mode         string `json:"mode"`
		Specific     string `json:"specific,omitempty"`
		SpecificType string `json:"specific_type,omitempty"`
	}
	dto := requestToolPolicyMetadataDTO{Mode: string(policy.Mode)}
	if specific, ok := policy.SpecificID(); ok {
		dto.Mode = string(ToolPolicySpecific)
		dto.Specific = specific.String()
		if specificType, ok := policy.SpecificToolType(); ok {
			dto.SpecificType = specificType
		}
	}
	raw, err := json.Marshal(dto)
	if err != nil {
		return "", InternalError("canonical request tool policy could not be encoded")
	}
	return string(raw), nil
}
