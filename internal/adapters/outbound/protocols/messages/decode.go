package messages

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/protocols"
	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

type bufferedResponseBody struct {
	ID         string                     `json:"id"`
	Model      string                     `json:"model"`
	Content    []bufferedContentBlockBody `json:"content"`
	StopReason string                     `json:"stop_reason"`
}

type bufferedContentBlockBody struct {
	Type      string         `json:"type"`
	Text      string         `json:"text"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Input     map[string]any `json:"input"`
	ToolUseID string         `json:"tool_use_id"`
}

var tokenUsagePathSpec = protocols.TokenUsagePathSpec{
	InputPaths: [][]string{
		{"usage", "input_tokens"},
		{"usage", "prompt_tokens"},
		{"usageMetadata", "promptTokenCount"},
		{"usage", "inputTokens"},
	},
	OutputPaths: [][]string{
		{"usage", "output_tokens"},
		{"usage", "completion_tokens"},
		{"usageMetadata", "candidatesTokenCount"},
		{"usage", "outputTokens"},
	},
	CacheReadPaths: [][]string{
		{"usage", "cache_read_input_tokens"},
		{"usage", "input_tokens_details", "cached_tokens"},
		{"usage", "prompt_tokens_details", "cached_tokens"},
		{"usageMetadata", "cachedContentTokenCount"},
		{"usage", "cacheReadInputTokens"},
	},
	CacheWritePaths: [][]string{
		{"usage", "cache_creation_input_tokens"},
		{"usage", "input_tokens_details", "cache_write_tokens"},
		{"usage", "prompt_tokens_details", "cache_write_tokens"},
		{"usage", "cacheWriteInputTokens"},
	},
}

func DecodeBufferedResult(raw []byte) (compatibility.CanonicalOutputValue, error) {
	var dto bufferedResponseBody
	if err := json.Unmarshal(raw, &dto); err != nil {
		return compatibility.CanonicalOutputValue{}, compatibility.InternalError("messages response is invalid JSON")
	}
	items := make([]compatibility.CanonicalItem, 0, len(dto.Content))
	for i, block := range dto.Content {
		switch strings.TrimSpace(block.Type) {
		case "text":
			items = append(items, compatibility.NewTextOutputItem("text_"+strconv.Itoa(i), block.Text))
		case "tool_use":
			itemID := strings.TrimSpace(block.ID)
			if itemID == "" {
				itemID = "tool_" + strconv.Itoa(i)
			}
			items = append(items, compatibility.NewToolUseOutputItem(itemID, strings.TrimSpace(block.ID), strings.TrimSpace(block.Name), cloneInput(block.Input)))
		default:
			return compatibility.CanonicalOutputValue{}, compatibility.InternalError("messages response content block is unsupported")
		}
	}
	return compatibility.NewConversationOutputWithUsage(
		dto.ID,
		dto.Model,
		items,
		dto.StopReason,
		protocols.ExtractTokenUsage(raw, tokenUsagePathSpec),
	), nil
}

func DecodeStream(body io.ReadCloser) compatibility.CanonicalOutputEventStream {
	return newStreamDecoder(body)
}

type canonicalOutputEventStreamCloser struct {
	reader       *protocols.SSEReaderCloser
	resultID     string
	model        string
	finishReason string
	started      bool
	pending      []compatibility.OutputEvent
	blocks       map[int]streamContentBlock
	latestUsage  compatibility.TokenUsage
}

type streamContentBlock struct {
	ItemID    string
	ItemKind  compatibility.ItemKind
	ToolUseID string
	Name      string
}

type streamEnvelope struct {
	Type string `json:"type"`
}

type messageStartFrame struct {
	Message struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	} `json:"message"`
}

type contentBlockStartFrame struct {
	Index        int `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
}

type contentBlockDeltaFrame struct {
	Index int `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
	} `json:"delta"`
}

type contentBlockStopFrame struct {
	Index int `json:"index"`
}

type messageDeltaFrame struct {
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
}

func newStreamDecoder(body io.ReadCloser) *canonicalOutputEventStreamCloser {
	return &canonicalOutputEventStreamCloser{
		reader:      protocols.NewSSEReader(body),
		blocks:      map[int]streamContentBlock{},
		latestUsage: compatibility.NewUnknownTokenUsage(),
	}
}

func (s *canonicalOutputEventStreamCloser) Next() (compatibility.OutputEvent, error) {
	if len(s.pending) > 0 {
		return s.shift(), nil
	}
	for {
		frame, err := s.reader.Next()
		if err != nil {
			return compatibility.OutputEvent{}, err
		}
		if strings.TrimSpace(frame.Data) == "" || frame.Event == "ping" {
			continue
		}
		frameUsage := protocols.ExtractTokenUsage([]byte(frame.Data), tokenUsagePathSpec)
		if !frameUsage.IsZero() {
			s.latestUsage = frameUsage
		}
		var envelope streamEnvelope
		if err := json.Unmarshal([]byte(frame.Data), &envelope); err != nil {
			return compatibility.OutputEvent{}, compatibility.InternalError("messages stream frame is invalid JSON")
		}
		if err := s.handleFrame(envelope.Type, frame.Data); err != nil {
			return compatibility.OutputEvent{}, err
		}
		if len(s.pending) > 0 {
			return s.shift(), nil
		}
	}
}

func (s *canonicalOutputEventStreamCloser) handleFrame(frameType string, raw string) error {
	switch strings.TrimSpace(frameType) {
	case "message_start":
		return s.handleMessageStart(raw)
	case "content_block_start":
		return s.handleContentBlockStart(raw)
	case "content_block_delta":
		return s.handleContentBlockDelta(raw)
	case "content_block_stop":
		return s.handleContentBlockStop(raw)
	case "message_delta":
		return s.handleMessageDelta(raw)
	case "message_stop":
		s.handleMessageStop()
		return nil
	case "ping":
		return nil
	default:
		return compatibility.InternalError("messages stream frame type is unsupported")
	}
}

func (s *canonicalOutputEventStreamCloser) handleMessageStart(raw string) error {
	var payload messageStartFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return compatibility.InternalError("messages stream message_start frame is invalid")
	}
	s.started = true
	s.resultID = payload.Message.ID
	s.model = payload.Message.Model
	s.pending = append(s.pending, compatibility.OutputEvent{
		Kind:     compatibility.OutputEventStarted,
		ResultID: s.resultID,
		Model:    s.model,
	})
	return nil
}

func (s *canonicalOutputEventStreamCloser) handleContentBlockStart(raw string) error {
	var payload contentBlockStartFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return compatibility.InternalError("messages stream content_block_start frame is invalid")
	}
	block := streamContentBlock{ItemID: "block_" + strconv.Itoa(payload.Index)}
	switch strings.TrimSpace(payload.ContentBlock.Type) {
	case "text":
		block.ItemKind = compatibility.ItemKindText
		block.ItemID = "text_" + strconv.Itoa(payload.Index)
		s.pending = append(s.pending, compatibility.OutputEvent{
			Kind:     compatibility.OutputEventItemStarted,
			ItemKind: compatibility.ItemKindText,
			ItemID:   block.ItemID,
			ResultID: s.resultID,
			Model:    s.model,
		})
	case "tool_use":
		block.ItemKind = compatibility.ItemKindToolUse
		block.ToolUseID = strings.TrimSpace(payload.ContentBlock.ID)
		if block.ToolUseID == "" {
			block.ToolUseID = "toolu_swobu_" + strconv.Itoa(payload.Index)
		}
		block.Name = strings.TrimSpace(payload.ContentBlock.Name)
		block.ItemID = block.ToolUseID
		s.pending = append(s.pending, compatibility.OutputEvent{
			Kind:      compatibility.OutputEventItemStarted,
			ItemKind:  compatibility.ItemKindToolUse,
			ItemID:    block.ItemID,
			ToolUseID: block.ToolUseID,
			Name:      block.Name,
			ResultID:  s.resultID,
			Model:     s.model,
		})
	default:
		return compatibility.InternalError("messages stream content block type is unsupported")
	}
	s.blocks[payload.Index] = block
	return nil
}

func (s *canonicalOutputEventStreamCloser) handleContentBlockDelta(raw string) error {
	var payload contentBlockDeltaFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return compatibility.InternalError("messages stream content_block_delta frame is invalid")
	}
	block, ok := s.blocks[payload.Index]
	if !ok {
		return nil
	}
	switch strings.TrimSpace(payload.Delta.Type) {
	case "text_delta":
		s.pending = append(s.pending, compatibility.OutputEvent{
			Kind:      compatibility.OutputEventTextDelta,
			ItemID:    block.ItemID,
			TextDelta: payload.Delta.Text,
			ResultID:  s.resultID,
			Model:     s.model,
		})
	case "input_json_delta":
		s.pending = append(s.pending, compatibility.OutputEvent{
			Kind:           compatibility.OutputEventToolUseArgumentsDelta,
			ItemID:         block.ItemID,
			ToolUseID:      block.ToolUseID,
			Name:           block.Name,
			ArgumentsDelta: payload.Delta.PartialJSON,
			ResultID:       s.resultID,
			Model:          s.model,
		})
	default:
		return compatibility.InternalError("messages stream delta type is unsupported")
	}
	return nil
}

func (s *canonicalOutputEventStreamCloser) handleContentBlockStop(raw string) error {
	var payload contentBlockStopFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return compatibility.InternalError("messages stream content_block_stop frame is invalid")
	}
	block, ok := s.blocks[payload.Index]
	if !ok {
		return nil
	}
	s.pending = append(s.pending, compatibility.OutputEvent{
		Kind:      compatibility.OutputEventItemCompleted,
		ItemKind:  block.ItemKind,
		ItemID:    block.ItemID,
		ToolUseID: block.ToolUseID,
		Name:      block.Name,
		ResultID:  s.resultID,
		Model:     s.model,
	})
	delete(s.blocks, payload.Index)
	return nil
}

func (s *canonicalOutputEventStreamCloser) handleMessageDelta(raw string) error {
	var payload messageDeltaFrame
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return compatibility.InternalError("messages stream message_delta frame is invalid")
	}
	s.finishReason = strings.TrimSpace(payload.Delta.StopReason)
	return nil
}

func (s *canonicalOutputEventStreamCloser) handleMessageStop() {
	finishReason := s.finishReason
	if finishReason == "" {
		finishReason = "completed"
	}
	s.pending = append(s.pending, compatibility.OutputEvent{
		Kind:         compatibility.OutputEventCompleted,
		ResultID:     s.resultID,
		Model:        s.model,
		FinishReason: finishReason,
		Usage:        s.latestUsage,
	})
}

func (s *canonicalOutputEventStreamCloser) Close() error {
	return s.reader.Close()
}

func (s *canonicalOutputEventStreamCloser) shift() compatibility.OutputEvent {
	event := s.pending[0]
	s.pending = s.pending[1:]
	return event
}
