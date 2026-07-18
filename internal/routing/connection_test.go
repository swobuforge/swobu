package routing

import "testing"

func TestCredentialLocatorsRequirePayload(t *testing.T) {
	constructors := map[string]func(string) error{
		"openai":         func(raw string) error { _, err := NewOpenAIConnection(raw); return err },
		"anthropic":      func(raw string) error { _, err := NewAnthropicConnection(raw); return err },
		"openrouter":     func(raw string) error { _, err := NewOpenRouterConnection(raw); return err },
		"chatgpt":        func(raw string) error { _, err := NewChatGPTConnection(raw); return err },
		"bedrock bearer": func(raw string) error { _, err := NewBedrockBearerTokenAuth(raw); return err },
		"custom header":  func(raw string) error { _, err := NewCustomHeaderAuth("Authorization", raw); return err },
	}
	for name, construct := range constructors {
		for _, raw := range []string{"env:", "file:", "keychain:", "secret:", "secretfile:"} {
			if err := construct(raw); err == nil {
				t.Errorf("%s accepted empty locator %q", name, raw)
			}
		}
	}
}

func TestCredentialLocatorsMatchResolverSyntax(t *testing.T) {
	for _, raw := range []string{"env:BAD NAME", "file:relative.txt", "keychain:../escape", "secretfile:chatgpt//default"} {
		if _, err := NewOpenAIConnection(raw); err == nil {
			t.Errorf("NewOpenAIConnection(%q) unexpectedly succeeded", raw)
		}
	}
	for _, raw := range []string{"env:OPENAI_API_KEY", "file:/tmp/token", "file:~/.config/swobu/token", "keychain:openai/default", "secretfile:chatgpt/plus/session_1"} {
		if _, err := NewOpenAIConnection(raw); err != nil {
			t.Errorf("NewOpenAIConnection(%q): %v", raw, err)
		}
	}
}
