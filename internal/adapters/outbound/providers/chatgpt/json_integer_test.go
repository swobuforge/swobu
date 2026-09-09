package chatgpt

import (
	"bytes"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestBackendCodecPreservesRawJSONIntegers(t *testing.T) {
	request := canonicaltest.LargeIntegerRequest(t, "gpt-5.4-mini")
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := newBackendCodec("chatgpt").Encode(provider.Request{
		Canonical: request,
		ToolNames: names,
		Delivery:  delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(document.RawBytes(), []byte("9007199254740993")); got != 3 {
		t.Fatalf("large integer occurrences = %d, want 3: %s", got, document.RawBytes())
	}
}
