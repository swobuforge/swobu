// event-state machine together so migration behavior stays recoverable.
package responses

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/adapters/outbound/protocols"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type responseEnvelope struct {
	ID         string `json:"id"`
	Model      string `json:"model"`
	OutputText string `json:"output_text"`
	Output     []struct {
		Type      string          `json:"type"`
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		CallID    string          `json:"call_id"`
		Name      string          `json:"name"`
		Arguments string          `json:"arguments"`
	} `json:"output"`
}

var tokenUsagePathSpec = protocols.TokenUsagePathSpec{
	InputPaths: [][]string{
		{"usage", "input_tokens"},
		{"usage", "prompt_tokens"},
		{"response", "usage", "input_tokens"},
		{"response", "usage", "prompt_tokens"},
		{"usageMetadata", "promptTokenCount"},
		{"usage", "inputTokens"},
		{"response", "usage", "inputTokens"},
	},
	OutputPaths: [][]string{
		{"usage", "output_tokens"},
		{"usage", "completion_tokens"},
		{"response", "usage", "output_tokens"},
		{"response", "usage", "completion_tokens"},
		{"usageMetadata", "candidatesTokenCount"},
		{"usage", "outputTokens"},
		{"response", "usage", "outputTokens"},
	},
	CacheReadPaths: [][]string{
		{"usage", "input_tokens_details", "cached_tokens"},
		{"usage", "prompt_tokens_details", "cached_tokens"},
		{"response", "usage", "input_tokens_details", "cached_tokens"},
		{"response", "usage", "prompt_tokens_details", "cached_tokens"},
		{"usage", "cache_read_input_tokens"},
		{"response", "usage", "cache_read_input_tokens"},
		{"usageMetadata", "cachedContentTokenCount"},
		{"usage", "cacheReadInputTokens"},
		{"response", "usage", "cacheReadInputTokens"},
	},
	CacheWritePaths: [][]string{
		{"usage", "input_tokens_details", "cache_write_tokens"},
		{"usage", "prompt_tokens_details", "cache_write_tokens"},
		{"response", "usage", "input_tokens_details", "cache_write_tokens"},
		{"response", "usage", "prompt_tokens_details", "cache_write_tokens"},
		{"usage", "cache_creation_input_tokens"},
		{"response", "usage", "cache_creation_input_tokens"},
		{"usage", "cacheWriteInputTokens"},
		{"response", "usage", "cacheWriteInputTokens"},
	},
}

func DecodeResponseBuffered(raw []byte) (canonical.CanonicalOutputValue, error) {
	var dto responseEnvelope
	if err := json.Unmarshal(raw, &dto); err != nil {
		return canonical.CanonicalOutputValue{}, canonical.InternalError("responses output is invalid JSON")
	}
	items, err := decodeOutputItems(dto.Output, dto.OutputText)
	if err != nil {
		return canonical.CanonicalOutputValue{}, err
	}
	return canonical.NewConversationOutputWithUsage(
		dto.ID,
		dto.Model,
		items,
		"completed",
		protocols.ExtractTokenUsage(raw, tokenUsagePathSpec),
	), nil
}

// DecodeResponseStream returns canonical envelope events directly for responses streams.
func DecodeResponseStream(body io.ReadCloser, exchangeID string) canonical.EventReader {
	return &responsesEventReader{
		exchangeID:  exchangeID,
		responseID:  canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		reader:      protocols.NewSSEReader(body),
		startedTool: map[string]bool{},
		toolEnvIDs:  map[string]canonical.EnvelopeID{},
		textOpen:    false,
		latestUsage: canonical.NewUnknownTokenUsage(),
	}
}

type responsesEventReader struct {
	exchangeID  string
	responseID  canonical.EnvelopeID
	reader      *protocols.SSEReaderCloser
	pending     []canonical.Event
	startedTool map[string]bool
	toolEnvIDs  map[string]canonical.EnvelopeID
	textOpen    bool
	textEnvID   canonical.EnvelopeID
	started     bool
	completed   bool
	latestUsage canonical.TokenUsage
	seq         int64
}

// ordered state machine over text, tool calls, reasoning, and terminal frames.
// variants while maintaining canonical output ordering.
func (s *responsesEventReader) Next(context.Context) (canonical.Event, error) {
	if len(s.pending) > 0 {
		event := s.pending[0]
		s.pending = s.pending[1:]
		return event, nil
	}
	for {
		event, err := s.reader.Next()
		if err != nil {
			if err == io.EOF && s.started && !s.completed {
				s.enqueueError("stream_unexpected_eof", "output stream ended before completed")
				s.closeOpenText(canonical.EnvelopeStatusError)
				s.closeOpenTools(canonical.EnvelopeStatusError)
				s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusError)
				s.completed = true
				if len(s.pending) > 0 {
					out := s.pending[0]
					s.pending = s.pending[1:]
					return out, nil
				}
			}
			return canonical.Event{}, err
		}
		if strings.TrimSpace(event.Data) == "[DONE]" { // swobu:io-string source=boundary
			continue
		}
		rawFrame := []byte(event.Data)
		frameUsage := protocols.ExtractTokenUsage(rawFrame, tokenUsagePathSpec)
		if !frameUsage.IsZero() {
			s.latestUsage = frameUsage
		}
		var frame streamFrame
		if err := json.Unmarshal(rawFrame, &frame); err != nil {
			return canonical.Event{}, canonical.InternalError("responses stream event is invalid JSON")
		}
		handled, nextEvent, nextErr := s.handleFrame(frame)
		if nextErr != nil {
			return canonical.Event{}, nextErr
		}
		if handled {
			return nextEvent, nil
		}
	}
}

func (s *responsesEventReader) Close(context.Context) error {
	return s.reader.Close()
}

func (s *responsesEventReader) nextSeq() int64 {
	s.seq++
	return s.seq
}

func (s *responsesEventReader) enqueue(ev canonical.Event) {
	ev.ExchangeID = s.exchangeID
	ev.Seq = s.nextSeq()
	ev.Time = time.Now().UTC()
	s.pending = append(s.pending, ev)
}

func (s *responsesEventReader) enqueueEnvelopeStart(id canonical.EnvelopeID, parent canonical.EnvelopeID, payload canonical.EnvelopeStartPayload, meta ...canonical.EventMetadataFields) {
	ev := canonical.Event{Kind: canonical.EventEnvelopeStart, EnvID: id, ParentID: parent, Payload: payload}
	if len(meta) > 0 {
		ev.Meta = meta[0]
	}
	s.enqueue(ev)
}

func (s *responsesEventReader) enqueueEnvelopeEnd(id canonical.EnvelopeID, kind canonical.EnvelopeKind, status canonical.EnvelopeStatus) {
	s.enqueue(canonical.Event{Kind: canonical.EventEnvelopeEnd, EnvID: id, Payload: canonical.EnvelopeEndPayload{Kind: kind, Status: status}})
}

func (s *responsesEventReader) enqueueTextDelta(id canonical.EnvelopeID, text string) {
	s.enqueue(canonical.Event{Kind: canonical.EventTextDelta, EnvID: id, Payload: canonical.TextDeltaPayload{Text: text}})
}

func (s *responsesEventReader) enqueueArgsDelta(id canonical.EnvelopeID, args string) {
	s.enqueue(canonical.Event{Kind: canonical.EventArgsDelta, EnvID: id, Payload: canonical.ArgsDeltaPayload{Args: args}})
}

func (s *responsesEventReader) enqueueUsage(usage canonical.TokenUsage) {
	s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseID, Payload: canonical.UsagePayload{Usage: usage}})
}

func (s *responsesEventReader) enqueueFinish(reason string) {
	s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseID, Payload: canonical.FinishPayload{Reason: reason}})
}

func (s *responsesEventReader) enqueueMetadata(values map[string]string) {
	s.enqueue(canonical.Event{Kind: canonical.EventMetadata, EnvID: s.responseID, Payload: canonical.MetadataPayload{Values: values}})
}

func (s *responsesEventReader) enqueueError(code string, message string) {
	s.enqueue(canonical.Event{Kind: canonical.EventError, EnvID: s.responseID, Payload: canonical.ErrorPayload{Code: code, Message: message}})
}

func (s *responsesEventReader) closeOpenText(status canonical.EnvelopeStatus) {
	if s.textOpen {
		s.enqueueEnvelopeEnd(s.textEnvID, canonical.EnvMessage, status)
		s.textOpen = false
		s.textEnvID = ""
	}
}

func (s *responsesEventReader) closeOpenTools(status canonical.EnvelopeStatus) {
	for itemID, envID := range s.toolEnvIDs {
		s.enqueueEnvelopeEnd(envID, canonical.EnvToolCall, status)
		delete(s.toolEnvIDs, itemID)
		delete(s.startedTool, itemID)
	}
}

func fallbackItemID(itemID string, callID string) string {
	if strings.TrimSpace(itemID) != "" { // swobu:io-string source=boundary
		return strings.TrimSpace(itemID) // swobu:io-string source=boundary
	}
	if strings.TrimSpace(callID) != "" { // swobu:io-string source=boundary
		return strings.TrimSpace(callID) // swobu:io-string source=boundary
	}
	return "tool_0"
}

func decodeOutputItems(items []struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
}, outputText string) ([]canonical.OutputItem, error) {
	output := make([]canonical.OutputItem, 0, len(items))
	for _, item := range items {
		itemType := strings.TrimSpace(item.Type) // swobu:io-string source=provider-wire
		switch itemType {
		case "message":
			parts, err := decodeMessageTextParts(item.Content)
			if err != nil {
				return nil, err
			}
			for idx, part := range parts {
				output = append(output, canonical.NewTextOutputItem(fmt.Sprintf("text_%d", len(output)+idx), part.Text))
			}
		case "function_call":
			input := map[string]any{}
			if strings.TrimSpace(item.Arguments) != "" { // swobu:io-string source=boundary
				if err := json.Unmarshal([]byte(item.Arguments), &input); err != nil {
					return nil, canonical.InternalError("responses function_call arguments are invalid")
				}
			}
			itemID := fallbackItemID("", item.CallID)
			output = append(output, canonical.NewToolUseOutputItem(itemID, strings.TrimSpace(item.CallID), strings.TrimSpace(item.Name), input)) // swobu:io-string source=boundary
		case "reasoning":
			continue
		default:
			return nil, canonical.UnsupportedOperation("responses output item type is not implemented")
		}
	}
	if len(output) == 0 && strings.TrimSpace(outputText) != "" { // swobu:io-string source=boundary
		output = append(output, canonical.NewTextOutputItem("text_0", outputText))
	}
	return output, nil
}
