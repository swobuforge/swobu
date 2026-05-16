package requestpath

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/ports"
)

func TestTokenUsageFromExecuteResponse_DefaultsUnknownWithoutEnvelopeProjection(t *testing.T) {
	inputTokens := 80
	outputTokens := 6
	cacheReadTokens := 44
	cacheWriteTokens := 3
	usage, err := canonical.NewTokenUsageWithOptional(&inputTokens, &outputTokens, &cacheReadTokens, &cacheWriteTokens)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}
	output := canonical.NewConversationOutputWithUsage(
		"resp_1",
		"m",
		[]canonical.CanonicalItem{canonical.NewTextOutputItem("text_0", "ok")},
		"completed",
		usage,
	)
	resp := ports.NewBufferedProviderResponse(output)

	mapped := tokenUsageFromExecuteResponse(resp)
	if !mapped.IsZero() {
		t.Fatalf("usage = %#v, want unknown usage", mapped)
	}
}
