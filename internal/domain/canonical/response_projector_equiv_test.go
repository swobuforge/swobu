package canonical

import (
	"context"
	"reflect"
	"testing"
)

// TestResponseProjectorMatchesRetainAndReproject is the correctness lock for
// epic-50 task 010: the incremental ResponseProjector (used by the checkpoint
// capture stream) must produce the exact same projected response as the legacy
// retain-and-reproject path (ReadClosedEnvelope + ProjectResponse) over the
// same events. Both run the same itemStreamAssembler under the hood, so this
// guards against any divergence introduced by folding one event at a time
// instead of re-reading the whole slice.
func TestResponseProjectorMatchesRetainAndReproject(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events func(*testing.T) []Event
	}{
		{"single message many deltas", func(*testing.T) []Event { return eventsSingleMessage() }},
		{"multiple messages", func(*testing.T) []Event { return eventsMultipleMessages() }},
		{"tool call with text args", func(*testing.T) []Event { return eventsToolCall() }},
		{"mixed atomic output and usage", eventsMixedAtomicOutput},
		{"empty items", func(*testing.T) []Event { return eventsEmpty() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := tc.events(t)

			closed, err := ReadClosedEnvelope(context.Background(), NewSliceEventReader(events), EnvResponse)
			if err != nil {
				t.Fatalf("ReadClosedEnvelope failed: %v", err)
			}
			legacy, err := closed.ProjectResponse()
			if err != nil {
				t.Fatalf("legacy projection failed: %v", err)
			}

			incremental, err := ProjectStream(context.Background(), NewSliceEventReader(events), ResponseBinding{})
			if err != nil {
				t.Fatalf("incremental projection failed: %v", err)
			}

			assertResponsesEqual(t, legacy, incremental)
		})
	}
}

// TestResponseProjectorRejectsMalformed confirms the incremental projector
// fails on malformed streams with an error, rather than silently producing a
// partial result.
func TestResponseProjectorRejectsMalformed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events func() []Event
	}{
		{"incomplete (no envelope end)", eventsNoEnvelopeEnd},
		{"failed envelope", eventsFailedEnvelope},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := tc.events()
			_, err := ProjectStream(context.Background(), NewSliceEventReader(events), ResponseBinding{})
			if err == nil {
				t.Fatalf("incremental projector accepted a malformed stream; want an error")
			}
		})
	}
}

func assertResponsesEqual(t *testing.T, legacy, incremental *CanonicalResponse) {
	t.Helper()
	if legacy.Model() != incremental.Model() {
		t.Errorf("Model: legacy %q != incremental %q", legacy.Model(), incremental.Model())
	}
	if legacy.Completion() != incremental.Completion() {
		t.Errorf("Completion: legacy %+v != incremental %+v", legacy.Completion(), incremental.Completion())
	}
	if legacy.Response() != incremental.Response() {
		t.Errorf("Response ref: legacy %+v != incremental %+v", legacy.Response(), incremental.Response())
	}
	if legacyUsage, incUsage := legacy.Usage(), incremental.Usage(); legacyUsage != incUsage {
		t.Errorf("Usage: legacy %+v != incremental %+v", legacyUsage, incUsage)
	}
	legacyItems, incItems := legacy.Items(), incremental.Items()
	if len(legacyItems) != len(incItems) {
		t.Fatalf("item count: legacy %d != incremental %d", len(legacyItems), len(incItems))
	}
	for i := range legacyItems {
		assertItemsEqual(t, i, legacyItems[i], incItems[i])
	}
}

func assertItemsEqual(t *testing.T, index int, legacy, incremental CanonicalItem) {
	t.Helper()
	// Message item.
	if legacyMessage, ok := legacy.Message(); ok {
		incMessage, ok := incremental.Message()
		if !ok {
			t.Fatalf("item %d: legacy is a message, incremental is not", index)
		}
		if legacyMessage.Role() != incMessage.Role() {
			t.Errorf("item %d role: %q != %q", index, legacyMessage.Role(), incMessage.Role())
		}
		lc, ic := legacyMessage.Content(), incMessage.Content()
		if len(lc) != len(ic) {
			t.Fatalf("item %d content parts: %d != %d", index, len(lc), len(ic))
		}
		for p := range lc {
			lt, lOk := lc[p].Text()
			it, iOk := ic[p].Text()
			if lOk != iOk || (lOk && lt.Text() != it.Text()) {
				t.Errorf("item %d part %d text differs", index, p)
			}
		}
		return
	}
	// Tool-call item.
	if legacyCall, ok := legacy.ToolCall(); ok {
		incCall, ok := incremental.ToolCall()
		if !ok {
			t.Fatalf("item %d: legacy is a tool call, incremental is not", index)
		}
		if legacyCall.CallID() != incCall.CallID() || legacyCall.Tool() != incCall.Tool() {
			t.Errorf("item %d tool-call identity differs", index)
		}
		legacyText, legacyIsText := legacyCall.Input().Text()
		incText, incIsText := incCall.Input().Text()
		if legacyIsText != incIsText || (legacyIsText && legacyText != incText) {
			t.Errorf("item %d tool-call text input differs", index)
		}
		return
	}
	if !reflect.DeepEqual(legacy, incremental) {
		t.Fatalf("item %d of kind %q differs", index, legacy.Kind())
	}
}

// --- event fixtures (production-shaped; ItemEvent-wrapped deltas) ---

// equivEnvID is the single response envelope every fixture event belongs to.
// ReadClosedEnvelope requires item events to carry the open response envelope's
// ID, so it must be threaded onto every event consistently.
const equivEnvID EnvelopeID = "resp_equiv:response:0"

func messageTextPartEvents(item uint32, deltas []string) []Event {
	start, _ := NewMessageStart(MessageRoleAssistant)
	out := []Event{
		{EnvID: equivEnvID, Kind: EventItemStart, Payload: ItemEvent{Position: ItemPosition{Item: item}, Payload: start}},
		{EnvID: equivEnvID, Kind: EventContentStart, Payload: ItemEvent{Position: ItemPosition{Item: item, Part: 0}, Payload: NewMessageContentStart(PartKindText)}},
	}
	for _, d := range deltas {
		out = append(out, Event{EnvID: equivEnvID, Kind: EventTextDelta, Payload: ItemEvent{Position: ItemPosition{Item: item, Part: 0}, Payload: TextDeltaPayload{Text: d}}})
	}
	return out
}

func completedMessageEvent(item uint32, role MessageRole, text string) Event {
	completed, _ := NewMessageItem(role, []MessagePart{NewTextMessagePart(text)})
	return Event{EnvID: equivEnvID, Kind: EventItemCompleted, Payload: ItemEvent{Position: ItemPosition{Item: item}, Payload: ItemCompletedPayload{Item: completed}}}
}

func responseWrap(itemEvents []Event) []Event {
	out := []Event{
		{EnvID: equivEnvID, Kind: EventEnvelopeStart, Payload: EnvelopeStartPayload{Kind: EnvResponse, Model: "equiv-model"}},
		{EnvID: equivEnvID, Kind: EventResponseIdentity, Payload: ResponseIdentityPayload{Response: ResponseRef{SwobuID: NewSwobuResponseID("resp_equiv")}}},
	}
	out = append(out, itemEvents...)
	out = append(out,
		Event{EnvID: equivEnvID, Kind: EventUsage, Payload: UsagePayload{}},
		Event{EnvID: equivEnvID, Kind: EventFinish, Payload: FinishPayload{Completion: Completed("stop")}},
		Event{EnvID: equivEnvID, Kind: EventEnvelopeEnd, Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted}},
	)
	return out
}

func eventsSingleMessage() []Event {
	deltas := []string{"Hel", "lo ", "wor", "ld", "—", "this", " ", "is", " one", " message."}
	joined := ""
	for _, d := range deltas {
		joined += d
	}
	return responseWrap(append(messageTextPartEvents(0, deltas), completedMessageEvent(0, MessageRoleAssistant, joined)))
}

func eventsMultipleMessages() []Event {
	d2 := []string{"sec", "ond", "!"}
	joined2 := ""
	for _, d := range d2 {
		joined2 += d
	}
	itemEvents := append(messageTextPartEvents(0, []string{"first "}), completedMessageEvent(0, MessageRoleAssistant, "first "))
	itemEvents = append(itemEvents, messageTextPartEvents(1, d2)...)
	itemEvents = append(itemEvents, completedMessageEvent(1, MessageRoleAssistant, joined2))
	return responseWrap(itemEvents)
}

func eventsToolCall() []Event {
	callID, _ := NewToolCallID("call_1")
	// Custom tools take text input, which the item-stream assembler validates by
	// exact string equality — so the concatenated args deltas must equal the
	// completed input text. Function tools require object input whose JSON
	// canonicalization would make this fixture fragile without exercising any
	// additional projection logic.
	tool, _ := NewRequestToolKey(ToolKindCustom, "shell")
	start, _ := NewToolCallStart(callID, tool)
	completed, _ := NewToolCallItem(callID, tool, NewTextToolInput("find-kittens"))
	return responseWrap([]Event{
		{EnvID: equivEnvID, Kind: EventItemStart, Payload: ItemEvent{Position: ItemPosition{Item: 0}, Payload: start}},
		{EnvID: equivEnvID, Kind: EventArgsDelta, Payload: ItemEvent{Position: ItemPosition{Item: 0}, Payload: ArgsDeltaPayload{Args: "find-"}}},
		{EnvID: equivEnvID, Kind: EventArgsDelta, Payload: ItemEvent{Position: ItemPosition{Item: 0}, Payload: ArgsDeltaPayload{Args: "kittens"}}},
		{EnvID: equivEnvID, Kind: EventItemCompleted, Payload: ItemEvent{Position: ItemPosition{Item: 0}, Payload: ItemCompletedPayload{Item: completed}}},
	})
}

func eventsMixedAtomicOutput(t *testing.T) []Event {
	t.Helper()
	searchCall, searchResult := responseWebSearchPair(t, "search_atomic")
	discoveryCall := responseDiscoveryCall(t, "discovery_atomic", DiscoveryExecutorProvider)
	discoveryCallValue, _ := discoveryCall.ToolCall()
	discovery, _ := NewToolDiscoveryResultItem(discoveryCallValue.CallID(), ToolSet{}, DiscoveryExecutorProvider)
	reasoning, _ := NewReasoningItem([]ReasoningPart{mustReasoningPart(t, ReasoningPartSummary, "summary")}, OpaqueThinking{})
	input, output, reasoningTokens := 11, 7, 3
	usage, _ := NewTokenUsage(TokenUsageParams{InputTokens: &input, OutputTokens: &output, ReasoningTokens: &reasoningTokens})
	return SynthesizeResponseEnvelopeEvents(
		"resp_equiv",
		ResponseRef{SwobuID: NewSwobuResponseID("resp_equiv")},
		"equiv-model",
		[]CanonicalItem{searchCall, searchResult, discoveryCall, discovery, reasoning},
		Completed("tool_calls"),
		usage,
	)
}

func eventsEmpty() []Event {
	return responseWrap(nil)
}

func eventsNoEnvelopeEnd() []Event {
	out := responseWrap([]Event{completedMessageEvent(0, MessageRoleAssistant, "x")})
	return out[:len(out)-1]
}

func eventsFailedEnvelope() []Event {
	out := responseWrap([]Event{completedMessageEvent(0, MessageRoleAssistant, "x")})
	out[len(out)-1] = Event{Kind: EventEnvelopeEnd, Payload: EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusError}}
	return out
}
