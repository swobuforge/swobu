package responses

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/responsesnative"
)

func TestBufferedResponsesCapturePreservesCompleteOpaqueOutputBatch(t *testing.T) {
	raw := []byte(`{"id":"resp_1","model":"gpt","status":"completed","output":[{"type":"message","id":"msg_1","status":"completed","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"done"}],"future":null},{"type":"reasoning","id":"rs_1","status":"completed","summary":[],"encrypted_content":"cipher","large":9007199254740993},{"type":"program","id":"pg_1","caller":"container","unknown":{"x":1}}]}`)
	decoded, err := (ProviderDocumentDecoder{}).DecodeProviderDocument(context.Background(), canonical.CanonicalRequest{}, carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}), "ex")
	if err != nil {
		t.Fatal(err)
	}
	drainResponseStream(t, decoded.Stream)
	batch, ok := decoded.ResponsesOutput.ResponsesOutput()
	if !ok || batch.Len() != 3 {
		t.Fatalf("native batch available=%v len=%d", ok, batch.Len())
	}
	items := batch.JSONObjects()
	for _, needle := range []string{`"phase":"final_answer"`, `"encrypted_content":"cipher"`, `9007199254740993`, `"type":"program"`, `"caller":"container"`} {
		if !strings.Contains(string(items[0])+string(items[1])+string(items[2]), needle) {
			t.Fatalf("native output lost %s: %s", needle, items)
		}
	}
}

func TestStreamingResponsesCaptureMatchesCompletedOutputBatch(t *testing.T) {
	item := `{"type":"reasoning","id":"rs_1","status":"completed","summary":[],"encrypted_content":"cipher","future":null}`
	sse := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"gpt\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":" + item + "}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"gpt\",\"status\":\"completed\",\"output\":[" + item + "]}}\n\n"
	decoded, err := (ProviderEnvelopeDecoder{}).DecodeProviderEnvelope(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(sse))}, "ex")
	if err != nil {
		t.Fatal(err)
	}
	drainResponseStream(t, decoded.Stream)
	batch, ok := decoded.ResponsesOutput.ResponsesOutput()
	if !ok || batch.Len() != 1 || string(batch.JSONObjects()[0]) != item {
		t.Fatalf("stream batch available=%v items=%s", ok, batch.JSONObjects())
	}
}

func TestStatelessResponsesReplayInterleavesEveryNativeBatch(t *testing.T) {
	firstInput := mustNativeInput(t, `{"type":"message","role":"user","content":"one","client_future":1}`)
	secondInput := mustNativeInput(t, `{"type":"function_call_output","call_id":"call_1","output":"two","caller":"client"}`)
	firstOutput := mustNativeOutput(t, `{"type":"reasoning","id":"rs_1","encrypted_content":"cipher_1"}`, `{"type":"function_call","id":"fc_1","call_id":"call_1","name":"f","arguments":"{}","caller":"model"}`)
	secondOutput := mustNativeOutput(t, `{"type":"program_output","id":"po_1","output":"three","future":null}`)
	history := responsesnative.NewHistory([]responsesnative.Turn{
		responsesnative.NewTurn(canonical.CanonicalRequest{}, firstInput, firstOutput),
		responsesnative.NewTurn(canonical.CanonicalRequest{}, secondInput, secondOutput),
	})
	current := mustNativeInput(t, `{"type":"message","role":"user","content":"four"}`)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gpt")})
	document, err := EncodeCarrierWithDecisions(EncodeInput{Request: request, Responses: responsesnative.NewRequestState(current, history)}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Input []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Input) != 6 {
		t.Fatalf("replayed items=%d payload=%s", len(payload.Input), document.RawBytes())
	}
	wantTypes := []string{"message", "reasoning", "function_call", "function_call_output", "program_output", "message"}
	for index, raw := range payload.Input {
		var item struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &item); err != nil || item.Type != wantTypes[index] {
			t.Fatalf("item %d = %s (%v), want %s", index, raw, err, wantTypes[index])
		}
	}
	joined := string(document.RawBytes())
	for _, needle := range []string{"cipher_1", `"caller":"model"`, `"future":null`, `"client_future":1`} {
		if !strings.Contains(joined, needle) {
			t.Fatalf("stateless replay lost %s: %s", needle, joined)
		}
	}
}

func TestStatelessResponsesNativeInputBypassesPortableWebSearchLowering(t *testing.T) {
	callID, _ := canonical.NewToolCallID("toolu_swobu_1_0")
	input, err := canonical.NewWebSearchToolInput(canonical.WebSearchCall{
		Action: canonical.WebSearchActionSearch, Queries: []string{"deadline"},
	})
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt"), Items: []canonical.CanonicalItem{call},
	})
	native := mustNativeInput(t,
		`{"type":"web_search_call","status":"completed","action":{"type":"search","query":"deadline"}}`,
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"verify it"}]}`,
	)
	document, err := EncodeCarrierWithDecisions(EncodeInput{Request: request, Responses: responsesnative.NewRequestState(native, responsesnative.History{})}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wire := string(document.RawBytes())
	if !strings.Contains(wire, `"type":"web_search_call"`) || !strings.Contains(wire, `"verify it"`) {
		t.Fatalf("native stateless input was not preserved: %s", wire)
	}
}

func TestNativeResponsesContinuationDoesNotReplayAncestralBatches(t *testing.T) {
	history := responsesnative.NewHistory([]responsesnative.Turn{
		responsesnative.NewTurn(canonical.CanonicalRequest{}, responsesnative.Items{}, mustNativeOutput(t, `{"type":"reasoning","encrypted_content":"must_not_replay"}`)),
	})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:            canonical.Specify("gpt"),
		PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_1", Responses: &canonical.ResponsesNativeRef{ProviderResponseID: "provider_1", TargetID: "target", TargetVersion: 1}},
	})
	document, err := EncodeCarrierWithDecisions(EncodeInput{Request: request, Responses: responsesnative.NewRequestState(mustNativeInput(t, `{"type":"function_call_output","call_id":"call_1","output":"ok"}`), history)}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wire := string(document.RawBytes())
	if !strings.Contains(wire, `"previous_response_id":"provider_1"`) || strings.Contains(wire, "must_not_replay") {
		t.Fatalf("native continuation replayed ancestry: %s", wire)
	}
}

func drainResponseStream(t *testing.T, stream canonical.ResponseStream) {
	t.Helper()
	for {
		if _, err := stream.Next(context.Background()); err != nil {
			if err == io.EOF {
				return
			}
			t.Fatal(err)
		}
	}
}

func mustNativeInput(t *testing.T, items ...string) responsesnative.Items {
	t.Helper()
	raw := make([][]byte, len(items))
	for index := range items {
		raw[index] = []byte(items[index])
	}
	input, err := responsesnative.NewItems(raw)
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func mustNativeOutput(t *testing.T, items ...string) responsesnative.Items {
	t.Helper()
	raw := make([][]byte, len(items))
	for index := range items {
		raw[index] = []byte(items[index])
	}
	output, err := responsesnative.NewItems(raw)
	if err != nil {
		t.Fatal(err)
	}
	return output
}
