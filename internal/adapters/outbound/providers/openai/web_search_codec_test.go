package openai

import (
	"bytes"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestOpenAIResponsesKeepsStandardWebSearchDialect(t *testing.T) {
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("search")})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{message}, Tools: canonical.Specify(set)})
	document, _, err := (protocolcodec.Codec{Protocol: protocolkind.Responses}).Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(document.RawBytes(), []byte(`"tools":[{"type":"web_search"}]`)) {
		t.Fatalf("final OpenAI JSON = %s", document.RawBytes())
	}
}
