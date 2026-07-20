package responses

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestDecodeResponseStream_DoesNotReopenAnonymousToolCallOnSecondDoneFrame(t *testing.T) {
	t.Parallel()

	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"call_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"Bash\"}}\n\n" +
		"event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"item_id\":\"call_1\",\"call_id\":\"call_1\",\"name\":\"Bash\",\"delta\":\"{\\\"command\\\":\\\"cat fixture\\\"}\"}\n\n" +
		"event: response.function_call_arguments.done\ndata: {\"type\":\"response.function_call_arguments.done\",\"output_index\":0,\"item_id\":\"call_1\",\"call_id\":\"call_1\",\"name\":\"Bash\",\"arguments\":\"{\\\"command\\\":\\\"cat fixture\\\"}\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"call_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"arguments\":\"{\\\"command\\\":\\\"cat fixture\\\"}\"}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[]}}\n\n"

	reader := decodeResponseStream(
		canonical.NewCanonicalRequest(canonical.RequestParams{Tools: canonicaltest.SpecifiedToolSet(t, canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "Bash"), "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]()))}),
		carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex_stream_tool_lifecycle",
		nil,
	)

	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	out, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	items := out.Items()
	if len(items) != 1 {
		t.Fatalf("output items len=%d, want 1", len(items))
	}
	item := items[0]
	if item.Kind() != canonical.ItemKindToolCall {
		t.Fatalf("output item kind=%s, want %s", item.Kind(), canonical.ItemKindToolCall)
	}
	toolUse, _ := item.ToolCall()
	if toolUse.CallID().String() != "call_1" {
		t.Fatalf("tool use id=%q, want call_1", toolUse.CallID().String())
	}
}

func TestDecodeResponseStream_IgnoresDuplicateResponseCreated(t *testing.T) {
	t.Parallel()

	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_2\",\"model\":\"m\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_2\",\"model\":\"m\",\"status\":\"completed\",\"output\":[]}}\n\n"

	reader := decodeResponseStream(
		canonical.CanonicalRequest{},
		carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex_stream_duplicate_created",
		nil,
	)

	starts := 0
	for {
		ev, err := reader.Next(context.Background())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next returned error: %v", err)
		}
		if ev.Kind == canonical.EventResponseIdentity {
			identity, ok := ev.Payload.(canonical.ResponseIdentityPayload)
			if !ok || identity.Response.Responses == nil || identity.Response.Responses.ProviderResponseID != "resp_1" {
				t.Fatalf("response identity=%#v", ev.Payload)
			}
		}
		if ev.Kind != canonical.EventEnvelopeStart {
			continue
		}
		payload, ok := ev.Payload.(canonical.EnvelopeStartPayload)
		if !ok {
			t.Fatalf("start payload type = %T, want EnvelopeStartPayload", ev.Payload)
		}
		if payload.Kind != canonical.EnvResponse {
			continue
		}
		starts++
		if starts > 1 {
			t.Fatalf("duplicate response.created emitted %d response starts", starts)
		}
	}
	if starts != 1 {
		t.Fatalf("response start count = %d, want 1", starts)
	}
}
