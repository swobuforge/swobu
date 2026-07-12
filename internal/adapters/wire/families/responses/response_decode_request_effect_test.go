package responses

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

func TestDecodeClientRequestWithEffects_RecordsResponsesRequestScars(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"model":"gpt-4o-mini",
		"input":[
			{"content":"hello"},
			{"type":"function_call","name":"search","arguments":{"query":"hello"}},
			{"type":"function_call_output","output":"ok"}
		]
	}`)
	sink := &recordingEffectSink{}

	_, _, err := (legacyClientRequestDecoder{}).DecodeClientRequestWithEffects(carrier.WireDocument{Family: protocolkind.Responses, Raw: raw}, sink, "ex_responses_decode")
	if err == nil {
		t.Fatal("expected DecodeClientRequestWithEffects to reject missing function_call_output call_id")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical.Error, got %T", err)
	}
	if compatErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeBadRequest)
	}
	if !strings.Contains(compatErr.Message, "call_id") {
		t.Fatalf("error message = %q, want call_id to be mentioned", compatErr.Message)
	}
	if len(sink.effects) != 4 {
		t.Fatalf("captured effects len=%d want=4", len(sink.effects))
	}
	want := []struct {
		feature compat.Feature
		outcome compat.Outcome
		subject compat.Subject
	}{
		{feature: compat.RequestInputShape, outcome: compat.Approx, subject: compat.Subject("wire:/input/0/type")},
		{feature: compat.RequestRole, outcome: compat.Approx, subject: compat.Subject("wire:/input/0/role")},
		{feature: compat.ToolCallID, outcome: compat.Approx, subject: compat.Subject("wire:/input/1/call_id")},
		{feature: compat.ToolResultID, outcome: compat.Reject, subject: compat.Subject("wire:/input/2/call_id")},
	}
	for i, effectItem := range sink.effects {
		compatEffect, ok := effectItem.(effect.Compatibility)
		if !ok {
			t.Fatalf("effect[%d] type = %T, want effect.Compatibility", i, effectItem)
		}
		if compatEffect.Feature != want[i].feature || compatEffect.Outcome != want[i].outcome || compatEffect.Subject != want[i].subject {
			t.Fatalf("effect[%d] = %#v, want %s %s %q", i, compatEffect, want[i].feature, want[i].outcome, want[i].subject)
		}
	}
}
