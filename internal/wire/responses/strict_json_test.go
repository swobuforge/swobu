package responses

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestDecodeAndReencodeRequest_DropsUnknownWireField(t *testing.T) {
	codec := testClientRequestDecoder{}
	req := []byte(`{"model":"gpt-4o-mini","input":"hi","future_vendor_knob":{"enabled":true,"value":123}}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest() error = %v", err)
	}
	if got.Model() != "gpt-4o-mini" {
		t.Fatalf("model = %q, want %q", got.Model(), "gpt-4o-mini")
	}
	items := got.Items()
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	message, _ := items[0].Message()
	text, _ := message.Content()[0].Text()
	if text.Text() != "hi" {
		t.Fatalf("item text = %q, want %q", text.Text(), "hi")
	}
	providerDocument, err := EncodeCarrier(got, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(providerDocument.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["future_vendor_knob"]; exists {
		t.Fatalf("unknown client field leaked to provider request: %s", providerDocument.RawBytes())
	}
	if payload["model"] != "gpt-4o-mini" {
		t.Fatalf("known Responses model changed: %#v", payload)
	}
}

func TestDecodeRequest_PreservesCustomToolFormatField(t *testing.T) {
	codec := testClientRequestDecoder{}
	wantFormat := `{"type":"grammar", "syntax":"lark", "definition":"start: \"x\" LF\n%import common.LF"}`
	customTool := canonicaltest.MustCustomTool(canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "apply_patch"), "edit files", canonical.NewToolFormatObject(canonicaltest.Object(t, wantFormat)))
	req := []byte(`{"model":"gpt-4o-mini","input":"hi","tools":[{"type":"custom","name":"` + customTool.Key().Name() + `","format":` + wantFormat + `}]}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest: %v", err)
	}
	if got.Model() != "gpt-4o-mini" {
		t.Fatalf("model = %q, want %q", got.Model(), "gpt-4o-mini")
	}
	tools := canonicaltest.Tools(got)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	custom, ok := tools[0].Custom()
	if !ok {
		t.Fatalf("tool = %T, want CustomTool declaration", tools[0])
	}
	if custom.Key().Name() != "apply_patch" {
		t.Fatalf("tool name = %q, want %q", custom.Key().Name(), "apply_patch")
	}
	if custom.Format().RawObject() != canonicaltest.Object(t, wantFormat).String() {
		t.Fatalf("custom format raw = %q, want canonical object", custom.Format().RawObject())
	}
}

func TestDecodeRequest_PreservesTopLevelInstructions(t *testing.T) {
	codec := testClientRequestDecoder{}
	req := []byte(`{"model":"gpt-4o-mini","instructions":"Use tools for filesystem work.","input":"inspect files"}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest: %v", err)
	}
	if canonicaltest.DirectiveText(got.Items()) != "Use tools for filesystem work." {
		t.Fatalf("instructions = %q, want top-level instructions", canonicaltest.DirectiveText(got.Items()))
	}
}

func assertJSONEqual(t *testing.T, gotRaw, wantRaw string) {
	t.Helper()
	var got any
	var want any
	if err := json.Unmarshal([]byte(gotRaw), &got); err != nil {
		t.Fatalf("json.Unmarshal(got): %v", err)
	}
	if err := json.Unmarshal([]byte(wantRaw), &want); err != nil {
		t.Fatalf("json.Unmarshal(want): %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("json mismatch\ngot:  %s\nwant: %s", gotRaw, wantRaw)
	}
}
