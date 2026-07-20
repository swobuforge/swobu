package wire_test

import (
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/wire"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
	"github.com/swobuforge/swobu/internal/wire/messages"
	"github.com/swobuforge/swobu/internal/wire/responses"
)

func TestToolKeyDoesNotInheritClientToolOrdinalAcrossProtocols(t *testing.T) {
	cases := []struct {
		family  protocolkind.ProtocolKind
		decoder interface {
			DecodeClientRequest(carrier.Document) (wire.ClientDecodeResult, error)
		}
		raw []byte
	}{
		{protocolkind.Responses, responses.ClientRequestDecoder{}, []byte(`{"model":"m","tools":[{"type":"function","name":"other","parameters":{"type":"object"}},{"type":"function","name":"search","parameters":{"type":"object"}}],"input":"hello"}`)},
		{protocolkind.ChatCompletions, chatcompletions.ClientRequestDecoder{}, []byte(`{"model":"m","tools":[{"type":"function","function":{"name":"search","parameters":{"type":"object"}}},{"type":"function","function":{"name":"other","parameters":{"type":"object"}}}],"messages":[{"role":"user","content":"hello"}]}`)},
		{protocolkind.Messages, messages.ClientRequestDecoder{}, []byte(`{"model":"m","tools":[{"name":"other","input_schema":{"type":"object"}},{"name":"search","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hello"}]}`)},
	}
	var want string
	for _, test := range cases {
		decoded, err := test.decoder.DecodeClientRequest(carrier.Document{Family: test.family, Raw: test.raw})
		if err != nil {
			t.Fatalf("%s decode: %v", test.family, err)
		}
		got := ""
		for _, declaration := range decoded.Request.Request.Tools() {
			if declaration.Key().Name() == "search" {
				got = declaration.Key().String()
			}
		}
		if got == "" {
			t.Fatalf("%s lost search tool", test.family)
		}
		if want == "" {
			want = got
		} else if got != want {
			t.Fatalf("%s search tool id = %q, want %q", test.family, got, want)
		}
	}
}
