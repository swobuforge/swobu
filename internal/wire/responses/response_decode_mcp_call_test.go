package responses

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeOutputItemsIgnoresMCPCallWithoutAdmittedConsumer(t *testing.T) {
	items := []responsesWireOutputItemDTO{{Type: "mcp_call", ID: "mcp_1", Name: "Read", Arguments: `{"path":"workspace/file.txt"}`}}
	decoded, err := decodeOutputItems(context.Background(), canonical.CanonicalRequest{}, items, "", "ex", nil)
	if err != nil || len(decoded) != 0 {
		t.Fatalf("MCP projection = %#v, %v", decoded, err)
	}
}

func TestDecodeResponseBufferedIgnoresMCPCallWithoutAdmittedConsumer(t *testing.T) {
	raw := []byte(`{"id":"resp_1","model":"m","output":[{"type":"mcp_call","id":"mcp_1","name":"Read","arguments":"{\"path\":\"workspace/file.txt\"}"}]}`)
	stream, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertResponsesProviderOutputItems(t, stream, 0)
}

func TestDecodeResponseStreamIgnoresUnknownOutputWithoutLosingKnownOutput(t *testing.T) {
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"program_output\",\"id\":\"po_1\"}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"program_output\",\"id\":\"po_1\",\"future\":true}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":1,\"content_index\":0,\"delta\":\"visible\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"visible\"}]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[]}}\n\n"
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
