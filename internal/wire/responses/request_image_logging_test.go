package responses

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

func TestResponsesDecodeLogsCorrelatedImageProvenance(t *testing.T) {
	restore, logs := captureResponsesDebugLogs()
	defer restore()

	raw := []byte(`{
		"model":"m",
		"tools":[{"type":"function","name":"inspect","parameters":{"type":"object"}}],
		"input":[
			{"type":"function_call","call_id":"call_image","name":"inspect","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_image","output":[
				{"type":"input_text","text":"private result"},
				{"type":"input_image","image_url":"https://secret.example/image.png"}
			]}
		]
	}`)
	_, err := (ClientRequestDecoder{ImageLimits: shared.ImageDecodeLimitPolicy{MaxInlineBytes: 8}}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{
			Opaque: map[string]string{"exchange_id": "req_images"},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	got := logs.String()
	for _, want := range []string{
		"event=responses_input_summary",
		"event=responses_request_tools",
		"event=responses_request_images",
		"exchange_id=req_images",
		"decode_view=full",
		"message_image_count=0",
		"tool_result_image_count=1",
		"first_tool_result_item=1",
		"first_tool_result_part=1",
		"first_source=url",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("logs missing %q\nlogs:\n%s", want, got)
		}
	}
	for _, secret := range []string{"secret.example", "private result", "call_image", "inspect"} {
		if strings.Contains(got, secret) {
			t.Fatalf("logs exposed %q\nlogs:\n%s", secret, got)
		}
	}
}

func TestResponsesDecodeLogsFullAndRebasedViewsWithOneExchangeID(t *testing.T) {
	restore, logs := captureResponsesDebugLogs()
	defer restore()

	raw := []byte(`{
		"model":"m",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
			{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hi"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"again"}]}
		]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{
			Opaque: map[string]string{"exchange_id": "req_rebase"},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Request.RebasedRequest == nil {
		t.Fatal("expected implicit history rebase")
	}
	got := logs.String()
	if strings.Count(got, "event=responses_input_summary") != 2 {
		t.Fatalf("input summary count != 2\nlogs:\n%s", got)
	}
	if strings.Count(got, "event=responses_request_tools") != 2 {
		t.Fatalf("tool summary count != 2\nlogs:\n%s", got)
	}
	for _, want := range []string{
		"exchange_id=req_rebase",
		"decode_view=full",
		"decode_view=rebased_current",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("logs missing %q\nlogs:\n%s", want, got)
		}
	}
}

func TestResponsesDecodeLogsContentFreeCompactionAndCorrelationShape(t *testing.T) {
	restore, logs := captureResponsesDebugLogs()
	defer restore()

	raw := []byte(`{
		"model":"m",
		"tools":[{"type":"function","name":"private_tool","parameters":{"type":"object"}}],
		"input":[
			{"type":"function_call","call_id":"private_call","name":"private_tool","arguments":"{\"secret\":true}"},
			{"type":"function_call_output","call_id":"private_call","output":"private result"},
			{"type":"compaction","encrypted_content":"private opaque state"},
			{"type":"function_call_output","call_id":"orphan_call","output":"orphan private result"}
		]
	}`)
	_, _ = (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{
			Opaque: map[string]string{"exchange_id": "req_structure"},
		}),
	)

	got := logs.String()
	for _, want := range []string{
		"event=responses_input_structure",
		"exchange_id=req_structure",
		"decode_view=full",
		"item_count=4",
		"compaction_count=1",
		"first_compaction_item=2",
		"last_compaction_item=2",
		"tool_call_count=1",
		"tool_result_count=2",
		"repeated_pending_tool_call_count=0",
		"first_repeated_pending_tool_call_item=-1",
		"unmatched_tool_result_count=1",
		"first_unmatched_tool_result_item=3",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("logs missing %q\nlogs:\n%s", want, got)
		}
	}
	for _, secret := range []string{"private_call", "orphan_call", "private_tool", "private opaque state", "private result", "secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("logs exposed %q\nlogs:\n%s", secret, got)
		}
	}
}

func captureResponsesDebugLogs() (func(), *bytes.Buffer) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return func() { slog.SetDefault(previous) }, &logs
}
