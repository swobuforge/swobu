package sse

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func testProjectionRequest(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	object, err := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	decl := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "weather"), "", canonical.NewToolSchemaObject(object), canonical.Unspecified[bool]())
	tools, err := canonical.NewToolSet([]canonical.ToolDeclaration{decl})
	if err != nil {
		t.Fatal(err)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{Tools: canonical.Specify(tools)})
}

func TestAdapterProjectsResolvedToolIdentityBeforeArgs(t *testing.T) {
	request := testProjectionRequest(t)
	adapter := NewEnvelopeEventAdapter()
	if got, err := adapter.Translate(canonical.Event{Kind: canonical.EventEnvelopeStart, EnvID: "r", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse, Model: "m"}}); err != nil || len(got) != 0 {
		t.Fatalf("response open=%v err=%v", got, err)
	}
	_, _ = adapter.Translate(canonical.Event{Kind: canonical.EventResponseIdentity, EnvID: "r", Payload: canonical.ResponseIdentityPayload{Response: canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID("resp_1")}}})
	callID, _ := canonical.NewToolCallID("call_1")
	tool := request.Tools()[0].Key()
	started, err := adapter.Translate(canonical.Event{Kind: canonical.EventItemStart, EnvID: "i", Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 7}, Payload: canonicaltest.MustToolCallStart(callID, tool)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(started) != 1 || started[0].ToolUseID != "call_1" || started[0].Name != "weather" {
		t.Fatalf("started events=%#v", started)
	}
	delta, err := adapter.Translate(canonical.Event{Kind: canonical.EventArgsDelta, EnvID: "i", Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 7}, Payload: canonical.ArgsDeltaPayload{Args: `{"loc`}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(delta) != 1 || delta[0].ItemID != "item_7" {
		t.Fatalf("delta=%#v", delta)
	}
}

func TestAdapterProjectsHistoricalToolWithoutCurrentDeclaration(t *testing.T) {
	adapter := NewEnvelopeEventAdapter()
	_, _ = adapter.Translate(canonical.Event{Kind: canonical.EventEnvelopeStart, EnvID: "r", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}})
	_, _ = adapter.Translate(canonical.Event{Kind: canonical.EventResponseIdentity, EnvID: "r", Payload: canonical.ResponseIdentityPayload{Response: canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID("resp_1")}}})
	callID, _ := canonical.NewToolCallID("call_1")
	tool := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "missing")
	events, err := adapter.Translate(canonical.Event{Kind: canonical.EventItemStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: canonicaltest.MustToolCallStart(callID, tool)}})
	if err != nil || len(events) != 1 || events[0].Name != "missing" {
		t.Fatalf("historical tool projection = %#v, err=%v", events, err)
	}
}

func TestAdapterPreservesContentPartCoordinates(t *testing.T) {
	adapter := NewEnvelopeEventAdapter()
	_, _ = adapter.Translate(canonical.Event{Kind: canonical.EventEnvelopeStart, EnvID: "r", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}})
	_, _ = adapter.Translate(canonical.Event{Kind: canonical.EventResponseIdentity, EnvID: "r", Payload: canonical.ResponseIdentityPayload{Response: canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID("resp_1")}}})
	_, _ = adapter.Translate(canonical.Event{Kind: canonical.EventItemStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 3}, Payload: canonicaltest.MustMessageStart(canonical.MessageRoleAssistant)}})
	started, err := adapter.Translate(canonical.Event{Kind: canonical.EventContentStart, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 3, Part: 2}, Payload: canonical.ContentStartPayload{Kind: canonical.PartKindText}}})
	if err != nil || len(started) != 1 || started[0].Kind != StreamEventContentStarted || started[0].ItemOrdinal != 3 || started[0].PartOrdinal != 2 {
		t.Fatalf("content start = %#v, err=%v", started, err)
	}
	delta, err := adapter.Translate(canonical.Event{Kind: canonical.EventTextDelta, Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 3, Part: 2}, Payload: canonical.TextDeltaPayload{Text: "hi"}}})
	if err != nil || len(delta) != 1 || delta[0].ItemOrdinal != 3 || delta[0].PartOrdinal != 2 {
		t.Fatalf("delta = %#v, err=%v", delta, err)
	}
}
