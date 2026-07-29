package responses

import "github.com/swobuforge/swobu/internal/domain/canonical"

func (s *responsesResponseStream) enqueueCompletedOutputItems(outputIndex *int, items []canonical.CanonicalItem) error {
	for index, item := range items {
		if err := s.enqueueCompletedOutputItemAt(outputIndex, uint32(index), item); err != nil {
			return err
		}
	}
	return nil
}

func (s *responsesResponseStream) enqueueCompletedOutputItemAt(outputIndex *int, ordinal uint32, item canonical.CanonicalItem) error {
	switch item.Kind() {
	case canonical.ItemKindMessage:
		message, _ := item.Message()
		start, err := canonical.NewMessageStart(message.Role())
		if err != nil {
			return err
		}
		s.enqueueItemStart(outputIndex, ordinal, start)
		for partIndex, part := range message.Content() {
			if text, ok := part.Text(); ok {
				s.enqueueContentStart(outputIndex, ordinal, uint32(partIndex))
				s.enqueueTextDelta(outputIndex, ordinal, uint32(partIndex), text.Text())
			}
		}
		s.enqueueItemCompleted(outputIndex, ordinal, item)
	case canonical.ItemKindToolCall:
		call, _ := item.ToolCall()
		start, err := canonical.NewToolCallStart(call.CallID(), call.Tool())
		if err != nil {
			return err
		}
		s.enqueueItemStart(outputIndex, ordinal, start)
		if object, ok := call.Input().Object(); ok {
			s.enqueueArgsDelta(outputIndex, ordinal, object.String())
		} else if text, ok := call.Input().Text(); ok {
			s.enqueueArgsDelta(outputIndex, ordinal, text)
		}
		s.enqueueItemCompleted(outputIndex, ordinal, item)
	case canonical.ItemKindReasoning:
		s.enqueueItemCompleted(outputIndex, ordinal, item)
	case canonical.ItemKindToolDiscoveryResult:
		s.enqueueItemCompleted(outputIndex, ordinal, item)
	case canonical.ItemKindToolResult:
		if result, ok := item.ToolResult(); !ok {
			return canonical.InternalError("responses completed tool result is invalid")
		} else if _, search := result.WebSearch(); !search {
			return canonical.InternalError("canonical Responses output contains a request-only content tool result")
		}
		s.enqueueItemCompleted(outputIndex, ordinal, item)
	default:
		return canonical.InternalError("Responses output received a request-only canonical item kind")
	}
	return nil
}
