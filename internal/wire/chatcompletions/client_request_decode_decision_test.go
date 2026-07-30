package chatcompletions

import (
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestDecodeClientRequestWithChanges_DropsUnknownToolCallAndPreservesKnownSibling(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"model":"gpt-4o-mini",
		"tools":[{"type":"function","function":{"name":"search","parameters":{"type":"object"}}}],
		"messages":[
			{"role":"user","tool_calls":[{"type":"function","function":{"name":"search","arguments":{"query":"hello"}}}]},
			{"role":"assistant","content":"kept","tool_calls":[{"type":"unsupported","id":"tc_2"}]}
		]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	request := decoded.Request.Request
	if got := joinItemText(request.Items()); got != "kept" {
		t.Fatalf("surviving text = %q, want kept", got)
	}
	if len(decoded.Changes) != 2 {
		t.Fatalf("captured changes len=%d want=2", len(decoded.Changes))
	}
	want := []struct {
		feature canonical.CapabilityPath
		outcome compat.Kind
		item    uint32
	}{
		{feature: canonical.RequestItemsToolCallCallID, outcome: compat.Approximation, item: 0},
		{feature: canonical.RequestItemsToolCallTool, outcome: compat.Omission, item: 1},
	}
	for i, effectItem := range decoded.Changes {
		compatEffect := effectItem
		item, ok := compatEffect.Occurrence.RequestItem()
		if compatEffect.Capability != want[i].feature || compatEffect.Kind != want[i].outcome || !ok || item != want[i].item {
			t.Fatalf("change[%d] = %#v, want %s %d", i, compatEffect, want[i].feature, want[i].item)
		}
	}
}

func TestDecodeClientRequestWithChanges_DropsUnknownToolDeclarationBesideKnown(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-4o-mini",
		"tools":[
			{"type":"function","function":{"name":"search","parameters":{"type":"object"}}},
			{"type":"future_tool","name":"ignored"}
		],
		"messages":[{"role":"user","content":"run"}]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	request := decoded.Request.Request
	environment, err := canonical.EffectiveTools(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(environment.Declarations()) != 1 {
		t.Fatalf("surviving tools = %#v, want one", environment.Declarations())
	}
	if len(decoded.Changes) != 1 {
		t.Fatalf("compatibility changes = %#v", decoded.Changes)
	}
	index, ok := decoded.Changes[0].Occurrence.ToolIndex()
	if !ok || index != 1 {
		t.Fatalf("compatibility changes = %#v", decoded.Changes)
	}
}

func TestDecodeClientRequestWithChanges_RecordsToolCallArgumentsScar(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"model":"gpt-4o-mini",
		"messages":[
			{"role":"user","tool_calls":[{"type":"function","id":"tc_1","function":{"name":"search","arguments":"oops"}}]}
		]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: raw})
	if err == nil {
		t.Fatal("expected DecodeClientRequestWithChanges to reject invalid function arguments")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical.Error, got %T", err)
	}
	if compatErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeBadRequest)
	}
	if !strings.Contains(compatErr.Message, "arguments") {
		t.Fatalf("error message = %q, want arguments to be mentioned", compatErr.Message)
	}
	if len(decoded.Changes) != 0 {
		t.Fatalf("failed lowering returned successful changes: %#v", decoded.Changes)
	}
}
