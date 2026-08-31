// translation in one place so request and stream semantics stay recoverable.
package responses

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

// DecodeResponsesToolPolicy maps responses tool_choice into canonical tool
// policy. String auto/required values remain direct. Specific tool selection is
// resolved against the declared tools so the semantic tool ID stays canonical.
func DecodeResponsesToolPolicy(raw json.RawMessage, tools []canonical.ToolDeclaration, changeLog *[]compat.Change, exchangeID string) (canonical.ToolPolicy, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw))) // swobu:io-string source=boundary
	if len(raw) == 0 || string(raw) == "null" {
		if len(tools) > 0 {
			return canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil), nil
		}
		return canonical.NewToolPolicy(canonical.ToolPolicyNone, nil), nil
	}

	var stringMode string
	if err := json.Unmarshal(raw, &stringMode); err == nil {
		mode, ok := canonical.ParseToolPolicyMode(stringMode)
		if !ok {
			return canonical.ToolPolicy{}, canonical.BadRequest("responses request tool_choice is invalid")
		}
		switch mode {
		case canonical.ToolPolicyNone:
			return canonical.NewToolPolicy(canonical.ToolPolicyNone, nil), nil
		case canonical.ToolPolicyAuto:
			return canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil), nil
		case canonical.ToolPolicyRequired:
			return canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), nil
		case canonical.ToolPolicySpecific:
			return canonical.ToolPolicy{}, canonical.BadRequest("responses request tool_choice specific requires a tool name")
		}
	}

	var objectMode struct {
		Type        string `json:"type"`
		Name        string `json:"name"`
		ServerLabel string `json:"server_label"`
		Function    struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &objectMode); err != nil {
		return canonical.ToolPolicy{}, canonical.BadRequest("responses request tool_choice is invalid")
	}
	normalizedTypeRaw := strings.TrimSpace(objectMode.Type) // swobu:io-string source=provider-wire
	normalizedType := strings.ToLower(normalizedTypeRaw)    // swobu:io-string source=domain
	switch normalizedType {
	case "auto":
		return canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil), nil
	case "required":
		return canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), nil
	case canonical.ToolTypeWebSearch:
		decl, _, err := canonical.ResolveToolDeclarationByKey(
			tools,
			canonical.WebSearchToolKey(),
			canonical.ToolTypeWebSearch,
		)
		if err != nil {
			return canonical.ToolPolicy{}, err
		}
		specific := decl.Key()
		return canonical.NewToolPolicy(canonical.ToolPolicySpecific, &specific), nil
	case "mcp":
		label := strings.TrimSpace(objectMode.ServerLabel) // swobu:io-string source=provider-wire
		if label == "" {
			return canonical.ToolPolicy{}, canonical.BadRequest("responses request MCP tool_choice requires server_label")
		}
		specific, err := canonical.NewToolKey("mcp", canonical.ToolKindMCP, label)
		if err != nil {
			return canonical.ToolPolicy{}, canonical.BadRequest("responses request MCP tool_choice server_label is invalid")
		}
		return canonical.NewToolPolicy(canonical.ToolPolicySpecific, &specific), nil
	case "function", "custom":
		name := strings.TrimSpace(objectMode.Name) // swobu:io-string source=provider-wire
		fieldPath := "tool_choice.name"
		if name == "" {
			name = strings.TrimSpace(objectMode.Function.Name) // swobu:io-string source=provider-wire
			fieldPath = "tool_choice.function.name"
		}
		if name == "" {
			return canonical.ToolPolicy{}, canonical.BadRequest("responses request tool_choice specific requires a tool name")
		}
		resolved, _, err := responsesResolveToolChoiceByWireName(tools, name, normalizedType, fieldPath, changeLog, exchangeID)
		if err != nil {
			return canonical.ToolPolicy{}, err
		}
		specific := resolved.Key()
		policy := canonical.NewToolPolicy(canonical.ToolPolicySpecific, &specific)
		return policy, nil
	default:
		return canonical.ToolPolicy{}, canonical.BadRequest("responses request tool_choice is invalid")
	}
}

type sseEnvelopeStreamEncoder struct {
	wire           ResponseStreamWireEncoder
	adapter        *sse.EnvelopeEventAdapter
	sequenceNumber uint64
}

func (s *sseEnvelopeStreamEncoder) EncodeEnvelopeEvent(event canonical.Event) ([][]byte, error) {
	streamEvents, err := s.adapter.Translate(event)
	if err != nil {
		return nil, err
	}
	frames := make([][]byte, 0, len(streamEvents))
	for _, streamEvent := range streamEvents {
		emitted, err := s.Encode(streamEvent)
		if err != nil {
			return nil, err
		}
		frames = append(frames, emitted...)
	}
	return frames, nil
}

func (s *sseEnvelopeStreamEncoder) Encode(event sse.StreamEvent) ([][]byte, error) {
	encoder := s.encoder()
	rawFrames, err := encoder.Encode(event)
	if err != nil {
		return nil, err
	}
	for _, raw := range rawFrames {
		logResponsesStreamProjectionFrame(raw)
	}
	frames := make([][]byte, 0, len(rawFrames))
	for _, raw := range rawFrames {
		frame, err := s.responsesSSEEventFrame(raw)
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

func (s *sseEnvelopeStreamEncoder) Finish() ([][]byte, error) {
	encoder := s.encoder()
	rawFrames, err := encoder.Finish()
	if err != nil {
		return nil, err
	}
	for _, raw := range rawFrames {
		logResponsesStreamProjectionFrame(raw)
	}
	frames := make([][]byte, 0, len(rawFrames))
	for _, raw := range rawFrames {
		frame, err := s.responsesSSEEventFrame(raw)
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

func (s *sseEnvelopeStreamEncoder) responsesSSEEventFrame(raw []byte) ([]byte, error) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("encode Responses SSE event name: %w", err)
	}
	if envelope.Type == "" {
		return nil, fmt.Errorf("encode Responses SSE event name: frame type is empty")
	}
	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return nil, fmt.Errorf("encode Responses SSE sequence: frame is not an object")
	}
	payload := make([]byte, 0, len(trimmed)+32)
	payload = append(payload, trimmed[:len(trimmed)-1]...)
	payload = append(payload, []byte(`,"sequence_number":`)...)
	payload = strconv.AppendUint(payload, s.sequenceNumber, 10)
	payload = append(payload, '}')
	s.sequenceNumber++
	return sse.SSEEventFrame(envelope.Type, payload), nil
}

func (s *sseEnvelopeStreamEncoder) encoder() *ResponseStreamWireEncoder {
	if s.wire.toolItems == nil {
		s.wire = NewResponseStreamWireEncoder()
	}
	return &s.wire
}
