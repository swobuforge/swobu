package configstore

import (
	"strings"
	"testing"
)

const allVariantsYAML = `schema_version: 1
workspaces:
  dev:
    default_route: chat
    routes:
      chat:
        tiers:
          - targets:
              - {id: openai, model: gpt-5, protocol: responses, connection: {openai: {credential: env:OPENAI_API_KEY}}}
              - {id: anthropic, model: claude, protocol: messages, connection: {anthropic: {credential: env:ANTHROPIC_API_KEY}}}
              - {id: openrouter, model: openai/gpt-5, protocol: chat_completions, connection: {openrouter: {credential: secret:openrouter/default}}}
              - {id: chatgpt, model: gpt-5, protocol: responses_stream, connection: {chatgpt: {credential: secretfile:cockpit/auth/chatgpt/default}}}
              - {id: ollama, model: llama, protocol: chat_completions, connection: {ollama: {}}}
              - {id: azure, model: deployment, protocol: responses, connection: {azure: {project_endpoint: https://example.services.ai.azure.com/api/projects/prod, credential: env:AZURE_OPENAI_API_KEY}}}
              - {id: bedrock, model: openai.gpt, protocol: responses_stream, connection: {bedrock: {region: eu-west-2, credential: env:BEDROCK_API_KEY}}}
              - {id: custom, model: local, protocol: chat_completions, connection: {custom: {base_url: http://127.0.0.1:8080/v1, auth: {header: {name: x-api-key, credential: env:CUSTOM_KEY}}}}}
`

func TestCodecRoundTripCoversEveryConnectionVariant(t *testing.T) {
	config, err := decode([]byte(allVariantsYAML))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encode(config)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decode(raw)
	if err != nil {
		t.Fatalf("decode encoded config: %v\n%s", err, raw)
	}
	if decoded.WorkspaceCount() != 1 {
		t.Fatalf("workspace count = %d", decoded.WorkspaceCount())
	}
	if strings.Contains(string(raw), "target_rank") {
		t.Fatalf("forbidden field emitted:\n%s", raw)
	}
	if !strings.Contains(string(raw), "custom:") || strings.Contains(string(raw), "openai_"+"compatible") {
		t.Fatalf("custom connection identity changed during round trip:\n%s", raw)
	}
}

func TestCodecAcceptsEmptyInstallation(t *testing.T) {
	config, err := decode([]byte("schema_version: 1\nworkspaces: {}\n"))
	if err != nil || config.WorkspaceCount() != 0 {
		t.Fatalf("config = %#v, error = %v", config, err)
	}
}

func TestCodecRejectsExplicitEmptyCustomAuth(t *testing.T) {
	raw := strings.Replace(allVariantsYAML, "auth: {header: {name: x-api-key, credential: env:CUSTOM_KEY}}", "auth: {}", 1)
	if _, err := decode([]byte(raw)); err == nil || !strings.Contains(err.Error(), "connection.custom.auth") {
		t.Fatalf("decode error = %v, want connection.custom.auth rejection", err)
	}
}

func TestCodecRejectsUnknownDuplicateAndLegacyFields(t *testing.T) {
	tests := map[string]string{
		"unknown root":   "schema_version: 1\nworkspaces: {}\nunknown: true\n",
		"startup field":  "schema_version: 1\nserver: {listen: '127.0.0.1:7926'}\nworkspaces: {}\n",
		"unknown nested": strings.Replace(allVariantsYAML, "credential: env:OPENAI_API_KEY", "credential: env:OPENAI_API_KEY, base_url: https://invalid.test", 1),
		"duplicate":      "schema_version: 1\nschema_version: 1\nworkspaces: {}\n",
		"legacy":         "bind_addr: 127.0.0.1:7926\nendpoints: []\n",
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := decode([]byte(raw)); err == nil {
				t.Fatal("decode unexpectedly succeeded")
			}
		})
	}
}

func TestDecodeUnknownFieldsDoNotLeakInternalTypeName(t *testing.T) {
	_, err := decode([]byte("bind_addr: 127.0.0.1:7926\nendpoints: []\n"))
	if err == nil {
		t.Fatal("decode unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), "documentDTO") {
		t.Fatalf("decode error leaks internal type name: %v", err)
	}
	if !strings.Contains(err.Error(), "bind_addr") || !strings.Contains(err.Error(), "endpoints") {
		t.Fatalf("decode error does not name the offending fields: %v", err)
	}
}

func TestCodecOutputIsDeterministicAndSortsBalancedTargets(t *testing.T) {
	config, err := decode([]byte(allVariantsYAML))
	if err != nil {
		t.Fatal(err)
	}
	first, _ := encode(config)
	second, _ := encode(config)
	if string(first) != string(second) {
		t.Fatal("encoding is nondeterministic")
	}
	if strings.Index(string(first), "id: anthropic") > strings.Index(string(first), "id: azure") {
		t.Fatalf("targets are not sorted by ID:\n%s", first)
	}
	if len(first) == 0 || first[len(first)-1] != '\n' {
		t.Fatal("encoded YAML lacks newline at EOF")
	}
}
