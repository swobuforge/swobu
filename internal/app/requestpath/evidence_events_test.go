package requestpath

import (
	"testing"

	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/ports"
)

func TestTokenUsageFromExecuteResponse_MapsBufferedOutputUsage(t *testing.T) {
	inputTokens := 80
	outputTokens := 6
	cacheReadTokens := 44
	cacheWriteTokens := 3
	usage, err := compatibility.NewTokenUsageWithOptional(&inputTokens, &outputTokens, &cacheReadTokens, &cacheWriteTokens)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}
	output := compatibility.NewConversationOutputWithUsage(
		"resp_1",
		"m",
		[]compatibility.CanonicalItem{compatibility.NewTextOutputItem("text_0", "ok")},
		"completed",
		usage,
	)
	resp := ports.NewBufferedExecuteResponse(output)

	mapped := tokenUsageFromExecuteResponse(resp)
	if got, ok := mapped.InputTokens(); !ok || got != 80 {
		t.Fatalf("input tokens = (%d,%v), want (80,true)", got, ok)
	}
	if got, ok := mapped.OutputTokens(); !ok || got != 6 {
		t.Fatalf("output tokens = (%d,%v), want (6,true)", got, ok)
	}
	if got, ok := mapped.CacheReadTokens(); !ok || got != 44 {
		t.Fatalf("cache read tokens = (%d,%v), want (44,true)", got, ok)
	}
	if got, ok := mapped.CacheWriteTokens(); !ok || got != 3 {
		t.Fatalf("cache write tokens = (%d,%v), want (3,true)", got, ok)
	}
}
