package generatecontent

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func (e ResponseStreamEncoder) EncodeResponseStream(ctx context.Context, request canonical.CanonicalRequest, events canonical.ResponseStream, _ delivery.Delivery) (wire.ClientByteStreamResult, error) {
	completion, complete, fail := wire.NewResponseCompletion()
	state := &generateContentStreamEncoder{adapter: sse.NewEnvelopeEventAdapter(), request: request.Clone(), complete: complete}
	body := wire.NewEncodedResponseBody(ctx, events, state.EncodeEnvelopeEvent, completion, fail)
	return wire.ClientByteStreamResult{Stream: carrier.ByteStream{MediaType: "text/event-stream", Body: body}, Completion: completion}, nil
}

func (ResponseStreamEncoder) EncodeResponseMessages(context.Context, canonical.CanonicalRequest, canonical.ResponseStream, delivery.Delivery) (wire.ClientMessageResult, error) {
	return wire.ClientMessageResult{}, canonical.ClientUnsupportedDelivery("GenerateContent does not support message-oriented client delivery", "Use buffered or SSE HTTP delivery and retry")
}

type generateContentStreamEncoder struct {
	adapter  *sse.EnvelopeEventAdapter
	request  canonical.CanonicalRequest
	items    []canonical.CanonicalItem
	pending  map[uint32]canonical.CanonicalItem
	frames   map[uint32][][]byte
	nextItem uint32
	complete func(*historyfingerprint.Response, []compat.Change)
}

func (e *generateContentStreamEncoder) EncodeEnvelopeEvent(event canonical.Event) ([][]byte, error) {
	events, err := e.adapter.Translate(event)
	if err != nil {
		return nil, err
	}
	var frames [][]byte
	for _, event := range events {
		frame, err := e.encode(event)
		if err != nil {
			return nil, err
		}
		if frame != nil {
			frames = append(frames, frame)
		}
	}
	return frames, nil
}

func (e *generateContentStreamEncoder) encode(event sse.StreamEvent) ([]byte, error) {
	switch event.Kind {
	case sse.StreamEventTextDelta:
		if event.ItemOrdinal < e.nextItem {
			return nil, canonical.BadRequest("GenerateContent stream emitted text for a completed item ordinal")
		}
		if event.ItemOrdinal == e.nextItem && len(e.items) > 0 && e.items[len(e.items)-1].Kind() == canonical.ItemKindMessage {
			return nil, canonical.InternalError("GenerateContent cannot preserve consecutive streamed message boundaries")
		}
		text := event.TextDelta
		frame, err := marshalSSE(responseDTO{Candidates: []candidateDTO{{Content: responseContentDTO{Role: "model", Parts: []responsePartDTO{{Text: &text}}}, Index: 0}}})
		if err != nil {
			return nil, err
		}
		if e.frames == nil {
			e.frames = make(map[uint32][][]byte)
		}
		e.frames[event.ItemOrdinal] = append(e.frames[event.ItemOrdinal], frame)
		return e.drainReady()
	case sse.StreamEventItemCompleted:
		if event.CompletedItem == nil {
			return nil, nil
		}
		if e.pending == nil {
			e.pending = make(map[uint32]canonical.CanonicalItem)
		}
		if event.ItemOrdinal < e.nextItem {
			return nil, canonical.BadRequest("GenerateContent stream completed an item ordinal more than once")
		}
		if _, exists := e.pending[event.ItemOrdinal]; exists {
			return nil, canonical.BadRequest("GenerateContent stream completed an item ordinal more than once")
		}
		e.pending[event.ItemOrdinal] = event.CompletedItem.Clone()
		return e.drainReady()
	case sse.StreamEventCompleted:
		if len(e.pending) != 0 || len(e.frames) != 0 {
			return nil, canonical.BadRequest("GenerateContent stream completed before all item ordinals were contiguous")
		}
		finish, err := finishReason(event.Completion)
		if err != nil {
			return nil, err
		}
		usage, usageChanges := projectUsage(event.Usage)
		frame, err := marshalSSE(responseDTO{Candidates: []candidateDTO{{Content: responseContentDTO{Role: "model", Parts: []responsePartDTO{}}, FinishReason: finish, Index: 0}}, UsageMetadata: usage})
		if err == nil {
			content, changes, projectionErr := projectResponseHistory(e.request, e.items)
			if projectionErr != nil {
				return nil, projectionErr
			}
			fingerprint, fingerprintErr := fingerprintContentsResponse(content)
			if fingerprintErr != nil {
				return nil, fingerprintErr
			}
			e.complete(&fingerprint, appendChanges(changes, usageChanges))
		}
		return frame, err
	case sse.StreamEventFailed:
		return nil, canonical.InternalError(event.ErrorMessage)
	default:
		return nil, nil
	}
}

func (e *generateContentStreamEncoder) drainReady() ([]byte, error) {
	var frames [][]byte
	for {
		if queued := e.frames[e.nextItem]; len(queued) > 0 {
			if len(e.items) > 0 && e.items[len(e.items)-1].Kind() == canonical.ItemKindMessage {
				return nil, canonical.InternalError("GenerateContent cannot preserve consecutive streamed message boundaries")
			}
			frames = append(frames, queued...)
			delete(e.frames, e.nextItem)
		}
		item, ready := e.pending[e.nextItem]
		if !ready {
			break
		}
		delete(e.pending, e.nextItem)
		frame, err := e.emitCompletedItem(item)
		if err != nil {
			return nil, err
		}
		e.items = append(e.items, item.Clone())
		e.nextItem++
		if frame != nil {
			frames = append(frames, frame)
		}
	}
	if len(frames) == 0 {
		return nil, nil
	}
	return bytes.Join(frames, nil), nil
}

func (e *generateContentStreamEncoder) emitCompletedItem(item canonical.CanonicalItem) ([]byte, error) {
	if item.Kind() == canonical.ItemKindMessage {
		message, _ := item.Message()
		for _, part := range message.Content() {
			if _, ok := part.Text(); !ok {
				return nil, canonical.InternalError("GenerateContent streaming supports completed text messages only")
			}
		}
		return nil, nil
	}
	content, _, err := projectResponseHistory(e.request, []canonical.CanonicalItem{item})
	if err != nil {
		return nil, err
	}
	parts := make([]responsePartDTO, 0, len(content.Parts))
	for _, part := range content.Parts {
		projected := responsePartDTO{Text: part.Text, Thought: part.Thought, InlineData: part.InlineData}
		if part.FunctionCall != nil {
			projected.FunctionCall = &responseFunctionCallDTO{ID: part.FunctionCall.ID, Name: part.FunctionCall.Name, Args: part.FunctionCall.Args}
		}
		parts = append(parts, projected)
	}
	if len(parts) == 0 {
		return nil, nil
	}
	return marshalSSE(responseDTO{Candidates: []candidateDTO{{Content: responseContentDTO{Role: "model", Parts: parts}, Index: 0}}})
}

func marshalSSE(value responseDTO) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return sse.SSEData(raw), nil
}
