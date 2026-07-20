package responses

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"strings"
	"testing"
)

func TestDecodeClientRequestRejectsMissingToolResultCallID(t *testing.T) {
	raw := []byte(`{"model":"m","tools":[{"type":"function","name":"search","parameters":{"type":"object"}}],"input":[{"type":"function_call","call_id":"call_1","name":"search","arguments":{"query":"hello"}},{"type":"function_call_output","output":"ok"}]}`)
	_, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	if err == nil || !strings.Contains(err.Error(), "call_id") {
		t.Fatalf("error=%v", err)
	}
}
