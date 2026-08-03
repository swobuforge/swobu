package routing

import "testing"

func TestCredentialLocatorsRequirePayload(t *testing.T) {
	constructors := map[string]func(string) error{
		"openai":        func(raw string) error { _, err := NewAPIKeyConnection(ProviderOpenAI, raw); return err },
		"anthropic":     func(raw string) error { _, err := NewAPIKeyConnection(ProviderAnthropic, raw); return err },
		"openrouter":    func(raw string) error { _, err := NewAPIKeyConnection(ProviderOpenRouter, raw); return err },
		"chatgpt":       func(raw string) error { _, err := NewAPIKeyConnection(ProviderChatGPT, raw); return err },
		"custom header": func(raw string) error { _, err := NewCustomHeaderAuth("Authorization", raw); return err },
	}
	for name, construct := range constructors {
		for _, raw := range []string{"env:", "file:", "secret:", "secretfile:"} {
			if err := construct(raw); err == nil {
				t.Errorf("%s accepted empty locator %q", name, raw)
			}
		}
	}
}

func TestCredentialLocatorsMatchResolverSyntax(t *testing.T) {
	for _, raw := range []string{"env:BAD NAME", "file:relative.txt", "secret:../escape", "secretfile:chatgpt//default"} {
		if _, err := NewAPIKeyConnection(ProviderOpenAI, raw); err == nil {
			t.Errorf("NewAPIKeyConnection(%q) unexpectedly succeeded", raw)
		}
	}
	for _, raw := range []string{"env:OPENAI_API_KEY", "file:/tmp/token", "file:~/.config/swobu/token", "secret:openai/default", "secretfile:chatgpt/plus/session_1"} {
		if _, err := NewAPIKeyConnection(ProviderOpenAI, raw); err != nil {
			t.Errorf("NewAPIKeyConnection(%q): %v", raw, err)
		}
	}
}

func TestZAIAccessIsClosedAndRequired(t *testing.T) {
	accesses := []struct {
		access  ZAIAccess
		baseURL string
	}{
		{ZAIAccessGeneralAPI, "https://api.z.ai/api/paas/v4"},
		{ZAIAccessCodingPlan, "https://api.z.ai/api/coding/paas/v4"},
	}
	if got := ZAIAccesses(); len(got) != len(accesses) {
		t.Fatalf("Z.AI accesses = %#v", got)
	}
	for _, test := range accesses {
		access := test.access
		parsed, err := ParseZAIAccess(string(access))
		if err != nil {
			t.Fatalf("ParseZAIAccess(%q): %v", access, err)
		}
		connection, err := NewZAIConnection(parsed, "env:ZAI_API_KEY")
		if err != nil {
			t.Fatalf("NewZAIConnection(%q): %v", access, err)
		}
		if connection.Access() != access || access.Label() == "" || connection.BaseURL() != test.baseURL {
			t.Fatalf("connection access projection = %#v, access = %q, want base URL %q", connection, access, test.baseURL)
		}
	}
	for _, raw := range []string{"", "default", "coding"} {
		if _, err := ParseZAIAccess(raw); err == nil {
			t.Errorf("ParseZAIAccess(%q) unexpectedly succeeded", raw)
		}
	}
	if got := ZAIAccess("future").Label(); got != "" {
		t.Fatalf("unknown access label = %q", got)
	}
	if got := (ZAIConnection{}).BaseURL(); got != "" {
		t.Fatalf("zero connection base URL = %q", got)
	}
	connection, err := NewZAIConnection(ZAIAccess(" coding_plan "), "env:ZAI_API_KEY")
	if err != nil {
		t.Fatalf("whitespace access: %v", err)
	}
	if connection.Access() != ZAIAccessCodingPlan || connection.BaseURL() != "https://api.z.ai/api/coding/paas/v4" {
		t.Fatalf("whitespace access was not normalized: %#v", connection)
	}
}
