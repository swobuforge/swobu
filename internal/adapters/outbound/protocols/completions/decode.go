package completions

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

func DecodeResponseBuffered(raw []byte) (canonical.CanonicalOutputValue, error) {
	var dto responseBody
	if err := json.Unmarshal(raw, &dto); err != nil {
		return canonical.CanonicalOutputValue{}, canonical.InternalError("completions response is invalid JSON")
	}
	if len(dto.Choices) == 0 {
		return canonical.CanonicalOutputValue{}, canonical.InternalError("completions response is missing choices")
	}
	choice := dto.Choices[0]
	return canonical.NewPromptOutputWithUsage(
		dto.ID,
		dto.Model,
		[]canonical.OutputItem{canonical.NewTextOutputItem("text_0", choice.Text)},
		choice.FinishReason,
		protocols.ExtractTokenUsage(raw, tokenUsagePathSpec),
	), nil
}

// DecodeResponseStream returns canonical envelope events directly for completions streams.
func DecodeResponseStream(body io.ReadCloser, exchangeID string) canonical.EventReader {
	return &completionsEventReader{
		exchangeID: exchangeID,
		responseID: canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID)),
		reader:     protocols.NewSSEReader(body),
	}
}

type completionsEventReader struct {
	exchangeID string
	responseID canonical.EnvelopeID
	reader     *protocols.SSEReaderCloser
	started    bool
	textOpen   bool
	textEnvID  canonical.EnvelopeID
	pending    []canonical.Event
	resultID   string
	model      string
	completed  bool
	usage      canonical.TokenUsage
	seq        int64
}

// variants while maintaining canonical output ordering.
func (s *completionsEventReader) Next(context.Context) (canonical.Event, error) {
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
				s.closeOpenTextWithStatus(canonical.EnvelopeStatusError)
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
		rawChunk := []byte(event.Data)
		chunkUsage := protocols.ExtractTokenUsage(rawChunk, tokenUsagePathSpec)
		if !chunkUsage.IsZero() {
			s.usage = chunkUsage
		}
		var chunk responseBody
		if err := json.Unmarshal(rawChunk, &chunk); err != nil {
			return canonical.Event{}, canonical.InternalError("completions stream chunk is invalid JSON")
		}
		if !s.started {
			s.started = true
			s.resultID = chunk.ID
			s.model = chunk.Model
			s.enqueueEnvelopeStart(s.responseID, "", canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse})
			s.enqueueMetadata(map[string]string{"result_id": chunk.ID, "model": chunk.Model})
			s.usage = canonical.NewUnknownTokenUsage()
			if !chunkUsage.IsZero() {
				s.usage = chunkUsage
			}
			out := s.pending[0]
			s.pending = s.pending[1:]
			return out, nil
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.Text != "" {
			if !s.textOpen {
				s.textOpen = true
				s.textEnvID = canonical.EnvelopeID(fmt.Sprintf("%s:item:text_0", s.responseID))
				s.enqueueEnvelopeStart(s.textEnvID, s.responseID, canonical.EnvelopeStartPayload{
					Kind: canonical.EnvMessage,
					Role: canonical.ItemAuthorAssistant,
				}, canonical.EventMetadataFields{NativeID: "text_0"})
			}
			s.enqueueTextDelta(s.textEnvID, choice.Text)
			event := s.pending[0]
			s.pending = s.pending[1:]
			return event, nil
		}
		if strings.TrimSpace(choice.FinishReason) != "" && !s.completed { // swobu:io-string source=boundary
			s.completed = true
			s.closeOpenTextWithStatus(canonical.EnvelopeStatusCompleted)
			s.enqueueUsage(s.usage)
			s.enqueueFinish(choice.FinishReason)
			s.enqueueEnvelopeEnd(s.responseID, canonical.EnvResponse, canonical.EnvelopeStatusCompleted)
			event := s.pending[0]
			s.pending = s.pending[1:]
			return event, nil
		}
	}
}

func (s *completionsEventReader) Close(context.Context) error {
	return s.reader.Close()
}

func (s *completionsEventReader) nextSeq() int64 {
	s.seq++
	return s.seq
}

func (s *completionsEventReader) enqueue(ev canonical.Event) {
	ev.ExchangeID = s.exchangeID
	ev.Seq = s.nextSeq()
	ev.Time = time.Now().UTC()
	s.pending = append(s.pending, ev)
}

func (s *completionsEventReader) enqueueEnvelopeStart(id canonical.EnvelopeID, parent canonical.EnvelopeID, payload canonical.EnvelopeStartPayload, meta ...canonical.EventMetadataFields) {
	ev := canonical.Event{
		Kind:     canonical.EventEnvelopeStart,
		EnvID:    id,
		ParentID: parent,
		Payload:  payload,
	}
	if len(meta) > 0 {
		ev.Meta = meta[0]
	}
	s.enqueue(ev)
}

func (s *completionsEventReader) enqueueEnvelopeEnd(id canonical.EnvelopeID, kind canonical.EnvelopeKind, status canonical.EnvelopeStatus) {
	s.enqueue(canonical.Event{
		Kind:  canonical.EventEnvelopeEnd,
		EnvID: id,
		Payload: canonical.EnvelopeEndPayload{
			Kind:   kind,
			Status: status,
		},
	})
}

func (s *completionsEventReader) enqueueTextDelta(id canonical.EnvelopeID, text string) {
	s.enqueue(canonical.Event{
		Kind:    canonical.EventTextDelta,
		EnvID:   id,
		Payload: canonical.TextDeltaPayload{Text: text},
	})
}

func (s *completionsEventReader) enqueueUsage(usage canonical.TokenUsage) {
	s.enqueue(canonical.Event{
		Kind:    canonical.EventUsage,
		EnvID:   s.responseID,
		Payload: canonical.UsagePayload{Usage: usage},
	})
}

func (s *completionsEventReader) enqueueFinish(reason string) {
	s.enqueue(canonical.Event{
		Kind:    canonical.EventFinish,
		EnvID:   s.responseID,
		Payload: canonical.FinishPayload{Reason: reason},
	})
}

func (s *completionsEventReader) enqueueMetadata(values map[string]string) {
	s.enqueue(canonical.Event{
		Kind:    canonical.EventMetadata,
		EnvID:   s.responseID,
		Payload: canonical.MetadataPayload{Values: values},
	})
}

func (s *completionsEventReader) enqueueError(code string, message string) {
	s.enqueue(canonical.Event{
		Kind:  canonical.EventError,
		EnvID: s.responseID,
		Payload: canonical.ErrorPayload{
			Code:    code,
			Message: message,
		},
	})
}

func (s *completionsEventReader) closeOpenTextWithStatus(status canonical.EnvelopeStatus) {
	if s.textOpen {
		s.enqueueEnvelopeEnd(s.textEnvID, canonical.EnvMessage, status)
		s.textOpen = false
		s.textEnvID = ""
	}
}
