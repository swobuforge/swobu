package responses

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func (s *responsesResponseStream) enqueueCompletedOutputItems(items []canonical.CanonicalItem) error {
	for _, item := range items {
		ordinal := s.nextOrdinal
		s.nextOrdinal++
		if err := s.enqueueCompletedOutputItemAt(ordinal, item); err != nil {
			return err
		}
	}
	return nil
}

func (s *responsesResponseStream) enqueueCompletedOutputItemAt(ordinal uint32, item canonical.CanonicalItem) error {
	if ordinal >= s.nextOrdinal {
		s.nextOrdinal = ordinal + 1
	}
	envID := canonical.EnvelopeID(fmt.Sprintf("%s:item:%d", s.responseEnvID, ordinal))
	switch item.Kind() {
	case canonical.ItemKindMessage:
		message, _ := item.Message()
		start, err := canonical.NewMessageStart(message.Role())
		if err != nil {
			return err
		}
		s.enqueueItemStart(envID, ordinal, start)
		for _, part := range message.Content() {
			if text, ok := part.Text(); ok {
				s.enqueue(canonical.Event{Kind: canonical.EventContentStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: ordinal}, Payload: canonical.NewMessageContentStart(canonical.PartKindText)}})
				s.enqueueTextDelta(envID, ordinal, text.Text())
			}
		}
		s.enqueueItemCompleted(envID, ordinal, item)
	case canonical.ItemKindToolCall:
		call, _ := item.ToolCall()
		start, err := canonical.NewToolCallStart(call.CallID(), call.Tool())
		if err != nil {
			return err
		}
		s.enqueueItemStart(envID, ordinal, start)
		if object, ok := call.Input().Object(); ok {
			s.enqueueArgsDelta(envID, ordinal, object.String())
		} else if text, ok := call.Input().Text(); ok {
			s.enqueueArgsDelta(envID, ordinal, text)
		}
		s.enqueueItemCompleted(envID, ordinal, item)
	case canonical.ItemKindReasoning:
		s.enqueueItemCompleted(envID, ordinal, item)
	case canonical.ItemKindToolResult:
		if result, ok := item.ToolResult(); !ok {
			return canonical.InternalError("responses completed tool result is invalid")
		} else if _, search := result.WebSearch(); !search {
			return canonical.InternalError("canonical Responses output contains a request-only content tool result")
		}
		s.enqueueItemCompleted(envID, ordinal, item)
	default:
		return canonical.NotImplemented("Swobu cannot project this canonical completed item kind to Responses output")
	}
	return nil
}
