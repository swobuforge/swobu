package target_add

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/mountedrender"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestPhase_IsTerminal(t *testing.T) {
	if PhaseClosed.IsTerminal() {
		t.Fatal("Closed should not be terminal")
	}
	if !PhaseCreated.IsTerminal() {
		t.Fatal("Created should be terminal")
	}
	if !PhaseCatalogFailed.IsTerminal() {
		t.Fatal("CatalogFailed should be terminal")
	}
	if PhaseChoosingProvider.IsTerminal() {
		t.Fatal("ChoosingProvider should not be terminal")
	}
}

func TestWorkflow_DefaultPhaseIsClosed(t *testing.T) {
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	if w.Phase.Get() != PhaseClosed {
		t.Fatalf("initial phase = %v, want Closed", w.Phase.Get())
	}
}

func TestWorkflow_OpenMovesToChoosingProvider(t *testing.T) {
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	w.Open()
	if w.Phase.Get() != PhaseChoosingProvider {
		t.Fatalf("phase after Open = %v, want ChoosingProvider", w.Phase.Get())
	}
}

func TestWorkflow_ChoosingProviderRendersBoundedPicker(t *testing.T) {
	opts := make([]readmodel.ProviderOptionReadModel, 112)
	for i := range opts {
		opts[i] = readmodel.ProviderOptionReadModel{
			ProviderSpec: fmt.Sprintf("provider-%03d", i),
			DisplayName:  fmt.Sprintf("Provider %d", i),
			SetupHint:    "API key",
		}
	}

	w := NewWorkflow("dev", sampleRoute(), nil, nil, WithProviderOptions(opts))
	w.Open()

	rendered, err := mountedrender.String(w, 120, 20)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if w.Phase.Get() != PhaseChoosingProvider {
		t.Fatalf("phase = %v, want ChoosingProvider", w.Phase.Get())
	}
	if got, want := strings.Count(rendered, "Provider "), 7; got != want {
		t.Fatalf("visible provider rows = %d, want %d\n%s", got, want, rendered)
	}
	if !strings.Contains(rendered, "7 of 112 shown") {
		t.Fatalf("footer missing bounded count:\n%s", rendered)
	}
	if strings.Contains(rendered, "base URL") || strings.Contains(rendered, "credential") || strings.Contains(rendered, "model _") || strings.Contains(rendered, "provider/model") {
		t.Fatalf("provider picker should not leak setup or raw input rows:\n%s", rendered)
	}
}

func TestWorkflow_ProviderPickerFilters(t *testing.T) {
	opts := []readmodel.ProviderOptionReadModel{
		{ProviderSpec: "openai", DisplayName: "OpenAI", SetupHint: "API key"},
		{ProviderSpec: "chatgpt", DisplayName: "ChatGPT", SetupHint: "browser login"},
		{ProviderSpec: "anthropic", DisplayName: "Anthropic", SetupHint: "API key"},
		{ProviderSpec: "openrouter", DisplayName: "OpenRouter", SetupHint: "API key"},
		{ProviderSpec: "ollama", DisplayName: "Ollama", SetupHint: "none"},
		{ProviderSpec: "azure", DisplayName: "Azure AI Foundry", SetupHint: "endpoint"},
		{ProviderSpec: "openai_compatible", DisplayName: "OpenAI Compatible", SetupHint: "endpoint"},
	}

	w := NewWorkflow("dev", sampleRoute(), nil, nil, WithProviderOptions(opts))
	w.Open()

	// Simulate typing "open" via the picker query state.
	picker := w.providerPicker()
	picker.Query.Set("open")

	rendered, err := mountedrender.String(w, 120, 20)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}

	// Prefix: OpenAI, OpenRouter. Token-subsequence: OpenAI Compatible.
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
	if strings.Contains(rendered, "Azure AI Foundry") {
		t.Fatalf("filtered list should NOT contain Azure AI Foundry:\n%s", rendered)
	}
	for _, want := range []string{"OpenAI", "OpenRouter", "OpenAI Compatible"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("filtered list missing %q:\n%s", want, rendered)
		}
	}
	if !strings.Contains(rendered, "> OpenAI") {
		t.Fatalf("expected OpenAI to be focused after filtering:\n%s", rendered)
	}
}

func TestWorkflow_ProviderPickerNoResults(t *testing.T) {
	opts := []readmodel.ProviderOptionReadModel{
		{ProviderSpec: "openai", DisplayName: "OpenAI", SetupHint: "API key"},
		{ProviderSpec: "ollama", DisplayName: "Ollama", SetupHint: "none"},
	}

	w := NewWorkflow("dev", sampleRoute(), nil, nil, WithProviderOptions(opts))
	w.Open()

	picker := w.providerPicker()
	picker.Query.Set("xyz")

	rendered, err := mountedrender.String(w, 120, 20)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}

	if !strings.Contains(rendered, "0 of 2 shown") {
		t.Fatalf("expected '0 of 2 shown' footer for empty search:\n%s", rendered)
	}
	if !strings.Contains(rendered, "provider") {
		t.Fatalf("expected 'provider' title in empty picker:\n%s", rendered)
	}
}

func TestWorkflow_SelectProviderMovesToProviderSetup(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	if w.Phase.Get() != PhaseProviderSetup {
		t.Fatalf("phase after SelectProvider = %v, want ProviderSetup", w.Phase.Get())
	}
	if w.Provider.Get() != "openai" {
		t.Fatalf("provider = %q, want openai", w.Provider.Get())
	}
	if got := w.ProviderSetup.Get().BlockReason; got != "missing OPENAI_API_KEY" {
		t.Fatalf("provider setup block reason = %q, want missing OPENAI_API_KEY", got)
	}
}

func TestWorkflow_RenderUsesProjectedProviderSetupRows(t *testing.T) {
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	w.TargetSetupQueries = fakeTargetSetupQueries{
		resolve: func(_ context.Context, req ports.ResolveProviderSetupRequest) (readmodel.ProviderSetupReadModel, error) {
			if req.ProviderSpec != "openai" {
				t.Fatalf("resolve provider spec = %q, want openai", req.ProviderSpec)
			}
			return readmodel.ProviderSetupReadModel{
				ProviderSpec:       "openai",
				DisplayName:        "Projected OpenAI",
				CredentialLabel:    "env:OPENAI_API_KEY",
				CredentialRef:      "env:OPENAI_API_KEY",
				CredentialRequired: true,
				ReadyForCatalog:    true,
			}, nil
		},
	}

	w.SelectProvider("openai")

	if w.Phase.Get() != PhaseProviderSetup {
		t.Fatalf("phase after projected setup = %v, want ProviderSetup", w.Phase.Get())
	}
	if !w.ProviderSetup.Get().ReadyForCatalog {
		t.Fatal("projected setup should be ready for catalog")
	}
	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if !strings.Contains(rendered, "new target · Projected OpenAI") {
		t.Fatalf("expected projected provider title in setup frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "credential") || !strings.Contains(rendered, "env:OPENAI_API_KEY") || !strings.Contains(rendered, "ok") {
		t.Fatalf("expected projected credential row in setup frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "loading catalog…") || !strings.Contains(rendered, "wait") {
		t.Fatalf("expected projected loading row in setup frame:\n%s", rendered)
	}
	if strings.Contains(rendered, "provider/model") || strings.Contains(rendered, "base URL") {
		t.Fatalf("projected setup should not leak raw input rows:\n%s", rendered)
	}
}

func TestWorkflow_ChatGPTSelectProviderShowsAuthStartWithoutStartingSession(t *testing.T) {
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	w.TargetSetupQueries = fakeTargetSetupQueries{
		resolve: func(_ context.Context, req ports.ResolveProviderSetupRequest) (readmodel.ProviderSetupReadModel, error) {
			if req.ProviderSpec != "chatgpt" {
				t.Fatalf("resolve provider spec = %q, want chatgpt", req.ProviderSpec)
			}
			return readmodel.ProviderSetupReadModel{
				ProviderSpec:    "chatgpt",
				DisplayName:     "Projected ChatGPT",
				CredentialLabel: "browser login",
				BlockReason:     "auth first",
				InteractiveAuth: true,
				AuthModes: []readmodel.AuthModeReadModel{{
					Mode:        string(profile.AuthModeChatGPTLogin),
					Kind:        "credential_ref",
					Requirement: "always",
					Interactive: true,
				}},
			}, nil
		},
	}

	w.SelectProvider("chatgpt")

	if w.Phase.Get() != PhaseProviderSetup {
		t.Fatalf("phase after projected chatgpt setup = %v, want ProviderSetup", w.Phase.Get())
	}
	if w.AuthSession.Get().SessionID != "" {
		t.Fatalf("auth session should not start on SelectProvider, got %q", w.AuthSession.Get().SessionID)
	}
	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if !strings.Contains(rendered, "new target · Projected ChatGPT") {
		t.Fatalf("expected projected chatgpt title in setup frame:\n%s", rendered)
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
}

func TestWorkflow_SetSetupReadyMovesToLoadingCatalog(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "https://api.openai.com/v1")
	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase after SetSetupReady = %v, want LoadingCatalog", w.Phase.Get())
	}
	if !w.CatalogLoading.Get() {
		t.Fatal("catalog loading should be visible after SetSetupReady")
	}
	if w.CredentialRef.Get() != "env:OPENAI_API_KEY" {
		t.Fatalf("credentialRef = %q", w.CredentialRef.Get())
	}
}

func TestWorkflow_ProbeCatalogUsesProviderDefaults(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	var got ports.ProbeProviderModelsRequest
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	w.TargetSetupQueries = fakeTargetSetupQueries{
		probe: func(_ context.Context, req ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
			got = req
			return readmodel.ModelCatalogReadModel{
				Deployments: []readmodel.ModelDeploymentReadModel{
					{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
				},
			}, nil
		},
	}
	w.Open()
	w.SelectProvider("openai")
	w.ContinueSetup()
	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase after ContinueSetup = %v, want LoadingCatalog", w.Phase.Get())
	}
	w.ProbeCatalog()

	if got.ProviderSpec != "openai" {
		t.Fatalf("probe provider spec = %q, want openai", got.ProviderSpec)
	}
	if got.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("probe base URL = %q, want default openai base URL", got.BaseURL)
	}
	if got.AuthHeader != "" {
		t.Fatalf("probe auth header = %q, want empty for openai", got.AuthHeader)
	}
	if got.ProviderProtocol != "responses" {
		t.Fatalf("probe provider protocol = %q, want responses", got.ProviderProtocol)
	}
	if w.Phase.Get() != PhaseChoosingModel {
		t.Fatalf("phase after probe = %v, want ChoosingModel", w.Phase.Get())
	}
}

func TestWorkflow_SetCatalogResultSuccessMovesToChoosingModel(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
		},
	})
	if w.Phase.Get() != PhaseChoosingModel {
		t.Fatalf("phase after catalog success = %v, want ChoosingModel", w.Phase.Get())
	}
	if len(w.Catalog.Get().Deployments) != 1 {
		t.Fatalf("catalog deployments = %d, want 1", len(w.Catalog.Get().Deployments))
	}
}

func TestWorkflow_SetCatalogResultFailureMovesToCatalogFailed(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
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

func TestWorkflow_ChatGPTStartAuthMovesToPending(t *testing.T) {
	var got ports.StartAuthSessionRequest
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
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

func TestWorkflow_ChatGPTAuthSuccessSetsCredentialAndLoadsCatalog(t *testing.T) {
	var got ports.ProbeProviderModelsRequest
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
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
					{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "responses_stream"},
				},
				ResolvedProviderProtocol: "responses_stream",
			}, nil
		},
	}

	w.SelectProvider("chatgpt")
	w.ContinueSetup()
	w.RefreshAuthSession()

	if w.CredentialRef.Get() != "chatgpt:acct_a" {
		t.Fatalf("credential ref = %q, want chatgpt:acct_a", w.CredentialRef.Get())
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
	if w.Phase.Get() != PhaseChoosingModel {
		t.Fatalf("phase after auth success = %v, want ChoosingModel", w.Phase.Get())
	}
	if len(w.Catalog.Get().Deployments) != 1 {
		t.Fatalf("catalog deployments = %d, want 1", len(w.Catalog.Get().Deployments))
	}
}

func TestWorkflow_ChatGPTCancelReturnsToProviderSetup(t *testing.T) {
	var canceled bool
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
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
	if w.Phase.Get() != PhaseProviderSetup {
		t.Fatalf("phase after cancel = %v, want ProviderSetup", w.Phase.Get())
	}
	if w.AuthSession.Get().SessionID != "" {
		t.Fatalf("auth session after cancel = %q, want empty", w.AuthSession.Get().SessionID)
	}
	if w.CredentialRef.Get() != "" {
		t.Fatalf("credential ref after cancel = %q, want empty", w.CredentialRef.Get())
	}
}

func TestWorkflow_SelectModelMovesToReadyToCreate(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"})
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase after select model = %v, want ReadyToCreate", w.Phase.Get())
	}
	if w.SelectedModel.Get().ModelName != "gpt-4.1" {
		t.Fatalf("model = %q", w.SelectedModel.Get().ModelName)
	}
}

func TestWorkflow_CreateCallsSaveTarget(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	var got ports.SaveTargetRequest
	w := NewWorkflow("dev", sampleRoute(), func(_ context.Context, r ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		got = r
		return readmodel.TargetReadModel{ID: "target-1"}, nil
	}, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:KEY", "https://api.openai.com/v1")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"})

	w.Create(context.Background())

	if w.Phase.Get() != PhaseCreated {
		t.Fatalf("phase after create = %v, want Created", w.Phase.Get())
	}
	if got.WorkspaceID != "dev" || got.Provider != "openai" || got.Model != "gpt-4.1" {
		t.Fatalf("SaveTarget request = %+v", got)
	}
	if got.Rank != 2 {
		t.Fatalf("rank = %d, want 2 (fallback after last step)", got.Rank)
	}
	if got.RouteID != "gpt" {
		t.Fatalf("routeID = %q, want gpt (original route preserved)", got.RouteID)
	}
}

func TestWorkflow_CreateErrorMovesToCreateFailed(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewWorkflow("dev", sampleRoute(), func(context.Context, ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		return readmodel.TargetReadModel{}, errors.New("save failed")
	}, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"})

	w.Create(context.Background())

	if w.Phase.Get() != PhaseCreateFailed {
		t.Fatalf("phase after create error = %v, want CreateFailed", w.Phase.Get())
	}
	if w.Error.Get() != "save failed" {
		t.Fatalf("error = %q", w.Error.Get())
	}
}

func TestWorkflow_BackReturnsToPreviousPhase(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("", "")

	if !w.Back() {
		t.Fatal("Back should consume")
	}
	if w.Phase.Get() != PhaseProviderSetup {
		t.Fatalf("phase after back from LoadingCatalog = %v, want ProviderSetup", w.Phase.Get())
	}

	if !w.Back() {
		t.Fatal("Back should consume")
	}
	if w.Phase.Get() != PhaseChoosingProvider {
		t.Fatalf("phase after back from ProviderSetup = %v, want ChoosingProvider", w.Phase.Get())
	}

	if !w.Back() {
		t.Fatal("Back should consume")
	}
	if w.Phase.Get() != PhaseClosed {
		t.Fatalf("phase after back from ChoosingProvider = %v, want Closed", w.Phase.Get())
	}
}

func TestWorkflow_BackFromClosedIsNoOp(t *testing.T) {
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	if w.Back() {
		t.Fatal("Back from Closed should not consume")
	}
}

func TestWorkflow_CloseFiresOnClose(t *testing.T) {
	var closed bool
	w := NewWorkflow("dev", sampleRoute(), nil, func() { closed = true })
	w.Open()
	w.Close()
	if w.Phase.Get() != PhaseClosed {
		t.Fatalf("phase = %v, want Closed", w.Phase.Get())
	}
	if !closed {
		t.Fatal("OnClose not fired")
	}
}

func TestWorkflow_DefaultPlacementFallbackAfterLastStep(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	route := readmodel.RouteReadModel{
		Targets: []readmodel.TargetReadModel{
			{Rank: 1}, {Rank: 1}, {Rank: 2},
		},
	}
	w := NewWorkflow("dev", route, nil, nil)
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

func TestWorkflow_DefaultPlacementForEmptyRouteIsStep1(t *testing.T) {
	w := NewWorkflow("dev", readmodel.RouteReadModel{ID: "gpt", ModelName: "gpt"}, nil, nil)

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

func TestWorkflow_UpdatePropsRefreshesRouteAndSave(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w1 := NewWorkflow("dev", sampleRoute(), nil, nil)
	w1.Open()
	w1.SelectProvider("openai")
	_ = w1.providerPicker()
	w1.SetSetupReady("", "")
	w1.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
		},
	})
	w1.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"})
	w1.OpenPlacementPicker()

	w2 := NewWorkflow("prod", readmodel.RouteReadModel{ID: "other", ModelName: "other"}, func(context.Context, ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
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
	if w1.Provider.Get() != "openai" {
		t.Fatalf("provider selection reset by UpdateProps")
	}
	if w1.providerPickerCache != nil {
		t.Fatal("provider picker cache should be refreshed by UpdateProps")
	}
	if w1.placementPickerCache != nil {
		t.Fatal("placement picker cache should be refreshed by UpdateProps")
	}
}

func TestWorkflow_KeyMapWhenOpen(t *testing.T) {
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	if w.KeyMap() != nil {
		t.Fatal("KeyMap should be nil when closed")
	}
	w.Open()
	if w.KeyMap() == nil {
		t.Fatal("KeyMap should be non-nil when open")
	}
}

// --- Placement tests ----------------------------------------------------

func TestWorkflow_PlacementPickerBuildsOptions(t *testing.T) {
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
	w := NewWorkflow("dev", route, nil, nil)
	w.SelectProvider("openai")
	w.SetSetupReady("", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"})
	w.OpenPlacementPicker()
	if w.Phase.Get() != PhaseChoosingPlacement {
		t.Fatalf("phase = %v, want ChoosingPlacement", w.Phase.Get())
	}
	opts := w.getPlacementOptions()
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

func TestWorkflow_SelectPlacementChangesRankAndReturns(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	route := sampleRoute()
	w := NewWorkflow("dev", route, nil, nil)
	w.SelectProvider("openai")
	w.SetSetupReady("", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"})
	w.OpenPlacementPicker()
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

func TestWorkflow_BackFromPlacementPickerReturnsToReadyToCreate(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	route := sampleRoute()
	w := NewWorkflow("dev", route, nil, nil)
	w.SelectProvider("openai")
	w.SetSetupReady("", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"})
	originalPlacement := w.Placement.Get()
	w.OpenPlacementPicker()
	w.Back()
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase = %v, want ReadyToCreate", w.Phase.Get())
	}
	if w.Placement.Get().Rank != originalPlacement.Rank {
		t.Fatalf("placement changed after back: %v -> %v", originalPlacement, w.Placement.Get())
	}
}

func TestWorkflow_CreateUsesSelectedPlacement(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	var request ports.SaveTargetRequest
	localSave := func(_ context.Context, req ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		request = req
		return readmodel.TargetReadModel{ID: "t-new"}, nil
	}
	route := sampleRoute()
	w := NewWorkflow("dev", route, localSave, nil)
	w.SelectProvider("openai")
	w.SetSetupReady("", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"})
	w.SelectPlacement(readmodel.PlacementOptionReadModel{
		Rank: 1, Weight: 1, Kind: readmodel.PlacementBalance,
	})
	w.Create(context.Background())
	if request.Rank != 1 {
		t.Fatalf("create rank = %d, want 1", request.Rank)
	}
	if request.Weight != 1 {
		t.Fatalf("create weight = %d, want 1", request.Weight)
	}
}

func TestWorkflow_FirstTargetSkipsPlacementPicker(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	var request ports.SaveTargetRequest
	w := NewWorkflow("dev", readmodel.RouteReadModel{ID: "gpt", ModelName: "gpt"}, func(_ context.Context, req ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		request = req
		return readmodel.TargetReadModel{ID: "target-new"}, nil
	}, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"})

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
	if got := strings.Count(rendered, "change ↵"); got != 2 {
		t.Fatalf("first target should only expose provider/model change actions, got %d:\n%s", got, rendered)
	}
	if strings.Contains(rendered, "choose placement") {
		t.Fatalf("first target should not render a placement chooser:\n%s", rendered)
	}

	w.OpenPlacementPicker()
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("empty-route placement picker should be a no-op, got %v", w.Phase.Get())
	}

	w.Create(context.Background())
	if request.Rank != 1 {
		t.Fatalf("create rank = %d, want 1", request.Rank)
	}
	if request.Weight != 1 {
		t.Fatalf("create weight = %d, want 1", request.Weight)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func TestWorkflow_CatalogFailedRendersRetryAndManual(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
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
	if !strings.Contains(rendered, "manual") || !strings.Contains(rendered, "enter model id") {
		t.Fatalf("expected manual entry row in frame:\n%s", rendered)
	}
	// Fixture snapshot for catalog failure wireframe.
	testkit.AssertVisual("catalog_failed").
		Fixture("testdata/target_add_workflow/fixture/catalog_failed.txt").
		Viewport(120, 40).
		Now(t, rendered)
}

func TestWorkflow_ModelPickerRendersBoundedModelList(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
			{ID: "gpt-4.1-mini", Name: "GPT-4.1 Mini", ModelName: "gpt-4.1-mini", DefaultProviderProtocol: "chat_completions"},
			{ID: "gpt-4o", Name: "GPT-4o", ModelName: "gpt-4o", DefaultProviderProtocol: "chat_completions"},
			{ID: "gpt-4o-mini", Name: "GPT-4o Mini", ModelName: "gpt-4o-mini", DefaultProviderProtocol: "chat_completions"},
			{ID: "gpt-4", Name: "GPT-4", ModelName: "gpt-4", DefaultProviderProtocol: "chat_completions"},
			{ID: "gpt-3.5-turbo", Name: "GPT-3.5", ModelName: "gpt-3.5-turbo", DefaultProviderProtocol: "chat_completions"},
		},
	})

	rendered, err := mountedrender.String(w, 120, 24)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if w.Phase.Get() != PhaseChoosingModel {
		t.Fatalf("phase = %v, want ChoosingModel", w.Phase.Get())
	}
	if !strings.Contains(rendered, "6 of 6 shown") {
		t.Fatalf("expected '6 of 6 shown' footer in model picker:\n%s", rendered)
	}
	// Should show some models.
	if !strings.Contains(rendered, "GPT-4.1") {
		t.Fatalf("expected GPT-4.1 in model picker:\n%s", rendered)
	}
	// Fixture snapshot for model picker wireframe.
	testkit.AssertVisual("model_picker").
		Fixture("testdata/target_add_workflow/fixture/model_picker.txt").
		Viewport(120, 24).
		Now(t, rendered)
}

func TestWorkflow_OllamaNoCredential(t *testing.T) {
	w := NewWorkflow("dev", readmodel.RouteReadModel{ID: "gpt", ModelName: "gpt"}, nil, nil)
	w.Open()
	w.SelectProvider("ollama")
	w.ContinueSetup()

	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase = %v, want LoadingCatalog", w.Phase.Get())
	}
	if !w.CatalogLoading.Get() {
		t.Fatal("catalog loading should be active for Ollama")
	}
	if w.CredentialRef.Get() != "" {
		t.Fatalf("credential ref should be empty for Ollama, got %q", w.CredentialRef.Get())
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

	// After probe, Ollama model picker appears from static catalog.
	w.ProbeCatalog()
	if w.Phase.Get() != PhaseChoosingModel {
		t.Fatalf("phase = %v, want ChoosingModel after Ollama probe", w.Phase.Get())
	}

	rendered2, _ := mountedrender.String(w, 120, 24)
	if !strings.Contains(rendered2, "Llama 3.2 3B") {
		t.Fatalf("expected Ollama model list after probe:\n%s", rendered2)
	}
	// Fixture snapshot for Ollama model picker wireframe.
	testkit.AssertVisual("ollama_model_picker").
		Fixture("testdata/target_add_workflow/fixture/ollama_model_picker.txt").
		Viewport(120, 24).
		Now(t, rendered2)
}

func TestWorkflow_ReadyToCreateRender(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"})

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
		Fixture("testdata/target_add_workflow/fixture/ready_to_create.txt").
		Viewport(120, 24).
		Now(t, rendered)
}

func TestWorkflow_FirstTargetReadyNoPlacementPicker(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	w := NewWorkflow("dev", readmodel.RouteReadModel{ID: "gpt", ModelName: "gpt"}, nil, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
		},
	})
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"})

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
		if strings.Contains(line, "placement") && strings.Contains(line, "change") {
			t.Fatalf("first target placement should NOT be changeable:\n%s", line)
		}
	}
	// Fixture snapshot for first-target ready wireframe.
	testkit.AssertVisual("first_target_ready").
		Fixture("testdata/target_add_workflow/fixture/first_target_ready.txt").
		Viewport(120, 24).
		Now(t, rendered)
}

func TestWorkflow_RetryCatalogReprobes(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	probeCount := 0
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	w.TargetSetupQueries = fakeTargetSetupQueries{
		probe: func(_ context.Context, req ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
			probeCount++
			if probeCount == 1 {
				return readmodel.ModelCatalogReadModel{Error: "timeout"}, nil
			}
			return readmodel.ModelCatalogReadModel{
				Deployments: []readmodel.ModelDeploymentReadModel{
					{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
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
	w.ProbeCatalog()

	if probeCount != 2 {
		t.Fatalf("probe count = %d, want 2", probeCount)
	}
	if w.Phase.Get() != PhaseChoosingModel {
		t.Fatalf("phase = %v, want ChoosingModel after retry success", w.Phase.Get())
	}
}

func TestWorkflow_ManualEntryCreatesModelWithRouteModelSplit(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	var got ports.SaveTargetRequest
	w := NewWorkflow("dev", sampleRoute(), func(_ context.Context, r ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		got = r
		return readmodel.TargetReadModel{ID: "target-1"}, nil
	}, nil)
	w.Open()
	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{Error: "no catalog"})

	// Simulate manual entry submission.
	row := ManualModelEntryRowComponent(w)
	row.OnSubmit("gpt-4.1-custom")

	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase = %v, want ReadyToCreate", w.Phase.Get())
	}
	if w.SelectedModel.Get().ModelName != "gpt-4.1-custom" {
		t.Fatalf("selected model = %q, want gpt-4.1-custom", w.SelectedModel.Get().ModelName)
	}

	w.Create(context.Background())

	if got.RouteID != "gpt" {
		t.Fatalf("routeID = %q, want gpt", got.RouteID)
	}
	if got.Model != "gpt-4.1-custom" {
		t.Fatalf("model = %q, want gpt-4.1-custom", got.Model)
	}
	if got.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", got.Provider)
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

type fakeTargetSetupQueries struct {
	resolve func(context.Context, ports.ResolveProviderSetupRequest) (readmodel.ProviderSetupReadModel, error)
	probe   func(context.Context, ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error)
}

type fakeTargetAuthCommands struct {
	start  func(context.Context, ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error)
	poll   func(context.Context, string) (readmodel.AuthSessionReadModel, error)
	cancel func(context.Context, string) error
	retry  func(context.Context, string) (readmodel.AuthSessionReadModel, error)
}

func (f fakeTargetSetupQueries) ListTargetProviders(context.Context) ([]readmodel.ProviderOptionReadModel, error) {
	return nil, nil
}

func (f fakeTargetSetupQueries) ResolveProviderSetup(ctx context.Context, req ports.ResolveProviderSetupRequest) (readmodel.ProviderSetupReadModel, error) {
	if f.resolve != nil {
		return f.resolve(ctx, req)
	}
	return projectProviderSetupLocal(req.ProviderSpec, req.BaseURL, req.CredentialRef), nil
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
