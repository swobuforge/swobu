package configstore

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
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
              - {id: deepseek, model: deepseek-v4-pro, connection: {deepseek: {credential: env:DEEPSEEK_API_KEY}}}
              - {id: openrouter, model: openai/gpt-5, protocol: chat_completions, connection: {openrouter: {credential: secret:openrouter/default}}}
              - {id: zai, model: manual-model, connection: {zai: {access: coding_plan, credential: env:ZAI_API_KEY}}}
              - {id: chatgpt, model: gpt-5, connection: {chatgpt: {credential: secretfile:cockpit/auth/chatgpt/default}}}
              - {id: ollama, model: llama, protocol: chat_completions, connection: {ollama: {}}}
              - {id: lm-studio, model: local-model, protocol: responses, connection: {lmstudio: {credential: env:LM_API_TOKEN}}}
              - {id: vllm, model: served-model, protocol: messages_stream, connection: {vllm: {base_url: https://inference.example/v1, credential: env:VLLM_API_KEY}}}
              - {id: azure, model: deployment, protocol: responses, connection: {azure: {project_endpoint: https://example.services.ai.azure.com/api/projects/prod, credential: env:AZURE_OPENAI_API_KEY}}}
              - {id: bedrock, model: openai.gpt, protocol: responses_stream, connection: {bedrock: {region: eu-west-2, endpoint: https://bedrock-mantle.eu-west-2.api.aws/openai/v1, credential: env:BEDROCK_API_KEY}}}
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
	if strings.Contains(string(raw), "protocol: chat_completions_stream") {
		t.Fatalf("derived Z.AI protocol was persisted:\n%s", raw)
	}
	if !strings.Contains(string(raw), "deepseek:") || strings.Count(string(raw), "protocol: messages_stream") != 1 {
		t.Fatalf("DeepSeek connection or derived protocol persistence is wrong:\n%s", raw)
	}
	if !strings.Contains(string(raw), "lmstudio:") || !strings.Contains(string(raw), "credential: env:LM_API_TOKEN") {
		t.Fatalf("LM Studio connection changed during round trip:\n%s", raw)
	}
	if !strings.Contains(string(raw), "vllm:") || !strings.Contains(string(raw), "credential: env:VLLM_API_KEY") {
		t.Fatalf("vLLM connection changed during round trip:\n%s", raw)
	}
}

func TestCodecRejectsAuthoredDeepSeekProtocol(t *testing.T) {
	raw := strings.Replace(
		allVariantsYAML,
		"id: deepseek, model: deepseek-v4-pro, connection:",
		"id: deepseek, model: deepseek-v4-pro, protocol: messages_stream, connection:",
		1,
	)
	if _, err := decode([]byte(raw)); err == nil || !strings.Contains(err.Error(), "provider protocol is derived and must be omitted") {
		t.Fatalf("decode error = %v, want authored DeepSeek protocol rejection", err)
	}
}

func TestCodecRejectsZAIWithoutExplicitAccess(t *testing.T) {
	raw := strings.Replace(allVariantsYAML, "access: coding_plan, ", "", 1)
	if _, err := decode([]byte(raw)); err == nil || !strings.Contains(err.Error(), "connection.zai.access") {
		t.Fatalf("decode error = %v, want missing Z.AI access rejection", err)
	}
	raw = strings.Replace(allVariantsYAML, "access: coding_plan", "access: enterprise", 1)
	if _, err := decode([]byte(raw)); err == nil || !strings.Contains(err.Error(), "connection.zai.access") {
		t.Fatalf("decode error = %v, want unknown Z.AI access rejection", err)
	}
}

func TestCodecRejectsMultipleAPIKeyProviderVariants(t *testing.T) {
	raw := strings.Replace(
		allVariantsYAML,
		"connection: {openai: {credential: env:OPENAI_API_KEY}}",
		"connection: {openai: {credential: env:OPENAI_API_KEY}, deepseek: {credential: env:DEEPSEEK_API_KEY}}",
		1,
	)
	if _, err := decode([]byte(raw)); err == nil || !strings.Contains(err.Error(), "exactly one provider variant") {
		t.Fatalf("decode error = %v, want multiple provider variant rejection", err)
	}
}

func TestCodecRejectsEveryAuthoredZAIProtocol(t *testing.T) {
	for _, protocol := range []string{"chat_completions_stream", "responses"} {
		raw := strings.Replace(
			allVariantsYAML,
			"id: zai, model: manual-model, connection:",
			"id: zai, model: manual-model, protocol: "+protocol+", connection:",
			1,
		)
		if _, err := decode([]byte(raw)); err == nil || !strings.Contains(err.Error(), "provider protocol is derived and must be omitted") {
			t.Fatalf("protocol %q decode error = %v, want authored derived protocol rejection", protocol, err)
		}
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

// bedrockTarget navigates the decoded config to the Bedrock target of the
// single workspace/route/tier in the variant fixtures below.
func bedrockTarget(t *testing.T, config routing.Config) routing.BedrockConnection {
	t.Helper()
	workspaces := config.Workspaces()
	if len(workspaces) != 1 {
		t.Fatalf("workspace count = %d", len(workspaces))
	}
	routes := workspaces[0].Routes()
	if len(routes) != 1 {
		t.Fatalf("route count = %d", len(routes))
	}
	tiers := routes[0].Tiers()
	if len(tiers) != 1 {
		t.Fatalf("tier count = %d", len(tiers))
	}
	for _, target := range tiers[0].Targets() {
		if connection, ok := target.Connection().(routing.BedrockConnection); ok {
			return connection
		}
	}
	t.Fatalf("no Bedrock target decoded")
	return routing.BedrockConnection{}
}

// An authored endpoint is a durable first-class fact: it round-trips verbatim
// through decode -> encode -> decode, and re-encoding persists the value rather
// than collapsing it back to a region-derived default.
func TestCodecRoundTripsAuthoredBedrockEndpoint(t *testing.T) {
	const endpoint = "https://bedrock-mantle.eu-west-2.api.aws/openai/v1"
	raw := strings.Replace(
		allVariantsYAML,
		"{id: bedrock, model: openai.gpt, protocol: responses_stream, connection: {bedrock: {region: eu-west-2, credential: env:BEDROCK_API_KEY}}}",
		"{id: bedrock, model: openai.gpt, protocol: responses_stream, connection: {bedrock: {region: eu-west-2, endpoint: "+endpoint+", credential: env:BEDROCK_API_KEY}}}",
		1,
	)
	config, err := decode([]byte(raw))
	if err != nil {
		t.Fatalf("decode authored endpoint: %v", err)
	}
	if connection := bedrockTarget(t, config); connection.Endpoint() != endpoint {
		t.Fatalf("decoded endpoint = %q, want %q", connection.Endpoint(), endpoint)
	}
	encoded, err := encode(config)
	if err != nil {
		t.Fatalf("encode authored endpoint: %v", err)
	}
	if !strings.Contains(string(encoded), "endpoint: "+endpoint) {
		t.Fatalf("encoded YAML dropped authored endpoint:\n%s", encoded)
	}
	decoded, err := decode(encoded)
	if err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	if connection := bedrockTarget(t, decoded); connection.Endpoint() != endpoint {
		t.Fatalf("round-tripped endpoint = %q, want %q", connection.Endpoint(), endpoint)
	}
}

// A legacy region-only Bedrock target is no longer executable because its
// inference namespace is unknown. Decode fails rather than inventing `/v1`.
func TestCodecRejectsLegacyBedrockConfigWithoutEndpoint(t *testing.T) {
	legacy := strings.Replace(allVariantsYAML, "endpoint: https://bedrock-mantle.eu-west-2.api.aws/openai/v1, ", "", 1)
	if _, err := decode([]byte(legacy)); err == nil {
		t.Fatal("decoded legacy Bedrock target without an endpoint")
	}
}
