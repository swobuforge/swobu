package chatcompletions

import (
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func (s *chatCompletionsEventReader) textOrdinal() uint32 {
	return 0
}

func (s *chatCompletionsEventReader) toolOrdinal(index int) uint32 {
	ordinal := uint32(index)
	if s.textEnvID != "" {
		ordinal++
	}
	return ordinal
}

func (s *chatCompletionsEventReader) nextSeq() int64 { s.seq++; return s.seq }

func (s *chatCompletionsEventReader) enqueue(ev canonical.Event) {
	ev.ExchangeID, ev.Seq, ev.Time = s.exchangeID, s.nextSeq(), time.Now().UTC()
	s.pending = append(s.pending, ev)
}

func (s *chatCompletionsEventReader) enqueueItem(kind canonical.EventKind, envID canonical.EnvelopeID, ordinal uint32, payload canonical.ItemEventPayload) {
	s.enqueue(canonical.Event{Kind: kind, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: ordinal}, Payload: payload}})
}

func (s *chatCompletionsEventReader) enqueueItemPart(kind canonical.EventKind, envID canonical.EnvelopeID, item, part uint32, payload canonical.ItemEventPayload) {
	s.enqueue(canonical.Event{Kind: kind, EnvID: envID, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: item, Part: part}, Payload: payload}})
}

func (s *chatCompletionsEventReader) enqueueEnvelopeStart(id canonical.EnvelopeID, parent canonical.EnvelopeID, payload canonical.EnvelopeStartPayload, meta ...canonical.EventMetadataFields) {
	ev := canonical.Event{Kind: canonical.EventEnvelopeStart, EnvID: id, ParentID: parent, Payload: payload}
	if len(meta) > 0 {
		ev.Meta = meta[0]
	}
	s.enqueue(ev)
}

func (s *chatCompletionsEventReader) enqueueEnvelopeEnd(id canonical.EnvelopeID, kind canonical.EnvelopeKind, status canonical.EnvelopeStatus) {
	s.enqueue(canonical.Event{Kind: canonical.EventEnvelopeEnd, EnvID: id, Payload: canonical.EnvelopeEndPayload{Kind: kind, Status: status}})
}

func (s *chatCompletionsEventReader) enqueueError(code string, message string) {
	s.enqueue(canonical.Event{Kind: canonical.EventError, EnvID: s.responseID, Payload: canonical.ErrorPayload{Code: code, Message: message}})
}

func (s *chatCompletionsEventReader) closeOpenChildren(canonical.EnvelopeStatus) {
	s.textOpen = false
	for index := range s.toolCalls {
		delete(s.toolCalls, index)
	}
}
