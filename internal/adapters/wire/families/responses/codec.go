// translation in one place so request and stream semantics stay recoverable.
package responses

import (
	"encoding/json"
	"strings"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// DecodeResponsesToolMode is intentionally permissive for unknown enum/object
// values. Known values map to canonical tool modes, and unknown values degrade
// to default mode for forward canonical. Invalid JSON type shapes still fail
// request validation.
func DecodeResponsesToolMode(raw json.RawMessage) (canonical.ToolMode, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw))) // swobu:io-string source=boundary
	if len(raw) == 0 || string(raw) == "null" {
		return canonical.ToolModeDefault, nil
	}

	var stringMode string
	if err := json.Unmarshal(raw, &stringMode); err == nil {
		normalizedMode := strings.ToLower(strings.TrimSpace(stringMode)) // swobu:io-string source=provider-wire
		switch normalizedMode {
		case "", "none":
			return canonical.ToolModeDefault, nil
		case "auto":
			return canonical.ToolModeAuto, nil
		case "required":
			return canonical.ToolModeRequired, nil
		default:
			return canonical.ToolModeDefault, nil
		}
	}

	var objectMode struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &objectMode); err != nil {
		return canonical.ToolModeDefault, canonical.BadRequest("responses request tool_choice is invalid")
	}
	normalizedType := strings.ToLower(strings.TrimSpace(objectMode.Type)) // swobu:io-string source=provider-wire
	switch normalizedType {
	case "function", "required":
		return canonical.ToolModeRequired, nil
	case "auto":
		return canonical.ToolModeAuto, nil
	default:
		return canonical.ToolModeDefault, nil
	}
}

type sseEnvelopeStreamEncoder struct {
	wire    ResponseStreamWireEncoder
	adapter *sse.EnvelopeEventAdapter
}

func (s *sseEnvelopeStreamEncoder) EncodeEnvelopeEvent(event canonical.Event) ([][]byte, error) {
	streamEvents := s.adapter.Translate(event)
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
		logResponsesEgressStreamFrame(raw)
	}
	frames := make([][]byte, 0, len(rawFrames))
	for _, raw := range rawFrames {
		frames = append(frames, sse.SSEData(raw))
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
		logResponsesEgressStreamFrame(raw)
	}
	frames := make([][]byte, 0, len(rawFrames))
	for _, raw := range rawFrames {
		frames = append(frames, sse.SSEData(raw))
	}
	return frames, nil
}

func (s *sseEnvelopeStreamEncoder) encoder() *ResponseStreamWireEncoder {
	if s.wire.toolItems == nil {
		s.wire = NewResponseStreamWireEncoder()
	}
	return &s.wire
}
