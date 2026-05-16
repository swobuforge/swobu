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
	return &canonicalEnvelopeStreamCloser{
		exchangeID:  exchangeID,
		responseID:  canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		reader:      protocols.NewSSEReader(body),
		startedTool: map[string]bool{},
		toolEnvIDs:  map[string]canonical.EnvelopeID{},
		textOpen:    false,
		latestUsage: canonical.NewUnknownTokenUsage(),
	}
}

type canonicalEnvelopeStreamCloser struct {
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
func (s *canonicalEnvelopeStreamCloser) Next(context.Context) (canonical.Event, error) {
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
		if strings.TrimSpace(event.Data) == "[DONE]" { // trimlowerlint:allow boundary canonicalization
			continue
		}
		rawFrame := []byte(event.Data)
		frameUsage := protocols.ExtractTokenUsage(rawFrame, tokenUsagePathSpec)
		if !frameUsage.IsZero() {
			s.latestUsage = frameUsage
		}
		var frame struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			Model     string `json:"model"`
			Delta     string `json:"delta"`
			Status    string `json:"status"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			ItemID    string `json:"item_id"`
			Arguments string `json:"arguments"`
			Response  struct {
				ID     string `json:"id"`
				Model  string `json:"model"`
				Status string `json:"status"`
			} `json:"response"`
			Item struct {
				ID     string `json:"id"`
				Type   string `json:"type"`
				CallID string `json:"call_id"`
				Name   string `json:"name"`
			} `json:"item"`
		}
		if err := json.Unmarshal(rawFrame, &frame); err != nil {
			return canonical.Event{}, canonical.InternalError("responses stream event is invalid JSON")
		}
		switch strings.TrimSpace(frame.Type) { // trimlowerlint:allow boundary canonicalization
		case "response.created":
			if !s.started {
				s.started = true
				s.enqueueEnvelopeStart(s.responseID, "", canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse})
			}
			resultID := strings.TrimSpace(frame.ID) // trimlowerlint:allow boundary canonicalization
			if resultID == "" {
				resultID = strings.TrimSpace(frame.Response.ID) // trimlowerlint:allow boundary canonicalization
			}
			model := strings.TrimSpace(frame.Model) // trimlowerlint:allow boundary canonicalization
			if model == "" {
				model = strings.TrimSpace(frame.Response.Model) // trimlowerlint:allow boundary canonicalization
			}
			s.enqueueMetadata(map[string]string{"result_id": resultID, "model": model})
			out := s.pending[0]
			s.pending = s.pending[1:]
			return out, nil
		case "response.output_text.delta":
			if !s.textOpen {
				s.textOpen = true
				s.textEnvID = canonical.EnvelopeID(fmt.Sprintf("%s:item:text_0", s.responseID))
				s.enqueueEnvelopeStart(s.textEnvID, s.responseID, canonical.EnvelopeStartPayload{
					Kind: canonical.EnvMessage,
					Role: canonical.ItemAuthorAssistant,
				}, canonical.EventMeta{NativeID: "text_0"})
			}
			s.enqueueTextDelta(s.textEnvID, frame.Delta)
			event := s.pending[0]
			s.pending = s.pending[1:]
			return event, nil
		case "response.function_call_arguments.delta":
			itemID := fallbackItemID(frame.ItemID, frame.CallID)
			if !s.startedTool[itemID] {
				s.startedTool[itemID] = true
				toolEnvID := canonical.EnvelopeID(fmt.Sprintf("%s:item:%s", s.responseID, itemID))
				s.toolEnvIDs[itemID] = toolEnvID
				s.enqueueEnvelopeStart(toolEnvID, s.responseID, canonical.EnvelopeStartPayload{
					Kind:      canonical.EnvToolCall,
					Name:      frame.Name,
					ToolUseID: frame.CallID,
				}, canonical.EventMeta{NativeID: itemID})
			}
			s.enqueueArgsDelta(s.toolEnvIDs[itemID], frame.Delta)
			event := s.pending[0]
			s.pending = s.pending[1:]
			return event, nil
		case "response.output_item.added":
			if strings.TrimSpace(frame.Item.Type) != "function_call" { // trimlowerlint:allow boundary canonicalization
				continue
			}
			itemID := fallbackItemID(frame.Item.ID, frame.Item.CallID)
			if s.startedTool[itemID] {
				continue
			}
			s.startedTool[itemID] = true
			toolEnvID := canonical.EnvelopeID(fmt.Sprintf("%s:item:%s", s.responseID, itemID))
			s.toolEnvIDs[itemID] = toolEnvID
			s.enqueueEnvelopeStart(toolEnvID, s.responseID, canonical.EnvelopeStartPayload{
				Kind:      canonical.EnvToolCall,
				Name:      frame.Item.Name,
				ToolUseID: frame.Item.CallID,
			}, canonical.EventMeta{NativeID: itemID})
			out := s.pending[0]
			s.pending = s.pending[1:]
			return out, nil
		case "response.function_call_arguments.done":
			itemID := fallbackItemID(frame.ItemID, frame.CallID)
			toolEnvID := s.toolEnvIDs[itemID]
			if toolEnvID != "" {
				s.enqueueEnvelopeEnd(toolEnvID, canonical.EnvToolCall, canonical.EnvelopeStatusCompleted)
			}
			delete(s.startedTool, itemID)
			delete(s.toolEnvIDs, itemID)
			event := s.pending[0]
			s.pending = s.pending[1:]
			return event, nil
		case "response.completed":
			s.completed = true
			status := strings.TrimSpace(frame.Status) // trimlowerlint:allow boundary canonicalization
			if status == "" {
				status = strings.TrimSpace(frame.Response.Status) // trimlowerlint:allow boundary canonicalization
			}
			s.closeOpenText(canonical.EnvelopeStatusCompleted)
			s.closeOpenTools(canonical.EnvelopeStatusCompleted)
			s.enqueueUsage(s.latestUsage)
			s.enqueueFinish(status)
			s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
			event := s.pending[0]
			s.pending = s.pending[1:]
			return event, nil
		case "error":
			return canonical.Event{}, canonical.InternalError("responses stream returned an error event")
		default:
			continue
		}
	}
}

func (s *canonicalEnvelopeStreamCloser) Close(context.Context) error {
	return s.reader.Close()
}

func (s *canonicalEnvelopeStreamCloser) nextSeq() int64 {
	s.seq++
	return s.seq
}

func (s *canonicalEnvelopeStreamCloser) enqueue(ev canonical.Event) {
	ev.ExchangeID = s.exchangeID
	ev.Seq = s.nextSeq()
	ev.Time = time.Now().UTC()
	s.pending = append(s.pending, ev)
}

func (s *canonicalEnvelopeStreamCloser) enqueueEnvelopeStart(id canonical.EnvelopeID, parent canonical.EnvelopeID, payload canonical.EnvelopeStartPayload, meta ...canonical.EventMeta) {
	ev := canonical.Event{Kind: canonical.EventEnvelopeStart, EnvID: id, ParentID: parent, Payload: payload}
	if len(meta) > 0 {
		ev.Meta = meta[0]
	}
	s.enqueue(ev)
}

func (s *canonicalEnvelopeStreamCloser) enqueueEnvelopeEnd(id canonical.EnvelopeID, kind canonical.EnvelopeKind, status canonical.EnvelopeStatus) {
	s.enqueue(canonical.Event{Kind: canonical.EventEnvelopeEnd, EnvID: id, Payload: canonical.EnvelopeEndPayload{Kind: kind, Status: status}})
}

func (s *canonicalEnvelopeStreamCloser) enqueueTextDelta(id canonical.EnvelopeID, text string) {
	s.enqueue(canonical.Event{Kind: canonical.EventTextDelta, EnvID: id, Payload: canonical.TextDeltaPayload{Text: text}})
}

func (s *canonicalEnvelopeStreamCloser) enqueueArgsDelta(id canonical.EnvelopeID, args string) {
	s.enqueue(canonical.Event{Kind: canonical.EventArgsDelta, EnvID: id, Payload: canonical.ArgsDeltaPayload{Args: args}})
}

func (s *canonicalEnvelopeStreamCloser) enqueueUsage(usage canonical.TokenUsage) {
	s.enqueue(canonical.Event{Kind: canonical.EventUsage, EnvID: s.responseID, Payload: canonical.UsagePayload{Usage: usage}})
}

func (s *canonicalEnvelopeStreamCloser) enqueueFinish(reason string) {
	s.enqueue(canonical.Event{Kind: canonical.EventFinish, EnvID: s.responseID, Payload: canonical.FinishPayload{Reason: reason}})
}

func (s *canonicalEnvelopeStreamCloser) enqueueMetadata(values map[string]string) {
	s.enqueue(canonical.Event{Kind: canonical.EventMetadata, EnvID: s.responseID, Payload: canonical.MetadataPayload{Values: values}})
}

func (s *canonicalEnvelopeStreamCloser) enqueueError(code string, message string) {
	s.enqueue(canonical.Event{Kind: canonical.EventError, EnvID: s.responseID, Payload: canonical.ErrorPayload{Code: code, Message: message}})
}

func (s *canonicalEnvelopeStreamCloser) closeOpenText(status canonical.EnvelopeStatus) {
	if s.textOpen {
		s.enqueueEnvelopeEnd(s.textEnvID, canonical.EnvMessage, status)
		s.textOpen = false
		s.textEnvID = ""
	}
}

func (s *canonicalEnvelopeStreamCloser) closeOpenTools(status canonical.EnvelopeStatus) {
	for itemID, envID := range s.toolEnvIDs {
		s.enqueueEnvelopeEnd(envID, canonical.EnvToolCall, status)
		delete(s.toolEnvIDs, itemID)
		delete(s.startedTool, itemID)
	}
}

func fallbackItemID(itemID string, callID string) string {
	if strings.TrimSpace(itemID) != "" { // trimlowerlint:allow boundary canonicalization
		return strings.TrimSpace(itemID) // trimlowerlint:allow boundary canonicalization
	}
	if strings.TrimSpace(callID) != "" { // trimlowerlint:allow boundary canonicalization
		return strings.TrimSpace(callID) // trimlowerlint:allow boundary canonicalization
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
		switch strings.TrimSpace(item.Type) { // trimlowerlint:allow boundary canonicalization
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
			if strings.TrimSpace(item.Arguments) != "" { // trimlowerlint:allow boundary canonicalization
				if err := json.Unmarshal([]byte(item.Arguments), &input); err != nil {
					return nil, canonical.InternalError("responses function_call arguments are invalid")
				}
			}
			itemID := fallbackItemID("", item.CallID)
			output = append(output, canonical.NewToolUseOutputItem(itemID, strings.TrimSpace(item.CallID), strings.TrimSpace(item.Name), input)) // trimlowerlint:allow boundary canonicalization
		case "reasoning":
			continue
		default:
			return nil, canonical.UnsupportedOperation("responses output item type is not implemented")
		}
	}
	if len(output) == 0 && strings.TrimSpace(outputText) != "" { // trimlowerlint:allow boundary canonicalization
		output = append(output, canonical.NewTextOutputItem("text_0", outputText))
	}
	return output, nil
}

func decodeMessageTextParts(raw json.RawMessage) ([]canonical.CanonicalItem, error) {
	var content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		return nil, canonical.InternalError("responses message content is invalid")
	}
	parts := make([]canonical.CanonicalItem, 0, len(content))
	for _, part := range content {
		partType := strings.TrimSpace(part.Type) // trimlowerlint:allow boundary canonicalization
		switch partType {
		case "text", "output_text", "input_text":
			parts = append(parts, canonical.NewTextItem(canonical.ItemAuthorAssistant, part.Text))
		default:
			return nil, canonical.UnsupportedOperation("responses message content part type is not implemented")
		}
	}
	return parts, nil
}
