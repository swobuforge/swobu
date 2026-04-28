package runtimeevidence

import "testing"

func TestTrafficEvent_ClonesAdaptationChain(t *testing.T) {
	requestID, err := ParseRequestID("req-1")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	route, err := NewRoute("backend-a", "m")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	inputTokens := 100
	outputTokens := 7
	cacheReadTokens := 64
	cacheWriteTokens := 4
	usage, err := NewTokenUsageWithOptional(&inputTokens, &outputTokens, &cacheReadTokens, &cacheWriteTokens)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}

	chain := []string{"compat", "responses"}
	event, err := NewTerminalTrafficEvent(TrafficEventInput{
		RequestID:           requestID,
		Endpoint:            "alpha",
		ClientProtocol:      "openai_compat",
		ClientHandler:       "codex",
		IngressFamily:       "chat_completions",
		NormalizedOp:        "/chat/completions",
		Route:               route,
		AdaptationChain:     chain,
		Result:              ResultClassSuccess,
		StatusCode:          200,
		TokenUsage:          usage,
		ModelRequested:      "client-model",
		ModelResolved:       "resolved-model",
		ModelResolutionMode: "default_missing",
	})
	if err != nil {
		t.Fatalf("NewTerminalTrafficEvent returned error: %v", err)
	}
	chain[0] = "mutated"
	if got := event.AdaptationChain()[0]; got != "compat" {
		t.Fatalf("adaptation chain[0] = %q, want %q", got, "compat")
	}
	if got := event.ModelRequested(); got != "client-model" {
		t.Fatalf("model requested = %q, want %q", got, "client-model")
	}
	if got := event.ModelResolved(); got != "resolved-model" {
		t.Fatalf("model resolved = %q, want %q", got, "resolved-model")
	}
	if got := event.ModelResolutionMode(); got != "default_missing" {
		t.Fatalf("model resolution mode = %q, want %q", got, "default_missing")
	}
	if got := event.ClientProtocol(); got != "openai_compat" {
		t.Fatalf("client protocol = %q, want %q", got, "openai_compat")
	}
	if got := event.ClientHandler(); got != "codex" {
		t.Fatalf("client handler = %q, want %q", got, "codex")
	}
	if got := event.IngressFamily(); got != "chat_completions" {
		t.Fatalf("ingress family = %q, want %q", got, "chat_completions")
	}
	if got := event.NormalizedOp(); got != "/chat/completions" {
		t.Fatalf("normalized op = %q, want %q", got, "/chat/completions")
	}
	if got, ok := event.TokenUsage().InputTokens(); !ok || got != 100 {
		t.Fatalf("token usage input = (%d,%v), want (100,true)", got, ok)
	}
	if got, ok := event.TokenUsage().OutputTokens(); !ok || got != 7 {
		t.Fatalf("token usage output = (%d,%v), want (7,true)", got, ok)
	}
	if got, ok := event.TokenUsage().CacheReadTokens(); !ok || got != 64 {
		t.Fatalf("token usage cache read = (%d,%v), want (64,true)", got, ok)
	}
	if got, ok := event.TokenUsage().CacheWriteTokens(); !ok || got != 4 {
		t.Fatalf("token usage cache write = (%d,%v), want (4,true)", got, ok)
	}
}

func TestTrafficEvent_RejectsTerminalInProgressResult(t *testing.T) {
	requestID, err := ParseRequestID("req-1")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	route, err := NewRoute("backend-a", "")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	if _, err := NewTerminalTrafficEvent(TrafficEventInput{
		RequestID:  requestID,
		Endpoint:   "alpha",
		Route:      route,
		Result:     ResultClassInProgress,
		StatusCode: 200,
	}); err == nil {
		t.Fatal("terminal events should reject in_progress result class")
	}
}

func TestTiming_RejectsDurationBeforeTTFB(t *testing.T) {
	if _, err := NewTiming(50, 10); err == nil {
		t.Fatal("NewTiming should reject duration before ttfb")
	}
}
