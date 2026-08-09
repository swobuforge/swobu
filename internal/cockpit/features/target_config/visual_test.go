package target_config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/profile"
)

const providerAuthoringFixtureDir = "testdata/provider_authoring/fixture"
const providerAuthoringFixtureHeight = 24
const chatGPTVisualLoginURL = "https://auth.openai.com/oauth/authorize?client_id=app_test&redirect_uri=http%3A%2F%2F127.0.0.1%3A7926%2Fcallback&response_type=code&scope=openid%20profile%20email&state=fixture-state"

var requiredProviderAuthoringVisualNames = []string{
	"api_key_missing", "api_key_environment_ready",
	"credential_source_menu", "credential_environment_input", "credential_file_browser", "credential_paste_input", "credential_optional_remove",
	"custom_loopback_anonymous", "custom_remote_credential_required", "custom_credential_header_picker", "custom_credential_header_open_value", "custom_ready",
	"custom_manual_model",
	"bedrock_aws_identity", "bedrock_environment_api_key", "bedrock_target_api_key", "bedrock_auth_failure", "bedrock_credential_menu",
	"azure_project_required", "azure_credential_required", "azure_protocol_required", "azure_ready",
	"ollama_default_url", "ollama_editing_url",
	"chatgpt_signed_out", "chatgpt_auth_mode_picker", "chatgpt_pending", "chatgpt_pending_auth_mode_picker", "chatgpt_device_pending", "chatgpt_open_failed", "chatgpt_signed_in", "chatgpt_failed",
	"model_picker", "deployment_picker", "protocol_picker", "ready_to_create",
}

type providerAuthoringVisualCase struct {
	name   string
	build  func(*testing.T) tui.Component
	render func(*testing.T, int) string
}

func TestProviderAuthoringVisualContract(t *testing.T) {
	for _, visual := range providerAuthoringVisualCases() {
		visual := visual
		t.Run(visual.name, func(t *testing.T) {
			for _, width := range providerAuthoringVisualWidths(visual.name) {
				width := width
				t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
					frame := ""
					if visual.render != nil {
						frame = visual.render(t, width)
					} else {
						frame = testkit.RenderMountedTrimmed(t, visual.build(t), width, providerAuthoringFixtureHeight)
					}
					fixture := fmt.Sprintf("%s/%s_%d.txt", providerAuthoringFixtureDir, visual.name, width)
					testkit.AssertVisual(visual.name).
						Fixture(fixture).
						Viewport(width, providerAuthoringFixtureHeight).
						Now(t, frame)
				})
			}
		})
	}
}

func TestProviderAuthoringVisualRegistryAndFixturesAreClosed(t *testing.T) {
	cases := providerAuthoringVisualCases()
	actualNames := make([]string, 0, len(cases))
	expectedFiles := make(map[string]bool, len(cases)+10)
	for _, visual := range cases {
		actualNames = append(actualNames, visual.name)
		for _, width := range providerAuthoringVisualWidths(visual.name) {
			expectedFiles[fmt.Sprintf("%s_%d.txt", visual.name, width)] = true
		}
	}
	wantNames := slices.Clone(requiredProviderAuthoringVisualNames)
	slices.Sort(actualNames)
	slices.Sort(wantNames)
	if !slices.Equal(actualNames, wantNames) {
		t.Fatalf("visual registry names = %v, want RFC names %v", actualNames, wantNames)
	}

	entries, err := os.ReadDir(providerAuthoringFixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	actualFiles := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			actualFiles[entry.Name()] = true
		}
	}
	for name := range expectedFiles {
		if !actualFiles[name] {
			t.Errorf("registered visual fixture missing: %s", name)
		}
	}
	for name := range actualFiles {
		if !expectedFiles[name] {
			t.Errorf("orphan visual fixture: %s", name)
		}
	}
}

func providerAuthoringVisualWidths(name string) []int {
	switch name {
	case "bedrock_aws_identity", "credential_file_browser", "model_picker", "protocol_picker",
		"chatgpt_auth_mode_picker", "chatgpt_pending", "chatgpt_pending_auth_mode_picker", "chatgpt_device_pending",
		"chatgpt_open_failed":
		return []int{80, 100, 120}
	default:
		return []int{100}
	}
}

func providerAuthoringVisualCases() []providerAuthoringVisualCase {
	return []providerAuthoringVisualCase{
		{name: "api_key_missing", build: func(t *testing.T) tui.Component {
			return authoringConfig(t, profile.ProviderSpecOpenAI, "", "")
		}},
		{name: "api_key_environment_ready", build: func(t *testing.T) tui.Component {
			w := authoringConfig(t, profile.ProviderSpecOpenAI, "", "env:OPENAI_API_KEY")
			selectReadyModel(w, "gpt-4.1", "responses_stream")
			return w
		}},
		{name: "credential_source_menu", render: func(t *testing.T, width int) string {
			return renderCredentialJourney(t, width, []tui.KeyEvent{{Key: tui.KeyEnter}}, nil)
		}},
		{name: "credential_environment_input", render: func(t *testing.T, width int) string {
			return renderCredentialJourney(t, width, []tui.KeyEvent{{Key: tui.KeyEnter}, {Key: tui.KeyEnter}}, nil)
		}},
		{name: "credential_file_browser", render: func(t *testing.T, width int) string {
			return renderCredentialJourney(t, width, []tui.KeyEvent{{Key: tui.KeyEnter}, {Key: tui.KeyDown}, {Key: tui.KeyEnter}}, func(w *TargetConfig) {
				w.credentialInitialPath = "/home/operator/.config/swobu"
				w.credentialReadDir = func(string) ([]ui.FileBrowserEntry, error) {
					return []ui.FileBrowserEntry{{Name: "openai.key"}, {Name: "anthropic.key"}, {Name: "development", IsDir: true}, {Name: "production", IsDir: true}}, nil
				}
			})
		}},
		{name: "credential_paste_input", render: func(t *testing.T, width int) string {
			keys := []tui.KeyEvent{{Key: tui.KeyEnter}, {Key: tui.KeyDown}, {Key: tui.KeyDown}, {Key: tui.KeyEnter}}
			for _, char := range "sk-secret-example" {
				keys = append(keys, tui.KeyEvent{Key: tui.KeyRune, Rune: char})
			}
			return renderCredentialJourney(t, width, keys, nil)
		}},
		{name: "custom_loopback_anonymous", build: func(t *testing.T) tui.Component {
			w := authoringConfig(t, profile.ProviderSpecCustom, "http://127.0.0.1:8080/v1", "")
			selectReadyModel(w, "local-model", "chat_completions_stream")
			return w
		}},
		{name: "custom_remote_credential_required", build: func(t *testing.T) tui.Component {
			return authoringConfig(t, profile.ProviderSpecCustom, "https://api.example.com/v1", "")
		}},
		{name: "custom_ready", build: func(t *testing.T) tui.Component {
			w := authoringConfig(t, profile.ProviderSpecCustom, "https://api.example.com/v1", "env:CUSTOM_API_KEY")
			selectReadyModel(w, "custom-model", "chat_completions_stream")
			return w
		}},
		{name: "custom_manual_model", build: func(t *testing.T) tui.Component {
			w := authoringConfig(t, profile.ProviderSpecCustom, "https://api.z.ai/api/anthropic", "secret:zai")
			w.Catalog.Set(catalogOperationState{Err: "model catalog endpoint is unsupported"})
			return w
		}},
		{name: "custom_credential_header_picker", render: func(t *testing.T, width int) string {
			return renderCustomHeaderJourney(t, width, "")
		}},
		{name: "custom_credential_header_open_value", render: func(t *testing.T, width int) string {
			return renderCustomHeaderJourney(t, width, "X-Custom-Token")
		}},
		{name: "bedrock_aws_identity", build: func(t *testing.T) tui.Component {
			return bedrockVisualConfig(t, "aws_identity", "", readmodel.AWSIdentityReadModel{
				State: "resolved", Account: "123456789012",
				ARN: "arn:aws:sts::123456789012:assumed-role/Developer/session",
			})
		}},
		{name: "bedrock_environment_api_key", build: func(t *testing.T) tui.Component {
			return bedrockVisualConfig(t, "explicit_api_key", "env:AWS_BEARER_TOKEN_BEDROCK", readmodel.AWSIdentityReadModel{})
		}},
		{name: "bedrock_target_api_key", build: func(t *testing.T) tui.Component {
			return bedrockVisualConfig(t, "explicit_api_key", "secret:bedrock-target", readmodel.AWSIdentityReadModel{})
		}},
		{name: "bedrock_auth_failure", build: func(t *testing.T) tui.Component {
			return bedrockVisualConfig(t, "aws_identity_failure", "", readmodel.AWSIdentityReadModel{})
		}},
		{name: "bedrock_credential_menu", render: func(t *testing.T, width int) string {
			return renderBedrockVisual(t, width, "aws_identity", "", readmodel.AWSIdentityReadModel{
				State: "resolved", Account: "123456789012",
				ARN: "arn:aws:sts::123456789012:assumed-role/Developer/session",
			}, []tui.KeyEvent{{Key: tui.KeyUp}, {Key: tui.KeyUp}, {Key: tui.KeyUp}, {Key: tui.KeyUp}, {Key: tui.KeyUp}, {Key: tui.KeyEnter}})
		}},
		{name: "credential_optional_remove", render: func(t *testing.T, width int) string {
			return renderBedrockVisual(t, width, "explicit_api_key", "secret:bedrock-target", readmodel.AWSIdentityReadModel{}, []tui.KeyEvent{{Key: tui.KeyUp}, {Key: tui.KeyUp}, {Key: tui.KeyUp}, {Key: tui.KeyUp}, {Key: tui.KeyUp}, {Key: tui.KeyEnter}})
		}},
		{name: "chatgpt_signed_out", build: func(t *testing.T) tui.Component {
			return authoringConfig(t, profile.ProviderSpecChatGPT, "", "")
		}},
		{name: "chatgpt_auth_mode_picker", render: renderChatGPTAuthModePicker},
		{name: "chatgpt_pending", build: func(t *testing.T) tui.Component {
			w := authoringConfig(t, profile.ProviderSpecChatGPT, "", "")
			w.AuthSession.Set(readmodel.AuthSessionReadModel{
				ProviderSpec: string(profile.ProviderSpecChatGPT), SessionID: "login-1", State: "pending",
				AuthorizeURL: chatGPTVisualLoginURL,
			})
			return w
		}},
		{name: "chatgpt_pending_auth_mode_picker", render: renderChatGPTPendingAuthModePicker},
		{name: "chatgpt_device_pending", render: renderChatGPTDevicePending},
		{name: "chatgpt_open_failed", render: renderChatGPTOpenFailure},
		{name: "chatgpt_signed_in", build: func(t *testing.T) tui.Component {
			w := authoringConfig(t, profile.ProviderSpecChatGPT, "", "secret:chatgpt/session")
			w.AuthSession.Set(readmodel.AuthSessionReadModel{ProviderSpec: string(profile.ProviderSpecChatGPT), State: "succeeded", CredentialRef: "secret:chatgpt/session"})
			selectReadyModel(w, "GPT-5.2", "responses_stream")
			return w
		}},
		{name: "chatgpt_failed", build: func(t *testing.T) tui.Component {
			w := authoringConfig(t, profile.ProviderSpecChatGPT, "", "")
			w.AuthSession.Set(readmodel.AuthSessionReadModel{ProviderSpec: string(profile.ProviderSpecChatGPT), State: "failed", ErrorMessage: "login expired"})
			w.Error.Set("login expired")
			return w
		}},
		{name: "azure_project_required", build: func(t *testing.T) tui.Component {
			return authoringConfig(t, profile.ProviderSpecAzure, "", "")
		}},
		{name: "azure_credential_required", render: renderAzureCredentialRequired},
		{name: "azure_protocol_required", build: func(t *testing.T) tui.Component {
			w := authoringConfig(t, profile.ProviderSpecAzure, "https://example.services.ai.azure.com/api/projects/demo", "env:AZURE_OPENAI_API_KEY")
			w.Route = readmodel.RouteReadModel{ID: "openai"}
			deployment := readmodel.ModelDeploymentReadModel{ID: "gpt-5.6-sol", Name: "gpt-5.6-sol", ModelName: "gpt-5.6-sol"}
			w.Catalog.Set(catalogOperationState{Result: readmodel.ModelCatalogReadModel{Deployments: []readmodel.ModelDeploymentReadModel{deployment}}})
			w.SelectModel(deployment)
			return w
		}},
		{name: "azure_ready", build: func(t *testing.T) tui.Component {
			w := authoringConfig(t, profile.ProviderSpecAzure, "https://example.services.ai.azure.com/api/projects/demo", "env:AZURE_OPENAI_API_KEY")
			selectReadyModel(w, "claude-opus-4-8", "messages_stream")
			return w
		}},
		{name: "ollama_default_url", build: func(t *testing.T) tui.Component {
			return authoringConfig(t, profile.ProviderSpecOllama, "", "")
		}},
		{name: "ollama_editing_url", build: func(t *testing.T) tui.Component {
			w := authoringConfig(t, profile.ProviderSpecOllama, "http://192.168.1.20:11434", "")
			return w
		}},
		{name: "model_picker", render: renderModelPicker},
		{name: "deployment_picker", render: renderDeploymentPicker},
		{name: "protocol_picker", render: renderProtocolPicker},
		{name: "ready_to_create", build: func(t *testing.T) tui.Component {
			w := authoringConfig(t, profile.ProviderSpecOpenAI, "", "env:OPENAI_API_KEY")
			selectReadyModel(w, "gpt-4.1", "responses_stream")
			return w
		}},
	}
}

func renderModelPicker(t *testing.T, width int) string {
	t.Helper()
	w := authoringConfig(t, profile.ProviderSpecOpenAI, "", "env:OPENAI_API_KEY")
	selectReadyModel(w, "GPT-4.1", "responses_stream")
	catalog := w.Catalog.Get()
	catalog.Result.Deployments = []readmodel.ModelDeploymentReadModel{
		{ID: "GPT-4.1", Name: "GPT-4.1", ModelName: "GPT-4.1"},
		{ID: "gpt-4.1-small", Name: "GPT-4.1 small", ModelName: "gpt-4.1-small"},
		{ID: "gpt-4o", Name: "GPT-4o", ModelName: "gpt-4o"},
	}
	w.Catalog.Set(catalog)
	keys := []tui.KeyEvent{{Key: tui.KeyUp}, {Key: tui.KeyUp}, {Key: tui.KeyUp}, {Key: tui.KeyEnter}}
	return renderAuthoringKeys(t, width, w, append(keys, runeKeys("gpt-4")...))
}

func renderDeploymentPicker(t *testing.T, width int) string {
	t.Helper()
	w := authoringConfig(t, profile.ProviderSpecAzure, "https://example.services.ai.azure.com/api/projects/demo", "env:AZURE_OPENAI_API_KEY")
	selectReadyModel(w, "claude-opus-4-8", "messages_stream")
	catalog := w.Catalog.Get()
	catalog.Result.Deployments = []readmodel.ModelDeploymentReadModel{
		{ID: "claude-opus-4-8", Name: "claude-opus-4-8", ModelName: "claude-opus-4-8"},
		{ID: "claude-sonnet-4-6", Name: "claude-sonnet-4-6", ModelName: "claude-sonnet-4-6"},
	}
	w.Catalog.Set(catalog)
	keys := []tui.KeyEvent{{Key: tui.KeyUp}, {Key: tui.KeyUp}, {Key: tui.KeyUp}, {Key: tui.KeyEnter}}
	return renderAuthoringKeys(t, width, w, append(keys, runeKeys("claude")...))
}

func renderProtocolPicker(t *testing.T, width int) string {
	t.Helper()
	w := authoringConfig(t, profile.ProviderSpecCustom, "https://api.example.com/v1", "env:CUSTOM_API_KEY")
	selectReadyModel(w, "model-x", "responses_stream")
	return renderAuthoringKeys(t, width, w, []tui.KeyEvent{{Key: tui.KeyUp}, {Key: tui.KeyUp}, {Key: tui.KeyEnter}})
}

func renderCustomHeaderJourney(t *testing.T, width int, query string) string {
	t.Helper()
	w := authoringConfig(t, profile.ProviderSpecCustom, "https://api.example.com/v1", "env:CUSTOM_API_KEY")
	selectReadyModel(w, "model-x", "responses_stream")
	keys := []tui.KeyEvent{{Key: tui.KeyUp}, {Key: tui.KeyUp}, {Key: tui.KeyUp}, {Key: tui.KeyUp}, {Key: tui.KeyEnter}}
	keys = append(keys, runeKeys(query)...)
	return renderAuthoringKeys(t, width, w, keys)
}

func renderAuthoringKeys(t *testing.T, width int, w *TargetConfig, keys []tui.KeyEvent) string {
	t.Helper()
	harness, err := testkit.NewHarnessAt(w, width, providerAuthoringFixtureHeight)
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	harness.Open()
	for _, key := range keys {
		harness.DispatchKey(key)
	}
	return harness.FrameTrimmed()
}

func runeKeys(value string) []tui.KeyEvent {
	keys := make([]tui.KeyEvent, 0, len(value))
	for _, char := range value {
		keys = append(keys, tui.KeyEvent{Key: tui.KeyRune, Rune: char})
	}
	return keys
}

func bedrockVisualConfig(t *testing.T, authentication, credential string, identity readmodel.AWSIdentityReadModel) *TargetConfig {
	t.Helper()
	w := authoringConfig(t, profile.ProviderSpecBedrock, "https://bedrock-mantle.eu-west-2.api.aws/anthropic/v1", credential)
	diagnosticAuthentication := strings.TrimSuffix(authentication, "_failure")
	catalog := readmodel.ModelCatalogReadModel{
		BedrockAuthentication: readmodel.BedrockAuthenticationEvidence{Authentication: readmodel.BedrockAuthenticationKind(diagnosticAuthentication), AWSIdentity: &identity},
	}
	if strings.HasSuffix(authentication, "_failure") {
		catalog.BedrockAuthentication.FailureStage = "authentication"
		catalog.BedrockAuthentication.Error = "AWS credentials are unavailable or expired"
	}
	if !strings.HasSuffix(authentication, "_failure") {
		catalog.Deployments = []readmodel.ModelDeploymentReadModel{{
			ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", ModelName: "Claude Sonnet 4.6",
		}}
	}
	if strings.HasSuffix(authentication, "_failure") {
		w.Catalog.Set(catalogOperationState{Result: catalog, Err: "authentication unavailable"})
	} else {
		w.Catalog.Set(catalogOperationState{Result: catalog})
	}
	if !strings.HasSuffix(authentication, "_failure") {
		selectReadyModel(w, "Claude Sonnet 4.6", "messages_stream")
	}
	return w
}

func renderBedrockVisual(t *testing.T, width int, authentication, credential string, identity readmodel.AWSIdentityReadModel, keys []tui.KeyEvent) string {
	t.Helper()
	w := bedrockVisualConfig(t, authentication, credential, identity)
	harness, err := testkit.NewHarnessAt(w, width, providerAuthoringFixtureHeight)
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	harness.Open()
	for _, key := range keys {
		harness.DispatchKey(key)
	}
	return harness.FrameTrimmed()
}

func renderCredentialJourney(t *testing.T, width int, keys []tui.KeyEvent, configure func(*TargetConfig)) string {
	t.Helper()
	w := authoringConfig(t, profile.ProviderSpecOpenAI, "", "")
	if configure != nil {
		configure(w)
	}
	harness, err := testkit.NewHarnessAt(w, width, providerAuthoringFixtureHeight)
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	harness.Open()
	for _, key := range keys {
		harness.DispatchKey(key)
	}
	return harness.FrameTrimmed()
}

func renderAzureCredentialRequired(t *testing.T, width int) string {
	t.Helper()
	w := authoringConfig(t, profile.ProviderSpecAzure, "", "")
	harness, err := testkit.NewHarnessAt(w, width, providerAuthoringFixtureHeight)
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	harness.Open()
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	for _, char := range "https://example.services.ai.azure.com/api/projects/demo" {
		harness.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: char})
	}
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	return harness.FrameTrimmed()
}

func renderChatGPTOpenFailure(t *testing.T, width int) string {
	t.Helper()
	var openedURL string
	restoreEffects := ui.RegisterEffectHooks(func(url string) error {
		openedURL = url
		return errors.New("no browser is available")
	}, nil, nil)
	defer restoreEffects()

	w := authoringConfig(t, profile.ProviderSpecChatGPT, "", "")
	w.AuthSession.Set(readmodel.AuthSessionReadModel{
		ProviderSpec: string(profile.ProviderSpecChatGPT),
		SessionID:    "login-1",
		State:        "pending",
		AuthorizeURL: chatGPTVisualLoginURL,
	})
	harness, err := testkit.NewHarnessAt(w, width, providerAuthoringFixtureHeight)
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	harness.Open()
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyUp})
	if frame := harness.FrameTrimmed(); !strings.Contains(frame, "> login URL") {
		t.Fatalf("login URL row was not selected before activation:\n%s", frame)
	}
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	if openedURL != chatGPTVisualLoginURL {
		t.Fatalf("opened URL = %q, want exact session URL %q", openedURL, chatGPTVisualLoginURL)
	}
	if got := w.Error.Get(); got != "" {
		t.Fatalf("open failure mutated form error = %q", got)
	}
	frame := harness.FrameTrimmed()
	if !strings.Contains(compactVisualLines(frame), chatGPTVisualLoginURL) {
		t.Fatalf("complete wrapped login URL is not visible:\n%s", frame)
	}
	return frame
}

func compactVisualLines(frame string) string {
	lines := strings.Split(frame, "\n")
	var compact strings.Builder
	for _, line := range lines {
		compact.WriteString(strings.TrimSpace(line))
	}
	return compact.String()
}

func renderChatGPTAuthModePicker(t *testing.T, width int) string {
	t.Helper()
	w := authoringConfig(t, profile.ProviderSpecChatGPT, "", "")
	harness, err := testkit.NewHarnessAt(w, width, providerAuthoringFixtureHeight)
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	harness.Open()
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	return harness.FrameTrimmed()
}

func renderChatGPTPendingAuthModePicker(t *testing.T, width int) string {
	t.Helper()
	w := authoringConfig(t, profile.ProviderSpecChatGPT, "", "")
	w.AuthSession.Set(readmodel.AuthSessionReadModel{
		ProviderSpec: string(profile.ProviderSpecChatGPT),
		SessionID:    "sess-browser",
		State:        "pending",
		AuthorizeURL: chatGPTVisualLoginURL,
	})
	harness, err := testkit.NewHarnessAt(w, width, providerAuthoringFixtureHeight)
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	harness.Open()
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyUp})
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyUp})
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	return harness.FrameTrimmed()
}

func renderChatGPTDevicePending(t *testing.T, width int) string {
	t.Helper()
	commands := &authCommandsStub{
		started: make(chan struct{}),
		polled:  make(chan struct{}, 1),
		start: func(_ context.Context, req ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
			if req.AuthMode != "device" {
				t.Fatalf("auth mode = %q, want device", req.AuthMode)
			}
			return readmodel.AuthSessionReadModel{
				ProviderSpec: string(profile.ProviderSpecChatGPT),
				SessionID:    "sess-device",
				AuthorizeURL: "https://auth.openai.com/codex/device",
				UserCode:     "ABCD-EFGH",
				State:        "pending",
			}, nil
		},
		poll: func(context.Context, string) (readmodel.AuthSessionReadModel, error) {
			return readmodel.AuthSessionReadModel{State: "pending"}, nil
		},
	}
	w := authoringConfig(t, profile.ProviderSpecChatGPT, "", "")
	w.TargetAuthCommands = commands
	harness, err := testkit.NewHarnessAt(w, width, providerAuthoringFixtureHeight)
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	harness.Open()
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	return harness.FrameTrimmed()
}

func authoringConfig(t *testing.T, provider profile.ProviderID, locator, credential string) *TargetConfig {
	t.Helper()
	peer := readmodel.TargetReadModel{ID: "primary", Model: "existing-model", Provider: "openai"}
	route := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{peer}}}}
	w := NewTargetConfig("dev", route, nil, nil)
	w.Open()
	w.SelectProvider(string(provider))
	if locator != "" {
		w.BaseURL.Set(locator)
		w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
			d.Locator = locator
			if provider == profile.ProviderSpecBedrock {
				d.Locator = profile.BedrockMantleRegionFromEndpoint(locator)
				d.Endpoint = locator
			}
			return d
		})
	}
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.CredentialRef = credential
		return d
	})
	return w
}

func selectReadyModel(w *TargetConfig, model, protocol string) {
	catalog := w.Catalog.Get()
	catalog.Result.Deployments = []readmodel.ModelDeploymentReadModel{{ID: model, Name: model, ModelName: model}}
	w.Catalog.Set(catalog)
	w.SelectedModel.Set(readmodel.ModelDeploymentReadModel{ID: model, Name: model, ModelName: model})
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.ModelID = model
		d.ProviderProtocol = protocol
		return d
	})
}
