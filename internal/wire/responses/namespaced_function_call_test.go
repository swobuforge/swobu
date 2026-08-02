package responses

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestNamespacedFunctionCallUsesAttemptScopedFlatAddress(t *testing.T) {
	raw := []byte(`{
		"model":"m",
		"tools":[{"type":"namespace","name":"crm","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}],
		"input":[{"type":"function_call","namespace":"crm","name":"lookup","call_id":"call_1","arguments":{}}]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(
		protocolkind.Responses, "application/json", nil, raw, carrier.Meta{},
	))
	if err != nil {
		t.Fatal(err)
	}
	call, ok := decoded.Request.Request.Items()[1].ToolCall()
	if !ok || call.Tool().Namespace() != "crm" || call.Tool().Name() != "lookup" {
		t.Fatalf("namespaced call identity = %#v", call.Tool())
	}
	names, _, err := provider.BuildAttemptToolNames(decoded.Request.Request)
	if err != nil {
		t.Fatal(err)
	}
	wireName, err := names.WireName(call.Tool())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeCarrierWithChanges(
		EncodeInput{Request: decoded.Request.Request, ToolNames: names}, delivery.BufferedDelivery(), nil, "", EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded.RawBytes())
	if strings.Contains(body, `"namespace"`) || !strings.Contains(body, `"name":"`+wireName+`"`) {
		t.Fatalf("projected flat call address was not preserved: %s", body)
	}
}

func TestNamespacedFunctionCallResponseRestoresClientAddress(t *testing.T) {
	key, _ := canonical.NewToolKey("mcp__microsoft_docs__", canonical.ToolKindFunction, "microsoft_docs_search")
	callID, _ := canonical.NewToolCallID("call_1")
	object, err := canonical.ParseJSONObject([]byte(`{"query":"Go on Azure"}`))
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(object))
	if err != nil {
		t.Fatal(err)
	}
	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID("resp_1")},
		"m", []canonical.CanonicalItem{call}, canonical.Completed("tool_calls"), canonical.TokenUsage{},
	)
	if err != nil {
		t.Fatal(err)
	}
	buffered, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	if body := string(buffered.Document.RawBytes()); !strings.Contains(body, `"namespace":"mcp__microsoft_docs__"`) {
		t.Fatalf("buffered response lost namespace: %s", body)
	}
	events := canonical.SynthesizeResponseEnvelopeEvents("ex", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage())
	streamed, err := (ResponseStreamEncoder{}).EncodeResponseStream(context.Background(), canonical.CanonicalRequest{}, canonical.NewSliceEventReader(events), delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(streamed.Stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"namespace":"mcp__microsoft_docs__"`) {
		t.Fatalf("streaming response lost namespace: %s", raw)
	}
}
