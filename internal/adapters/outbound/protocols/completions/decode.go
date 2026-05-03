package completions

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/protocols"
	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

type responseBody struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Text         string `json:"text"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

var tokenUsagePathSpec = protocols.TokenUsagePathSpec{
	InputPaths: [][]string{
		{"usage", "prompt_tokens"},
		{"usage", "input_tokens"},
		{"usageMetadata", "promptTokenCount"},
		{"usage", "inputTokens"},
	},
	OutputPaths: [][]string{
		{"usage", "completion_tokens"},
		{"usage", "output_tokens"},
		{"usageMetadata", "candidatesTokenCount"},
		{"usage", "outputTokens"},
	},
	CacheReadPaths: [][]string{
		{"usage", "prompt_tokens_details", "cached_tokens"},
		{"usage", "input_tokens_details", "cached_tokens"},
		{"usage", "cache_read_input_tokens"},
		{"usageMetadata", "cachedContentTokenCount"},
		{"usage", "cacheReadInputTokens"},
	},
	CacheWritePaths: [][]string{
		{"usage", "prompt_tokens_details", "cache_write_tokens"},
		{"usage", "input_tokens_details", "cache_write_tokens"},
		{"usage", "cache_creation_input_tokens"},
		{"usage", "cacheWriteInputTokens"},
	},
}

func DecodeBufferedResult(raw []byte) (compatibility.CanonicalOutputValue, error) {
	var dto responseBody
	if err := json.Unmarshal(raw, &dto); err != nil {
		return compatibility.CanonicalOutputValue{}, compatibility.InternalError("completions response is invalid JSON")
	}
	if len(dto.Choices) == 0 {
		return compatibility.CanonicalOutputValue{}, compatibility.InternalError("completions response is missing choices")
	}
	choice := dto.Choices[0]
	return compatibility.NewPromptOutputWithUsage(
		dto.ID,
		dto.Model,
		[]compatibility.OutputItem{compatibility.NewTextOutputItem("text_0", choice.Text)},
		choice.FinishReason,
		protocols.ExtractTokenUsage(raw, tokenUsagePathSpec),
	), nil
}

func DecodeStream(body io.ReadCloser) compatibility.CanonicalOutputEventStream {
	return &canonicalOutputEventStreamCloser{reader: protocols.NewSSEReader(body)}
}

type canonicalOutputEventStreamCloser struct {
	reader    *protocols.SSEReaderCloser
	started   bool
	textOpen  bool
	pending   []compatibility.OutputEvent
	resultID  string
	model     string
	completed bool
	usage     compatibility.TokenUsage
}

// variants while maintaining canonical output ordering.
func (s *canonicalOutputEventStreamCloser) Next() (compatibility.OutputEvent, error) {
	if len(s.pending) > 0 {
		event := s.pending[0]
		s.pending = s.pending[1:]
		return event, nil
	}
	for {
		event, err := s.reader.Next()
		if err != nil {
			return compatibility.OutputEvent{}, err
		}
		if strings.TrimSpace(event.Data) == "[DONE]" {
			continue
		}
		rawChunk := []byte(event.Data)
		chunkUsage := protocols.ExtractTokenUsage(rawChunk, tokenUsagePathSpec)
		if !chunkUsage.IsZero() {
			s.usage = chunkUsage
		}
		var chunk responseBody
		if err := json.Unmarshal(rawChunk, &chunk); err != nil {
			return compatibility.OutputEvent{}, compatibility.InternalError("completions stream chunk is invalid JSON")
		}
		if !s.started {
			s.started = true
			s.resultID = chunk.ID
			s.model = chunk.Model
			s.usage = compatibility.NewUnknownTokenUsage()
			if !chunkUsage.IsZero() {
				s.usage = chunkUsage
			}
			return compatibility.OutputEvent{
				Kind:     compatibility.OutputEventStarted,
				ResultID: chunk.ID,
				Model:    chunk.Model,
			}, nil
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.Text != "" {
			if !s.textOpen {
				s.textOpen = true
				s.pending = append(s.pending, compatibility.OutputEvent{
					Kind:     compatibility.OutputEventItemStarted,
					ItemKind: compatibility.OutputItemText,
					ItemID:   "text_0",
					ResultID: s.resultID,
					Model:    s.model,
				})
			}
			s.pending = append(s.pending, compatibility.OutputEvent{
				Kind:      compatibility.OutputEventTextDelta,
				ResultID:  s.resultID,
				Model:     s.model,
				ItemID:    "text_0",
				TextDelta: choice.Text,
			})
			event := s.pending[0]
			s.pending = s.pending[1:]
			return event, nil
		}
		if strings.TrimSpace(choice.FinishReason) != "" && !s.completed {
			s.completed = true
			if s.textOpen {
				s.pending = append(s.pending, compatibility.OutputEvent{
					Kind:     compatibility.OutputEventItemCompleted,
					ItemKind: compatibility.OutputItemText,
					ItemID:   "text_0",
					ResultID: s.resultID,
					Model:    s.model,
				})
				s.textOpen = false
			}
			s.pending = append(s.pending, compatibility.OutputEvent{
				Kind:         compatibility.OutputEventCompleted,
				ResultID:     s.resultID,
				Model:        s.model,
				FinishReason: choice.FinishReason,
				Usage:        s.usage,
			})
			event := s.pending[0]
			s.pending = s.pending[1:]
			return event, nil
		}
	}
}

func (s *canonicalOutputEventStreamCloser) Close() error {
	return s.reader.Close()
}
