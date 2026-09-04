package configstore

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestCredentialProvenFreeProviderExampleStaysWithinLiveEvidence(t *testing.T) {
	raw, err := os.ReadFile("../../examples/free-provider-route/swobu.yaml")
	if err != nil {
		t.Fatal(err)
	}
	config, err := decode(raw)
	if err != nil {
		t.Fatalf("decode free-provider example: %v", err)
	}
	slug, _ := routing.ParseWorkspaceSlug("free-demo")
	workspace, ok := config.Workspace(slug)
	if !ok {
		t.Fatal("free-demo workspace is missing")
	}
	if got := workspace.DefaultRoute().String(); got != "free" {
		t.Fatalf("default route = %q, want free", got)
	}
	routeName, _ := routing.ParseRouteName("free")
	route, ok := workspace.Route(routeName)
	if !ok {
		t.Fatal("free route is missing")
	}

	want := []struct {
		provider   string
		model      string
		credential string
	}{
		{provider: "nvidia", model: "nvidia/nemotron-mini-4b-instruct", credential: "env:NVIDIA_API_KEY"},
		{provider: "workersai", model: "@cf/google/gemma-4-26b-a4b-it", credential: "env:CLOUDFLARE_API_TOKEN"},
		{provider: "openrouter", model: "openrouter/free", credential: "env:OPENROUTER_API_KEY"},
		{provider: "groq", model: "openai/gpt-oss-20b", credential: "env:GROQ_API_KEY"},
		{provider: "mistral", model: "ministral-3b-2512", credential: "env:MISTRAL_API_KEY"},
		{provider: "llm7", model: "default", credential: "env:LLM7_API_KEY"},
		{provider: "cerebras", model: "gemma-4-31b", credential: "env:CEREBRAS_API_KEY"},
	}
	tiers := route.Tiers()
	if len(tiers) != 1 {
		t.Fatalf("tier count = %d, want 1", len(tiers))
	}
	targets := tiers[0].Targets()
	if len(targets) != len(want) {
		t.Fatalf("target count = %d, want %d", len(targets), len(want))
	}
	for index, expected := range want {
		target := targets[index]
		if provider := string(target.Provider()); provider != expected.provider {
			t.Fatalf("target %d provider = %q, want %q", index+1, provider, expected.provider)
		}
		if model := target.Model().String(); model != expected.model {
			t.Fatalf("target %d model = %q, want %q", index+1, model, expected.model)
		}
		connection, ok := target.Connection().(routing.StandardConnection)
		if !ok {
			t.Fatalf("target %d connection = %T, want routing.StandardConnection", index+1, target.Connection())
		}
		if credential := connection.Credential().String(); credential != expected.credential {
			t.Fatalf("target %d credential = %q, want %q", index+1, credential, expected.credential)
		}
		if string(target.Provider()) == "gemini" {
			t.Fatalf("target %d contains excluded Gemini provider", index+1)
		}
	}
}

const allVariantsYAML = `schema_version: 1
workspaces:
  dev:
    default_route: chat
    routes:
      chat:
        tiers:
          - targets:
              - {id: openai, model: gpt-5, protocol: responses, connection: {openai: {credential: env:OPENAI_API_KEY}}}
              - {id: meta, model: muse-spark-1.2, connection: {meta: {credential: env:MODEL_API_KEY}}}
              - {id: anthropic, model: claude, protocol: messages, connection: {anthropic: {credential: env:ANTHROPIC_API_KEY}}}
              - {id: deepseek, model: deepseek-v4-pro, connection: {deepseek: {credential: env:DEEPSEEK_API_KEY}}}
              - {id: kimi, model: kimi-k3, connection: {kimi: {credential: env:MOONSHOT_API_KEY}}}
              - {id: mistral, model: mistral-small-latest, connection: {mistral: {base_url: https://api.eu.mistral.ai/v1, credential: env:MISTRAL_API_KEY}}}
              - {id: cerebras, model: my-org-gpt-oss-120b, connection: {cerebras: {credential: env:CEREBRAS_API_KEY}}}
              - {id: workersai, model: '@cf/meta/example', protocol: responses_stream, connection: {workersai: {base_url: https://api.cloudflare.com/client/v4/accounts/account-id/ai/v1, credential: env:CLOUDFLARE_API_TOKEN}}}
              - {id: llm7, model: fast, connection: {llm7: {credential: env:LLM7_API_KEY}}}
              - {id: nvidia, model: publisher/model, connection: {nvidia: {credential: env:NVIDIA_API_KEY}}}
              - {id: ovhcloud, model: provider-selected-model, connection: {ovhcloud: {credential: env:OVH_AI_ENDPOINTS_ACCESS_TOKEN}}}
              - {id: modelscope, model: 'ZhipuAI/GLM-5.1:DashScope', connection: {modelscope: {credential: env:MODELSCOPE_TOKEN}}}
              - {id: compactifai, model: quasar-438b, protocol: chat_completions, connection: {compactifai: {credential: env:COMPACTIFAI_API_KEY}}}
              - {id: runpod, model: served-model, protocol: responses_stream, connection: {runpod: {base_url: abc123, credential: env:RUNPOD_API_KEY}}}
              - {id: together, model: zai-org/GLM-5.1, connection: {together: {credential: env:TOGETHER_API_KEY}}}
              - {id: deepinfra, model: deploy_id:private, connection: {deepinfra: {credential: env:DEEPINFRA_TOKEN}}}
              - {id: scaleway, model: served-model, protocol: responses_stream, connection: {scaleway: {base_url: https://dedicated.example/v1}}}
              - {id: sambanova, model: served-model, protocol: messages_stream, connection: {sambanova: {base_url: https://stack.example/v1, credential: env:SAMBANOVA_API_KEY}}}
              - {id: stepfun, model: step-router-v1, protocol: messages_stream, connection: {stepfun: {base_url: https://api.stepfun.com/step_plan/v1, credential: env:STEP_API_KEY}}}
              - {id: friendli, model: ENDPOINT_ID:OPTIONAL_ADAPTER_ROUTE, protocol: messages_stream, connection: {friendli: {base_url: https://friendli-gateway.example/v1}}}
              - {id: nebius, model: dedicated-routing-key, protocol: responses_stream, connection: {nebius: {base_url: https://api.tokenfactory.us-central1.nebius.com/v1, credential: env:NEBIUS_API_KEY}}}
              - {id: gmi, model: exact-model, protocol: messages, connection: {gmi: {base_url: https://gmi.example/v1, credential: env:GMI_API_KEY}}}
              - {id: gemini, model: operator-selected-model, connection: {gemini: {credential: env:GEMINI_API_KEY}}}
              - {id: groq, model: served-model, protocol: responses_stream, connection: {groq: {credential: env:GROQ_API_KEY}}}
              - {id: fireworks, model: accounts/acme/deployments/deploy-1, protocol: responses_stream, connection: {fireworks: {base_url: https://direct.example/v1, credential: env:FIREWORKS_API_KEY}}}
              - {id: novita, model: deployment/model, connection: {novita: {base_url: https://deployment.example/v1, credential: env:NOVITA_API_KEY}}}
              - {id: baseten, model: served-model, protocol: messages_stream, connection: {baseten: {base_url: https://deployment.example/v1, credential: env:BASETEN_API_KEY}}}
              - {id: hyperbolic, model: exact-model, connection: {hyperbolic: {credential: env:HYPERBOLIC_API_KEY}}}
              - {id: siliconflow, model: Pro/model, protocol: messages_stream, connection: {siliconflow: {credential: env:SILICONFLOW_API_KEY}}}
              - {id: openrouter, model: openai/gpt-5, protocol: chat_completions, connection: {openrouter: {credential: secret:openrouter/default}}}
              - {id: zai, model: manual-model, connection: {zai: {access: coding_plan, credential: env:ZAI_API_KEY}}}
              - {id: chatgpt, model: gpt-5, connection: {chatgpt: {credential: secretfile:cockpit/auth/chatgpt/default}}}
              - {id: ollama, model: llama, protocol: chat_completions, connection: {ollama: {}}}
              - {id: lm-studio, model: local-model, protocol: responses, connection: {lmstudio: {credential: env:LM_API_TOKEN}}}
              - {id: vllm, model: served-model, protocol: messages_stream, connection: {vllm: {base_url: https://inference.example/v1, credential: env:VLLM_API_KEY}}}
              - {id: azure, model: deployment, protocol: responses, connection: {azure: {project_endpoint: https://example.services.ai.azure.com/api/projects/prod, credential: env:AZURE_OPENAI_API_KEY}}}
              - {id: bedrock, model: openai.gpt, protocol: responses_stream, connection: {bedrock: {region: eu-west-2, endpoint: https://bedrock-mantle.eu-west-2.api.aws/openai/v1, credential: env:BEDROCK_API_KEY}}}
              - {id: custom, model: local, protocol: chat_completions, connection: {custom: {base_url: http://127.0.0.1:8080/v1, auth: {header: {name: x-api-key, credential: env:CUSTOM_KEY}}}}}
              - {id: opencode-zen, model: default, protocol: chat_completions, connection: {opencode-zen: {credential: env:OPENCODE_ZEN_API_KEY}}}
              - {id: nous, model: default, protocol: chat_completions, connection: {nous: {credential: env:NOUS_API_KEY}}}
              - {id: commandcode, model: default, protocol: chat_completions, connection: {commandcode: {credential: env:COMMANDCODE_API_KEY}}}
              - {id: venice, model: default, protocol: chat_completions, connection: {venice: {credential: env:VENICE_API_KEY}}}
`

func TestCodecRoundTripCoversEveryConnectionVariant(t *testing.T) {
	config, err := decode([]byte(allVariantsYAML))
	if err != nil {
		t.Fatal(err)
	}
	assertConfigCoversEveryProfile(t, config)
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
	if !strings.Contains(string(raw), "protocol: responses_stream") || !strings.Contains(string(raw), "protocol: messages") {
		t.Fatalf("selected concrete provider protocols were not persisted:\n%s", raw)
	}
	if !strings.Contains(string(raw), "deepseek:") {
		t.Fatalf("DeepSeek connection persistence is wrong:\n%s", raw)
	}
	if !strings.Contains(string(raw), "kimi:") {
		t.Fatalf("Kimi connection persistence is wrong:\n%s", raw)
	}
	if !strings.Contains(string(raw), "mistral:") || !strings.Contains(string(raw), "base_url: https://api.eu.mistral.ai/v1") || !strings.Contains(string(raw), "credential: env:MISTRAL_API_KEY") {
		t.Fatalf("Mistral connection persistence is wrong:\n%s", raw)
	}
	if !strings.Contains(string(raw), "cerebras:") || !strings.Contains(string(raw), "credential: env:CEREBRAS_API_KEY") {
		t.Fatalf("Cerebras connection persistence is wrong:\n%s", raw)
	}
	if !strings.Contains(string(raw), "workersai:") || !strings.Contains(string(raw), "base_url: https://api.cloudflare.com/client/v4/accounts/account-id/ai/v1") || !strings.Contains(string(raw), "credential: env:CLOUDFLARE_API_TOKEN") {
		t.Fatalf("Workers AI endpoint/credential changed during round trip:\n%s", raw)
	}
	if !strings.Contains(string(raw), "llm7:") || !strings.Contains(string(raw), "credential: env:LLM7_API_KEY") {
		t.Fatalf("LLM7 connection persistence is wrong:\n%s", raw)
	}
	if !strings.Contains(string(raw), "runpod:") || !strings.Contains(string(raw), "base_url: https://api.runpod.ai/v2/abc123/openai/v1") || !strings.Contains(string(raw), "credential: env:RUNPOD_API_KEY") {
		t.Fatalf("Runpod endpoint normalization or credential persistence is wrong:\n%s", raw)
	}
	if !strings.Contains(string(raw), "nvidia:") || !strings.Contains(string(raw), "credential: env:NVIDIA_API_KEY") || strings.Contains(string(raw), "base_url: https://integrate.api.nvidia.com/v1") {
		t.Fatalf("NVIDIA hosted connection or derived protocol persistence is wrong:\n%s", raw)
	}
	if !strings.Contains(string(raw), "ovhcloud:") || !strings.Contains(string(raw), "credential: env:OVH_AI_ENDPOINTS_ACCESS_TOKEN") || strings.Contains(string(raw), "base_url: https://oai.endpoints.kepler.ai.cloud.ovh.net/v1") {
		t.Fatalf("OVHcloud fixed endpoint or optional credential persistence is wrong:\n%s", raw)
	}
	if !strings.Contains(string(raw), "modelscope:") || !strings.Contains(string(raw), "model: ZhipuAI/GLM-5.1:DashScope") || !strings.Contains(string(raw), "credential: env:MODELSCOPE_TOKEN") || strings.Contains(string(raw), "base_url: https://api-inference.modelscope.cn/v1") {
		t.Fatalf("ModelScope opaque model, fixed endpoint, or credential persistence is wrong:\n%s", raw)
	}
	if !strings.Contains(string(raw), "gemini:") || !strings.Contains(string(raw), "credential: env:GEMINI_API_KEY") {
		t.Fatalf("Gemini connection persistence is wrong:\n%s", raw)
	}
	if !strings.Contains(string(raw), "together:") {
		t.Fatalf("Together AI connection persistence is wrong:\n%s", raw)
	}
	if !strings.Contains(string(raw), "deepinfra:") || !strings.Contains(string(raw), "credential: env:DEEPINFRA_TOKEN") || strings.Contains(string(raw), "fail_fast") {
		t.Fatalf("DeepInfra connection or transient fail-fast boundary is wrong:\n%s", raw)
	}
	if !strings.Contains(string(raw), "scaleway:") || !strings.Contains(string(raw), "base_url: https://dedicated.example/v1") || strings.Contains(string(raw), "credential: env:SCW_SECRET_KEY") {
		t.Fatalf("Scaleway endpoint/optional credential changed during round trip:\n%s", raw)
	}
	if !strings.Contains(string(raw), "sambanova:") || !strings.Contains(string(raw), "base_url: https://stack.example/v1") || !strings.Contains(string(raw), "credential: env:SAMBANOVA_API_KEY") {
		t.Fatalf("SambaNova endpoint/credential changed during round trip:\n%s", raw)
	}
	if !strings.Contains(string(raw), "stepfun:") || !strings.Contains(string(raw), "base_url: https://api.stepfun.com/step_plan/v1") || !strings.Contains(string(raw), "credential: env:STEP_API_KEY") {
		t.Fatalf("StepFun endpoint/credential changed during round trip:\n%s", raw)
	}
	if !strings.Contains(string(raw), "lmstudio:") || !strings.Contains(string(raw), "credential: env:LM_API_TOKEN") {
		t.Fatalf("LM Studio connection changed during round trip:\n%s", raw)
	}
	if !strings.Contains(string(raw), "vllm:") || !strings.Contains(string(raw), "credential: env:VLLM_API_KEY") {
		t.Fatalf("vLLM connection changed during round trip:\n%s", raw)
	}
	if !strings.Contains(string(raw), "friendli:") || !strings.Contains(string(raw), "base_url: https://friendli-gateway.example/v1") || strings.Contains(string(raw), "credential: env:FRIENDLI_TOKEN") {
		t.Fatalf("Friendli endpoint/optional credential changed during round trip:\n%s", raw)
	}
	if !strings.Contains(string(raw), "nebius:") || !strings.Contains(string(raw), "base_url: https://api.tokenfactory.us-central1.nebius.com/v1") || !strings.Contains(string(raw), "credential: env:NEBIUS_API_KEY") {
		t.Fatalf("Nebius endpoint/credential changed during round trip:\n%s", raw)
	}
	if !strings.Contains(string(raw), "gmi:") || !strings.Contains(string(raw), "base_url: https://gmi.example/v1") || !strings.Contains(string(raw), "credential: env:GMI_API_KEY") {
		t.Fatalf("GMI endpoint/credential changed during round trip:\n%s", raw)
	}
	if !strings.Contains(string(raw), "groq:") || !strings.Contains(string(raw), "credential: env:GROQ_API_KEY") || strings.Contains(string(raw), "service_tier") {
		t.Fatalf("Groq endpoint/credential or transient capacity boundary is wrong:\n%s", raw)
	}
	if !strings.Contains(string(raw), "fireworks:") || !strings.Contains(string(raw), "base_url: https://direct.example/v1") || !strings.Contains(string(raw), "credential: env:FIREWORKS_API_KEY") {
		t.Fatalf("Fireworks exact endpoint/credential changed during round trip:\n%s", raw)
	}
}

func TestGeminiAmbientAndExplicitCredentialRoundTripWithoutAuthResidue(t *testing.T) {
	for name, connection := range map[string]string{
		"ambient ADC":      "connection: {gemini: {}}",
		"explicit API key": "connection: {gemini: {credential: env:GEMINI_API_KEY}}",
	} {
		t.Run(name, func(t *testing.T) {
			raw := []byte("schema_version: 1\nworkspaces:\n  dev:\n    default_route: chat\n    routes:\n      chat:\n        tiers:\n          - targets:\n              - {id: gemini, model: gemini-model, " + connection + "}\n")
			config, err := decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := encode(config)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decode(encoded); err != nil {
				t.Fatalf("decode round trip: %v\n%s", err, encoded)
			}
			text := string(encoded)
			for _, forbidden := range []string{"auth_mode", "quota_project", "access_token", "project_id", "_stream"} {
				if strings.Contains(text, forbidden) {
					t.Fatalf("Gemini round trip persisted %q:\n%s", forbidden, text)
				}
			}
			if name == "ambient ADC" && !strings.Contains(text, "gemini: {}") {
				t.Fatalf("ambient Gemini connection changed shape:\n%s", text)
			}
			if name == "explicit API key" && !strings.Contains(text, "credential: env:GEMINI_API_KEY") {
				t.Fatalf("explicit Gemini credential missing:\n%s", text)
			}
		})
	}
}

// TestConnectionDTOCarriesOnlyShapeDraft guards the YAML N+1 boundary. Adding
// an ordinary provider must add neither a provider-keyed Go DTO field nor a
// codec arm: the one dynamic provider key resolves through profile metadata
// into routing's closed shape draft.
func TestConnectionDTOCarriesOnlyShapeDraft(t *testing.T) {
	typeOfDTO := reflect.TypeOf(connectionDTO{})
	if typeOfDTO.NumField() != 1 {
		t.Fatalf("connectionDTO field count = %d, want one shape draft", typeOfDTO.NumField())
	}
	field := typeOfDTO.Field(0)
	if field.Name != "Draft" || field.Type != reflect.TypeOf(routing.ConnectionDraft{}) {
		t.Fatalf("connectionDTO field = %s %s, want Draft %s", field.Name, field.Type, reflect.TypeOf(routing.ConnectionDraft{}))
	}
}

func assertConfigCoversEveryProfile(t *testing.T, config routing.Config) {
	t.Helper()
	seen := make(map[string]struct{})
	for _, workspace := range config.Workspaces() {
		for _, route := range workspace.Routes() {
			for _, tier := range route.Tiers() {
				for _, target := range tier.Targets() {
					seen[string(target.Provider())] = struct{}{}
				}
			}
		}
	}
	for _, provider := range profile.All() {
		if _, ok := seen[string(provider.ProviderID)]; !ok {
			t.Fatalf("all-provider YAML fixture omits %q", provider.ProviderID)
		}
	}
	if len(seen) != len(profile.All()) {
		t.Fatalf("all-provider YAML fixture has %d provider keys, want %d", len(seen), len(profile.All()))
	}
}

func TestCodecNebiusUsesPublicEndpointWhenOmitted(t *testing.T) {
	config, err := decode([]byte(`schema_version: 1
workspaces:
  personal:
    default_route: coding
    routes:
      coding:
        tiers:
          - targets:
              - id: nebius-public
                model: meta-llama/Llama-3.3-70B-Instruct
                protocol: responses_stream
                connection: {nebius: {credential: env:NEBIUS_API_KEY}}
`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encode(config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("nebius:\n")) || bytes.Contains(raw, []byte("base_url:")) {
		t.Fatalf("Nebius default persistence =\n%s", raw)
	}
}

func TestCodecFriendliUsesDefaultEndpointWhenOmitted(t *testing.T) {
	config, err := decode([]byte(`schema_version: 1
workspaces:
  personal:
    default_route: chat
    routes:
      chat:
        tiers:
          - targets:
              - id: friendli
                model: zai-org/GLM-5.2
                protocol: chat_completions_stream
                connection: {friendli: {credential: env:FRIENDLI_TOKEN}}
`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encode(config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("friendli:\n")) || bytes.Contains(raw, []byte("base_url:")) {
		t.Fatalf("Friendli default persistence =\n%s", raw)
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

func TestCodecPersistsKimiConnectionWithDerivedProtocol(t *testing.T) {
	config, err := decode([]byte(`schema_version: 1
workspaces:
  personal:
    default_route: coding
    routes:
      coding:
        tiers:
          - targets:
              - id: kimi-k3
                model: kimi-k3
                connection: {kimi: {credential: env:MOONSHOT_API_KEY}}
`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encode(config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("kimi:\n")) || bytes.Contains(raw, []byte("protocol:")) {
		t.Fatalf("Kimi persistence =\n%s", raw)
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
	if _, err := decode([]byte(raw)); err == nil || !strings.Contains(err.Error(), "exactly one provider key") {
		t.Fatalf("decode error = %v, want multiple provider variant rejection", err)
	}
}

func TestCodecRejectsEveryAuthoredZAIProtocol(t *testing.T) {
	for _, protocol := range []string{"chat_completions_stream"} {
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

const versionedTargetYAML = `schema_version: 1
workspaces:
  dev:
    default_route: chat
    routes:
      chat:
        tiers:
          - targets:
              - id: x
                model: gpt-5
                protocol: responses
                connection: {openai: {credential: env:OPENAI_API_KEY}}
              - id: keep
                model: gpt-5
                protocol: responses
                connection: {openai: {credential: env:OPENAI_API_KEY}}
`

func TestCodecDistinguishesLegacyOmittedTargetVersionFromExplicitZero(t *testing.T) {
	config, err := decode([]byte(versionedTargetYAML))
	if err != nil {
		t.Fatalf("decode legacy omitted version: %v", err)
	}
	raw, err := encode(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(regexp.MustCompile(`(?m)^\s+version: 1$`).FindAll(raw, -1)) != 2 {
		t.Fatalf("one-way migration did not emit both target versions:\n%s", raw)
	}

	explicitZero := strings.Replace(versionedTargetYAML, "id: x\n", "id: x\n                version: 0\n", 1)
	if _, err := decode([]byte(explicitZero)); err == nil || !strings.Contains(err.Error(), "version") || !strings.Contains(err.Error(), "greater than zero") {
		t.Fatalf("explicit-zero decode error = %v, want strict rejection", err)
	}
}

func TestCodecRestoresPositiveTargetVersionAndRequiresMatchingLedger(t *testing.T) {
	versioned := strings.Replace(versionedTargetYAML, "id: x\n", "id: x\n                version: 7\n", 1)
	config, err := decode([]byte(versioned))
	if err != nil {
		t.Fatalf("decode positive version: %v", err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	workspace, _ := config.Workspace(slug)
	id, _ := routing.ParseTargetID("x")
	if got := workspace.TargetGenerations()[id]; got != 7 {
		t.Fatalf("restored generation = %d, want 7", got)
	}

	for _, generation := range []uint64{6, 8} {
		withLedger := strings.Replace(versioned, "    routes:\n", "    target_generations: {x: "+fmt.Sprint(generation)+"}\n    routes:\n", 1)
		if _, err := decode([]byte(withLedger)); err == nil || !strings.Contains(err.Error(), "must equal the active target version") {
			t.Fatalf("mismatched ledger %d error = %v", generation, err)
		}
	}
}

func TestCodecRejectsInvalidGenerationLedgerEntries(t *testing.T) {
	for name, ledger := range map[string]string{
		"zero":       "{x: 0}",
		"invalid ID": "{'bad/id': 1}",
	} {
		t.Run(name, func(t *testing.T) {
			raw := strings.Replace(versionedTargetYAML, "    routes:\n", "    target_generations: "+ledger+"\n    routes:\n", 1)
			if _, err := decode([]byte(raw)); err == nil {
				t.Fatal("invalid generation ledger unexpectedly decoded")
			}
		})
	}
}

func TestCodecPersistsAbsentTargetGenerationAcrossRestart(t *testing.T) {
	config, err := decode([]byte(versionedTargetYAML))
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	routeName, _ := routing.ParseRouteName("chat")
	workspace, _ := config.Workspace(slug)
	route, _ := workspace.Route(routeName)
	original := route.Spec()
	keepOnly := routing.RouteSpec{Tiers: []routing.TierSpec{{Targets: []routing.TargetSpec{original.Tiers[0].Targets[1]}}}}
	withoutX, err := config.ApplyRouteSpec(slug, routeName, keepOnly)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encode(withoutX)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "x: 1") {
		t.Fatalf("tombstoned generation missing from persistence:\n%s", raw)
	}
	restarted, err := decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	readd := keepOnly
	readd.Tiers = append(readd.Tiers, routing.TierSpec{Targets: []routing.TargetSpec{original.Tiers[0].Targets[0]}})
	next, err := restarted.ApplyRouteSpec(slug, routeName, readd)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ = next.Workspace(slug)
	route, _ = workspace.Route(routeName)
	for _, tier := range route.Tiers() {
		for _, target := range tier.Targets() {
			if target.ID().String() == "x" && target.Version() != 2 {
				t.Fatalf("re-added generation after restart = %d, want 2", target.Version())
			}
		}
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

func TestFixedStandardProvidersRejectBaseURLKeyEvenWhenEmpty(t *testing.T) {
	for _, provider := range profile.All() {
		if provider.ConnectionShape != routing.ConnectionShapeStandard || provider.Locator.Kind != profile.LocatorFixed {
			continue
		}
		for _, baseURL := range []string{"", "https://override.example/v1"} {
			t.Run(string(provider.ProviderID)+"/base_url="+baseURL, func(t *testing.T) {
				raw := strings.Replace(
					allVariantsYAML,
					"connection: {"+string(provider.ProviderID)+": {credential:",
					"connection: {"+string(provider.ProviderID)+": {base_url: "+baseURL+", credential:",
					1,
				)
				if _, err := decode([]byte(raw)); err == nil || !strings.Contains(err.Error(), "base_url") {
					t.Fatalf("fixed-provider base_url error = %v", err)
				}
			})
		}
	}
}

func TestCredentialUnsupportedStandardProvidersRejectCredentialKey(t *testing.T) {
	for _, provider := range profile.All() {
		if provider.ConnectionShape != routing.ConnectionShapeStandard || provider.Credential.Requirement != profile.CredentialUnsupported {
			continue
		}
		t.Run(string(provider.ProviderID), func(t *testing.T) {
			raw := strings.Replace(
				allVariantsYAML,
				"connection: {"+string(provider.ProviderID)+": {}}",
				"connection: {"+string(provider.ProviderID)+": {credential: env:UNSUPPORTED_TOKEN}}",
				1,
			)
			if _, err := decode([]byte(raw)); err == nil || !strings.Contains(err.Error(), "credential") {
				t.Fatalf("unsupported credential key error = %v", err)
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
