package responses

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeOutputItemsRejectsMCPCallWithoutAdmittedConsumer(t *testing.T) {
	items := []responsesWireOutputItemDTO{{Type: "mcp_call", ID: "mcp_1", Name: "Read", Arguments: json.RawMessage(`{"path":"workspace/file.txt"}`)}}
	_, err := decodeOutputItems(context.Background(), canonical.CanonicalRequest{}, items, "", "ex", nil)
	assertResponsesBackendError(t, err)
}

func TestDecodeResponseBufferedRejectsMCPCallWithoutAdmittedConsumer(t *testing.T) {
	raw := []byte(`{"id":"resp_1","model":"m","output":[{"type":"mcp_call","id":"mcp_1","name":"Read","arguments":"{\"path\":\"workspace/file.txt\"}"}]}`)
	_, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex", nil)
	assertResponsesBackendError(t, err)
}

func TestDecodeResponseStreamIgnoresUnknownOutputWithoutLosingKnownOutput(t *testing.T) {
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"program_output\",\"id\":\"po_1\"}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"program_output\",\"id\":\"po_1\",\"future\":true}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":1,\"content_index\":0,\"delta\":\"visible\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"visible\"}]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[{\"type\":\"program_output\",\"id\":\"po_1\",\"future\":true},{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"visible\"}]}]}}\n\n"
	stream := decodeResponseStream(
		canonical.CanonicalRequest{},
		carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex", nil,
	)
	assertResponsesProviderOutputItems(t, stream, 1)
	assertResponsesStreamDrop(t, stream, 1)
}

func TestDecodeResponseStreamAllUnknownMessageContentRejectsEmptyResidual(t *testing.T) {
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"future_content\",\"value\":\"ignored\"}]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[]}}\n\n"
	stream := decodeResponseStream(
		canonical.CanonicalRequest{},
		carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex", nil,
	)
	if err := drainResponsesStream(stream); err == nil {
		t.Fatal("all-erased streamed message was reported as successful output")
	}
	assertResponsesStreamDrop(t, stream, 1)
}

func TestDecodeResponseStreamUsesTerminalFallbackWhenNoOutputLifecycleWasObserved(t *testing.T) {
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[{\"type\":\"program_output\",\"id\":\"po_1\"},{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"visible\"}]}]}}\n\n"
	stream := decodeResponseStream(
		canonical.CanonicalRequest{},
		carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex", nil,
	)
	assertResponsesProviderOutputItems(t, stream, 1)
}

func TestDecodeResponseStreamIgnoredOutputDoesNotSuppressTerminalFallback(t *testing.T) {
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"program_output\",\"id\":\"po_1\"}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[{\"type\":\"program_output\",\"id\":\"po_1\"},{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"visible\"}]}]}}\n\n"
	stream := decodeResponseStream(
		canonical.CanonicalRequest{},
		carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex", nil,
	)
	assertResponsesProviderOutputItems(t, stream, 1)
	assertResponsesStreamDrop(t, stream, 1)
}

func TestDecodeResponseStreamUnknownLifecyclePreservesTerminalOutputText(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"future_output\",\"id\":\"future_1\"}}\n\n" +
		responsesCompletedFrame(`[{"type":"future_output","id":"future_1"}]`, "visible answer")
	stream := decodeResponseStream(
		canonical.CanonicalRequest{},
		carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex", nil,
	)
	bound := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "swobu_1"})
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(bound), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	items := response.Items()
	if len(items) != 1 {
		t.Fatalf("canonical output items = %#v, want one output_text message", items)
	}
	message, ok := items[0].Message()
	if !ok {
		t.Fatalf("canonical output item = %#v, want message", items[0])
	}
	text, _ := message.Content()[0].Text()
	if text.Text() != "visible answer" {
		t.Fatalf("output_text message = %q, want visible answer", text.Text())
	}
	assertResponsesStreamDrop(t, stream, 1)
}

func TestDecodeResponseStreamCompletedMessageDoesNotDuplicateTerminalOutputText(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"visible answer\"}]}}\n\n" +
		responsesCompletedFrame(`[{"type":"message","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"visible answer"}]}]`, "visible answer")
	stream := decodeResponseStream(
		canonical.CanonicalRequest{},
		carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex", nil,
	)
	assertResponsesProviderOutputItems(t, stream, 1)
}

func TestDecodeResponseStreamRejectsProviderMCPDeltaDespiteKnownSibling(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.mcp_call_arguments.delta\ndata: {\"type\":\"response.mcp_call_arguments.delta\",\"output_index\":0,\"item_id\":\"mcp_1\",\"delta\":\"one\"}\n\n" +
		"event: response.mcp_call_arguments.done\ndata: {\"type\":\"response.mcp_call_arguments.done\",\"output_index\":0,\"item_id\":\"mcp_1\",\"arguments\":\"one\"}\n\n" +
		responsesCompletedFrame(`[{"type":"mcp_call","id":"mcp_1","name":"Read","arguments":"one"},{"type":"message","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"visible"}]}]`, "")
	stream := decodeResponseStream(
		canonical.CanonicalRequest{},
		carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex", nil,
	)
	assertResponsesBackendError(t, drainResponsesStream(stream))
}

func TestDecodeResponseStreamAddedMessageRecoversFromTerminalOutput(t *testing.T) {
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"visible\"}]}]}}\n\n"
	stream := decodeResponseStream(
		canonical.CanonicalRequest{},
		carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex", nil,
	)
	assertResponsesProviderOutputItems(t, stream, 1)
}

func TestDecodeResponseStreamAddedFunctionCallRecoversFromTerminalOutput(t *testing.T) {
	key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "lookup")
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	declaration, _ := canonical.NewFunctionTool(key, "", canonical.NewToolSchemaObject(schemaObject), canonical.Unspecified[bool]())
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	tools, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{tools}})
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"status\":\"completed\",\"arguments\":\"{}\"}]}}\n\n"
	stream := decodeResponseStream(
		request,
		carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex", nil,
	)
	assertResponsesProviderOutputItems(t, stream, 1)
}

func TestDecodeResponseStreamDeduplicatesUnknownEventsByTypeAndItem(t *testing.T) {
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.future.delta\ndata: {\"type\":\"response.future.delta\",\"item_id\":\"item_1\",\"delta\":\"a\"}\n\n" +
		"event: response.future.delta\ndata: {\"type\":\"response.future.delta\",\"item_id\":\"item_1\",\"delta\":\"b\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"visible\"}]}]}}\n\n"
	stream := decodeResponseStream(
		canonical.CanonicalRequest{},
		carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex", nil,
	)
	assertResponsesProviderOutputItems(t, stream, 1)
	if len(stream.Changes()) != 0 {
		t.Fatalf("wire-only events created semantic changes: %#v", stream.Changes())
	}
}

func TestDecodeResponseStreamRejectsProviderMCPCallDespiteKnownSibling(t *testing.T) {
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"mcp_call\",\"id\":\"mcp_1\",\"name\":\"Read\",\"arguments\":\"{\\\"path\\\":\\\"workspace/file.txt\\\"}\"}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"mcp_call\",\"id\":\"mcp_1\",\"name\":\"Read\",\"arguments\":\"{\\\"path\\\":\\\"workspace/file.txt\\\"}\"}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":1,\"content_index\":0,\"delta\":\"visible\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"visible\"}]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[]}}\n\n"
	stream := decodeResponseStream(
		canonical.CanonicalRequest{},
		carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex", nil,
	)
	assertResponsesBackendError(t, drainResponsesStream(stream))
}

func assertResponsesErrorCode(t *testing.T, err error, want canonical.ErrorCode) {
	t.Helper()
	var canonicalError canonical.Error
	if !errors.As(err, &canonicalError) || canonicalError.Code != want {
		t.Fatalf("error = %T %v, want %s", err, err, want)
	}
}

func assertResponsesBackendError(t *testing.T, err error) {
	t.Helper()
	var backendError canonical.BackendError
	if !errors.As(err, &backendError) {
		t.Fatalf("error = %T %v, want backend error", err, err)
	}
}

func assertResponsesProviderOutputItems(t *testing.T, stream canonical.ResponseStream, want int) {
	t.Helper()
	bound := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "swobu_1"})
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(bound), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items()) != want {
		t.Fatalf("canonical output items = %#v, want %d", response.Items(), want)
	}
}

func drainResponsesStream(stream canonical.ResponseStream) error {
	for {
		_, err := stream.Next(context.Background())
		if err != nil {
			return err
		}
	}
}

func assertResponsesStreamDrop(t *testing.T, stream *responsesResponseStream, want int) {
	t.Helper()
	count := 0
	for _, decision := range stream.Changes() {
		if decision.Capability == canonical.ResponseItemsKind &&
			decision.Kind == compat.Omission {
			count++
		}
	}
	if count != want {
		t.Fatalf("omission changes = %d, want %d; all changes = %#v", count, want, stream.Changes())
	}
}
