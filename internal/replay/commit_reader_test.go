package replay

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

const testReplaySemanticLimit = int64(40 << 20)

func successfulResponseEvents(t *testing.T, nativeID string) []canonical.Event {
	t.Helper()
	item := mustMessageItem(canonical.MessageRoleAssistant, "hello")
	response := canonical.ResponseRef{}
	if nativeID != "" {
		response.Responses = &canonical.ResponsesNativeRef{ProviderResponseID: canonical.NewResponsesNativeResponseID(nativeID)}
	}
	return []canonical.Event{
		{Seq: 1, Kind: canonical.EventEnvelopeStart, EnvID: "response", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse, Model: "m"}},
		{Seq: 2, Kind: canonical.EventResponseIdentity, EnvID: "response", Payload: canonical.ResponseIdentityPayload{Response: response}},
		{Seq: 3, Kind: canonical.EventItemStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: canonicaltest.MustMessageStart(canonical.MessageRoleAssistant)}},
		{Seq: 4, Kind: canonical.EventContentStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0, Part: 0}, Payload: canonical.ContentStartPayload{Kind: canonical.PartKindText}}},
		{Seq: 5, Kind: canonical.EventTextDelta, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0, Part: 0}, Payload: canonical.TextDeltaPayload{Text: "hello"}}},
		{Seq: 6, Kind: canonical.EventItemCompleted, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: canonical.ItemCompletedPayload{Item: item}}},
		{Seq: 7, Kind: canonical.EventFinish, EnvID: "response", Payload: canonical.FinishPayload{Reason: "stop"}},
		{Seq: 8, Kind: canonical.EventEnvelopeEnd, EnvID: "response", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	}
}

func TestCommitReaderStreamsBeforeCaptureAndCommitsBeforeTerminalSuccess(t *testing.T) {
	store := newSpyStore()
	binding := canonical.ResponseBinding{SwobuID: canonical.NewSwobuResponseID("resp_swobu")}
	bound := canonical.NewBoundResponseIdentityStream(canonical.NewSliceEventReader(successfulResponseEvents(t, "")), binding)
	reader := NewCommitReader(bound, TerminalCommitConfig{WorkspaceSlug: "workspace", Binding: binding, Store: store, MaxSemanticBytes: testReplaySemanticLimit})

	for index := 0; index < 7; index++ {
		event, err := reader.Next(context.Background())
		if err != nil {
			t.Fatalf("nonterminal event %d: %v", index, err)
		}
		if event.Kind == canonical.EventEnvelopeEnd {
			t.Fatalf("terminal event escaped before capture at %d", index)
		}
		if len(store.calls) != 0 {
			t.Fatalf("capture ran before terminal success: %v", store.calls)
		}
	}

	terminal, err := reader.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Kind != canonical.EventEnvelopeEnd || len(store.calls) != 1 || store.calls[0] != "Put" {
		t.Fatalf("terminal=%#v store calls=%v", terminal, store.calls)
	}
}

func TestCommitReaderCloseBeforeTerminalDoesNotCommit(t *testing.T) {
	store := newSpyStore()
	binding := canonical.ResponseBinding{SwobuID: canonical.NewSwobuResponseID("resp_swobu")}
	bound := canonical.NewBoundResponseIdentityStream(canonical.NewSliceEventReader(successfulResponseEvents(t, "")), binding)
	reader := NewCommitReader(bound, TerminalCommitConfig{WorkspaceSlug: "workspace", Binding: binding, Store: store, MaxSemanticBytes: testReplaySemanticLimit})
	if _, err := reader.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.calls) != 0 {
		t.Fatalf("client cancellation committed replay: %v", store.calls)
	}
}

func collectCommitReader(t *testing.T, reader *CommitReader) []canonical.Event {
	t.Helper()
	var events []canonical.Event
	for {
		event, err := reader.Next(context.Background())
		if errors.Is(err, io.EOF) {
			return events
		}
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
}

func TestCommitReaderObservesBoundIdentityAndStoresCompletedCheckpoints(t *testing.T) {
	store := newSpyStore()
	binding := canonical.ResponseBinding{SwobuID: canonical.NewSwobuResponseID("resp_swobu"), TargetID: "target", TargetVersion: 4}
	bound := canonical.NewBoundResponseIdentityStream(canonical.NewSliceEventReader(successfulResponseEvents(t, "provider_1")), binding)
	reader := NewCommitReader(bound, TerminalCommitConfig{
		WorkspaceSlug:    "workspace",
		Binding:          binding,
		Store:            store,
		SemanticRequest:  requestWithURLImage(t, "https://example.test/image.png"),
		ResolvedMedia:    mustResolvedMedia(t, canonical.RequestPartRef{Item: 0, Part: 0}, "https://example.test/image.png", canonical.ImageMediaPNG, []byte("exact")),
		MaxSemanticBytes: testReplaySemanticLimit,
	})

	events := collectCommitReader(t, reader)
	if len(events) != 8 {
		t.Fatalf("events=%d, want 8", len(events))
	}
	identity, ok := events[1].Payload.(canonical.ResponseIdentityPayload)
	if !ok || identity.Response.SwobuID.String() != "resp_swobu" {
		t.Fatalf("identity=%#v", events[1].Payload)
	}
	if identity.Response.Responses == nil || identity.Response.Responses.TargetID != "target" || identity.Response.Responses.TargetVersion != 4 {
		t.Fatalf("native identity=%#v", identity.Response.Responses)
	}
	record, ok := store.records[workspaceRecordID{workspaceSlug: "workspace", id: "resp_swobu"}]
	if !ok {
		t.Fatal("completed response was not committed")
	}
	if got := record.Response.Items(); len(got) != 1 || canonicalItemText(got[0]) != "hello" {
		t.Fatalf("committed items=%#v", got)
	}
	if record.ResolvedMedia.AssetCount() != 1 || string(record.ResolvedMedia.assets[0].bytes) != "exact" {
		t.Fatalf("resolved media = %#v", record.ResolvedMedia)
	}
}

func requestWithURLImage(t *testing.T, rawURL string) canonical.CanonicalRequest {
	t.Helper()
	image, err := canonical.NewURLImage(rawURL, canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewImageMessagePart(image)})
	if err != nil {
		t.Fatal(err)
	}
	return makeRequest("m", []canonical.CanonicalItem{message}, nil)
}

func mustResolvedMedia(t *testing.T, position canonical.RequestPartRef, sourceURL string, mediaType canonical.ImageMediaType, data []byte) ResolvedMedia {
	t.Helper()
	media, err := (ResolvedMedia{}).Bind(position, sourceURL, mediaType, data)
	if err != nil {
		t.Fatal(err)
	}
	return media
}

type rejectingStore struct{ err error }

func (s rejectingStore) Get(context.Context, string, canonical.SwobuResponseID) (Record, bool, error) {
	return Record{}, false, nil
}
func (s rejectingStore) Put(context.Context, string, Record) error { return s.err }

type failingResponseStream struct {
	events []canonical.Event
	index  int
	err    error
}

func (s *failingResponseStream) Next(context.Context) (canonical.Event, error) {
	if s.index < len(s.events) {
		event := s.events[s.index]
		s.index++
		return event, nil
	}
	return canonical.Event{}, s.err
}
func (*failingResponseStream) Close(context.Context) error { return nil }

func TestCommitReaderReplacesCompletedTerminalWhenCommitFails(t *testing.T) {
	binding := canonical.ResponseBinding{SwobuID: canonical.NewSwobuResponseID("resp_swobu")}
	bound := canonical.NewBoundResponseIdentityStream(canonical.NewSliceEventReader(successfulResponseEvents(t, "")), binding)
	reader := NewCommitReader(bound, TerminalCommitConfig{
		WorkspaceSlug:    "workspace",
		Binding:          binding,
		Store:            rejectingStore{err: errors.New("disk full")},
		MaxSemanticBytes: testReplaySemanticLimit,
	})
	events := collectCommitReader(t, reader)
	if len(events) != 9 {
		t.Fatalf("events=%d, want 9", len(events))
	}
	failure, ok := events[len(events)-2].Payload.(canonical.ErrorPayload)
	if !ok || failure.Code != "replay_capture_failed" {
		t.Fatalf("failure=%#v", events[len(events)-2])
	}
	end := events[len(events)-1].Payload.(canonical.EnvelopeEndPayload)
	if end.Status != canonical.EnvelopeStatusError || !IsTerminalCommitFailure(reader.CommitError()) {
		t.Fatalf("end=%#v commit error=%v", end, reader.CommitError())
	}
}

func TestCommitReaderFailsClosedWhenProviderEndsBeforeTerminal(t *testing.T) {
	events := successfulResponseEvents(t, "")[:3]
	reader := NewCommitReader(canonical.NewSliceEventReader(events), TerminalCommitConfig{})
	got := collectCommitReader(t, reader)
	if len(got) != 5 {
		t.Fatalf("events=%d, want 5", len(got))
	}
	failure := got[len(got)-2].Payload.(canonical.ErrorPayload)
	if failure.Code != "provider_stream_incomplete" {
		t.Fatalf("failure code=%q", failure.Code)
	}
}

func TestCommitReaderTurnsProviderErrorAfterStartIntoTerminalFailure(t *testing.T) {
	upstream := &failingResponseStream{events: successfulResponseEvents(t, "")[:3], err: errors.New("decode failed")}
	reader := NewCommitReader(upstream, TerminalCommitConfig{})
	got := collectCommitReader(t, reader)
	if len(got) != 5 {
		t.Fatalf("events=%d, want 5", len(got))
	}
	failure := got[len(got)-2].Payload.(canonical.ErrorPayload)
	if failure.Code != "provider_stream_decode_failed" {
		t.Fatalf("failure code=%q", failure.Code)
	}
}

func TestCommitReaderRejectsOversizedSemanticRecordBeforeStore(t *testing.T) {
	store := newSpyStore()
	binding := canonical.ResponseBinding{SwobuID: canonical.NewSwobuResponseID("resp_swobu")}
	bound := canonical.NewBoundResponseIdentityStream(canonical.NewSliceEventReader(successfulResponseEvents(t, "")), binding)
	reader := NewCommitReader(bound, TerminalCommitConfig{
		WorkspaceSlug:    "workspace",
		Binding:          binding,
		Store:            store,
		SemanticRequest:  makeRequest("m", makeItems(strings.Repeat("x", int(testReplaySemanticLimit)+1)), nil),
		MaxSemanticBytes: testReplaySemanticLimit,
	})
	got := collectCommitReader(t, reader)
	failure := got[len(got)-2].Payload.(canonical.ErrorPayload)
	if failure.Code != "replay_capture_failed" || len(store.calls) != 0 {
		t.Fatalf("failure=%#v store calls=%v", failure, store.calls)
	}
}
