package responses

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestDecodeRequest_IgnoresUnknownField(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"gpt-4o-mini","input":"hi","unexpected":true}`)
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
	if items[0].Text != "hi" {
		t.Fatalf("item text = %q, want %q", items[0].Text, "hi")
	}
}

func TestDecodeRequest_PreservesCustomToolFormatField(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	wantFormat := `{"type":"grammar", "syntax":"lark", "definition":"start: \"x\" LF\n%import common.LF"}`
	customTool := canonical.NewCustomToolDecl("apply_patch", "apply_patch", "edit files", canonical.NewToolFormatObject(wantFormat))
	req := []byte(`{"model":"gpt-4o-mini","input":"hi","tools":[{"type":"custom","name":"` + customTool.ToolName() + `","format":` + wantFormat + `}]}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest: %v", err)
	}
	if got.Model() != "gpt-4o-mini" {
		t.Fatalf("model = %q, want %q", got.Model(), "gpt-4o-mini")
	}
	tools := got.Tools()
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	custom, ok := tools[0].(canonical.CustomToolDecl)
	if !ok {
		t.Fatalf("tool = %T, want CustomToolDecl", tools[0])
	}
	if custom.ToolName() != "apply_patch" {
		t.Fatalf("tool name = %q, want %q", custom.ToolName(), "apply_patch")
	}
	if custom.Format.RawObject() != wantFormat {
		t.Fatalf("custom format raw = %q, want %q", custom.Format.RawObject(), wantFormat)
	}
}

func TestDecodeRequest_PreservesTopLevelInstructions(t *testing.T) {
	codec := legacyClientRequestDecoder{}
	req := []byte(`{"model":"gpt-4o-mini","instructions":"Use tools for filesystem work.","input":"inspect files"}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest: %v", err)
	}
	if got.Instructions() != "Use tools for filesystem work." {
		t.Fatalf("instructions = %q, want top-level instructions", got.Instructions())
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
