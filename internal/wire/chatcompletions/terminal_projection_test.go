package chatcompletions

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestChatResponseToolCallOverridesProviderStopTerminal(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	callID, err := canonical.NewToolCallID("call_1")
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := canonical.ParseJSONObject([]byte(`{"query":"status"}`))
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(arguments))
	if err != nil {
		t.Fatal(err)
	}
	response := canonicaltest.Response(t, "resp_1", "m", []canonical.CanonicalItem{call}, canonical.Completed("stop"))
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded.Document.RawBytes(), []byte(`"finish_reason":"tool_calls"`)) {
		t.Fatalf("Chat response terminal = %s, want tool_calls", encoded.Document.RawBytes())
	}

	events := canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		"exchange", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage(),
	))
	streamed, err := (ResponseStreamEncoder{}).EncodeResponseStream(
		context.Background(), canonical.CanonicalRequest{}, events, delivery.StreamingDelivery(delivery.FramingSSE),
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(streamed.Stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"finish_reason":"tool_calls"`)) {
		t.Fatalf("Chat stream terminal = %s, want tool_calls", raw)
	}
}

func TestChatResponseTextPreservesProviderStopTerminal(t *testing.T) {
	message, err := canonical.NewMessageItem(
		canonical.MessageRoleAssistant,
		[]canonical.MessagePart{canonical.NewTextMessagePart("done")},
	)
	if err != nil {
		t.Fatal(err)
	}
	response := canonicaltest.Response(t, "resp_1", "m", []canonical.CanonicalItem{message}, canonical.Completed("stop"))
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded.Document.RawBytes(), []byte(`"finish_reason":"stop"`)) || bytes.Contains(encoded.Document.RawBytes(), []byte(`"finish_reason":"tool_calls"`)) {
		t.Fatalf("text response terminal = %s, want stop", encoded.Document.RawBytes())
	}
}

func TestChatResponseEmptyToolCallsRemainTextStop(t *testing.T) {
	raw := []byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"done","tool_calls":[]},"finish_reason":"stop"}]}`)
	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex_empty_tools", nil)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := canonical.ReadClosedEnvelope(
		context.Background(),
		canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_empty_tools"}),
		canonical.EnvResponse,
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, *response)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded.Document.RawBytes(), []byte(`"finish_reason":"stop"`)) || bytes.Contains(encoded.Document.RawBytes(), []byte(`"finish_reason":"tool_calls"`)) {
		t.Fatalf("empty-tool response terminal = %s, want stop", encoded.Document.RawBytes())
	}
}
