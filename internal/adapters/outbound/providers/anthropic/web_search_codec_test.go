package anthropic

import (
	"bytes"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestAnthropicMessagesOwnsFinalWebSearchDialect(t *testing.T) {
	request := anthropicWebSearchRequest(t)
	codec := messagesCodec{Codec: protocolcodec.Codec{Protocol: protocolkind.Messages}}
	document, _, err := codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(document.RawBytes(), []byte(`"type":"web_search_20260209"`)) || !bytes.Contains(document.RawBytes(), []byte(`"allowed_callers":["direct"]`)) {
		t.Fatalf("final Anthropic JSON = %s", document.RawBytes())
	}
}

func anthropicWebSearchRequest(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	set, err := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	if err != nil {
		t.Fatal(err)
	}
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("search")})
	return canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{message}, Tools: canonical.Specify(set)})
}
