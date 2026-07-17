package target_config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/mountedrender"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
)

func TestPhase_IsTerminal(t *testing.T) {
	if PhaseClosed.IsTerminal() {
		t.Fatal("Closed should not be terminal")
	}
	if !PhaseCreated.IsTerminal() {
		t.Fatal("Created should be terminal")
	}
	if PhaseCatalogFailed.IsTerminal() {
		t.Fatal("CatalogFailed must remain recoverable")
	}
	if PhaseAuthFailed.IsTerminal() {
		t.Fatal("AuthFailed must remain recoverable")
	}
	if PhaseCreateFailed.IsTerminal() {
		t.Fatal("CreateFailed must remain recoverable")
	}
	if PhaseConfiguring.IsTerminal() {
		t.Fatal("ChoosingProvider should not be terminal")
	}
}

func TestTargetConfig_DefaultPhaseIsClosed(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	if w.Phase.Get() != PhaseClosed {
		t.Fatalf("initial phase = %v, want Closed", w.Phase.Get())
	}
}

func TestTargetConfig_OpenMovesToChoosingProvider(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase after Open = %v, want ChoosingProvider", w.Phase.Get())
	}
}

func TestTargetConfig_ChoosingProviderRendersSelectablePickerRows(t *testing.T) {
	opts := make([]readmodel.ProviderOptionReadModel, 112)
	for i := range opts {
		opts[i] = readmodel.ProviderOptionReadModel{
			ProviderSpec: fmt.Sprintf("provider-%03d", i),
			DisplayName:  fmt.Sprintf("Provider %d", i),
			SetupHint:    "API key",
		}
	}

	w := newTargetConfigSeededWithProviders("dev", sampleRoute(), nil, nil, opts)
	w.Open()

	rendered, err := mountedrender.String(w, 120, 20)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase = %v, want ChoosingProvider", w.Phase.Get())
	}
	if got, want := strings.Count(rendered, "Provider "), 7; got != want {
		t.Fatalf("visible provider rows = %d, want %d\n%s", got, want, rendered)
	}
	if !strings.Contains(rendered, "> Provider 0") {
		t.Fatalf("first provider option should be selected:\n%s", rendered)
	}
	if !strings.Contains(rendered, "7 of 112 shown") {
		t.Fatalf("provider picker footer should show bounded count:\n%s", rendered)
	}
	if strings.Contains(rendered, "base URL") || strings.Contains(rendered, "credential") || strings.Contains(rendered, "model _") || strings.Contains(rendered, "provider/model") {
		t.Fatalf("provider picker should not leak setup or raw input rows:\n%s", rendered)
	}
}

func TestTargetConfig_ProviderPickerFilters(t *testing.T) {
	opts := []readmodel.ProviderOptionReadModel{
		{ProviderSpec: "openai", DisplayName: "OpenAI", SetupHint: "API key"},
		{ProviderSpec: "chatgpt", DisplayName: "ChatGPT", SetupHint: "browser login"},
		{ProviderSpec: "anthropic", DisplayName: "Anthropic", SetupHint: "API key"},
		{ProviderSpec: "openrouter", DisplayName: "OpenRouter", SetupHint: "API key"},
		{ProviderSpec: "ollama", DisplayName: "Ollama", SetupHint: "none"},
		{ProviderSpec: "azure", DisplayName: "Azure AI", SetupHint: "endpoint"},
		{ProviderSpec: "openai_compatible", DisplayName: "Custom Endpoint", SetupHint: "endpoint"},
	}

	w := newTargetConfigSeededWithProviders("dev", sampleRoute(), nil, nil, opts)
	w.Open()

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	for _, r := range "open" {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: r})
	}
	rendered := h.Frame()

	// Prefix: OpenAI, OpenRouter. Token-subsequence: Custom Endpoint.
	// ChatGPT does NOT match "open" (spec=chatgpt, display=ChatGPT).
	if !strings.Contains(rendered, "3 of 7 shown") {
		t.Fatalf("expected '3 of 7 shown' footer after filtering:\n%s", rendered)
	}
	if strings.Contains(rendered, "Ollama") {
		t.Fatalf("filtered list should NOT contain Ollama:\n%s", rendered)
	}
	if strings.Contains(rendered, "Anthropic") {
		t.Fatalf("filtered list should NOT contain Anthropic:\n%s", rendered)
	}
	if strings.Contains(rendered, "Azure AI") {
		t.Fatalf("filtered list should NOT contain Azure AI:\n%s", rendered)
	}
	for _, want := range []string{"OpenAI", "OpenRouter", "Custom Endpoint"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("filtered list missing %q:\n%s", want, rendered)
		}
	}
	if !strings.Contains(rendered, "> OpenAI") {
		t.Fatalf("expected OpenAI to be focused after filtering:\n%s", rendered)
	}
}

func TestTargetConfig_ProviderPickerNoResults(t *testing.T) {
	opts := []readmodel.ProviderOptionReadModel{
		{ProviderSpec: "openai", DisplayName: "OpenAI", SetupHint: "API key"},
		{ProviderSpec: "ollama", DisplayName: "Ollama", SetupHint: "none"},
	}

	w := newTargetConfigSeededWithProviders("dev", sampleRoute(), nil, nil, opts)
	w.Open()

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	for _, r := range "xyz" {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: r})
	}
	rendered := h.Frame()

	if !strings.Contains(rendered, "0 of 2 shown") {
		t.Fatalf("expected '0 of 2 shown' footer for empty search:\n%s", rendered)
	}
	if !strings.Contains(rendered, "provider") {
		t.Fatalf("expected 'provider' title in empty picker:\n%s", rendered)
	}
}

func TestTargetConfig_SelectProviderMovesToProviderSetup(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase after SelectProvider = %v, want ProviderSetup", w.Phase.Get())
	}
	if w.Draft.Get().ProviderSpec != "openai" {
		t.Fatalf("provider = %q, want openai", w.Draft.Get().ProviderSpec)
	}
	if got := w.setupState().BlockReason; got != "enter credential" {
		t.Fatalf("provider setup block reason = %q, want enter credential", got)
	}
}

func TestTargetConfig_RenderUsesProviderSpecificOpenAISetupRows(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)

	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")

	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase after provider setup = %v, want LoadingCatalog", w.Phase.Get())
	}
	if !w.setupState().ReadyForCatalog {
		t.Fatal("OpenAI setup with explicit credential should be ready for catalog")
	}
	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if !strings.Contains(rendered, "new target · OpenAI") {
		t.Fatalf("expected provider title in setup frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "loading catalog…") || !strings.Contains(rendered, "wait") {
		t.Fatalf("expected loading row in setup frame:\n%s", rendered)
	}
	if strings.Contains(rendered, "provider/model") || strings.Contains(rendered, "base URL") {
		t.Fatalf("provider-specific setup should not leak raw input rows:\n%s", rendered)
	}
}

func TestTargetConfig_ChatGPTSelectProviderShowsAuthStartWithoutStartingSession(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)

	w.SelectProvider("chatgpt")

	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase after chatgpt setup = %v, want ProviderSetup", w.Phase.Get())
	}
	if w.AuthSession.Get().SessionID != "" {
		t.Fatalf("auth session should not start on SelectProvider, got %q", w.AuthSession.Get().SessionID)
	}
	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if !strings.Contains(rendered, "new target · ChatGPT") {
		t.Fatalf("expected chatgpt title in setup frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "browser login") || !strings.Contains(rendered, "start ↵") {
		t.Fatalf("expected interactive auth start row in setup frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "model") || !strings.Contains(rendered, "blocked") || !strings.Contains(rendered, "auth first") {
		t.Fatalf("expected model blocked row in setup frame:\n%s", rendered)
	}
	if strings.Contains(rendered, "signed in") || strings.Contains(rendered, "loading catalog…") {
		t.Fatalf("chatgpt setup should not advance into auth success or catalog loading:\n%s", rendered)
	}
	testkit.AssertVisual("chatgpt_auth_start").
		Fixture("testdata/target_config_component/fixture/chatgpt_auth_start.txt").
		Viewport(120, 40).
		Now(t, rendered)
}

func TestTargetConfig_AzureSetupShowsFlatScannableRows(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("azure")

	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase after azure select = %v, want ProviderSetup", w.Phase.Get())
	}
	if got := w.setupState().BlockReason; got != "enter project" {
		t.Fatalf("azure block reason = %q, want enter project", got)
	}

	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if !strings.Contains(rendered, "new target · Azure AI") || !strings.Contains(rendered, "cancel ↵") {
		t.Fatalf("expected unclipped azure parent row with cancel action:\n%s", rendered)
	}
	if strings.Contains(rendered, "new target · Azure AI                                                                                      can") {
		t.Fatalf("parent cancel action is clipped:\n%s", rendered)
	}
	if !strings.Contains(rendered, "provider          Azure AI") || !strings.Contains(rendered, "change ↵") {
		t.Fatalf("expected provider change row:\n%s", rendered)
	}
	if !strings.Contains(rendered, "> project           endpoint required") || !strings.Contains(rendered, "edit ↵") {
		t.Fatalf("expected focused project blocker row:\n%s", rendered)
	}
	for _, row := range [][]string{
		{"credential", "blocked", "project first"},
		{"deployment", "blocked", "project first"},
		{"protocol", "blocked", "deployment"},
		{"routing", "step 1"},
		{"create", "project first"},
	} {
		assertRenderedLineContains(t, rendered, row...)
	}
	if got := strings.Count(rendered, "> "); got != 1 {
		t.Fatalf("focused marker count = %d, want 1:\n%s", got, rendered)
	}
	testkit.AssertVisual("azure_project_endpoint_setup").
		Fixture("testdata/target_config_component/fixture/azure_project_endpoint_setup.txt").
		Viewport(120, 40).
		Now(t, rendered)
}

func TestTargetConfig_ProviderFormsMatchRFCShapeAtComponentWidths(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		title     string
		setupRow  string
		blocked   string
		forbidden []string
	}{
		{
			name:      "azure",
			provider:  "azure",
			title:     "new target · Azure AI",
			setupRow:  "project endpoint required edit ↵",
			blocked:   "deployment blocked project first",
			forbidden: []string{"Azure AI Foundry", "resource locator"},
		},
		{
			name:      "openai-compatible",
			provider:  "openai_compatible",
			title:     "new target · Custom Endpoint",
			setupRow:  "backend URL enter backend URL edit ↵",
			blocked:   "model blocked backend first",
			forbidden: []string{"base URL", "setup incomplete"},
		},
		{
			name:      "bedrock",
			provider:  "bedrock",
			title:     "new target · AWS Bedrock",
			setupRow:  "region required choose ↵",
			blocked:   "model blocked region first",
			forbidden: []string{"Mantle URL", "auth mode", "bearer token"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, width := range []int{80, 100, 120} {
				t.Run(fmt.Sprintf("%d", width), func(t *testing.T) {
					w := NewTargetConfig("dev", sampleRoute(), nil, nil)
					w.SelectProvider(tt.provider)

					rendered, err := mountedrender.String(w, width, 16)
					if err != nil {
						t.Fatalf("RenderString: %v", err)
					}
					assertRenderedLineContains(t, rendered, strings.Fields(tt.title)...)
					if tt.provider == "bedrock" {
						assertRenderedLineContains(t, rendered, "connection", "Mantle", "default")
					}
					assertRenderedLineContains(t, rendered, strings.Fields(tt.setupRow)...)
					if tt.blocked != "" {
						assertRenderedLineContains(t, rendered, strings.Fields(tt.blocked)...)
					}
					for _, forbidden := range tt.forbidden {
						if strings.Contains(rendered, forbidden) {
							t.Fatalf("rendered forbidden RFC-residue %q at width %d:\n%s", forbidden, width, rendered)
						}
					}
				})
			}
		})
	}
}

func TestTargetConfig_BedrockProviderSelectionCanonicalizesBeforeFlowLookup(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider(" bedrock ")

	rendered, err := mountedrender.String(w, 120, 16)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	assertRenderedLineContains(t, rendered, "provider", "AWS Bedrock", "change", "↵")
	assertRenderedLineContains(t, rendered, "connection", "Mantle", "default")
	assertRenderedLineContains(t, rendered, "region", "required", "choose", "↵")
	if strings.Contains(rendered, "region            enter region") || strings.Contains(rendered, "region enter region edit ↵") {
		t.Fatalf("Bedrock selection fell through to generic endpoint input:\n%s", rendered)
	}
}

func TestTargetConfig_AzureReadyFlowUsesDeploymentLabel(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("azure")
	w.BaseURL.Set("https://contact-8837-resource.services.ai.azure.com/api/projects/contact-8837")
	w.Draft.Update(func(d endpointintent.TargetDraft) endpointintent.TargetDraft {
		d.CredentialRef = "env:AZURE_OPENAI_API_KEY"
		return d
	})
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1-prod", Name: "gpt-4.1-prod", ModelName: "gpt-4.1-prod"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1-prod", Name: "gpt-4.1-prod", ModelName: "gpt-4.1-prod"})

	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase = %v, want ReadyToCreate", w.Phase.Get())
	}
	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	// Protocol row is enterable (required) after a deployment with multiple
	// protocols; the operator opens it manually (no auto-open).
	assertRenderedLineContains(t, rendered, "protocol", "required", "choose", "↵")
	assertRenderedLineContains(t, rendered, "deployment", "gpt-4.1-prod", "change", "↵")
	assertRenderedLineContains(t, rendered, "protocol")
	if strings.Contains(rendered, "model       gpt-4.1-prod") || strings.Contains(rendered, "model             gpt-4.1-prod") {
		t.Fatalf("Azure selected deployment must not render with model label:\n%s", rendered)
	}
}

func TestTargetConfig_AzureEndpointRowAcceptsTyping(t *testing.T) {
	var got ports.ProbeProviderModelsRequest
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.TargetSetupQueries = fakeTargetSetupQueries{
		probe: func(_ context.Context, req ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
			got = req
			return readmodel.ModelCatalogReadModel{
				Deployments: []readmodel.ModelDeploymentReadModel{
					{ID: "kimi", Name: "Kimi", ModelName: "Kimi-K2.6", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
				},
			}, nil
		},
	}
	w.SelectProvider("azure")
	w.Draft.Update(func(d endpointintent.TargetDraft) endpointintent.TargetDraft {
		d.CredentialRef = "env:AZURE_OPENAI_API_KEY"
		return d
	})

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	testkit.AssertFocusedFrame(t, h.Frame(), "> project")
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	if !strings.Contains(h.Frame(), "save ↵") || !strings.Contains(h.Frame(), "_") {
		t.Fatalf("azure locator row did not enter edit mode:\n%s", h.Frame())
	}
	for _, r := range "https://example-resource.services.ai.azure.com/api/projects/example" {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: r})
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if got.BaseURL != "https://example-resource.services.ai.azure.com/api/projects/example" {
		t.Fatalf("probe base URL = %q, want typed endpoint", got.BaseURL)
	}
	if w.BaseURL.Get() != got.BaseURL {
		t.Fatalf("target config base URL = %q, want %q", w.BaseURL.Get(), got.BaseURL)
	}
}

func TestTargetConfig_AzureEndpointSubmitClearsProjectCursorBeforeCredential(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("azure")

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	testkit.AssertFocusedFrame(t, h.Frame(), "> project")
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	for _, r := range "https://contact-8837-resource.services.ai.azure.com/api/projects/contact-8837" {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: r})
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	frame := h.Frame()
	if got := strings.Count(frame, "> "); got != 1 {
		t.Fatalf("expected exactly one focus marker after endpoint submit, got %d:\n%s", got, frame)
	}
	if strings.Contains(frame, "> project") {
		t.Fatalf("project row kept stale edit marker after submit:\n%s", frame)
	}
	testkit.AssertFocusedFrame(t, frame, "> credential")
}

func TestTargetConfig_AzureRejectsExecutionEndpoint(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("azure")

	row := AzureProjectEndpointInput(w, true)
	row.OnSubmit("https://contact-8837-resource.openai.azure.com/openai/v1")

	if got := w.Error.Get(); got != "not a project endpoint" {
		t.Fatalf("error = %q, want not a project endpoint", got)
	}
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase = %v, want ProviderSetup", w.Phase.Get())
	}
	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	assertRenderedLineContains(t, rendered, "project", "https://contact-8837-resource.openai.azure.com/openai/v1", "edit", "↵")
	assertRenderedLineContains(t, rendered, "credential", "blocked", "project first")
	assertRenderedLineContains(t, rendered, "deployment", "blocked", "project first")
}

func TestTargetConfig_AzureRejectsRandomProjectHost(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("azure")

	row := AzureProjectEndpointInput(w, true)
	row.OnSubmit("https://foo.example.com/api/projects/x")

	if got := w.Error.Get(); got != "not an Azure AI project endpoint" {
		t.Fatalf("error = %q, want not an Azure AI project endpoint", got)
	}
}

func TestTargetConfig_AzureProjectRemainsVisibleAndCredentialIsEditable(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("azure")
	w.BaseURL.Set("https://contact-8837-resource.services.ai.azure.com/api/projects/contact-8837")
	w.advanceFromSetup()

	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase after endpoint = %v, want ProviderSetup", w.Phase.Get())
	}

	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	assertRenderedLineContains(t, rendered, "project", "contact-8837-resource/contact-8837", "edit", "↵")
	assertRenderedLineContains(t, rendered, "credential", "credential", "required", "choose", "↵")
	assertRenderedLineContains(t, rendered, "deployment", "blocked", "credential")
	if strings.Contains(rendered, "missing AZURE_OPENAI_API_KEY") {
		t.Fatalf("credential row should not render environment-default missing copy:\n%s", rendered)
	}

	row := CredentialControl(credentialFixture(w))
	row.Activate()
	credentialFixture(w).selectRef("env:AZURE_OPENAI_API_KEY")

	if w.Draft.Get().CredentialRef != "env:AZURE_OPENAI_API_KEY" {
		t.Fatalf("credential ref = %q, want env:AZURE_OPENAI_API_KEY", w.Draft.Get().CredentialRef)
	}
	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase after credential submit = %v, want LoadingCatalog", w.Phase.Get())
	}
}

func TestTargetConfig_CustomEndpointCredentialHeaderPickerUsesSharedOptions(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("openai_compatible")
	w.BaseURL.Set("https://lab.example/v1")
	w.advanceFromSetup()

	// The credential-header picker is the credential-header ui.Select body; the
	// row's entered state is owned by ui.Select, so the unit test builds the
	// picker directly (backout unused) to assert its option shape.
	picker := CredentialHeaderPicker(w, nil)
	if picker.Mode != ui.SearchPickerOpen {
		t.Fatalf("picker mode = %v, want SearchPickerOpen (open-set)", picker.Mode)
	}
	filtered := picker.Options
	if len(filtered) != 3 {
		t.Fatalf("picker option count = %d, want 3 common headers (no custom escape hatch)", len(filtered))
	}
	if filtered[0].Label != "Authorization" || filtered[1].Label != "x-api-key" || filtered[2].Label != "api-key" {
		t.Fatalf("picker labels = %#v", filtered)
	}
}

func TestTargetConfig_CustomEndpointCredentialHeaderTypedQueryCommitsCustomHeader(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("openai_compatible")
	w.BaseURL.Set("https://lab.example/v1")
	w.advanceFromSetup()
	picker := CredentialHeaderPicker(w, nil)

	// Typing a non-listed header and using the query commits it directly — no
	// "custom..." detour, no second overlay. Selecting commits and the row's
	// ui.Select backs out (the unit test drives the picker in isolation).
	picker.OnSelect(ui.Selection{Value: "X-Custom-Auth", Source: ui.SelectionQuery})

	if got := w.Draft.Get().ProviderOptions.OpenAICompatible.CredentialHeader; got != "X-Custom-Auth" {
		t.Fatalf("credential header = %q, want X-Custom-Auth", got)
	}
	if !w.CredentialHeaderEdited.Get() {
		t.Fatal("custom credential header should mark operator-edited state")
	}
}

func TestTargetConfig_CustomEndpointCredentialHeaderDefaultsFollowBackendURL(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "generic", baseURL: "https://lab.example/v1", want: "Authorization"},
		{name: "anthropic path", baseURL: "https://gw.example/anthropic/v1/messages", want: "x-api-key"},
		{name: "azure openai", baseURL: "https://foo.openai.azure.com/openai/deployments/bar", want: "api-key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := NewTargetConfig("dev", sampleRoute(), nil, nil)
			w.SelectProvider("openai_compatible")
			w.BaseURL.Set(tc.baseURL)
			w.advanceFromSetup()
			if got := w.Draft.Get().ProviderOptions.OpenAICompatible.CredentialHeader; got != tc.want {
				t.Fatalf("credential header = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTargetConfig_CustomEndpointHidesCredentialHeaderWhenCredentialIsNone(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("openai_compatible")
	w.BaseURL.Set("http://127.0.0.1:8000/v1")
	w.advanceFromSetup()
	if !w.CredentialIsNone() {
		t.Fatal("loopback custom endpoint should treat missing credential as none")
	}
	if w.ShouldRenderCredentialHeaderRow() {
		t.Fatal("credential header row should be hidden when credential is none")
	}
}

func TestTargetConfig_ProviderSetupFlowInventory(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*TargetConfig)
		wants  [][]string
		forbid []string
	}{
		{
			name:  "openai credential first",
			setup: func(w *TargetConfig) { w.SelectProvider("openai") },
			wants: [][]string{
				{"provider", "OpenAI", "change", "↵"},
				{"credential", "credential", "required", "choose", "↵"},
				{"model", "blocked", "credential"},
			},
			forbid: []string{"endpoint", "credential header"},
		},
		{
			name: "openai credential visible while loading",
			setup: func(w *TargetConfig) {
				w.SelectProvider("openai")
				w.Draft.Update(func(d endpointintent.TargetDraft) endpointintent.TargetDraft {
					d.CredentialRef = "env:OPENAI_API_KEY"
					return d
				})
				w.advanceFromSetup()
			},
			wants: [][]string{
				{"provider", "OpenAI", "change", "↵"},
				{"credential", "env:OPENAI_API_KEY", "change", "↵"},
				{"model", "loading catalog", "wait"},
			},
		},
		{
			name:  "chatgpt auth first",
			setup: func(w *TargetConfig) { w.SelectProvider("chatgpt") },
			wants: [][]string{
				{"provider", "ChatGPT", "change", "↵"},
				{"auth", "browser login", "start", "↵"},
				{"model", "blocked", "auth first"},
			},
			forbid: []string{"credential", "endpoint", "credential header"},
		},
		{
			name:  "anthropic credential first",
			setup: func(w *TargetConfig) { w.SelectProvider("anthropic") },
			wants: [][]string{
				{"provider", "Anthropic", "change", "↵"},
				{"credential", "credential", "required", "choose", "↵"},
				{"model", "blocked", "credential"},
			},
			forbid: []string{"endpoint", "credential header"},
		},
		{
			name:  "openrouter credential first",
			setup: func(w *TargetConfig) { w.SelectProvider("openrouter") },
			wants: [][]string{
				{"provider", "OpenRouter", "change", "↵"},
				{"credential", "credential", "required", "choose", "↵"},
				{"model", "blocked", "credential"},
			},
			forbid: []string{"endpoint", "credential header"},
		},
		{
			name: "ollama jumps directly to model catalog",
			setup: func(w *TargetConfig) {
				w.SelectProvider("ollama")
				w.ContinueSetup()
			},
			wants: [][]string{
				{"provider", "Ollama", "change", "↵"},
				{"model", "loading catalog", "wait"},
			},
			forbid: []string{"credential", "endpoint", "credential header"},
		},
		{
			name:  "azure project first",
			setup: func(w *TargetConfig) { w.SelectProvider("azure") },
			wants: [][]string{
				{"provider", "Azure AI", "change", "↵"},
				{"project", "endpoint", "required", "edit", "↵"},
				{"credential", "blocked", "project", "first"},
				{"deployment", "blocked", "project", "first"},
				{"protocol", "blocked", "deployment"},
				{"create", "project", "first"},
			},
			forbid: []string{"credential header"},
		},
		{
			name: "azure keeps endpoint while asking for credential",
			setup: func(w *TargetConfig) {
				w.SelectProvider("azure")
				w.BaseURL.Set("https://contact-8837-resource.services.ai.azure.com/api/projects/contact-8837")
				w.advanceFromSetup()
			},
			wants: [][]string{
				{"provider", "Azure AI", "change", "↵"},
				{"project", "contact-8837-resource/contact-8837", "edit", "↵"},
				{"credential", "credential", "required", "choose", "↵"},
				{"deployment", "blocked", "credential"},
				{"protocol", "blocked", "deployment"},
				{"create", "credential"},
			},
			forbid: []string{"credential header"},
		},
		{
			name: "azure keeps setup facts while loading deployment catalog",
			setup: func(w *TargetConfig) {
				w.SelectProvider("azure")
				w.BaseURL.Set("https://contact-8837-resource.services.ai.azure.com/api/projects/contact-8837")
				w.Draft.Update(func(d endpointintent.TargetDraft) endpointintent.TargetDraft {
					d.CredentialRef = "env:AZURE_OPENAI_API_KEY"
					return d
				})
				w.advanceFromSetup()
			},
			wants: [][]string{
				{"provider", "Azure AI", "change", "↵"},
				{"project", "contact-8837-resource/contact-8837", "edit", "↵"},
				{"credential", "env:AZURE_OPENAI_API_KEY", "change", "↵"},
				{"deployment", "loading", "wait"},
				{"protocol", "blocked", "deployment"},
				{"create", "model", "first"},
			},
			forbid: []string{"credential header"},
		},
		{
			name:  "openai compatible backend first",
			setup: func(w *TargetConfig) { w.SelectProvider("openai_compatible") },
			wants: [][]string{
				{"provider", "Custom Endpoint", "change", "↵"},
				{"backend URL", "enter backend URL", "edit", "↵"},
				{"model", "blocked", "backend first"},
			},
			forbid: []string{"credential header", "credential"},
		},
		{
			name: "custom endpoint keeps backend and credential header while asking credential",
			setup: func(w *TargetConfig) {
				w.SelectProvider("openai_compatible")
				w.BaseURL.Set("https://lab.example/v1")
				w.advanceFromSetup()
			},
			wants: [][]string{
				{"provider", "Custom Endpoint", "change", "↵"},
				{"backend URL", "https://lab.example/v1", "edit", "↵"},
				{"credential header", "Authorization", "change", "↵"},
				{"credential", "credential", "required", "choose", "↵"},
				{"model", "blocked", "credential"},
			},
		},
		{
			name: "custom endpoint loopback hides credential header when none",
			setup: func(w *TargetConfig) {
				w.SelectProvider("openai_compatible")
				w.BaseURL.Set("http://127.0.0.1:8000/v1")
				w.advanceFromSetup()
			},
			wants: [][]string{
				{"provider", "Custom Endpoint", "change", "↵"},
				{"backend URL", "http://127.0.0.1:8000/v1", "edit", "↵"},
				{"credential", "none", "change", "↵"},
				{"model", "loading catalog", "wait"},
			},
			forbid: []string{"credential header"},
		},
		{
			name:  "bedrock region first",
			setup: func(w *TargetConfig) { w.SelectProvider("bedrock") },
			wants: [][]string{
				{"provider", "AWS Bedrock", "change", "↵"},
				{"connection", "Mantle", "default"},
				{"region", "required", "choose", "↵"},
				{"model", "blocked", "region first"},
			},
			forbid: []string{"Mantle URL", "auth mode", "bearer token", "endpoint"},
		},
		{
			name: "bedrock keeps region and endpoint while asking auth",
			setup: func(w *TargetConfig) {
				w.SelectProvider("bedrock")
				w.SelectBedrockRegion("eu-west-2")
			},
			wants: [][]string{
				{"provider", "AWS Bedrock", "change", "↵"},
				{"connection", "Mantle", "default"},
				{"region", "eu-west-2", "change", "↵"},
				{"endpoint", "https://bedrock-mantle.eu-west-2.api.aws/v1", "derived"},
				{"auth", "required", "choose", "↵"},
				{"model", "blocked", "auth first"},
			},
			forbid: []string{"Mantle URL", "auth mode", "bearer token"},
		},
		{
			name: "bedrock aws profile auth asks profile before loading catalog",
			setup: func(w *TargetConfig) {
				w.SelectProvider("bedrock")
				w.SelectBedrockRegion("eu-west-2")
				w.SelectProviderAuthMode("aws_profile")
			},
			wants: [][]string{
				{"provider", "AWS Bedrock", "change", "↵"},
				{"connection", "Mantle", "default"},
				{"region", "eu-west-2", "change", "↵"},
				{"endpoint", "https://bedrock-mantle.eu-west-2.api.aws/v1", "derived"},
				{"auth", "AWS profile", "change", "↵"},
				{"profile", "required", "choose", "↵"},
				{"model", "blocked", "profile first"},
			},
			forbid: []string{"Mantle URL", "auth mode", "bearer token", "credential        ", "source"},
		},
		{
			name: "bedrock aws env auth loads catalog from sdk chain",
			setup: func(w *TargetConfig) {
				w.SelectProvider("bedrock")
				w.SelectBedrockRegion("eu-west-2")
				w.SelectProviderAuthMode("aws_env_session")
			},
			wants: [][]string{
				{"provider", "AWS Bedrock", "change", "↵"},
				{"connection", "Mantle", "default"},
				{"region", "eu-west-2", "change", "↵"},
				{"endpoint", "https://bedrock-mantle.eu-west-2.api.aws/v1", "derived"},
				{"auth", "AWS env", "change", "↵"},
				{"model", "loading catalog", "wait"},
			},
			forbid: []string{"Mantle URL", "auth mode", "bearer token", "credential        ", "profile           ", "source"},
		},
		{
			name: "bedrock api key auth asks credential after auth",
			setup: func(w *TargetConfig) {
				w.SelectProvider("bedrock")
				w.SelectBedrockRegion("eu-west-2")
				w.SelectProviderAuthMode("env")
			},
			wants: [][]string{
				{"provider", "AWS Bedrock", "change", "↵"},
				{"connection", "Mantle", "default"},
				{"region", "eu-west-2", "change", "↵"},
				{"endpoint", "https://bedrock-mantle.eu-west-2.api.aws/v1", "derived"},
				{"auth", "Bedrock API key", "change", "↵"},
				{"credential", "credential", "required", "choose", "↵"},
				{"model", "blocked", "credential"},
			},
			forbid: []string{"Mantle URL", "auth mode", "AWS profile"},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			w := NewTargetConfig("dev", sampleRoute(), nil, nil)
			tt.setup(w)

			rendered, err := mountedrender.String(w, 120, 24)
			if err != nil {
				t.Fatalf("RenderString: %v", err)
			}
			for _, want := range tt.wants {
				assertRenderedLineContains(t, rendered, want...)
			}
			for _, forbidden := range tt.forbid {
				if strings.Contains(rendered, forbidden) {
					t.Fatalf("rendered forbidden flow row %q:\n%s", forbidden, rendered)
				}
			}
		})
	}
}

func TestTargetConfig_CredentialChooserFocusMarkers(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("openai")
	credentialFixture(w).open()

	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}

	lines := strings.Split(rendered, "\n")
	markerCount := 0
	for _, line := range lines {
		if strings.Contains(line, "> ") {
			markerCount++
		}
	}
	t.Logf("Rendered frame:\n%s", rendered)
	if markerCount != 1 {
		t.Fatalf("expected exactly 1 focus marker (> ) in credential chooser, got %d\n%s", markerCount, rendered)
	}
	if strings.Contains(rendered, "\n     credential\n") {
		t.Fatalf("credential chooser should not render a duplicate title row:\n%s", rendered)
	}
	assertRenderedLineContains(t, rendered, "credential", "required", "choose", "↵")
	credentialLine := renderedLineContaining(t, rendered, "credential", "required", "choose", "↵")
	envLine := renderedLineContaining(t, rendered, "env var", "select", "↵")
	fileLine := renderedLineContaining(t, rendered, "file", "select", "↵")
	pasteLine := renderedLineContaining(t, rendered, "paste secret", "select", "↵")
	credentialColumn := strings.Index(credentialLine, "credential")
	envColumn := strings.Index(envLine, "env var")
	fileColumn := strings.Index(fileLine, "file")
	pasteColumn := strings.Index(pasteLine, "paste secret")
	markerColumn := strings.Index(envLine, ">")
	if envColumn <= credentialColumn {
		t.Fatalf("credential child options should be visually nested under credential row:\n%s", rendered)
	}
	if markerColumn < 0 || envColumn-markerColumn > len("> ")+1 {
		t.Fatalf("credential child option value should follow its marker without a label gutter:\n%s", rendered)
	}
	if fileColumn != envColumn || pasteColumn != envColumn {
		t.Fatalf("credential child options should share one nested column:\n%s", rendered)
	}
}

func TestTargetConfig_ProviderFormFramesHaveOneFocusMarker(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	frames := map[string]string{}

	credential := NewTargetConfig("dev", sampleRoute(), nil, nil)
	credential.Open()
	credential.SelectProvider("openai")
	credentialFixture(credential).open()
	frames["credential chooser"] = renderTargetConfigFrame(t, credential, 120, 24)

	env := NewTargetConfig("dev", sampleRoute(), nil, nil)
	env.Open()
	env.SelectProvider("openai")
	credentialFixture(env).open()
	credentialFixture(env).openEnv()
	frames["env credential"] = renderTargetConfigFrame(t, env, 120, 24)

	model := NewTargetConfig("dev", sampleRoute(), nil, nil)
	model.Open()
	model.SelectProvider("openai")
	model.SetSetupReady("env:OPENAI_API_KEY", "")
	model.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "babbage-002", Name: "babbage-002", ModelName: "babbage-002", SupportedProviderProtocols: []string{"chat_completions"}},
			{ID: "davinci-002", Name: "davinci-002", ModelName: "davinci-002", SupportedProviderProtocols: []string{"chat_completions"}},
		},
	})
	frames["model picker"] = renderTargetConfigFrame(t, model, 120, 24)

	for name, frame := range frames {
		assertOneFocusMarker(t, name, frame)
	}
}

func TestTargetConfig_OpenAIEnvCredentialFlowHasNoDetachedSaveRow(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	credentialFixture(w).open()
	credentialFixture(w).openEnv()

	rendered := renderTargetConfigFrame(t, w, 120, 24)
	assertRenderedLineContains(t, rendered, "env var", "OPENAI_API_KEY", "edit", "↵")
	if strings.Contains(rendered, "save") {
		t.Fatalf("env credential source must not render a detached save row:\n%s", rendered)
	}
}

func TestTargetConfig_PasteCredentialFlowHasNoDetachedSaveRow(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	credentialFixture(w).open()
	credentialFixture(w).openPaste()

	rendered := renderTargetConfigFrame(t, w, 120, 24)
	assertRenderedLineContains(t, rendered, "secret", "_", "save", "↵")
	if strings.Contains(rendered, "name") || strings.Contains(rendered, "openai") && strings.Contains(rendered, "name") {
		t.Fatalf("paste credential source must not ask for an internal key name:\n%s", rendered)
	}
	if strings.Contains(rendered, "\n     save") || strings.Contains(rendered, "\n        save") {
		t.Fatalf("paste credential source must not render a detached save row:\n%s", rendered)
	}
}

func TestTargetConfig_PasteSecretSelectionImmediatelyEditsSecret(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	credentialFixture(w).open()

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	frame := h.Frame()
	assertRenderedLineContains(t, frame, "secret", "_", "save", "↵")
	if strings.Contains(frame, "paste secret") {
		t.Fatalf("paste secret menu should be replaced by the editor after one Enter:\n%s", frame)
	}
}

func TestTargetConfig_PasteSecretEditKeepsSelectionAndEnterSubmits(t *testing.T) {
	var got ports.StorePastedCredentialRequest
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.CredentialCommands = fakeTargetCredentialCommands{
		store: func(_ context.Context, req ports.StorePastedCredentialRequest) (ports.StorePastedCredentialResult, error) {
			got = req
			return ports.StorePastedCredentialResult{CredentialRef: "secret:" + req.Name}, nil
		},
	}
	w.Open()
	w.SelectProvider("openai")
	credentialFixture(w).open()

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 's'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'k'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if got.Secret != "sk" {
		t.Fatalf("stored secret = %q, want sk", got.Secret)
	}
	if !strings.HasPrefix(w.Draft.Get().CredentialRef, "secret:") {
		t.Fatalf("credential ref = %q, want stored secret ref", w.Draft.Get().CredentialRef)
	}
	if frame := h.Frame(); strings.Contains(frame, "> protocol") {
		t.Fatalf("secret edit Down should not move selection to protocol before submit:\n%s", frame)
	}
}

func TestTargetConfig_DisclosureKeepsContinuationRowsVisible(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")

	credential := NewTargetConfig("dev", sampleRoute(), nil, nil)
	credential.Open()
	credential.SelectProvider("openai")
	credentialFixture(credential).open()
	credentialFrame := renderTargetConfigFrame(t, credential, 120, 24)
	assertRenderedLineContains(t, credentialFrame, "model", "blocked", "credential")
	assertRenderedLineContains(t, credentialFrame, "protocol", "blocked", "model first")
	assertRenderedLineContains(t, credentialFrame, "routing", "fallback after step 1", "change", "↵")
	assertRenderedLineContains(t, credentialFrame, "create", "credential")

	model := NewTargetConfig("dev", sampleRoute(), nil, nil)
	model.Open()
	model.SelectProvider("openai")
	model.SetSetupReady("env:OPENAI_API_KEY", "")
	model.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "babbage-002", Name: "babbage-002", ModelName: "babbage-002", SupportedProviderProtocols: []string{"chat_completions"}},
			{ID: "davinci-002", Name: "davinci-002", ModelName: "davinci-002", SupportedProviderProtocols: []string{"chat_completions"}},
		},
	})
	modelFrame := renderTargetConfigFrame(t, model, 120, 24)
	assertRenderedLineContains(t, modelFrame, "protocol", "blocked", "model first")
	assertRenderedLineContains(t, modelFrame, "routing", "fallback after step 1", "change", "↵")
	assertRenderedLineContains(t, modelFrame, "create", "model first")
}

func TestTargetConfig_CredentialChooserRequiresExplicitSelection(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("openai")

	if got := w.Draft.Get().CredentialRef; got != "" {
		t.Fatalf("credential ref = %q, want empty before explicit choice", got)
	}

	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	assertRenderedLineContains(t, rendered, "credential", "credential", "required", "choose", "↵")
	if strings.Contains(rendered, "env:OPENAI_API_KEY") {
		t.Fatalf("suggested env ref should not appear before chooser opens:\n%s", rendered)
	}

	credentialFixture(w).open()
	rendered, err = mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	assertRenderedLineContains(t, rendered, "env var", "select", "↵")
	assertRenderedLineContains(t, rendered, "file", "select", "↵")
	assertRenderedLineContains(t, rendered, "paste secret", "select", "↵")
	if got := w.Draft.Get().CredentialRef; got != "" {
		t.Fatalf("credential ref = %q, want empty after viewing menu", got)
	}

	credentialFixture(w).openEnv()
	rendered, err = mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	assertRenderedLineContains(t, rendered, "env var", "OPENAI_API_KEY", "edit", "↵")
	if strings.Contains(rendered, "save") {
		t.Fatalf("credential env source must not render a detached save row:\n%s", rendered)
	}
	if got := w.Draft.Get().CredentialRef; got != "" {
		t.Fatalf("credential ref = %q, want empty after viewing env suggestion", got)
	}

	credentialFixture(w).saveEnv(credentialFixture(w).envName.Get())
	if got := w.Draft.Get().CredentialRef; got != "env:OPENAI_API_KEY" {
		t.Fatalf("credential ref = %q, want explicit env ref", got)
	}
	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase after credential selection = %v, want LoadingCatalog", w.Phase.Get())
	}
}

func TestTargetConfig_CredentialFileBrowserRendersWithOneFocusMarker(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("openai")
	credentialFixture(w).open()
	credentialFixture(w).openFile()

	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	// File browser should show title + dir row + "../" + entries + hint.
	assertRenderedLineContains(t, rendered, "credential", "file")
	if !strings.Contains(rendered, "dir") {
		t.Fatalf("file browser missing dir row:\n%s", rendered)
	}
	if !strings.Contains(rendered, "../") {
		t.Fatalf("file browser missing ../ entry:\n%s", rendered)
	}
	if !strings.Contains(rendered, "open") {
		t.Fatalf("file browser missing open action:\n%s", rendered)
	}
	if !strings.Contains(rendered, "search") {
		t.Fatalf("file browser missing hint:\n%s", rendered)
	}
	if got := strings.Count(rendered, ">"); got != 1 {
		t.Fatalf("file browser should render exactly one focus marker, got %d:\n%s", got, rendered)
	}
}

func TestTargetConfig_CredentialMenuEscapeReturnsToProviderSetup(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("openai")
	credentialFixture(w).open()

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	focusRowUntilFrameContains(t, h, "> file", 24)
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if w.Phase.Get() == PhaseClosed {
		t.Fatal("Escape from credential menu closed the whole target config")
	}
	if got := credentialFixture(w).stage.Get(); got != credStageClosed {
		t.Fatalf("credential stage after Escape = %v, want closed", got)
	}
}

// focusRowUntilFrameContains presses Down until the mounted frame shows want or
// steps is exhausted. It proves Escape behavior at a specific focused overlay
// row rather than at whatever row happened to auto-focus.
func focusRowUntilFrameContains(t *testing.T, h *testkit.MockAppHarness, want string, steps int) {
	t.Helper()
	for i := 0; i < steps; i++ {
		if strings.Contains(h.Frame(), want) {
			return
		}
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	}
	if !strings.Contains(h.Frame(), want) {
		t.Fatalf("focus never reached %q after %d steps:\n%s", want, steps, h.Frame())
	}
}

func TestTargetConfig_EscapeOnUnenteredInteriorRowClosesFeature(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.Draft.Update(func(d endpointintent.TargetDraft) endpointintent.TargetDraft {
		d.CredentialRef = "secret:openai"
		return d
	})
	w.SelectedModel.Set(readmodel.ModelDeploymentReadModel{ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"responses"}, DefaultProviderProtocol: "responses"})
	w.Draft.Update(func(d endpointintent.TargetDraft) endpointintent.TargetDraft {
		d.ProviderProtocol = "responses"
		return d
	})
	w.Phase.Set(PhaseReadyToCreate)

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	for i := 0; i < 24 && !strings.Contains(h.Frame(), "> model"); i++ {
		h.FocusNext()
	}
	if !strings.Contains(h.Frame(), "> model") {
		t.Fatalf("focus never reached model row:\n%s", h.Frame())
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if got := w.Phase.Get(); got != PhaseClosed {
		t.Fatalf("Escape on unentered interior row should close target config, phase=%v\nframe:\n%s", got, h.Frame())
	}
}

func TestTargetConfig_CredentialFileBrowserEscapeReturnsToCredentialMenu(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("openai")
	credentialFixture(w).open()
	credentialFixture(w).openFile()

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if w.Phase.Get() == PhaseClosed {
		t.Fatal("Escape from file browser closed the whole target config")
	}
	if got := credentialFixture(w).stage.Get(); got != credStageMenu {
		t.Fatalf("credential stage after Escape = %v, want menu", got)
	}
}

func TestTargetConfig_CredentialChooserFileRefSupportsProvidersWithoutEnvSuggestion(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("bedrock")
	w.SelectBedrockRegion("eu-west-2")
	w.SelectProviderAuthMode("env")
	credentialFixture(w).open()

	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if strings.Contains(rendered, "env:OPENAI_API_KEY") {
		t.Fatalf("Bedrock API-key chooser should not borrow another provider suggestion:\n%s", rendered)
	}
	if strings.Contains(rendered, "none") {
		t.Fatalf("Bedrock API-key chooser should not allow none:\n%s", rendered)
	}
	assertRenderedLineContains(t, rendered, "env var", "select", "↵")
	assertRenderedLineContains(t, rendered, "file", "select", "↵")

	credentialFixture(w).openFile()
	credentialFixture(w).filePath.Set("/tmp/bedrock-token")
	credentialFixture(w).saveFile(credentialFixture(w).filePath.Get())
	if got := w.Draft.Get().CredentialRef; got != "file:/tmp/bedrock-token" {
		t.Fatalf("credential ref = %q, want file ref", got)
	}
	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase after file credential = %v, want LoadingCatalog", w.Phase.Get())
	}
}

func TestTargetConfig_CredentialChooserNoneOnlyWhenProviderPolicyAllows(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("openai_compatible")
	w.BaseURL.Set("http://127.0.0.1:8000/v1")
	w.refreshSetup()
	credentialFixture(w).open()

	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	assertRenderedLineContains(t, rendered, "none", "select", "↵")

	credentialFixture(w).selectNone()
	if got := w.Draft.Get().CredentialRef; got != "" {
		t.Fatalf("credential ref = %q, want empty for none", got)
	}
	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase after none credential = %v, want LoadingCatalog", w.Phase.Get())
	}

	remote := NewTargetConfig("dev", sampleRoute(), nil, nil)
	remote.SelectProvider("openai_compatible")
	remote.BaseURL.Set("https://remote.example/v1")
	remote.advanceFromSetup()
	credentialFixture(remote).open()
	remoteRendered, err := mountedrender.String(remote, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if strings.Contains(remoteRendered, "none") {
		t.Fatalf("remote backend should not render none action:\n%s", remoteRendered)
	}
}

func TestTargetConfig_PasteSecretStoresCredentialRefOnly(t *testing.T) {
	var got ports.StorePastedCredentialRequest
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.CredentialCommands = fakeTargetCredentialCommands{
		store: func(_ context.Context, req ports.StorePastedCredentialRequest) (ports.StorePastedCredentialResult, error) {
			got = req
			return ports.StorePastedCredentialResult{CredentialRef: "secret:" + req.Name}, nil
		},
	}
	w.SelectProvider("openai")
	credentialFixture(w).open()
	credentialFixture(w).openPaste()

	credentialFixture(w).secret.Set("")
	credentialFixture(w).savePasted(context.TODO())
	if w.Error.Get() != "secret first" {
		t.Fatalf("error = %q, want secret first", w.Error.Get())
	}

	credentialFixture(w).secret.Set("sk-test")
	credentialFixture(w).savePasted(context.TODO())
	if got.ProviderSpec != "openai" || got.Secret != "sk-test" {
		t.Fatalf("store request = %#v", got)
	}
	if !strings.HasPrefix(got.Name, "cockpit/target/openai/dev/gpt/target/") {
		t.Fatalf("generated credential slot = %q, want semantic cockpit target prefix", got.Name)
	}
	if strings.TrimPrefix(got.Name, "cockpit/target/openai/dev/gpt/target/") == "" {
		t.Fatalf("generated credential slot = %q, want unique suffix", got.Name)
	}
	if w.Draft.Get().CredentialRef != "secret:"+got.Name {
		t.Fatalf("credential ref = %q, want returned secret ref", w.Draft.Get().CredentialRef)
	}
	if credentialFixture(w).secret.Get() != "" {
		t.Fatalf("secret should be cleared after store, got %q", credentialFixture(w).secret.Get())
	}
	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase after pasted secret = %v, want LoadingCatalog", w.Phase.Get())
	}
}

func TestTargetConfig_StoredCredentialDisplayMatchesPickerSource(t *testing.T) {
	target := readmodel.TargetReadModel{
		ID:            "target-1",
		Provider:      "azure",
		Model:         "Kimi-K2.6",
		BaseURL:       "https://contact-8837-resource.services.ai.azure.com",
		CredentialRef: "secret:cockpit/target/azure/dev/kimi-k2-6/target-1/abcd1234",
		Rank:          1,
		Weight:        1,
	}
	route := sampleRoute()
	route.Targets = []readmodel.TargetReadModel{target}
	w := NewEditTargetConfig("dev", route, target, nil, nil)
	w.Open()
	credentialFixture(w).open()

	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	assertRenderedLineContains(t, rendered, "credential", "secret", "change", "↵")
	assertRenderedLineContains(t, rendered, "paste secret", "select", "↵")
	if strings.Contains(rendered, "keychain") {
		t.Fatalf("stored secret should not display as an unlisted keychain choice:\n%s", rendered)
	}
}

func TestTargetConfig_BedrockAuthModePickerKeepsDerivedEndpointVisible(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("bedrock")
	w.SelectBedrockRegion("eu-west-2")

	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	assertRenderedLineContains(t, rendered, "provider", "AWS Bedrock", "change", "↵")
	assertRenderedLineContains(t, rendered, "connection", "Mantle", "default")
	assertRenderedLineContains(t, rendered, "region", "eu-west-2", "change", "↵")
	assertRenderedLineContains(t, rendered, "endpoint", "https://bedrock-mantle.eu-west-2.api.aws/v1", "derived")
	// The auth row is enterable (required); the picker body is not auto-opened.
	assertRenderedLineContains(t, rendered, "auth", "required", "choose", "↵")
	if strings.Contains(rendered, "model loading") || strings.Contains(rendered, "credential enter credential") {
		t.Fatalf("auth row should not skip into later Bedrock setup rows:\n%s", rendered)
	}

	// The auth ui.Select body's picker lists the three Bedrock auth modes.
	picker := bedrockAuthModePicker(w, nil)
	want := map[string]bool{"Bedrock API key": false, "AWS profile": false, "AWS env": false}
	for _, o := range picker.Options {
		if _, ok := want[o.Label]; ok {
			want[o.Label] = true
		}
	}
	for label, seen := range want {
		if !seen {
			t.Fatalf("auth picker missing %q; options=%v", label, picker.Options)
		}
	}
}

func TestTargetConfig_BedrockProfilePickerUsesAWSSharedConfigProfiles(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config")
	credentialsPath := filepath.Join(tmp, "credentials")
	if err := os.WriteFile(configPath, []byte(`
[profile work-prod]
region = eu-west-2
[default]
region = us-east-2
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(credentialsPath, []byte(`
[sandbox]
aws_access_key_id = test
aws_secret_access_key = test
`), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsPath)

	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.SelectProvider("bedrock")
	w.SelectBedrockRegion("eu-west-2")
	w.SelectProviderAuthMode("aws_profile")

	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	// The profile row is enterable (required); the picker body is not auto-opened.
	assertRenderedLineContains(t, rendered, "profile", "required", "choose", "↵")

	// The profile ui.Select body's picker lists the shared-config profiles.
	picker := BedrockProfilePicker(w, nil)
	got := make(map[string]bool, len(picker.Options))
	for _, o := range picker.Options {
		got[o.Label] = true
	}
	for _, want := range []string{"work-prod", "default", "sandbox"} {
		if !got[want] {
			t.Fatalf("profile picker missing %q; options=%v", want, picker.Options)
		}
	}
}

func TestEditTargetConfig_BedrockDoesNotInferDefaultAuth(t *testing.T) {
	target := readmodel.TargetReadModel{
		ID:               "target-1",
		Provider:         "bedrock",
		Model:            "anthropic.claude-3-5-sonnet",
		ProviderProtocol: "messages",
		BaseURL:          "https://bedrock-mantle.eu-west-2.api.aws/v1",
		Rank:             1,
		Weight:           1,
	}
	route := sampleRoute()
	route.Targets = []readmodel.TargetReadModel{target}
	w := NewEditTargetConfig("dev", route, target, nil, nil)
	w.Phase.Set(PhaseConfiguring)
	w.refreshSetup()

	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	assertRenderedLineContains(t, rendered, "provider", "AWS Bedrock", "fixed")
	assertRenderedLineContains(t, rendered, "connection", "Mantle", "default")
	assertRenderedLineContains(t, rendered, "region", "eu-west-2", "change", "↵")
	assertRenderedLineContains(t, rendered, "endpoint", "https://bedrock-mantle.eu-west-2.api.aws/v1", "derived")
	assertRenderedLineContains(t, rendered, "auth", "required", "choose", "↵")
	assertRenderedLineContains(t, rendered, "model", "blocked", "auth first")
	if strings.Contains(rendered, "AWS profile") || strings.Contains(rendered, "loading catalog") {
		t.Fatalf("edit target config must not infer a Bedrock auth selection:\n%s", rendered)
	}
}

func TestEditTargetConfig_BedrockAWSProfileWithoutRefDoesNotInferDefaultProfile(t *testing.T) {
	target := readmodel.TargetReadModel{
		ID:               "target-1",
		Provider:         "bedrock",
		Model:            "anthropic.claude-3-5-sonnet",
		ProviderProtocol: "messages",
		BaseURL:          "https://bedrock-mantle.eu-west-2.api.aws/v1",
		AuthMode:         "aws_profile",
		Rank:             1,
		Weight:           1,
	}
	route := sampleRoute()
	route.Targets = []readmodel.TargetReadModel{target}
	w := NewEditTargetConfig("dev", route, target, nil, nil)
	w.Phase.Set(PhaseConfiguring)
	w.refreshSetup()

	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	assertRenderedLineContains(t, rendered, "auth", "AWS profile", "change", "↵")
	assertRenderedLineContains(t, rendered, "profile", "required", "choose", "↵")
	assertRenderedLineContains(t, rendered, "model", "blocked", "profile first")
	if strings.Contains(rendered, "profile           default") || strings.Contains(rendered, "loading catalog") {
		t.Fatalf("edit target config must not infer default profile without profile ref:\n%s", rendered)
	}
}

func TestUpdateTarget_BlankEditInstanceSeedsFromTarget(t *testing.T) {
	target := readmodel.TargetReadModel{
		ID:               "target-1",
		Provider:         "azure",
		Model:            "gpt-5.4",
		ProviderProtocol: "responses",
		BaseURL:          "https://contact-8837-resource.services.ai.azure.com/api/projects/contact-8837",
		CredentialRef:    "secret:azure",
		Rank:             1,
	}
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)

	w.UpdateTarget("dev", sampleRoute(), target)
	w.Open()

	if got := w.Draft.Get().ProviderSpec; got != "azure" {
		t.Fatalf("provider after UpdateTarget seed = %q, want azure", got)
	}
	if got := w.SelectedModel.Get().ModelName; got != "gpt-5.4" {
		t.Fatalf("selected model after UpdateTarget seed = %q, want gpt-5.4", got)
	}
	if got := w.Phase.Get(); got != PhaseReadyToCreate {
		t.Fatalf("phase after opening seeded edit target = %v, want ReadyToCreate", got)
	}
}

func TestUpdateTarget_PreservesEnteredEditStateWhenAlreadyPopulated(t *testing.T) {
	target := readmodel.TargetReadModel{ID: "target-1", Provider: "openai", Model: "gpt-4.1", ProviderProtocol: "responses", Rank: 1}
	w := NewEditTargetConfig("dev", sampleRoute(), target, nil, nil)
	w.Draft.Update(func(d endpointintent.TargetDraft) endpointintent.TargetDraft {
		d.ProviderSpec = "typed-provider"
		return d
	})
	w.SelectedModel.Set(readmodel.ModelDeploymentReadModel{ModelName: "typed-model"})
	w.Draft.Update(func(d endpointintent.TargetDraft) endpointintent.TargetDraft {
		d.ProviderProtocol = "typed-protocol"
		return d
	})

	w.UpdateTarget("dev", sampleRoute(), target)

	if got := w.Draft.Get().ProviderSpec; got != "typed-provider" {
		t.Fatalf("provider after populated UpdateTarget = %q, want typed-provider", got)
	}
	if got := w.SelectedModel.Get().ModelName; got != "typed-model" {
		t.Fatalf("selected model after populated UpdateTarget = %q, want typed-model", got)
	}
	if got := w.Draft.Get().ProviderProtocol; got != "typed-protocol" {
		t.Fatalf("protocol after populated UpdateTarget = %q, want typed-protocol", got)
	}
}

func TestTargetConfig_SetSetupReadyMovesToLoadingCatalog(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "https://api.openai.com/v1")
	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase after SetSetupReady = %v, want LoadingCatalog", w.Phase.Get())
	}
	if !w.CatalogLoading.Get() {
		t.Fatal("catalog loading should be visible after SetSetupReady")
	}
	if w.Draft.Get().CredentialRef != "env:OPENAI_API_KEY" {
		t.Fatalf("credentialRef = %q", w.Draft.Get().CredentialRef)
	}
}

func TestTargetConfig_ProbeCatalogUsesProviderDefaults(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	var got ports.ProbeProviderModelsRequest
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.TargetSetupQueries = fakeTargetSetupQueries{
		probe: func(_ context.Context, req ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
			got = req
			return readmodel.ModelCatalogReadModel{
				Deployments: []readmodel.ModelDeploymentReadModel{
					{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
				},
			}, nil
		},
	}
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "https://api.openai.com/v1")
	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase after SetSetupReady = %v, want LoadingCatalog", w.Phase.Get())
	}
	w.ProbeCatalog()

	if got.ProviderSpec != "openai" {
		t.Fatalf("probe provider spec = %q, want openai", got.ProviderSpec)
	}
	if got.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("probe base URL = %q, want default openai base URL", got.BaseURL)
	}
	if got.AuthHeader != "" {
		t.Fatalf("probe credential header = %q, want empty for openai", got.AuthHeader)
	}
	if got.ProviderProtocol != "responses" {
		t.Fatalf("probe provider protocol = %q, want responses", got.ProviderProtocol)
	}
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase after probe = %v, want ChoosingModel", w.Phase.Get())
	}
}

func TestTargetConfig_SetCatalogResultSuccessMovesToChoosingModel(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
		},
	})
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase after catalog success = %v, want ChoosingModel", w.Phase.Get())
	}
	if len(w.Catalog.Get().Deployments) != 1 {
		t.Fatalf("catalog deployments = %d, want 1", len(w.Catalog.Get().Deployments))
	}
}

func TestTargetConfig_SetCatalogResultFailureMovesToCatalogFailed(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{Error: "401 unauthorized"})
	if w.Phase.Get() != PhaseCatalogFailed {
		t.Fatalf("phase after catalog failure = %v, want CatalogFailed", w.Phase.Get())
	}
	if w.Error.Get() != "401 unauthorized" {
		t.Fatalf("error = %q, want 401 unauthorized", w.Error.Get())
	}
}

func TestTargetConfig_ChatGPTStartAuthMovesToPending(t *testing.T) {
	var got ports.StartAuthSessionRequest
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.TargetAuthCommands = fakeTargetAuthCommands{
		start: func(_ context.Context, req ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
			got = req
			return readmodel.AuthSessionReadModel{
				ProviderSpec:  "chatgpt",
				SessionID:     "sess-1",
				State:         "pending",
				AuthorizeURL:  "https://auth.openai.com/oauth/authorize",
				UserCode:      "ABCD-1234",
				CredentialRef: "",
			}, nil
		},
	}
	w.SelectProvider("chatgpt")
	w.ContinueSetup()

	if got.ProviderSpec != "chatgpt" {
		t.Fatalf("start auth provider = %q, want chatgpt", got.ProviderSpec)
	}
	if got.AuthMode != "browser" {
		t.Fatalf("start auth mode = %q, want browser", got.AuthMode)
	}
	if !strings.HasPrefix(got.EndpointRef, "subject:dev#gpt") {
		t.Fatalf("start auth endpoint ref = %q, want transient subject locator", got.EndpointRef)
	}
	if w.Phase.Get() != PhaseAuthPending {
		t.Fatalf("phase after chatgpt auth start = %v, want AuthPending", w.Phase.Get())
	}
	if w.AuthSession.Get().SessionID != "sess-1" {
		t.Fatalf("auth session id = %q, want sess-1", w.AuthSession.Get().SessionID)
	}
}

func TestTargetConfig_ChatGPTAuthSuccessSetsCredentialAndLoadsCatalog(t *testing.T) {
	var got ports.ProbeProviderModelsRequest
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.TargetAuthCommands = fakeTargetAuthCommands{
		start: func(_ context.Context, _ ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
			return readmodel.AuthSessionReadModel{
				ProviderSpec: "chatgpt",
				SessionID:    "sess-1",
				State:        "pending",
				AuthorizeURL: "https://auth.openai.com/oauth/authorize",
			}, nil
		},
		poll: func(_ context.Context, sessionID string) (readmodel.AuthSessionReadModel, error) {
			if sessionID != "sess-1" {
				t.Fatalf("poll session id = %q, want sess-1", sessionID)
			}
			return readmodel.AuthSessionReadModel{
				ProviderSpec:  "chatgpt",
				SessionID:     "sess-1",
				State:         "succeeded",
				CredentialRef: "chatgpt:acct_a",
			}, nil
		},
	}
	w.TargetSetupQueries = fakeTargetSetupQueries{
		probe: func(_ context.Context, req ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
			got = req
			return readmodel.ModelCatalogReadModel{
				Deployments: []readmodel.ModelDeploymentReadModel{
					{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"responses_stream"}, DefaultProviderProtocol: "responses_stream"},
				},
				ResolvedProviderProtocol: "responses_stream",
			}, nil
		},
	}

	w.SelectProvider("chatgpt")
	w.ContinueSetup()
	w.RefreshAuthSession()

	if w.Draft.Get().CredentialRef != "chatgpt:acct_a" {
		t.Fatalf("credential ref = %q, want chatgpt:acct_a", w.Draft.Get().CredentialRef)
	}
	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase after auth success = %v, want LoadingCatalog", w.Phase.Get())
	}
	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if !strings.Contains(rendered, "auth") || !strings.Contains(rendered, "signed in") {
		t.Fatalf("expected signed-in auth row after auth success:\n%s", rendered)
	}
	if strings.Contains(rendered, "credential") {
		t.Fatalf("unexpected credential row after auth success:\n%s", rendered)
	}
	if !strings.Contains(rendered, "loading catalog…") {
		t.Fatalf("expected loading row after auth success:\n%s", rendered)
	}
	w.ProbeCatalog()
	if got.ProviderSpec != "chatgpt" {
		t.Fatalf("probe provider spec = %q, want chatgpt", got.ProviderSpec)
	}
	if got.CredentialRef != "chatgpt:acct_a" {
		t.Fatalf("probe credential ref = %q, want chatgpt:acct_a", got.CredentialRef)
	}
	if got.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("probe base URL = %q, want ChatGPT default execute base URL", got.BaseURL)
	}
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase after auth success = %v, want ChoosingModel", w.Phase.Get())
	}
	if len(w.Catalog.Get().Deployments) != 1 {
		t.Fatalf("catalog deployments = %d, want 1", len(w.Catalog.Get().Deployments))
	}
}

func TestTargetConfig_ChatGPTCancelReturnsToProviderSetup(t *testing.T) {
	var canceled bool
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.TargetAuthCommands = fakeTargetAuthCommands{
		start: func(_ context.Context, _ ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
			return readmodel.AuthSessionReadModel{
				ProviderSpec: "chatgpt",
				SessionID:    "sess-1",
				State:        "pending",
				AuthorizeURL: "https://auth.openai.com/oauth/authorize",
			}, nil
		},
		cancel: func(_ context.Context, sessionID string) error {
			canceled = sessionID == "sess-1"
			return nil
		},
	}

	w.SelectProvider("chatgpt")
	w.ContinueSetup()
	w.CancelAuthSession()

	if !canceled {
		t.Fatal("expected cancel auth command to be called")
	}
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase after cancel = %v, want ProviderSetup", w.Phase.Get())
	}
	if w.AuthSession.Get().SessionID != "" {
		t.Fatalf("auth session after cancel = %q, want empty", w.AuthSession.Get().SessionID)
	}
	if w.Draft.Get().CredentialRef != "" {
		t.Fatalf("credential ref after cancel = %q, want empty", w.Draft.Get().CredentialRef)
	}
}

func TestTargetConfig_ChatGPTAuthFailedRender(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.TargetAuthCommands = fakeTargetAuthCommands{
		start: func(_ context.Context, _ ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
			return readmodel.AuthSessionReadModel{}, errors.New("auth service unavailable")
		},
	}

	w.SelectProvider("chatgpt")
	w.ContinueSetup()

	if w.Phase.Get() != PhaseAuthFailed {
		t.Fatalf("phase = %v, want AuthFailed", w.Phase.Get())
	}

	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if !strings.Contains(rendered, "failed") || !strings.Contains(rendered, "back ↵") {
		t.Fatalf("expected auth failed row in frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "auth service unavailable") {
		t.Fatalf("expected auth error text in frame:\n%s", rendered)
	}
	testkit.AssertVisual("chatgpt_auth_failed").
		Fixture("testdata/target_config_component/fixture/chatgpt_auth_failed.txt").
		Viewport(120, 40).
		Now(t, rendered)
}

// TestTargetConfig_ChatGPTCreateCallsSaveTargetWithResponsesStream proves the
// full ChatGPT happy path persists a target whose provider protocol is the
// concrete responses_stream (never auto). Login is simulated through the
// TargetAuthCommands seam, so no real OpenAI login is required.
func TestTargetConfig_ChatGPTCreateCallsSaveTargetWithResponsesStream(t *testing.T) {
	var got ports.SaveTargetRequest
	route := readmodel.RouteReadModel{ID: "gpt", ModelName: "gpt"}
	w := NewTargetConfig("dev", route, func(_ context.Context, req ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		got = req
		return readmodel.TargetReadModel{ID: "t-new"}, nil
	}, nil)
	w.TargetAuthCommands = fakeTargetAuthCommands{
		start: func(_ context.Context, _ ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
			return readmodel.AuthSessionReadModel{
				ProviderSpec: "chatgpt",
				SessionID:    "sess-1",
				State:        "pending",
				AuthorizeURL: "https://auth.openai.com/oauth/authorize",
			}, nil
		},
		poll: func(_ context.Context, _ string) (readmodel.AuthSessionReadModel, error) {
			return readmodel.AuthSessionReadModel{
				ProviderSpec:  "chatgpt",
				SessionID:     "sess-1",
				State:         "succeeded",
				CredentialRef: "chatgpt:acct_a",
			}, nil
		},
	}

	w.Open()
	w.SelectProvider("chatgpt")
	w.ContinueSetup()
	w.RefreshAuthSession()
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-5.5", Name: "GPT-5.5", ModelName: "gpt-5.5", SupportedProviderProtocols: []string{"responses_stream"}, DefaultProviderProtocol: "responses_stream"},
		},
		ResolvedProviderProtocol: "responses_stream",
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{
		ID: "gpt-5.5", Name: "GPT-5.5", ModelName: "gpt-5.5",
		SupportedProviderProtocols: []string{"responses_stream"}, DefaultProviderProtocol: "responses_stream",
	})

	w.Create(context.TODO())

	if w.Phase.Get() != PhaseCreated {
		t.Fatalf("phase after create = %v, want Created", w.Phase.Get())
	}
	if got.Draft.ProviderSpec != "chatgpt" {
		t.Fatalf("draft provider = %q, want chatgpt", got.Draft.ProviderSpec)
	}
	if got.Draft.ProviderProtocol != "responses_stream" {
		t.Fatalf("draft provider protocol = %q, want responses_stream", got.Draft.ProviderProtocol)
	}
	if got.Draft.ModelID != "gpt-5.5" {
		t.Fatalf("draft model = %q, want gpt-5.5", got.Draft.ModelID)
	}
	if got.Draft.CredentialRef != "chatgpt:acct_a" {
		t.Fatalf("draft credential ref = %q, want chatgpt:acct_a", got.Draft.CredentialRef)
	}
	if got.Draft.Rank != 1 {
		t.Fatalf("draft rank = %d, want 1 (first target)", got.Draft.Rank)
	}
	if got.RouteID != "gpt" {
		t.Fatalf("routeID = %q, want gpt (route identity preserved separate from target model)", got.RouteID)
	}
}

// TestTargetConfig_ChatGPTAuthRetryRestartsSession proves the auth-failure retry
// row asks the daemon to retry the established session and returns to the
// pending auth phase with a fresh authorize URL.
func TestTargetConfig_ChatGPTAuthRetryRestartsSession(t *testing.T) {
	var retrySessionID string
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.TargetAuthCommands = fakeTargetAuthCommands{
		start: func(_ context.Context, _ ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
			return readmodel.AuthSessionReadModel{
				ProviderSpec: "chatgpt",
				SessionID:    "sess-1",
				State:        "pending",
				AuthorizeURL: "https://auth.openai.com/oauth/authorize",
			}, nil
		},
		poll: func(_ context.Context, _ string) (readmodel.AuthSessionReadModel, error) {
			return readmodel.AuthSessionReadModel{
				ProviderSpec: "chatgpt",
				SessionID:    "sess-1",
				State:        "failed",
				ErrorMessage: "token exchange failed",
			}, nil
		},
		retry: func(_ context.Context, sessionID string) (readmodel.AuthSessionReadModel, error) {
			retrySessionID = sessionID
			return readmodel.AuthSessionReadModel{
				ProviderSpec: "chatgpt",
				SessionID:    "sess-2",
				State:        "pending",
				AuthorizeURL: "https://auth.openai.com/oauth/authorize?attempt=2",
			}, nil
		},
	}

	w.SelectProvider("chatgpt")
	w.ContinueSetup()
	if w.Phase.Get() != PhaseAuthPending {
		t.Fatalf("phase after start = %v, want AuthPending", w.Phase.Get())
	}
	w.RefreshAuthSession()
	if w.Phase.Get() != PhaseAuthFailed {
		t.Fatalf("phase after failed poll = %v, want AuthFailed", w.Phase.Get())
	}

	w.startInteractiveAuth()

	if retrySessionID != "sess-1" {
		t.Fatalf("retry session id = %q, want sess-1", retrySessionID)
	}
	if w.Phase.Get() != PhaseAuthPending {
		t.Fatalf("phase after retry = %v, want AuthPending", w.Phase.Get())
	}
	if w.AuthSession.Get().SessionID != "sess-2" {
		t.Fatalf("session id after retry = %q, want sess-2", w.AuthSession.Get().SessionID)
	}
	if w.Error.Get() != "" {
		t.Fatalf("error after retry = %q, want empty", w.Error.Get())
	}
}

// TestTargetConfig_ChatGPTAuthRetryStartsFreshWhenNoSession proves retry starts a
// fresh auth attempt when the failure happened before any session was
// established (e.g. a start error).
func TestTargetConfig_ChatGPTAuthRetryStartsFreshWhenNoSession(t *testing.T) {
	var startCount int
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.TargetAuthCommands = fakeTargetAuthCommands{
		start: func(_ context.Context, _ ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
			startCount++
			if startCount == 1 {
				return readmodel.AuthSessionReadModel{}, errors.New("auth service unavailable")
			}
			return readmodel.AuthSessionReadModel{
				ProviderSpec: "chatgpt",
				SessionID:    "sess-1",
				State:        "pending",
				AuthorizeURL: "https://auth.openai.com/oauth/authorize",
			}, nil
		},
	}

	w.SelectProvider("chatgpt")
	w.ContinueSetup()
	if w.Phase.Get() != PhaseAuthFailed {
		t.Fatalf("phase after failed start = %v, want AuthFailed", w.Phase.Get())
	}

	w.startInteractiveAuth()

	if w.Phase.Get() != PhaseAuthPending {
		t.Fatalf("phase after retry = %v, want AuthPending", w.Phase.Get())
	}
	if w.AuthSession.Get().SessionID != "sess-1" {
		t.Fatalf("session id after retry = %q, want sess-1", w.AuthSession.Get().SessionID)
	}
}

func TestTargetConfig_SelectModelMovesToReadyToCreate(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"})
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase after select model = %v, want ReadyToCreate", w.Phase.Get())
	}
	if w.SelectedModel.Get().ModelName != "gpt-4.1" {
		t.Fatalf("model = %q", w.SelectedModel.Get().ModelName)
	}
}

func TestTargetConfig_SelectModelWithMultipleProtocolsRequiresProtocol(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1"})

	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase after select model = %v, want ReadyToCreate", w.Phase.Get())
	}
	// Multiple protocols: protocol stays unselected and the row is enterable
	// (manual open), not auto-opened.
	if w.Draft.Get().ProviderProtocol != "" {
		t.Fatalf("selected protocol = %q, want empty", w.Draft.Get().ProviderProtocol)
	}

	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	// Protocol row is enterable (required): options exist, protocol unselected.
	// The picker is no longer auto-opened (manual enter); the picker's options are
	// still available, validated by SelectProtocol below.
	assertRenderedLineContains(t, rendered, "protocol", "required", "choose", "↵")

	w.SelectProtocol("responses_stream")
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase after protocol select = %v, want ReadyToCreate", w.Phase.Get())
	}
	if w.Draft.Get().ProviderProtocol != "responses_stream" {
		t.Fatalf("selected protocol = %q, want responses_stream", w.Draft.Get().ProviderProtocol)
	}
}

func TestTargetConfig_CreateBlocksWithoutProtocol(t *testing.T) {
	var called bool
	w := NewTargetConfig("dev", sampleRoute(), func(_ context.Context, r ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		called = true
		return readmodel.TargetReadModel{ID: "target-1"}, nil
	}, nil)
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1"})
	w.Phase.Set(PhaseReadyToCreate)

	w.Create(context.TODO())

	if called {
		t.Fatal("Create called SaveTarget without protocol")
	}
	if got := w.Error.Get(); got != "protocol first" {
		t.Fatalf("error = %q, want protocol first", got)
	}
}

func TestTargetConfig_CreateCallsSaveTarget(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	var got ports.SaveTargetRequest
	w := NewTargetConfig("dev", sampleRoute(), func(_ context.Context, r ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		got = r
		return readmodel.TargetReadModel{ID: "target-1"}, nil
	}, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:KEY", "https://api.openai.com/v1")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"})

	w.Create(context.TODO())

	if w.Phase.Get() != PhaseCreated {
		t.Fatalf("phase after create = %v, want Created", w.Phase.Get())
	}
	if got.WorkspaceID != "dev" || got.Draft.ProviderSpec != "openai" || got.Draft.ModelID != "gpt-4.1" {
		t.Fatalf("SaveTarget request = %+v", got)
	}
	if got.Draft.Rank != 2 {
		t.Fatalf("rank = %d, want 2 (fallback after last step)", got.Draft.Rank)
	}
	if got.RouteID != "gpt" {
		t.Fatalf("routeID = %q, want gpt (original route preserved)", got.RouteID)
	}
}

func TestEditTargetConfig_PreservesOpenAICompatibleAuthHeader(t *testing.T) {
	var got ports.SaveTargetRequest
	target := readmodel.TargetReadModel{
		ID:               "target-1",
		Provider:         "openai_compatible",
		Model:            "gpt-4.1",
		ProviderProtocol: "chat_completions",
		BaseURL:          "https://lab.example/v1",
		AuthHeader:       "X-API-Key",
		CredentialRef:    "env:LAB_API_KEY",
		Rank:             1,
		Weight:           1,
	}
	route := sampleRoute()
	route.Targets = []readmodel.TargetReadModel{target}
	w := NewEditTargetConfig("dev", route, target, func(_ context.Context, r ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		got = r
		return readmodel.TargetReadModel{ID: "target-1"}, nil
	}, nil)

	w.Open()
	w.Create(context.TODO())

	if got.Draft.ProviderOptions.OpenAICompatible.CredentialHeader != "X-API-Key" {
		t.Fatalf("credential header = %q, want X-API-Key", got.Draft.ProviderOptions.OpenAICompatible.CredentialHeader)
	}
}

func TestEditTargetConfig_RendersRowLevelEditGrammar(t *testing.T) {
	target := readmodel.TargetReadModel{
		ID:               "target-1",
		Provider:         "openai",
		Model:            "gpt-4.1",
		ProviderProtocol: "responses_stream",
		CredentialRef:    "env:OPENAI_API_KEY",
		Rank:             1,
		Weight:           1,
	}
	route := sampleRoute()
	route.Targets = []readmodel.TargetReadModel{target}
	w := NewEditTargetConfig("dev", route, target, nil, nil)
	w.UpdateProviderOptions([]readmodel.ProviderOptionReadModel{{ProviderSpec: "openai", DisplayName: "OpenAI"}})
	w.Open()

	rendered, err := mountedrender.String(w, 120, 18)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	for _, want := range []string{"edit target · OpenAI", "provider", "fixed", "delete", "target", "delete ↵"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("render missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "save ↵") {
		t.Fatalf("existing target must not render a whole-target save:\n%s", rendered)
	}
}

func TestEditTargetConfig_AzureDeploymentPickerUsesCatalogProbe(t *testing.T) {
	var got ports.ProbeProviderModelsRequest
	target := readmodel.TargetReadModel{
		ID:               "target-1",
		Provider:         "azure",
		Model:            "gpt-5.3-codex",
		ProviderProtocol: "chat_completions_stream",
		BaseURL:          "https://contact-8837-resource.services.ai.azure.com/api/projects/contact-8837",
		CredentialRef:    "secret:cockpit/target/azure/dev/gpt/target-1/abcd1234",
		Rank:             1,
		Weight:           1,
	}
	route := sampleRoute()
	route.Targets = []readmodel.TargetReadModel{target}
	w := NewEditTargetConfig("dev", route, target, nil, nil)
	w.TargetSetupQueries = fakeTargetSetupQueries{
		probe: func(_ context.Context, req ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
			got = req
			return readmodel.ModelCatalogReadModel{
				Deployments: []readmodel.ModelDeploymentReadModel{
					{ID: "gpt-5.3-codex", Name: "gpt-5.3-codex", ModelName: "gpt-5.3-codex", SupportedProviderProtocols: []string{"chat_completions_stream"}, DefaultProviderProtocol: "chat_completions_stream"},
					{ID: "gpt-4.1-prod", Name: "gpt-4.1-prod", ModelName: "gpt-4.1-prod", SupportedProviderProtocols: []string{"chat_completions_stream"}, DefaultProviderProtocol: "chat_completions_stream"},
				},
			}, nil
		},
	}
	w.Open()
	// Entering the model row re-probes the catalog (RFC Phase 5 OnEnter probe).
	w.tryProbeModelCatalog()

	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase after deployment change = %v, want LoadingCatalog", w.Phase.Get())
	}
	loading, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString loading: %v", err)
	}
	assertRenderedLineContains(t, loading, "deployment", "loading", "wait")

	w.ProbeCatalog()
	if got.ProviderSpec != "azure" {
		t.Fatalf("probe provider spec = %q, want azure", got.ProviderSpec)
	}
	if got.BaseURL != target.BaseURL {
		t.Fatalf("probe base URL = %q, want %q", got.BaseURL, target.BaseURL)
	}
	if got.CredentialRef != target.CredentialRef {
		t.Fatalf("probe credential ref = %q, want %q", got.CredentialRef, target.CredentialRef)
	}

	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	// The selected deployment renders as the model row value; the picker body is
	// manual-enter now (covered by the model picker render tests), so this test
	// focuses on the probe request carrying the stored credential + base URL.
	assertRenderedLineContains(t, rendered, "deployment", "gpt-5.3-codex", "change", "↵")
}

func TestEditTargetConfig_AzureProtocolPickerHydratesSelectedDeploymentFromCatalog(t *testing.T) {
	var got ports.ProbeProviderModelsRequest
	target := readmodel.TargetReadModel{
		ID:               "target-1",
		Provider:         "azure",
		Model:            "claude-opus-4-8",
		ProviderProtocol: "messages",
		BaseURL:          "https://swobu-useast-resource.services.ai.azure.com/api/projects/swobu-useast",
		CredentialRef:    "file:/home/metrofun/.config/azure-useast-resource.key",
		Rank:             1,
		Weight:           1,
	}
	route := sampleRoute()
	route.Targets = []readmodel.TargetReadModel{target}
	w := NewEditTargetConfig("dev", route, target, nil, nil)
	w.TargetSetupQueries = fakeTargetSetupQueries{
		probe: func(_ context.Context, req ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
			got = req
			return readmodel.ModelCatalogReadModel{
				Deployments: []readmodel.ModelDeploymentReadModel{
					{
						ID:                         "claude-opus-4-8",
						Name:                       "claude-opus-4-8",
						ModelName:                  "claude-opus-4-8",
						ModelPublisher:             "Anthropic",
						SupportedProviderProtocols: []string{"messages", "messages_stream"},
						DefaultProviderProtocol:    "messages",
					},
				},
			}, nil
		},
	}
	w.Open()

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	initial := h.Frame()
	assertRenderedLineContains(t, initial, "protocol", "messages", "change", "↵")

	// Entering the mounted protocol row is the operator action that hydrates the
	// Azure catalog before presenting provider-specific protocol choices.
	focusRowUntilFrameContains(t, h, "> protocol", 24)
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	if w.Phase.Get() == PhaseLoadingCatalog {
		w.ProbeCatalog()
	}
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase after protocol hydration = %v, want ReadyToCreate", w.Phase.Get())
	}

	if got.ProviderSpec != "azure" {
		t.Fatalf("probe provider spec = %q, want azure", got.ProviderSpec)
	}
	if got.BaseURL != target.BaseURL {
		t.Fatalf("probe base URL = %q, want %q", got.BaseURL, target.BaseURL)
	}
	if got.CredentialRef != target.CredentialRef {
		t.Fatalf("probe credential ref = %q, want %q", got.CredentialRef, target.CredentialRef)
	}
	if got := w.SelectedModel.Get().ModelPublisher; got != "Anthropic" {
		t.Fatalf("selected model publisher = %q, want Anthropic", got)
	}

	rendered := h.Frame()
	assertRenderedLineContains(t, rendered, "protocol", "messages", "change", "↵")
	assertRenderedLineContains(t, rendered, "messages_stream", "Anthropic", "select", "↵")
	assertRenderedLineContains(t, rendered, "messages", "Anthropic", "select", "↵")
	if !strings.Contains(rendered, "2 of 2 shown") {
		t.Fatalf("expected two Anthropic protocol options:\n%s", rendered)
	}
}

func TestEditTargetConfig_DeleteConfirmationStaysInRowAndEscapes(t *testing.T) {
	target := readmodel.TargetReadModel{
		ID:               "target-1",
		Provider:         "openai",
		Model:            "gpt-4.1",
		ProviderProtocol: "responses_stream",
		CredentialRef:    "env:OPENAI_API_KEY",
		Rank:             1,
		Weight:           1,
	}
	route := sampleRoute()
	route.Targets = []readmodel.TargetReadModel{target}
	var confirmed bool
	w := NewEditTargetConfig("dev", route, target, nil, nil)
	w.OnDeleteConfirmed = func() error {
		confirmed = true
		return nil
	}
	w.Open()

	DeleteControl(w).Activate()

	rendered, err := mountedrender.String(w, 120, 18)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	assertRenderedLineContains(t, rendered, "delete", "delete target?", "confirm", "↵")
	if strings.Contains(rendered, "delete openai/gpt-4.1") {
		t.Fatalf("delete confirmation should stay inside target config row:\n%s", rendered)
	}

	if !w.Back() {
		t.Fatal("Escape/Back should consume armed delete confirmation")
	}
	if w.DeleteArmed.Get() {
		t.Fatal("Back should disarm delete confirmation")
	}
	if w.Phase.Get() == PhaseClosed {
		t.Fatal("Back from delete confirmation should not close target config")
	}

	DeleteControl(w).Activate()
	DeleteControl(w).Activate()
	if !confirmed {
		t.Fatal("second delete activation should confirm deletion")
	}
}

func TestEditTargetConfig_DeleteFailureStaysLocal(t *testing.T) {
	target := readmodel.TargetReadModel{
		ID:               "target-1",
		Provider:         "openai",
		Model:            "gpt-4.1",
		ProviderProtocol: "responses_stream",
		CredentialRef:    "env:OPENAI_API_KEY",
		Rank:             1,
		Weight:           1,
	}
	route := sampleRoute()
	route.Targets = []readmodel.TargetReadModel{target}
	w := NewEditTargetConfig("dev", route, target, nil, nil)
	w.OnDeleteConfirmed = func() error { return errors.New("delete refused") }
	w.Open()

	DeleteControl(w).Activate()
	DeleteControl(w).Activate()

	if got := w.Error.Get(); got != "delete refused" {
		t.Fatalf("delete error = %q, want delete refused", got)
	}
	if !w.DeleteArmed.Get() {
		t.Fatal("failed delete should keep confirmation armed for retry or escape")
	}
}

func TestEditTargetConfig_SelectProtocolCommitsRow(t *testing.T) {
	target := readmodel.TargetReadModel{
		ID:               "target-1",
		Provider:         "openai",
		Model:            "gpt-4.1",
		ProviderProtocol: "responses",
		CredentialRef:    "env:OPENAI_API_KEY",
		Rank:             1,
		Weight:           1,
	}
	route := sampleRoute()
	route.Targets = []readmodel.TargetReadModel{target}
	var got ports.SaveTargetRequest
	var saved bool
	w := NewEditTargetConfig("dev", route, target, func(_ context.Context, r ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		got = r
		out := target
		out.ProviderProtocol = r.Draft.ProviderProtocol
		return out, nil
	}, nil)
	w.OnSaved = func(readmodel.TargetReadModel) { saved = true }
	w.SelectedModel.Set(readmodel.ModelDeploymentReadModel{
		ID:                         "gpt-4.1",
		Name:                       "gpt-4.1",
		ModelName:                  "gpt-4.1",
		SupportedProviderProtocols: []string{"responses", "responses_stream"},
	})
	w.SelectProtocol("responses_stream")

	if got.TargetID != "target-1" || got.Draft.ProviderProtocol != "responses_stream" {
		t.Fatalf("SaveTarget request = %+v", got)
	}
	if !saved {
		t.Fatal("row-level protocol commit did not notify OnSaved")
	}
}

func TestTargetConfig_PersistsBedrockRegionAndAuthInTargetDraft(t *testing.T) {
	var got ports.SaveTargetRequest
	w := NewTargetConfig("dev", sampleRoute(), func(_ context.Context, r ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		got = r
		return readmodel.TargetReadModel{ID: "target-1"}, nil
	}, nil)

	w.Open()
	w.SelectProvider("bedrock")
	w.SelectBedrockRegion("eu-west-2")
	w.SelectProviderAuthMode("aws_env_session")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{{
			ID:                         "anthropic.claude-3-7-sonnet",
			ModelName:                  "anthropic.claude-3-7-sonnet",
			SupportedProviderProtocols: []string{"messages_stream"},
			DefaultProviderProtocol:    "messages_stream",
		}},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{
		ID:                         "anthropic.claude-3-7-sonnet",
		ModelName:                  "anthropic.claude-3-7-sonnet",
		SupportedProviderProtocols: []string{"messages_stream"},
		DefaultProviderProtocol:    "messages_stream",
	})
	w.Create(context.TODO())

	if got.Draft.ProviderOptions.Bedrock.AuthMode != "aws_env_session" {
		t.Fatalf("bedrock auth mode = %q, want aws_env_session", got.Draft.ProviderOptions.Bedrock.AuthMode)
	}
	if got.Draft.ProviderOptions.Bedrock.Region != "eu-west-2" {
		t.Fatalf("bedrock region = %q, want eu-west-2", got.Draft.ProviderOptions.Bedrock.Region)
	}
	if got.Draft.CredentialRef != "" {
		t.Fatalf("credential ref = %q, want empty for aws_env_session", got.Draft.CredentialRef)
	}
}

func TestTargetConfig_PersistsBedrockProfileInTargetDraft(t *testing.T) {
	var got ports.SaveTargetRequest
	w := NewTargetConfig("dev", sampleRoute(), func(_ context.Context, r ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		got = r
		return readmodel.TargetReadModel{ID: "target-1"}, nil
	}, nil)

	w.Open()
	w.SelectProvider("bedrock")
	w.SelectBedrockRegion("eu-west-2")
	w.SelectProviderAuthMode("aws_profile")
	w.SelectBedrockProfile("work-prod")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{{
			ID:                         "anthropic.claude-3-7-sonnet",
			ModelName:                  "anthropic.claude-3-7-sonnet",
			SupportedProviderProtocols: []string{"messages_stream"},
			DefaultProviderProtocol:    "messages_stream",
		}},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{
		ID:                         "anthropic.claude-3-7-sonnet",
		ModelName:                  "anthropic.claude-3-7-sonnet",
		SupportedProviderProtocols: []string{"messages_stream"},
		DefaultProviderProtocol:    "messages_stream",
	})
	w.Create(context.TODO())

	if got.Draft.ProviderOptions.Bedrock.AuthMode != "aws_profile" {
		t.Fatalf("bedrock auth mode = %q, want aws_profile", got.Draft.ProviderOptions.Bedrock.AuthMode)
	}
	if got.Draft.ProviderOptions.Bedrock.ProfileName != "work-prod" {
		t.Fatalf("bedrock profile name = %q, want work-prod", got.Draft.ProviderOptions.Bedrock.ProfileName)
	}
	if got.Draft.ProviderOptions.Bedrock.Region != "eu-west-2" {
		t.Fatalf("bedrock region = %q, want eu-west-2", got.Draft.ProviderOptions.Bedrock.Region)
	}
	if got.Draft.CredentialRef != "profile:work-prod" {
		t.Fatalf("credential ref = %q, want profile:work-prod", got.Draft.CredentialRef)
	}
}

func TestTargetConfig_BedrockProfileProbeCarriesAuthMode(t *testing.T) {
	var got ports.ProbeProviderModelsRequest
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.TargetSetupQueries = fakeTargetSetupQueries{
		probe: func(_ context.Context, req ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
			got = req
			return readmodel.ModelCatalogReadModel{
				Deployments: []readmodel.ModelDeploymentReadModel{
					{ID: "anthropic.claude", Name: "anthropic.claude", ModelName: "anthropic.claude", SupportedProviderProtocols: []string{"messages"}, DefaultProviderProtocol: "messages"},
				},
			}, nil
		},
	}

	w.SelectProvider("bedrock")
	w.SelectBedrockRegion("us-east-1")
	w.SelectProviderAuthMode("aws_profile")
	w.SelectBedrockProfile("default")
	w.ProbeCatalog()

	if got.ProviderSpec != "bedrock" {
		t.Fatalf("provider spec = %q, want bedrock", got.ProviderSpec)
	}
	if got.AuthMode != "aws_profile" {
		t.Fatalf("auth mode = %q, want aws_profile", got.AuthMode)
	}
	if got.CredentialRef != "profile:default" {
		t.Fatalf("credential ref = %q, want profile:default", got.CredentialRef)
	}
}

func TestTargetConfig_CreateErrorMovesToCreateFailed(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", sampleRoute(), func(context.Context, ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		return readmodel.TargetReadModel{}, errors.New("save failed")
	}, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "https://api.openai.com/v1")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"})

	w.Create(context.TODO())

	if w.Phase.Get() != PhaseCreateFailed {
		t.Fatalf("phase after create error = %v, want CreateFailed", w.Phase.Get())
	}
	if w.Error.Get() != "save failed" {
		t.Fatalf("error = %q", w.Error.Get())
	}
}

func TestTargetConfig_BackClosesFeatureOutsideLocalScopes(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "https://api.openai.com/v1")

	if !w.Back() {
		t.Fatal("Back should consume")
	}
	if w.Phase.Get() != PhaseClosed {
		t.Fatalf("phase after back = %v, want Closed", w.Phase.Get())
	}
}

func TestTargetConfig_BackFromReadyFormClosesFeature(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{Deployments: staticCatalogFallback("openai")})

	// A custom (typed) model resolves to several protocols with no default. The
	// protocol row is enterable (required) at ReadyToCreate — manual open, with
	// no auto-open that Escape could toggle.
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "custom-model", Name: "custom-model", ModelName: "custom-model"})
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase after custom model = %v, want ReadyToCreate", w.Phase.Get())
	}
	if w.Draft.Get().ProviderProtocol != "" {
		t.Fatalf("protocol = %q, want empty (not auto-chosen)", w.Draft.Get().ProviderProtocol)
	}

	// No row is entered, so feature Back closes rather than walking phases.
	if !w.Back() {
		t.Fatal("Back should consume")
	}
	if w.Phase.Get() != PhaseClosed {
		t.Fatalf("Back should close target config, phase=%v", w.Phase.Get())
	}
}

func TestTargetConfig_BackFromClosedIsNoOp(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	if w.Back() {
		t.Fatal("Back from Closed should not consume")
	}
}

func TestTargetConfig_CloseFiresOnClose(t *testing.T) {
	var closed bool
	w := NewTargetConfig("dev", sampleRoute(), nil, func() { closed = true })
	w.Open()
	w.Close()
	if w.Phase.Get() != PhaseClosed {
		t.Fatalf("phase = %v, want Closed", w.Phase.Get())
	}
	if !closed {
		t.Fatal("OnClose not fired")
	}
}

func TestTargetConfig_DefaultPlacementFallbackAfterLastStep(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	route := readmodel.RouteReadModel{
		Targets: []readmodel.TargetReadModel{
			{Rank: 1}, {Rank: 1}, {Rank: 2},
		},
	}
	w := NewTargetConfig("dev", route, nil, nil)
	p := w.Placement.Get()
	if p.Rank != 3 {
		t.Fatalf("default rank = %d, want 3", p.Rank)
	}
	if p.Kind != readmodel.PlacementFallback {
		t.Fatalf("kind = %v, want Fallback", p.Kind)
	}
	if got, want := p.Summary(), "fallback after step 2"; got != want {
		t.Fatalf("default placement summary = %q, want %q", got, want)
	}
}

func TestTargetConfig_DefaultPlacementForEmptyRouteIsStep1(t *testing.T) {
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "gpt", ModelName: "gpt"}, nil, nil)

	p := w.Placement.Get()
	if p.Rank != 1 {
		t.Fatalf("default rank = %d, want 1", p.Rank)
	}
	if p.Weight != 1 {
		t.Fatalf("default weight = %d, want 1", p.Weight)
	}
	if p.Kind != readmodel.PlacementFallback {
		t.Fatalf("kind = %v, want Fallback", p.Kind)
	}
	if got, want := p.Summary(), "step 1"; got != want {
		t.Fatalf("default placement summary = %q, want %q", got, want)
	}
}

func TestTargetConfig_UpdatePropsRefreshesRouteAndSave(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w1 := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w1.Open()
	w1.SelectProvider("openai")
	w1.SetSetupReady("env:OPENAI_API_KEY", "")
	w1.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
		},
	})
	w1.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"})
	w1.placementOptions()

	w2 := NewTargetConfig("prod", readmodel.RouteReadModel{ID: "other", ModelName: "other"}, func(context.Context, ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		return readmodel.TargetReadModel{}, nil
	}, nil)

	w1.UpdateProps(w2)

	if w1.WorkspaceID != "prod" {
		t.Fatalf("workspaceID = %q", w1.WorkspaceID)
	}
	if w1.Route.ID != "other" {
		t.Fatalf("route.ID = %q", w1.Route.ID)
	}
	// User selection state must survive UpdateProps.
	if w1.Draft.Get().ProviderSpec != "openai" {
		t.Fatalf("provider selection reset by UpdateProps")
	}
}

func TestTargetConfig_MountedModelRowRefreshesProps(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{Deployments: []readmodel.ModelDeploymentReadModel{
		{ID: "gpt-4.1", Name: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}},
	}})

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	if frame := h.Frame(); !strings.Contains(frame, "model             required") {
		t.Fatalf("initial model row must be required:\n%s", frame)
	}

	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", Name: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}})
	if frame := h.Frame(); !strings.Contains(frame, "model             gpt-4.1") || !strings.Contains(frame, "change ↵") {
		t.Fatalf("mounted model row retained stale props after selection:\n%s", frame)
	}
}

func newTargetConfigSeededWithProviders(workspaceID readmodel.WorkspaceID, route readmodel.RouteReadModel, save SaveTargetFunc, onClose func(), opts []readmodel.ProviderOptionReadModel) *TargetConfig {
	w := NewTargetConfig(workspaceID, route, save, onClose)
	w.UpdateProviderOptions(opts)
	return w
}

func TestTargetConfig_KeyMapWhenOpen(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	if w.KeyMap() != nil {
		t.Fatal("KeyMap should be nil when closed")
	}
	w.Open()
	if w.KeyMap() == nil {
		t.Fatal("KeyMap should be non-nil when open")
	}
}

// --- Placement tests ----------------------------------------------------

func TestTargetConfig_PlacementPickerBuildsOptions(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	route := readmodel.RouteReadModel{
		ID:        "gpt",
		ModelName: "gpt",
		Targets: []readmodel.TargetReadModel{
			{ID: "t1", Rank: 1, Weight: 1},
			{ID: "t2", Rank: 1, Weight: 1},
			{ID: "t3", Rank: 2, Weight: 1},
		},
	}
	w := NewTargetConfig("dev", route, nil, nil)
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"})
	w.placementOptions()
	opts := w.placementOptions()
	if len(opts) != 3 {
		t.Fatalf("placement options count = %d, want 3: %+v", len(opts), opts)
	}
	var hasBalance1, hasBalance2, hasFallback3 bool
	for _, o := range opts {
		if o.Rank == 1 && o.Kind == readmodel.PlacementBalance {
			hasBalance1 = true
		}
		if o.Rank == 2 && o.Kind == readmodel.PlacementBalance {
			hasBalance2 = true
		}
		if o.Rank == 3 && o.Kind == readmodel.PlacementFallback && o.Summary() == "fallback after step 2" {
			hasFallback3 = true
		}
		if o.Summary() == "fallback after step 1" {
			t.Fatalf("unexpected per-step fallback option: %+v", o)
		}
	}
	if !hasBalance1 {
		t.Fatalf("missing balance with step 1 option: %+v", opts)
	}
	if !hasBalance2 {
		t.Fatalf("missing balance with step 2 option: %+v", opts)
	}
	if !hasFallback3 {
		t.Fatalf("missing fallback after step 2 option: %+v", opts)
	}
}

func TestTargetConfig_SelectPlacementChangesRankAndReturns(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	route := sampleRoute()
	w := NewTargetConfig("dev", route, nil, nil)
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"})
	w.placementOptions()
	w.SelectPlacement(readmodel.PlacementOptionReadModel{
		Rank: 1, Weight: 1, Kind: readmodel.PlacementBalance,
	})
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase = %v, want ReadyToCreate", w.Phase.Get())
	}
	if w.Placement.Get().Rank != 1 {
		t.Fatalf("placement rank = %d, want 1", w.Placement.Get().Rank)
	}
}

func TestTargetConfig_BackFromPlacementPickerReturnsToReadyToCreate(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	route := sampleRoute()
	w := NewTargetConfig("dev", route, nil, nil)
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"})
	originalPlacement := w.Placement.Get()

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	// Create is autofocused in the ready state; routing is the row directly
	// above it. Enter opens the placement Select body (the picker).
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyUp})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	if !strings.Contains(h.Frame(), "balance with step 1") {
		t.Fatalf("entering placement should reveal the picker:\n%s", h.Frame())
	}

	// Escape backs the Select out locally; root Back does not know this row.
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	if strings.Contains(h.Frame(), "balance with step 1") {
		t.Fatalf("escape should back out of the placement picker:\n%s", h.Frame())
	}
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase = %v, want ReadyToCreate", w.Phase.Get())
	}
	if w.Placement.Get().Rank != originalPlacement.Rank {
		t.Fatalf("routing changed after back: %v -> %v", originalPlacement, w.Placement.Get())
	}
}

func TestTargetConfig_CreateUsesSelectedPlacement(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	var request ports.SaveTargetRequest
	localSave := func(_ context.Context, req ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		request = req
		return readmodel.TargetReadModel{ID: "t-new"}, nil
	}
	route := sampleRoute()
	w := NewTargetConfig("dev", route, localSave, nil)
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"})
	w.SelectPlacement(readmodel.PlacementOptionReadModel{
		Rank: 1, Weight: 1, Kind: readmodel.PlacementBalance,
	})
	w.Create(context.TODO())
	if request.Draft.Rank != 1 {
		t.Fatalf("create rank = %d, want 1", request.Draft.Rank)
	}
	if request.Draft.Weight != 1 {
		t.Fatalf("create weight = %d, want 1", request.Draft.Weight)
	}
}

func TestTargetConfig_FirstTargetSkipsPlacementPicker(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	var request ports.SaveTargetRequest
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "gpt", ModelName: "gpt"}, func(_ context.Context, req ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		request = req
		return readmodel.TargetReadModel{ID: "target-new"}, nil
	}, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"})

	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase = %v, want ReadyToCreate", w.Phase.Get())
	}
	if !strings.Contains(rendered, "step 1") {
		t.Fatalf("expected fixed first-target placement summary:\n%s", rendered)
	}
	if got := strings.Count(rendered, "change ↵"); got != 3 {
		t.Fatalf("first target should expose provider/credential/model change actions, got %d:\n%s", got, rendered)
	}
	if strings.Contains(rendered, "choose placement") {
		t.Fatalf("first target should not render a placement chooser:\n%s", rendered)
	}

	w.placementOptions()
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("empty-route placement picker should be a no-op, got %v", w.Phase.Get())
	}

	w.Create(context.TODO())
	if request.Draft.Rank != 1 {
		t.Fatalf("create rank = %d, want 1", request.Draft.Rank)
	}
	if request.Draft.Weight != 1 {
		t.Fatalf("create weight = %d, want 1", request.Draft.Weight)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func assertRenderedLineContains(t testing.TB, rendered string, parts ...string) {
	t.Helper()
	_ = renderedLineContaining(t, rendered, parts...)
}

func renderedLineContaining(t testing.TB, rendered string, parts ...string) string {
	t.Helper()
	for _, line := range strings.Split(rendered, "\n") {
		matched := true
		for _, part := range parts {
			if !strings.Contains(line, part) {
				matched = false
				break
			}
		}
		if matched {
			return line
		}
	}
	t.Fatalf("no rendered line contains %v:\n%s", parts, rendered)
	return ""
}

func renderTargetConfigFrame(t testing.TB, w *TargetConfig, width int, height int) string {
	t.Helper()
	rendered, err := mountedrender.String(w, width, height)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	return rendered
}

func assertOneFocusMarker(t testing.TB, name string, rendered string) {
	t.Helper()
	if got := countFocusMarkers(rendered); got != 1 {
		t.Fatalf("%s frame focus markers = %d, want 1:\n%s", name, got, rendered)
	}
}

func countFocusMarkers(rendered string) int {
	count := 0
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "> ") {
			count++
		}
	}
	return count
}

func leadingSpaces(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func TestTargetConfig_CatalogFailedRendersRetryAndModelPicker(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{Error: "401 unauthorized"})

	if w.Phase.Get() != PhaseCatalogFailed {
		t.Fatalf("phase = %v, want CatalogFailed", w.Phase.Get())
	}
	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if !strings.Contains(rendered, "catalog failed") || !strings.Contains(rendered, "retry ↵") {
		t.Fatalf("expected catalog failed retry row:\n%s", rendered)
	}
	if !strings.Contains(rendered, "401 unauthorized") {
		t.Fatalf("expected error text in frame:\n%s", rendered)
	}
	// RFC Phase 5: catalog failure renders only the retry row. The model picker
	// is gone from this state (the operator retries, then enters the model row).
	if strings.Contains(rendered, "select ↵") {
		t.Fatalf("catalog-failed frame must not render a model picker:\n%s", rendered)
	}
	// Fixture snapshot for catalog failure wireframe.
	testkit.AssertVisual("catalog_failed").
		Fixture("testdata/target_config_component/fixture/catalog_failed.txt").
		Viewport(120, 40).
		Now(t, rendered)
}

func TestTargetConfig_ModelPickerRendersBoundedModelList(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
			{ID: "gpt-4.1-mini", Name: "GPT-4.1 Mini", ModelName: "gpt-4.1-mini", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
			{ID: "gpt-4o", Name: "GPT-4o", ModelName: "gpt-4o", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
			{ID: "gpt-4o-mini", Name: "GPT-4o Mini", ModelName: "gpt-4o-mini", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
			{ID: "gpt-4", Name: "GPT-4", ModelName: "gpt-4", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
			{ID: "gpt-3.5-turbo", Name: "GPT-3.5", ModelName: "gpt-3.5-turbo", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
		},
	})

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	// The model row is autofocused in ChoosingModel; Enter reveals the picker
	// body (a fresh SearchPicker — entered state is local to the mounted Select).
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	rendered := h.Frame()
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase = %v, want ChoosingModel", w.Phase.Get())
	}
	if !strings.Contains(rendered, "6 of 6 shown") {
		t.Fatalf("expected '6 of 6 shown' footer in model picker:\n%s", rendered)
	}
	// Should show some models.
	if !strings.Contains(rendered, "GPT-4.1") {
		t.Fatalf("expected GPT-4.1 in model picker:\n%s", rendered)
	}
	assertOneFocusMarker(t, "model picker", rendered)
	// Fixture snapshot for model picker wireframe.
	testkit.AssertVisual("model_picker").
		Fixture("testdata/target_config_component/fixture/model_picker.txt").
		Viewport(120, 40).
		Now(t, rendered)
}

func TestTargetConfig_ModelPickerSearchFilters(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: append(
			manyModelDeployments("GPT-4.1", 12),
			manyModelDeployments("Claude Sonnet", 35)...,
		),
	})

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // enter model Select
	// "gpt" filters to the GPT deployments (the SearchPicker activates on Space,
	// so single-token queries are typed through the key path).
	for _, r := range "gpt" {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: r})
	}

	rendered := h.Frame()
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase = %v, want ChoosingModel", w.Phase.Get())
	}
	if !strings.Contains(rendered, "GPT-4.1 001") {
		t.Fatalf("expected filtered GPT model in model picker:\n%s", rendered)
	}
	if strings.Contains(rendered, "Claude Sonnet") {
		t.Fatalf("filtered model picker should not show unrelated catalog rows:\n%s", rendered)
	}
	if !strings.Contains(rendered, "6 of 47 shown") {
		t.Fatalf("expected filtered footer in model picker:\n%s", rendered)
	}
	testkit.AssertVisual("model_picker_search").
		Fixture("testdata/target_config_component/fixture/model_picker_search.txt").
		Viewport(120, 40).
		Now(t, rendered)
}

func TestTargetConfig_ModelPickerManyResultsRenderAsSelectableRows(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: manyModelDeployments("Claude Sonnet", 112),
	})

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // enter model Select

	rendered := h.Frame()
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase = %v, want ChoosingModel", w.Phase.Get())
	}
	if !strings.Contains(rendered, "Claude Sonnet 001") {
		t.Fatalf("expected first catalog row to render as selectable row:\n%s", rendered)
	}
	testkit.AssertVisual("model_picker_many").
		Fixture("testdata/target_config_component/fixture/model_picker_many.txt").
		Viewport(120, 40).
		Now(t, rendered)
}

func TestTargetConfig_ModelPickerEmptySearch(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: manyModelDeployments("GPT-4.1", 47),
	})

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // enter model Select
	for _, r := range "xyz" {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: r})
	}

	rendered := h.Frame()
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase = %v, want ChoosingModel", w.Phase.Get())
	}
	if !strings.Contains(rendered, "0 of 47 shown") {
		t.Fatalf("expected empty-search footer in model picker:\n%s", rendered)
	}
	if strings.Contains(rendered, "select ↵") {
		t.Fatalf("empty-search picker should not render selectable options:\n%s", rendered)
	}
	testkit.AssertVisual("model_picker_empty").
		Fixture("testdata/target_config_component/fixture/model_picker_empty.txt").
		Viewport(120, 40).
		Now(t, rendered)
}

func TestTargetConfig_OllamaNoCredential(t *testing.T) {
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "gpt", ModelName: "gpt"}, nil, nil)
	w.Open()
	w.SelectProvider("ollama")
	w.ContinueSetup()

	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase = %v, want LoadingCatalog", w.Phase.Get())
	}
	if !w.CatalogLoading.Get() {
		t.Fatal("catalog loading should be active for Ollama")
	}
	if w.Draft.Get().CredentialRef != "" {
		t.Fatalf("credential ref should be empty for Ollama, got %q", w.Draft.Get().CredentialRef)
	}

	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if strings.Contains(rendered, "credential") {
		t.Fatalf("Ollama setup should NOT show credential row:\n%s", rendered)
	}
	if !strings.Contains(rendered, "loading catalog") {
		t.Fatalf("expected catalog loading row for Ollama:\n%s", rendered)
	}
	testkit.AssertVisual("ollama_setup").
		Fixture("testdata/target_config_component/fixture/ollama_setup.txt").
		Viewport(120, 24).
		Now(t, rendered)

	// After probe, the model row is enterable; entering it reveals the Ollama
	// model picker from the static catalog.
	w.ProbeCatalog()
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase = %v, want ChoosingModel after Ollama probe", w.Phase.Get())
	}

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // model row autofocused → enter picker

	rendered2 := h.Frame()
	if !strings.Contains(rendered2, "Llama 3.2 3B") {
		t.Fatalf("expected Ollama model list after probe:\n%s", rendered2)
	}
	// Fixture snapshot for Ollama model picker wireframe.
	testkit.AssertVisual("ollama_model_picker").
		Fixture("testdata/target_config_component/fixture/ollama_model_picker.txt").
		Viewport(120, 40).
		Now(t, rendered2)
}

func TestTargetConfig_ReadyToCreateRender(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"})

	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase = %v, want ReadyToCreate", w.Phase.Get())
	}
	if !strings.Contains(rendered, "create") {
		t.Fatalf("expected create row in ready state:\n%s", rendered)
	}
	// Model is shown as its ModelName (not display name).
	if !strings.Contains(rendered, "gpt-4.1") {
		t.Fatalf("expected selected model in ready state:\n%s", rendered)
	}
	// Fixture snapshot for ready-to-create wireframe (existing route with targets).
	testkit.AssertVisual("ready_to_create").
		Fixture("testdata/target_config_component/fixture/ready_to_create.txt").
		Viewport(120, 24).
		Now(t, rendered)
}

func TestTargetConfig_PlacementPickerRender(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", readmodel.RouteReadModel{
		ID:        "gpt",
		ModelName: "gpt",
		Targets: []readmodel.TargetReadModel{
			{ID: "t1", Rank: 1, Weight: 1},
			{ID: "t2", Rank: 2, Weight: 1},
		},
	}, nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"})

	// The placement Select owns its entered state, so the picker body is only
	// visible through the live harness (a fresh mountedrender.String would build
	// a new, not-entered Select). Create is autofocused; routing is one row up.
	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyUp})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	rendered := h.Frame()
	if !strings.Contains(rendered, "balance with step 1") || !strings.Contains(rendered, "fallback after step 2") {
		t.Fatalf("expected placement options in placement picker:\n%s", rendered)
	}
	testkit.AssertVisual("placement_picker").
		Fixture("testdata/target_config_component/fixture/placement_picker.txt").
		Viewport(120, 40).
		Now(t, rendered)
}

func TestTargetConfig_FirstTargetReadyNoPlacementPicker(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "gpt", ModelName: "gpt"}, nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"})

	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase = %v, want ReadyToCreate", w.Phase.Get())
	}
	// First target shows fixed placement "step 1" as read-only (no action).
	if !strings.Contains(rendered, "step 1") {
		t.Fatalf("expected 'step 1' fixed placement for first target:\n%s", rendered)
	}
	// Verify the placement row line does NOT have "change" — it is read-only.
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "routing") && strings.Contains(line, "change") {
			t.Fatalf("first target placement should NOT be changeable:\n%s", line)
		}
	}
	// Fixture snapshot for first-target ready wireframe.
	testkit.AssertVisual("first_target_ready").
		Fixture("testdata/target_config_component/fixture/first_target_ready.txt").
		Viewport(120, 24).
		Now(t, rendered)
}

func TestTargetConfig_RetryCatalogReprobes(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	probeCount := 0
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.TargetSetupQueries = fakeTargetSetupQueries{
		probe: func(_ context.Context, req ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
			probeCount++
			if probeCount == 1 {
				return readmodel.ModelCatalogReadModel{Error: "timeout"}, nil
			}
			return readmodel.ModelCatalogReadModel{
				Deployments: []readmodel.ModelDeploymentReadModel{
					{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
				},
			}, nil
		},
	}
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.ProbeCatalog()
	if w.Phase.Get() != PhaseCatalogFailed {
		t.Fatalf("phase = %v, want CatalogFailed", w.Phase.Get())
	}

	w.RetryCatalog()

	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase = %v, want LoadingCatalog while retrying", w.Phase.Get())
	}
	if !strings.Contains(rendered, "loading catalog…") {
		t.Fatalf("expected loading state during retry:\n%s", rendered)
	}
	testkit.AssertVisual("catalog_retrying").
		Fixture("testdata/target_config_component/fixture/catalog_retrying.txt").
		Viewport(120, 40).
		Now(t, rendered)

	w.ProbeCatalog()

	if probeCount != 2 {
		t.Fatalf("probe count = %d, want 2", probeCount)
	}
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase = %v, want ChoosingModel after retry success", w.Phase.Get())
	}
}

func TestTargetConfig_CustomModelViaPickerCreatesModelWithRouteModelSplit(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	var got ports.SaveTargetRequest
	w := NewTargetConfig("dev", sampleRoute(), func(_ context.Context, r ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		got = r
		return readmodel.TargetReadModel{ID: "target-1"}, nil
	}, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{Deployments: []readmodel.ModelDeploymentReadModel{
		{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions", "responses_stream"}, DefaultProviderProtocol: "chat_completions"},
	}})

	// The model row is enterable once the catalog loads; its open-set picker
	// accepts a typed query as the candidate value ("use ↵") even when it
	// matches no listed deployment.
	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // enter model Select
	for _, r := range "gpt-4.1-custom" {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: r})
	}
	rendered := h.Frame()
	if !strings.Contains(rendered, "gpt-4.1-custom") || !strings.Contains(rendered, "use ↵") {
		t.Fatalf("expected open-set query row in picker:\n%s", rendered)
	}

	// Commit the typed query as the model id (the open-set OnSelect path for a
	// value matching no listed deployment).
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1-custom", Name: "gpt-4.1-custom", ModelName: "gpt-4.1-custom"})

	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase = %v, want ReadyToCreate", w.Phase.Get())
	}
	// Protocol is unselected and enterable after a custom model (manual open).
	if w.SelectedModel.Get().ModelName != "gpt-4.1-custom" {
		t.Fatalf("selected model = %q, want gpt-4.1-custom", w.SelectedModel.Get().ModelName)
	}
	w.SelectProtocol("responses_stream")

	w.Create(context.TODO())

	if got.RouteID != "gpt" {
		t.Fatalf("routeID = %q, want gpt", got.RouteID)
	}
	if got.Draft.ModelID != "gpt-4.1-custom" {
		t.Fatalf("model = %q, want gpt-4.1-custom", got.Draft.ModelID)
	}
	if got.Draft.ProviderSpec != "openai" {
		t.Fatalf("provider = %q, want openai", got.Draft.ProviderSpec)
	}
}

func sampleRoute() readmodel.RouteReadModel {
	return readmodel.RouteReadModel{
		ID:        "gpt",
		ModelName: "gpt",
		Targets: []readmodel.TargetReadModel{
			{ID: "t1", Rank: 1, Weight: 1},
		},
	}
}

func TestTargetConfig_LiveProviderSelectionRecomposesWithoutRemount(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	if frame := h.Frame(); !strings.Contains(frame, "provider") {
		t.Fatalf("provider picker missing before selection:\n%s", frame)
	}

	w.SelectProvider("openai")
	frame := h.Frame()
	if !strings.Contains(frame, "OpenAI") || !strings.Contains(frame, "credential") {
		t.Fatalf("provider branch did not recompose in mounted app:\n%s", frame)
	}
}

func TestTargetConfig_LiveAuthAndErrorUpdatesRecomposeWithoutRemount(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("chatgpt")
	w.Phase.Set(PhaseAuthPending)
	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	w.AuthSession.Set(readmodel.AuthSessionReadModel{SessionID: "auth-1", AuthorizeURL: "https://example.test/auth", UserCode: "CODE-123"})
	frame := h.Frame()
	if !strings.Contains(frame, "https://example.test/auth") || !strings.Contains(frame, "CODE-123") {
		t.Fatalf("auth session update did not recompose mounted app:\n%s", frame)
	}
	w.Error.Set("auth display failure")
	if frame = h.Frame(); !strings.Contains(frame, "auth display failure") {
		t.Fatalf("error update did not recompose mounted app:\n%s", frame)
	}
}

func TestTargetConfig_LiveBedrockAuthModeRecomposesWithoutRemount(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("bedrock")
	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	w.SelectBedrockRegion("us-east-1")
	w.SelectProviderAuthMode("aws_profile")
	frame := h.Frame()
	if !strings.Contains(frame, "profile") {
		t.Fatalf("Bedrock auth-mode update did not reveal profile row in mounted app:\n%s", frame)
	}
}

func manyModelDeployments(prefix string, count int) []readmodel.ModelDeploymentReadModel {
	if count < 0 {
		count = 0
	}
	out := make([]readmodel.ModelDeploymentReadModel, 0, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("%s %03d", prefix, i+1)
		modelID := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
		out = append(out, readmodel.ModelDeploymentReadModel{
			ID:                         modelID,
			Name:                       name,
			ModelName:                  modelID,
			SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions",
		})
	}
	return out
}

type fakeTargetSetupQueries struct {
	probe func(context.Context, ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error)
}

type fakeTargetAuthCommands struct {
	start  func(context.Context, ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error)
	poll   func(context.Context, string) (readmodel.AuthSessionReadModel, error)
	cancel func(context.Context, string) error
	retry  func(context.Context, string) (readmodel.AuthSessionReadModel, error)
}

type fakeTargetCredentialCommands struct {
	store func(context.Context, ports.StorePastedCredentialRequest) (ports.StorePastedCredentialResult, error)
}

func (f fakeTargetSetupQueries) ProbeProviderModels(ctx context.Context, req ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
	if f.probe != nil {
		return f.probe(ctx, req)
	}
	return readmodel.ModelCatalogReadModel{}, nil
}

func (f fakeTargetAuthCommands) StartAuthSession(ctx context.Context, req ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
	if f.start != nil {
		return f.start(ctx, req)
	}
	return readmodel.AuthSessionReadModel{}, nil
}

func (f fakeTargetAuthCommands) PollAuthSession(ctx context.Context, sessionID string) (readmodel.AuthSessionReadModel, error) {
	if f.poll != nil {
		return f.poll(ctx, sessionID)
	}
	return readmodel.AuthSessionReadModel{}, nil
}

func (f fakeTargetAuthCommands) CancelAuthSession(ctx context.Context, sessionID string) error {
	if f.cancel != nil {
		return f.cancel(ctx, sessionID)
	}
	return nil
}

func (f fakeTargetAuthCommands) RetryAuthSession(ctx context.Context, sessionID string) (readmodel.AuthSessionReadModel, error) {
	if f.retry != nil {
		return f.retry(ctx, sessionID)
	}
	return readmodel.AuthSessionReadModel{}, nil
}

func (f fakeTargetCredentialCommands) StorePastedCredential(ctx context.Context, req ports.StorePastedCredentialRequest) (ports.StorePastedCredentialResult, error) {
	if f.store != nil {
		return f.store(ctx, req)
	}
	return ports.StorePastedCredentialResult{}, nil
}
