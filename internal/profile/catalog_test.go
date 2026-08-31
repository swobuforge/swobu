package profile

import (
	"slices"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestCatalog_SpecSupport(t *testing.T) {
	t.Parallel()

	if !SupportsSpec("openai") {
		t.Fatal("openai provider missing from catalog")
	}
	if !SupportsSpec("chatgpt") {
		t.Fatal("chatgpt provider missing from catalog")
	}
	if !SupportsSpec("gemini") {
		t.Fatal("gemini provider missing from catalog")
	}
	if !SupportsSpec("anthropic") {
		t.Fatal("anthropic provider spec should be supported")
	}
	if !SupportsSpec("deepseek") {
		t.Fatal("deepseek provider spec should be supported")
	}
	if !SupportsSpec("bedrock") {
		t.Fatal("bedrock provider spec should be supported")
	}
	if !SupportsSpec("azure") {
		t.Fatal("azure provider spec should be supported")
	}
	if !SupportsSpec("custom") {
		t.Fatal("custom provider spec should be supported")
	}
	if !SupportsSpec("lmstudio") {
		t.Fatal("LM Studio provider spec should be supported")
	}
	if !SupportsSpec("vllm") {
		t.Fatal("vLLM provider spec should be supported")
	}
	if !SupportsSpec("friendli") {
		t.Fatal("Friendli provider spec should be supported")
	}
	if !SupportsSpec("together") {
		t.Fatal("Together AI provider spec should be supported")
	}
	if !SupportsSpec("deepinfra") {
		t.Fatal("DeepInfra provider spec should be supported")
	}
	if !SupportsSpec("scaleway") {
		t.Fatal("Scaleway provider spec should be supported")
	}
	if !SupportsSpec("sambanova") {
		t.Fatal("SambaNova provider spec should be supported")
	}
	if !SupportsSpec("stepfun") {
		t.Fatal("StepFun provider spec should be supported")
	}
	if !SupportsSpec("nebius") {
		t.Fatal("Nebius Token Factory provider spec should be supported")
	}
	if !SupportsSpec("gmi") {
		t.Fatal("GMI Cloud provider spec should be supported")
	}
	if !SupportsSpec("groq") {
		t.Fatal("Groq provider spec should be supported")
	}
	if !SupportsSpec("fireworks") {
		t.Fatal("Fireworks provider spec should be supported")
	}
	for _, spec := range []string{"novita", "baseten", "hyperbolic", "siliconflow"} {
		if !SupportsSpec(spec) {
			t.Fatalf("%s provider spec should be supported", spec)
		}
	}
	if !SupportsSpec("ovhcloud") {
		t.Fatal("OVHcloud provider spec should be supported")
	}
	if !SupportsSpec("modelscope") {
		t.Fatal("ModelScope provider spec should be supported")
	}
	for _, spec := range []string{"opencode-zen", "nous", "commandcode", "venice"} {
		if !SupportsSpec(spec) {
			t.Fatalf("%s provider spec should be supported", spec)
		}
	}
	obsoleteIdentity := "openai_" + "compatible"
	if SupportsSpec(obsoleteIdentity) {
		t.Fatal("obsolete custom-endpoint provider identity must fail closed")
	}
}

// TestCatalogPreservesCurrentProviderInventoryAndConnectionShapes keeps the
// RFC's current-provider preservation claim explicit. Catalog membership is
// owned here; routing and transport boundaries must consume only these four
// durable shape reasons, never recreate this inventory.

func TestCatalog_MetaModelAPIIsDerivedStreamingResponses(t *testing.T) {
	profile, ok := profileFor(string(ProviderSpecMeta))
	if !ok {
		t.Fatal("Meta Model API provider missing from catalog")
	}
	if profile.ProviderDisplayName != "Meta Model API" || profile.ConnectionShape != routing.ConnectionShapeStandard {
		t.Fatalf("Meta profile identity = %#v", profile)
	}
	if profile.Locator.Kind != LocatorFixed || profile.Locator.Default != "https://api.meta.ai/v1" {
		t.Fatalf("Meta locator = %#v", profile.Locator)
	}
	if profile.Credential.Requirement != CredentialRequired || profile.Credential.Authoring != CredentialAuthoringReference || profile.Credential.SuggestedEnvVar != "MODEL_API_KEY" {
		t.Fatalf("Meta credential = %#v", profile.Credential)
	}
	if profile.ModelDiscovery != ModelDiscoveryModeAdvisory || !profile.VisibleInOperatorUI {
		t.Fatalf("Meta authoring facts = %#v", profile)
	}
	if got := ConcreteProviderProtocolsForSpec(string(ProviderSpecMeta)); !slices.Equal(got, []string{"responses_stream"}) {
		t.Fatalf("Meta protocols = %v", got)
	}
	if got, derived := DerivedProtocolForSpec(string(ProviderSpecMeta)); !derived || got != "responses_stream" {
		t.Fatalf("Meta derived protocol = %q, %v", got, derived)
	}
}

func TestCatalogPreservesCurrentProviderInventoryAndConnectionShapes(t *testing.T) {
	profiles := All()
	if len(profiles) != 41 {
		t.Fatalf("provider profile count = %d, want 41", len(profiles))
	}

	seen := make(map[ProviderID]struct{}, len(profiles))
	shapes := make(map[routing.ConnectionShape]int)
	for _, provider := range profiles {
		if provider.ProviderID == "" {
			t.Fatal("provider catalog contains an empty provider id")
		}
		if _, duplicate := seen[provider.ProviderID]; duplicate {
			t.Fatalf("provider catalog duplicates %q", provider.ProviderID)
		}
		seen[provider.ProviderID] = struct{}{}
		shapes[provider.ConnectionShape]++
	}
	for _, providerID := range []ProviderID{
		ProviderSpecOllama, ProviderSpecLMStudio, ProviderSpecVLLM,
		ProviderSpecOpenAI, ProviderSpecMeta, ProviderSpecChatGPT, ProviderSpecGemini, ProviderSpecAnthropic,
		ProviderSpecDeepSeek, ProviderSpecKimi, ProviderSpecMistral, ProviderSpecCerebras, ProviderSpecWorkersAI, ProviderSpecLLM7, ProviderSpecNVIDIA, ProviderSpecRunPod, ProviderSpecFriendli,
		ProviderSpecTogether, ProviderSpecDeepInfra, ProviderSpecScaleway,
		ProviderSpecSambaNova, ProviderSpecStepFun, ProviderSpecNebius,
		ProviderSpecGMI, ProviderSpecGroq, ProviderSpecFireworks,
		ProviderSpecOpenRouter, ProviderSpecZAI, ProviderSpecBedrock,
		ProviderSpecAzure, ProviderSpecCustom,
		ProviderSpecNovita, ProviderSpecBaseten, ProviderSpecHyperbolic, ProviderSpecSiliconFlow, ProviderSpecOVHCloud, ProviderSpecModelScope,
		ProviderSpecOpenCodeZen, ProviderSpecNous, ProviderSpecCommandCode, ProviderSpecVenice,
	} {
		if _, ok := seen[providerID]; !ok {
			t.Fatalf("provider catalog omits %q", providerID)
		}
	}

	for shape, want := range map[routing.ConnectionShape]int{
		routing.ConnectionShapeStandard: 38,
		routing.ConnectionShapeZAI:      1,
		routing.ConnectionShapeBedrock:  1,
		routing.ConnectionShapeCustom:   1,
	} {
		if got := shapes[shape]; got != want {
			t.Fatalf("connection shape %d count = %d, want %d", shape, got, want)
		}
	}
}

func TestCustomEndpointLoopbackCredentialPolicyParsesHostname(t *testing.T) {
	spec := string(ProviderSpecCustom)
	for _, raw := range []string{"http://localhost:8080/v1", "http://127.0.0.1:8080/v1", "http://[::1]:8080/v1"} {
		if RequiresCredential(spec, raw) {
			t.Errorf("RequiresCredential(%q) = true, want false", raw)
		}
	}
	for _, raw := range []string{"http://localhost.evil.example/v1", "http://127.0.0.1.evil.example/v1", "https://localhost/v1", "not a url"} {
		if !RequiresCredential(spec, raw) {
			t.Errorf("RequiresCredential(%q) = false, want true", raw)
		}
	}
}

func TestCatalog_DefaultsAndCredentialPolicy(t *testing.T) {
	t.Parallel()

	if got := DefaultExecuteBaseURL("chatgpt"); got != "https://api.openai.com/v1" {
		t.Fatalf("chatgpt default base URL = %q", got)
	}
	chatgpt, ok := profileFor("chatgpt")
	if !ok || chatgpt.Credential.Requirement != CredentialRequired || chatgpt.Credential.Authoring != CredentialAuthoringInteractive {
		t.Fatalf("ChatGPT credential contract = %#v; want required interactive authoring", chatgpt.Credential)
	}
	if got := DefaultExecuteBaseURL("openrouter"); got != "https://openrouter.ai/api/v1" {
		t.Fatalf("openrouter default base URL = %q", got)
	}
	if got := DefaultExecuteBaseURL("deepseek"); got != "https://api.deepseek.com/anthropic/v1" {
		t.Fatalf("deepseek default base URL = %q", got)
	}
	if got := DefaultEnvKeyForSpec("deepseek"); got != "DEEPSEEK_API_KEY" {
		t.Fatalf("deepseek default env key = %q", got)
	}
	if got := ConcreteProviderProtocolsForSpec("deepseek"); len(got) != 1 || got[0] != "messages_stream" {
		t.Fatalf("deepseek protocols = %v", got)
	}
	if got, ok := DerivedProtocolForSpec("deepseek"); !ok || got != "messages_stream" {
		t.Fatalf("deepseek derived protocol = %q, %v", got, ok)
	}
	if got, ok := DerivedProtocolForSpec("zai"); !ok || got != "chat_completions_stream" {
		t.Fatalf("Z.AI derived protocol = %q, %v", got, ok)
	}
	if got := DefaultExecuteBaseURL("gemini"); got != "https://generativelanguage.googleapis.com/v1" {
		t.Fatalf("Gemini API default base URL = %q", got)
	}
	if RequiresCredential("gemini", DefaultExecuteBaseURL("gemini")) || DefaultEnvKeyForSpec("gemini") != "GEMINI_API_KEY" {
		t.Fatal("Gemini API credential policy is wrong")
	}
	gemini, _ := profileFor("gemini")
	if gemini.Credential.Authoring != CredentialAuthoringAmbientOrReference || gemini.Credential.AmbientLabel != "Google identity (ADC)" || gemini.Credential.ReferenceLabel != "Gemini API key" {
		t.Fatalf("Gemini credential authoring = %#v", gemini.Credential)
	}
	if got, ok := DerivedProtocolForSpec("gemini"); !ok || got != "interactions_stream" {
		t.Fatalf("Gemini derived protocol = %q, %v", got, ok)
	}
	if got := ConcreteProviderProtocolsForSpec("gemini"); !slices.Equal(got, []string{"interactions_stream"}) {
		t.Fatalf("Gemini protocols = %#v", got)
	}
	if got, ok := DerivedProtocolForSpec("together"); !ok || got != "chat_completions_stream" {
		t.Fatalf("Together AI derived protocol = %q, %v", got, ok)
	}
	if got, ok := DerivedProtocolForSpec("deepinfra"); !ok || got != "chat_completions_stream" {
		t.Fatalf("DeepInfra derived protocol = %q, %v", got, ok)
	}
	if got, ok := derivedProtocolForProfile(Profile{
		ProviderID:        ProviderID("single-semantic"),
		ProviderProtocols: []ProviderProtocolSpec{{Name: "responses", Kind: protocolkind.Responses}},
	}); !ok || got != "responses" {
		t.Fatalf("single-semantic profile derived protocol = %q, %v", got, ok)
	}
	if got, ok := derivedProtocolForProfile(Profile{
		ProviderID: ProviderID("multi-semantic"),
		ProviderProtocols: []ProviderProtocolSpec{
			{Name: "responses", Kind: protocolkind.Responses},
			{Name: "messages", Kind: protocolkind.Messages},
		},
	}); ok || got != "" {
		t.Fatalf("multi-semantic profile derived protocol = %q, %v", got, ok)
	}
	if !RequiresCredential("openrouter", DefaultExecuteBaseURL("openrouter")) {
		t.Fatal("openrouter should require credential")
	}
	if RequiresCredential("ollama", "http://127.0.0.1:11434/v1") {
		t.Fatal("ollama should not require credential")
	}
	if got := DefaultExecuteBaseURL("lmstudio"); got != "http://127.0.0.1:1234/v1" {
		t.Fatalf("LM Studio default base URL = %q", got)
	}
	if RequiresCredential("lmstudio", DefaultExecuteBaseURL("lmstudio")) {
		t.Fatal("LM Studio credential should be optional")
	}
	if got := DefaultExecuteBaseURL("vllm"); got != "http://127.0.0.1:8000/v1" {
		t.Fatalf("vLLM default base URL = %q", got)
	}
	if got := DefaultExecuteBaseURL("friendli"); got != "https://api.friendli.ai/serverless/v1" {
		t.Fatalf("Friendli default base URL = %q", got)
	}
	if got := DefaultExecuteBaseURL("together"); got != "https://api.together.ai/v1" {
		t.Fatalf("Together AI default base URL = %q", got)
	}
	if got := DefaultExecuteBaseURL("deepinfra"); got != "https://api.deepinfra.com/v1/openai" {
		t.Fatalf("DeepInfra default base URL = %q", got)
	}
	if !RequiresCredential("deepinfra", DefaultExecuteBaseURL("deepinfra")) || DefaultEnvKeyForSpec("deepinfra") != "DEEPINFRA_TOKEN" {
		t.Fatal("DeepInfra credential policy is wrong")
	}
	if got := ConcreteProviderProtocolsForSpec("deepinfra"); !slices.Equal(got, []string{"chat_completions_stream"}) {
		t.Fatalf("DeepInfra protocols = %#v", got)
	}
	if got := DefaultExecuteBaseURL("scaleway"); got != "https://api.scaleway.ai/v1" {
		t.Fatalf("Scaleway default base URL = %q", got)
	}
	if RequiresCredential("scaleway", DefaultExecuteBaseURL("scaleway")) || DefaultEnvKeyForSpec("scaleway") != "SCW_SECRET_KEY" {
		t.Fatal("Scaleway credential should be optional with SCW_SECRET_KEY suggestion")
	}
	if got := ConcreteProviderProtocolsForSpec("scaleway"); !slices.Equal(got, []string{"responses_stream", "chat_completions_stream"}) {
		t.Fatalf("Scaleway protocols = %#v", got)
	}
	if got := DefaultExecuteBaseURL("sambanova"); got != "https://api.sambanova.ai/v1" {
		t.Fatalf("SambaNova default base URL = %q", got)
	}
	if !RequiresCredential("sambanova", DefaultExecuteBaseURL("sambanova")) || DefaultEnvKeyForSpec("sambanova") != "SAMBANOVA_API_KEY" {
		t.Fatal("SambaNova credential policy is wrong")
	}
	if got := ConcreteProviderProtocolsForSpec("sambanova"); !slices.Equal(got, []string{"chat_completions_stream", "responses_stream", "messages_stream", "chat_completions", "responses", "messages"}) {
		t.Fatalf("SambaNova protocols = %#v", got)
	}
	if got := DefaultExecuteBaseURL("stepfun"); got != "https://api.stepfun.com/v1" {
		t.Fatalf("StepFun default base URL = %q", got)
	}
	if !RequiresCredential("stepfun", DefaultExecuteBaseURL("stepfun")) || DefaultEnvKeyForSpec("stepfun") != "STEP_API_KEY" {
		t.Fatal("StepFun credential policy is wrong")
	}
	if got := ConcreteProviderProtocolsForSpec("stepfun"); !slices.Equal(got, []string{"chat_completions_stream", "messages_stream", "responses_stream"}) {
		t.Fatalf("StepFun protocols = %#v", got)
	}
	if got := DefaultExecuteBaseURL("nebius"); got != "https://api.tokenfactory.nebius.com/v1" {
		t.Fatalf("Nebius Token Factory default base URL = %q", got)
	}
	if !RequiresCredential("nebius", DefaultExecuteBaseURL("nebius")) || DefaultEnvKeyForSpec("nebius") != "NEBIUS_API_KEY" {
		t.Fatal("Nebius Token Factory credential should be required with NEBIUS_API_KEY suggestion")
	}
	if got := ConcreteProviderProtocolsForSpec("nebius"); !slices.Equal(got, []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream"}) {
		t.Fatalf("Nebius protocols = %#v", got)
	}
	if got := DefaultExecuteBaseURL("gmi"); got != "https://api.gmi-serving.com/v1" {
		t.Fatalf("GMI default base URL = %q", got)
	}
	if !RequiresCredential("gmi", DefaultExecuteBaseURL("gmi")) || DefaultEnvKeyForSpec("gmi") != "GMI_API_KEY" {
		t.Fatal("GMI credential policy is wrong")
	}
	if got := ConcreteProviderProtocolsForSpec("gmi"); !slices.Equal(got, []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream", "messages", "messages_stream"}) {
		t.Fatalf("GMI protocols = %#v", got)
	}
	if got := DefaultExecuteBaseURL("groq"); got != "https://api.groq.com/openai/v1" {
		t.Fatalf("Groq default base URL = %q", got)
	}
	if !RequiresCredential("groq", DefaultExecuteBaseURL("groq")) || DefaultEnvKeyForSpec("groq") != "GROQ_API_KEY" {
		t.Fatal("Groq credential policy is wrong")
	}
	if got := ConcreteProviderProtocolsForSpec("groq"); !slices.Equal(got, []string{"responses_stream", "chat_completions_stream"}) {
		t.Fatalf("Groq protocols = %#v", got)
	}
	if got := DefaultExecuteBaseURL("fireworks"); got != "https://api.fireworks.ai/inference/v1" {
		t.Fatalf("Fireworks default base URL = %q", got)
	}
	if !RequiresCredential("fireworks", DefaultExecuteBaseURL("fireworks")) || DefaultEnvKeyForSpec("fireworks") != "FIREWORKS_API_KEY" || ModelDiscoveryModeForSpec("fireworks") != ModelDiscoveryModeNone {
		t.Fatal("Fireworks credential or manual catalog policy is wrong")
	}
	if got := ConcreteProviderProtocolsForSpec("fireworks"); !slices.Equal(got, []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream", "messages", "messages_stream"}) {
		t.Fatalf("Fireworks protocols = %#v", got)
	}
	ovhcloud, ok := profileFor("ovhcloud")
	if !ok {
		t.Fatal("OVHcloud profile missing")
	}
	if ovhcloud.ProviderDisplayName != "OVHcloud AI Endpoints" || ovhcloud.Locator != (LocatorSpec{Kind: LocatorFixed, Default: "https://oai.endpoints.kepler.ai.cloud.ovh.net/v1"}) {
		t.Fatalf("OVHcloud identity and locator = %#v", ovhcloud)
	}
	if ovhcloud.Credential != (CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "OVH_AI_ENDPOINTS_ACCESS_TOKEN"}) {
		t.Fatalf("OVHcloud credential = %#v", ovhcloud.Credential)
	}
	if !ovhcloud.VisibleInOperatorUI || ModelDiscoveryModeForSpec("ovhcloud") != ModelDiscoveryModeAdvisory {
		t.Fatal("OVHcloud must be operator-visible with advisory model discovery")
	}
	if RequiresCredential("ovhcloud", DefaultExecuteBaseURL("ovhcloud")) {
		t.Fatal("OVHcloud access token must be optional")
	}
	if got := ConcreteProviderProtocolsForSpec("ovhcloud"); !slices.Equal(got, []string{"chat_completions_stream"}) {
		t.Fatalf("OVHcloud protocols = %#v", got)
	}
	if got, derived := DerivedProtocolForSpec("ovhcloud"); !derived || got != "chat_completions_stream" {
		t.Fatalf("OVHcloud derived protocol = %q, %v", got, derived)
	}
	modelscope, ok := profileFor("modelscope")
	if !ok {
		t.Fatal("ModelScope profile missing")
	}
	if modelscope.ProviderDisplayName != "ModelScope API-Inference" || modelscope.Locator != (LocatorSpec{Kind: LocatorFixed, Default: "https://api-inference.modelscope.cn/v1"}) {
		t.Fatalf("ModelScope identity and locator = %#v", modelscope)
	}
	if modelscope.Credential != (CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "MODELSCOPE_TOKEN"}) {
		t.Fatalf("ModelScope credential = %#v", modelscope.Credential)
	}
	if !modelscope.VisibleInOperatorUI || ModelDiscoveryModeForSpec("modelscope") != ModelDiscoveryModeAdvisory || !RequiresCredential("modelscope", DefaultExecuteBaseURL("modelscope")) {
		t.Fatal("ModelScope must be visible, advisory, and credential-required")
	}
	if got := ConcreteProviderProtocolsForSpec("modelscope"); !slices.Equal(got, []string{"chat_completions_stream"}) {
		t.Fatalf("ModelScope protocols = %#v", got)
	}
	if got, derived := DerivedProtocolForSpec("modelscope"); !derived || got != "chat_completions_stream" {
		t.Fatalf("ModelScope derived protocol = %q, %v", got, derived)
	}
	if !RequiresCredential("together", DefaultExecuteBaseURL("together")) || DefaultEnvKeyForSpec("together") != "TOGETHER_API_KEY" {
		t.Fatal("Together AI credential should be required with TOGETHER_API_KEY suggestion")
	}
	if RequiresCredential("friendli", DefaultExecuteBaseURL("friendli")) || DefaultEnvKeyForSpec("friendli") != "FRIENDLI_TOKEN" {
		t.Fatal("Friendli credential should be optional with FRIENDLI_TOKEN suggestion")
	}
	if got := ConcreteProviderProtocolsForSpec("friendli"); !slices.Equal(got, []string{"chat_completions_stream", "responses_stream", "messages_stream"}) {
		t.Fatalf("Friendli protocols = %#v", got)
	}
	if RequiresCredential("vllm", DefaultExecuteBaseURL("vllm")) || DefaultEnvKeyForSpec("vllm") != "VLLM_API_KEY" {
		t.Fatal("vLLM credential should be optional with VLLM_API_KEY suggestion")
	}
	if RequiresCredential("custom", "http://localhost:9999/v1") {
		t.Fatal("localhost custom endpoint should not require credential")
	}
	if !RequiresCredential("custom", "https://lab.example/v1") {
		t.Fatal("remote custom endpoint should require credential")
	}
	if !RequiresLocator("azure") {
		t.Fatal("azure should require an explicit endpoint")
	}
	if !RequiresLocator("bedrock") {
		t.Fatal("bedrock should require an explicit endpoint")
	}
	if got := DefaultEnvKeyForSpec("azure"); got != "AZURE_OPENAI_API_KEY" {
		t.Fatalf("azure default env key = %q", got)
	}
	if got := DefaultEnvKeyForSpec("bedrock"); got != "AWS_BEARER_TOKEN_BEDROCK" {
		t.Fatalf("bedrock default env key = %q", got)
	}
	if got := DefaultExecuteBaseURL("zai"); got != "" {
		t.Fatalf("Z.AI must not have an access-independent endpoint, got %q", got)
	}
	for _, entry := range All() {
		shape, ok := ConnectionShapeForSpec(string(entry.ProviderID))
		if !ok || shape != entry.ConnectionShape {
			t.Fatalf("connection shape for %q = %d, %v; want %d, true", entry.ProviderID, shape, ok, entry.ConnectionShape)
		}
	}
	for _, spec := range []string{"openai", "azure", "ollama", "vllm"} {
		if shape, _ := ConnectionShapeForSpec(spec); shape != routing.ConnectionShapeStandard {
			t.Fatalf("%s connection shape = %d; want standard", spec, shape)
		}
	}
	for _, tc := range []struct {
		spec string
		want routing.ConnectionShape
	}{
		{"zai", routing.ConnectionShapeZAI},
		{"bedrock", routing.ConnectionShapeBedrock},
		{"custom", routing.ConnectionShapeCustom},
	} {
		if shape, _ := ConnectionShapeForSpec(tc.spec); shape != tc.want {
			t.Fatalf("%s connection shape = %d; want %d", tc.spec, shape, tc.want)
		}
	}
	if got := ConcreteProviderProtocolsForSpec("zai"); len(got) != 1 || got[0] != "chat_completions_stream" {
		t.Fatalf("Z.AI protocols = %#v, want fixed Chat Completions", got)
	}
	if got := DefaultAuthHeaderForSpec("custom"); got != "Authorization" {
		t.Fatalf("custom endpoint default auth header = %q", got)
	}
	authHeaders := SupportedAuthHeadersForSpec("custom")
	if len(authHeaders) != 3 {
		t.Fatalf("custom endpoint auth headers=%v want 3 common choices", authHeaders)
	}
	if authHeaders[0] != "Authorization" || authHeaders[1] != "x-api-key" || authHeaders[2] != "api-key" {
		t.Fatalf("custom endpoint auth headers=%v want [Authorization x-api-key api-key]", authHeaders)
	}
	if got := ConcreteProviderProtocolsForSpec("custom"); !slices.Equal(got, []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream", "messages", "messages_stream"}) {
		t.Fatalf("custom endpoint concrete protocols=%v want semantic standard families", got)
	}
	for _, tc := range []struct {
		baseURL string
		want    string
	}{
		{"https://lab.example/v1", "Authorization"},
		{"", "Authorization"},
		{"https://gw.example/anthropic/v1/messages", "x-api-key"},
		{"https://foo.openai.azure.com/openai/deployments/x", "api-key"},
		{"https://foo.cognitiveservices.azure.com/openai", "api-key"},
		{"https://foo.services.ai.azure.com/anthropic/v1/messages", "x-api-key"},
	} {
		if got := InferredCredentialHeaderForBackendURL(tc.baseURL); got != tc.want {
			t.Fatalf("inferred credential header for %q = %q, want %q", tc.baseURL, got, tc.want)
		}
	}
	if !RequiresCredential("azure", "") {
		t.Fatal("azure should require credential")
	}
	if RequiresCredential("bedrock", "https://bedrock-mantle.us-east-1.api.aws/v1") {
		t.Fatal("bedrock profile layer should allow ambient AWS auth without credential_ref")
	}

	bedrockProtocols := ConcreteProviderProtocolsForSpec("bedrock")
	if len(bedrockProtocols) != 6 {
		t.Fatalf("bedrock concrete protocols=%v want exactly 6", bedrockProtocols)
	}
	if bedrockProtocols[0] != "responses" || bedrockProtocols[1] != "responses_stream" || bedrockProtocols[2] != "chat_completions" {
		t.Fatalf("bedrock concrete protocols=%v want semantic Responses first", bedrockProtocols)
	}
	if !SupportsProviderProtocolForSpec("bedrock", "chat_completions") || !SupportsProviderProtocolForSpec("bedrock", "chat_completions_stream") || !SupportsProviderProtocolForSpec("bedrock", "messages") || !SupportsProviderProtocolForSpec("bedrock", "messages_stream") {
		t.Fatal("bedrock should support chat_completions and messages")
	}
	if SupportsProviderProtocolForSpec("bedrock", "converse") || SupportsProviderProtocolForSpec("bedrock", "invoke_model") {
		t.Fatal("bedrock must not advertise native runtime protocol names")
	}
}

func TestNormalizeProviderProtocolForSpecPreservesConcreteDeliveryToken(t *testing.T) {
	for _, tc := range []struct {
		provider string
		legacy   string
		want     string
	}{
		{provider: "openai", legacy: "responses_stream", want: "responses_stream"},
		{provider: "anthropic", legacy: "messages_stream", want: "messages_stream"},
		{provider: "gemini", legacy: "interactions_stream", want: "interactions_stream"},
		{provider: "runpod", legacy: "chat_completions_stream", want: "chat_completions_stream"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			got, err := NormalizeProviderProtocolForSpec(tc.provider, tc.legacy)
			if err != nil || got != tc.want {
				t.Fatalf("NormalizeProviderProtocolForSpec(%q, %q) = %q, %v; want %q", tc.provider, tc.legacy, got, err, tc.want)
			}
		})
	}
}

func TestSupportsProviderProtocolForSpecRejectsAbsentOrUnknownProtocol(t *testing.T) {
	for _, protocol := range []string{"", "not_a_protocol"} {
		if SupportsProviderProtocolForSpec("openai", protocol) {
			t.Fatalf("%q is not a supported concrete provider protocol", protocol)
		}
	}
}

func TestConcreteProviderProtocolsForSpecPreservesDeliveryVariants(t *testing.T) {
	if got := ConcreteProviderProtocolsForSpec("runpod"); !slices.Equal(got, []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream", "messages", "messages_stream"}) {
		t.Fatalf("RunPod concrete protocols = %v", got)
	}
	if got := ConcreteProviderProtocolsForSpec("sambanova"); !slices.Equal(got, []string{"chat_completions_stream", "responses_stream", "messages_stream", "chat_completions", "responses", "messages"}) {
		t.Fatalf("SambaNova concrete protocols = %v", got)
	}
}

func TestCatalog_ProviderSetupKeywordsAreSearchOnly(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"ollama":     "model, protocol",
		"lmstudio":   "local, model, Responses, Chat Completions, Messages, Codex, Claude Code",
		"vllm":       "inference, server, Responses, Chat Completions, Messages, Codex, Claude Code",
		"openai":     "credential, model, protocol",
		"chatgpt":    "sign in, model, protocol",
		"anthropic":  "credential, model, protocol",
		"openrouter": "credential, model, protocol",
		"bedrock":    "region, Bedrock API key, AWS credentials, model, protocol",
		"azure":      "endpoint, credential, deployment, protocol",
		"custom":     "backend URL, credential, credential header, model, protocol",
	}

	for spec, want := range cases {
		if got := ProviderSetupKeywordSummaryForSpec(spec); got != want {
			t.Fatalf("provider setup keyword summary for %q = %q, want %q", spec, got, want)
		}
	}
}

func TestCatalog_ProviderAuthoringMatrix(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		locator    LocatorSpec
		credential CredentialSpec
		noun       string
	}{
		"ollama":      {LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "http://127.0.0.1:11434/v1"}, CredentialSpec{Requirement: CredentialUnsupported, Authoring: CredentialAuthoringNone}, "model"},
		"lmstudio":    {LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "http://127.0.0.1:1234/v1"}, CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "LM_API_TOKEN"}, "model"},
		"vllm":        {LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "http://127.0.0.1:8000/v1"}, CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "VLLM_API_KEY"}, "model"},
		"friendli":    {LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "https://api.friendli.ai/serverless/v1"}, CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "FRIENDLI_TOKEN"}, "model"},
		"scaleway":    {LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "https://api.scaleway.ai/v1"}, CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "SCW_SECRET_KEY"}, "model"},
		"sambanova":   {LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "https://api.sambanova.ai/v1"}, CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "SAMBANOVA_API_KEY"}, "model"},
		"stepfun":     {LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "https://api.stepfun.com/v1"}, CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "STEP_API_KEY"}, "model"},
		"together":    {LocatorSpec{Kind: LocatorFixed, Default: "https://api.together.ai/v1"}, CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "TOGETHER_API_KEY"}, "model"},
		"openai":      {LocatorSpec{Kind: LocatorFixed, Default: "https://api.openai.com/v1"}, CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "OPENAI_API_KEY"}, "model"},
		"chatgpt":     {LocatorSpec{Kind: LocatorFixed, Default: "https://api.openai.com/v1"}, CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringInteractive}, "model"},
		"gemini":      {LocatorSpec{Kind: LocatorFixed, Default: "https://generativelanguage.googleapis.com/v1"}, CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringAmbientOrReference, SuggestedEnvVar: "GEMINI_API_KEY", AmbientLabel: "Google identity (ADC)", ReferenceLabel: "Gemini API key"}, "model"},
		"anthropic":   {LocatorSpec{Kind: LocatorFixed, Default: "https://api.anthropic.com/v1"}, CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "ANTHROPIC_API_KEY"}, "model"},
		"openrouter":  {LocatorSpec{Kind: LocatorFixed, Default: "https://openrouter.ai/api/v1"}, CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "OPENROUTER_API_KEY"}, "model"},
		"bedrock":     {LocatorSpec{Kind: LocatorAWSRegion, Label: "region"}, CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringAmbientOrReference, SuggestedEnvVar: "AWS_BEARER_TOKEN_BEDROCK", AmbientLabel: "AWS identity", ReferenceLabel: "Bedrock API key"}, "model"},
		"azure":       {LocatorSpec{Kind: LocatorAzureProject, Label: "project"}, CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "AZURE_OPENAI_API_KEY"}, "deployment"},
		"custom":      {LocatorSpec{Kind: LocatorBaseURL, Label: "backend URL"}, CredentialSpec{Requirement: CredentialRequiredOutsideLoopback, Authoring: CredentialAuthoringReference}, "model"},
		"novita":      {LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "https://api.novita.ai/openai/v1"}, CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "NOVITA_API_KEY"}, "model"},
		"baseten":     {LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "https://inference.baseten.co/v1"}, CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "BASETEN_API_KEY"}, "model"},
		"hyperbolic":  {LocatorSpec{Kind: LocatorFixed, Default: "https://api.hyperbolic.xyz/v1"}, CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "HYPERBOLIC_API_KEY"}, "model"},
		"siliconflow": {LocatorSpec{Kind: LocatorFixed, Default: "https://api.siliconflow.cn/v1"}, CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "SILICONFLOW_API_KEY"}, "model"},
		"ovhcloud":    {LocatorSpec{Kind: LocatorFixed, Default: "https://oai.endpoints.kepler.ai.cloud.ovh.net/v1"}, CredentialSpec{Requirement: CredentialOptional, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "OVH_AI_ENDPOINTS_ACCESS_TOKEN"}, "model"},
		"modelscope":  {LocatorSpec{Kind: LocatorFixed, Default: "https://api-inference.modelscope.cn/v1"}, CredentialSpec{Requirement: CredentialRequired, Authoring: CredentialAuthoringReference, SuggestedEnvVar: "MODELSCOPE_TOKEN"}, "model"},
	}
	for spec, want := range cases {
		got, ok := LocatorSpecForProvider(spec)
		if !ok {
			t.Fatalf("locator spec for %q missing", spec)
		}
		if got != want.locator {
			t.Fatalf("locator for %q = %+v, want %+v", spec, got, want.locator)
		}
		provider, ok := profileFor(spec)
		if !ok {
			t.Fatalf("profile for %q missing", spec)
		}
		if provider.Credential != want.credential {
			t.Fatalf("credential for %q = %+v, want %+v", spec, provider.Credential, want.credential)
		}
		if got := CatalogItemLabelForSpec(spec); got != want.noun {
			t.Fatalf("catalog noun for %q = %q, want %q", spec, got, want.noun)
		}
	}
}

func TestCatalog_ModelDiscoveryModeIsExplicit(t *testing.T) {
	t.Parallel()
	for _, provider := range All() {
		if provider.ModelDiscovery == ModelDiscoveryModeInvalid {
			t.Fatalf("provider %q has no explicit model discovery mode", provider.ProviderID)
		}
	}
	if got := ModelDiscoveryModeForSpec("zai"); got != ModelDiscoveryModeNone {
		t.Fatalf("Z.AI model discovery mode = %v, want none", got)
	}
}

func TestCatalog_ChatGPTProviderProtocols_AreConcreteResponsesOnly(t *testing.T) {
	t.Parallel()

	protocols := ConcreteProviderProtocolsForSpec("chatgpt")
	if !slices.Equal(protocols, []string{"responses_stream"}) {
		t.Fatalf("chatgpt protocols = %v, want [responses_stream]", protocols)
	}
	if !SupportsProviderProtocolForSpec("chatgpt", "responses_stream") {
		t.Fatal("chatgpt must declare responses protocol")
	}
}

func TestCatalog_AllStandardProvidersSharePreferenceOrderedProtocolSet(t *testing.T) {
	t.Parallel()

	want := []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream", "messages", "messages_stream"}
	for _, provider := range []string{"ollama", "lmstudio", "vllm"} {
		protocols := ConcreteProviderProtocolsForSpec(provider)
		if !slices.Equal(protocols, want) {
			t.Fatalf("%s protocols = %v, want %v", provider, protocols, want)
		}
		if got := protocols[0]; got != "responses" {
			t.Fatalf("%s default protocol = %q, want first preference responses", provider, got)
		}
	}
}

func TestCatalog_ConcreteProviderProtocolsForSpec_OrderIsCanonical(t *testing.T) {
	t.Parallel()

	openAI := ConcreteProviderProtocolsForSpec("openai")
	if len(openAI) < 2 {
		t.Fatalf("openai concrete protocols=%v want at least 2", openAI)
	}
	if openAI[0] != "responses" || openAI[1] != "responses_stream" {
		t.Fatalf("openai concrete protocol order=%v want [responses responses_stream ...]", openAI)
	}

	chatgpt := ConcreteProviderProtocolsForSpec("chatgpt")
	if len(chatgpt) != 1 || chatgpt[0] != "responses_stream" {
		t.Fatalf("chatgpt concrete protocols=%v want [responses_stream]", chatgpt)
	}
}
