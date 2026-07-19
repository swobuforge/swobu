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

func TestDecodeClientRequestWithDecisions_RecordsToolCallIDAndKindScars(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"model":"gpt-4o-mini",
		"messages":[
			{"role":"user","tool_calls":[{"type":"function","function":{"name":"search","arguments":{"query":"hello"}}}]},
			{"role":"assistant","tool_calls":[{"type":"unsupported","id":"tc_2"}]}
		]
	}`)
	sink := &recordingDecisionSink{}

	_, _, err := (legacyClientRequestDecoder{}).DecodeClientRequestWithDecisions(carrier.Document{Family: protocolkind.ChatCompletions, Raw: raw}, sink, "ex_chatcompletions_decode")
	if err == nil {
		t.Fatal("expected DecodeClientRequestWithDecisions to reject unsupported tool call type")
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
		{feature: compat.RequestItemsToolUseID, outcome: compat.Approx, subject: compat.Subject("wire:/messages/0/tool_calls/0/id")},
		{feature: compat.RequestItemsToolType, outcome: compat.Reject, subject: compat.Subject("wire:/messages/1/tool_calls/0/type")},
	}
	for i, effectItem := range sink.effects {
		compatEffect := effectItem
		if compatEffect.Feature != want[i].feature || compatEffect.Outcome != want[i].outcome || compatEffect.Subject != want[i].subject {
			t.Fatalf("effect[%d] = %#v, want %s %s %q", i, compatEffect, want[i].feature, want[i].outcome, want[i].subject)
		}
	}
}

func TestDecodeClientRequestWithDecisions_RecordsToolCallArgumentsScar(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"model":"gpt-4o-mini",
		"messages":[
			{"role":"user","tool_calls":[{"type":"function","id":"tc_1","function":{"name":"search","arguments":"oops"}}]}
		]
	}`)
	sink := &recordingDecisionSink{}

	_, _, err := (legacyClientRequestDecoder{}).DecodeClientRequestWithDecisions(carrier.Document{Family: protocolkind.ChatCompletions, Raw: raw}, sink, "ex_chatcompletions_args")
	if err == nil {
		t.Fatal("expected DecodeClientRequestWithDecisions to reject invalid function arguments")
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
	compatEffect := sink.effects[0]
	if compatEffect.Feature != compat.RequestItemsToolInput || compatEffect.Outcome != compat.Reject {
		t.Fatalf("captured effect = %#v, want tool.call_arguments reject", compatEffect)
	}
	if compatEffect.Subject != compat.Subject("wire:/messages/0/tool_calls/0/function/arguments") {
		t.Fatalf("captured subject = %q, want wire:/messages/0/tool_calls/0/function/arguments", compatEffect.Subject)
	}
}
