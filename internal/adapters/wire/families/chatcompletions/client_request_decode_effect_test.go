package chatcompletions

import (
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
)

func TestDecodeClientRequestWithEffects_RecordsToolCallIDAndKindScars(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"model":"gpt-4o-mini",
		"messages":[
			{"role":"user","tool_calls":[{"type":"function","function":{"name":"search","arguments":{"query":"hello"}}}]},
			{"role":"assistant","tool_calls":[{"type":"unsupported","id":"tc_2"}]}
		]
	}`)
	sink := &recordingEffectSink{}

	_, _, err := (legacyClientRequestDecoder{}).DecodeClientRequestWithEffects(carrier.WireDocument{Family: protocolkind.ChatCompletions, Raw: raw}, sink, "ex_chatcompletions_decode")
	if err == nil {
		t.Fatal("expected DecodeClientRequestWithEffects to reject unsupported tool call type")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical.Error, got %T", err)
	}
	if compatErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeBadRequest)
	}
	if !strings.Contains(compatErr.Message, "unsupported tool call type") {
		t.Fatalf("error message = %q, want unsupported tool call type", compatErr.Message)
	}
	if len(sink.effects) != 2 {
		t.Fatalf("captured effects len=%d want=2", len(sink.effects))
	}
	want := []struct {
		feature compat.Feature
		outcome compat.Outcome
		subject compat.Subject
	}{
		{feature: compat.ToolCallID, outcome: compat.Approx, subject: compat.Subject("wire:/messages/0/tool_calls/0/id")},
		{feature: compat.ToolCallKind, outcome: compat.Reject, subject: compat.Subject("wire:/messages/1/tool_calls/0/type")},
	}
	for i, effectItem := range sink.effects {
		compatEffect, ok := effectItem.(effect.CompatibilityEffect)
		if !ok {
			t.Fatalf("effect[%d] type = %T, want effect.CompatibilityEffect", i, effectItem)
		}
		if compatEffect.Feature != want[i].feature || compatEffect.Outcome != want[i].outcome || compatEffect.Subject != want[i].subject {
			t.Fatalf("effect[%d] = %#v, want %s %s %q", i, compatEffect, want[i].feature, want[i].outcome, want[i].subject)
		}
	}
}

func TestDecodeClientRequestWithEffects_RecordsToolCallArgumentsScar(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"model":"gpt-4o-mini",
		"messages":[
			{"role":"user","tool_calls":[{"type":"function","id":"tc_1","function":{"name":"search","arguments":"oops"}}]}
		]
	}`)
	sink := &recordingEffectSink{}

	_, _, err := (legacyClientRequestDecoder{}).DecodeClientRequestWithEffects(carrier.WireDocument{Family: protocolkind.ChatCompletions, Raw: raw}, sink, "ex_chatcompletions_args")
	if err == nil {
		t.Fatal("expected DecodeClientRequestWithEffects to reject invalid function arguments")
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
	if len(sink.effects) != 1 {
		t.Fatalf("captured effects len=%d want=1", len(sink.effects))
	}
	compatEffect, ok := sink.effects[0].(effect.CompatibilityEffect)
	if !ok {
		t.Fatalf("captured effect type = %T, want effect.CompatibilityEffect", sink.effects[0])
	}
	if compatEffect.Feature != compat.ToolCallArguments || compatEffect.Outcome != compat.Reject {
		t.Fatalf("captured effect = %#v, want tool.call_arguments reject", compatEffect)
	}
	if compatEffect.Subject != compat.Subject("wire:/messages/0/tool_calls/0/function/arguments") {
		t.Fatalf("captured subject = %q, want wire:/messages/0/tool_calls/0/function/arguments", compatEffect.Subject)
	}
}
