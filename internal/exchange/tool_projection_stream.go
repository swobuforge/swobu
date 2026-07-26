package exchange

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

type canonicalToolProjectionStream struct {
	upstream canonical.ResponseStream
	table    provider.ToolProjectionTable
}

func newCanonicalToolProjectionStream(upstream canonical.ResponseStream, table provider.ToolProjectionTable) canonical.ResponseStream {
	return &canonicalToolProjectionStream{upstream: upstream, table: table}
}

func (s *canonicalToolProjectionStream) Next(ctx context.Context) (canonical.Event, error) {
	event, err := s.upstream.Next(ctx)
	if err != nil {
		return canonical.Event{}, err
	}
	itemEvent, ok := event.Payload.(canonical.ItemEvent)
	if !ok {
		return event, nil
	}
	switch payload := itemEvent.Payload.(type) {
	case canonical.ItemStartPayload:
		start, ok := payload.ToolCall()
		if !ok {
			return event, nil
		}
		original, ok := s.table.OriginalKey(start.Tool)
		if !ok {
			if fixedCanonicalToolKey(start.Tool) {
				return event, nil
			}
			return canonical.Event{}, canonical.InternalError("provider tool call has no attempted projection")
		}
		projected, err := canonical.NewToolCallStart(start.CallID, original)
		if err != nil {
			return canonical.Event{}, err
		}
		itemEvent.Payload = projected
	case canonical.ItemCompletedPayload:
		call, ok := payload.Item.ToolCall()
		if !ok {
			return event, nil
		}
		original, ok := s.table.OriginalKey(call.Tool())
		if !ok {
			if fixedCanonicalToolKey(call.Tool()) {
				return event, nil
			}
			return canonical.Event{}, canonical.InternalError("completed provider tool call has no attempted projection")
		}
		item, rebuildErr := canonical.NewToolCallItem(call.CallID(), original, call.Input())
		if rebuildErr != nil {
			return canonical.Event{}, rebuildErr
		}
		itemEvent.Payload = canonical.ItemCompletedPayload{Item: item}
	}
	event.Payload = itemEvent
	return event, nil
}

func fixedCanonicalToolKey(key canonical.ToolKey) bool {
	return key.Kind() == canonical.ToolKindWebSearch || key.Kind() == canonical.ToolKindDiscovery
}

func (s *canonicalToolProjectionStream) Close(ctx context.Context) error {
	return s.upstream.Close(ctx)
}
