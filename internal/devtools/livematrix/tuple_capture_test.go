package livematrix

import "testing"

func TestBuildRequest_ChatToolScenarioIncludesTools(t *testing.T) {
	_, path, payload, err := buildScenarioRequest(ScenarioCase{Protocol: "chat_completions", Scenario: "tool_min", Model: "m", Transport: "sse_streaming"})
	if err != nil {
		t.Fatalf("buildRequest returned error: %v", err)
	}
	if path != "/chat/completions" {
		t.Fatalf("path = %q, want %q", path, "/chat/completions")
	}
	if _, ok := payload["tools"]; !ok {
		t.Fatalf("payload missing tools: %#v", payload)
	}
	if stream, _ := payload["stream"].(bool); !stream {
		t.Fatalf("stream = %v, want true", payload["stream"])
	}
}

func TestResolveScenarioCase_UsesProviderDefaultsAndEnvModel(t *testing.T) {
	t.Setenv("OPENAI_MODEL", "gpt-4.1-mini")
	resolved, err := ResolveScenarioCase(ScenarioCase{ID: "x", Provider: "openai", ModelEnv: "OPENAI_MODEL"})
	if err != nil {
		t.Fatalf("ResolveScenarioCase returned error: %v", err)
	}
	if resolved.BaseURL == "" {
		t.Fatal("resolved base url empty")
	}
	if resolved.Model != "gpt-4.1-mini" {
		t.Fatalf("model = %q, want gpt-4.1-mini", resolved.Model)
	}
}
