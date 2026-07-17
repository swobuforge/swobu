package target_config

import (
	"context"
	"strings"
	"testing"
	"time"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/mountedrender"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

// TestTargetConfig_EndToEnd walks provider → setup/loading → catalog → model → ready
// through the actionable component tree mounted by Render().
func TestTargetConfig_EndToEnd(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	route := readmodel.RouteReadModel{
		ID: "gpt", ModelName: "gpt",
		Targets: []readmodel.TargetReadModel{{ID: "t1", Rank: 1, Weight: 1}},
	}
	w := newTargetConfigSeededWithProviders("dev", route, nil, nil, []readmodel.ProviderOptionReadModel{
		{ProviderSpec: "openai", DisplayName: "OpenAI", SetupHint: "API key"},
		{ProviderSpec: "anthropic", DisplayName: "Anthropic", SetupHint: "API key"},
	})
	w.Open()

	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}

	// Phase: ChoosingProvider — provider picker should be visible.
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase = %v, want ChoosingProvider", w.Phase.Get())
	}
	if !strings.Contains(rendered, "provider") {
		t.Fatalf("expected 'provider' in frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "add target") {
		t.Fatalf("expected add-target title in provider picker:\n%s", rendered)
	}
	if !strings.Contains(rendered, "OpenAI") {
		t.Fatalf("expected 'OpenAI' in provider picker:\n%s", rendered)
	}
	if !strings.Contains(rendered, "> OpenAI") {
		t.Fatalf("expected focused provider option in provider picker:\n%s", rendered)
	}
	if strings.Contains(rendered, "API key") || strings.Contains(rendered, "endpoint") || strings.Contains(rendered, "credential, model, protocol") {
		t.Fatalf("provider picker leaked setup detail into label-only picker:\n%s", rendered)
	}

	// Select provider via API (simulates picker activation).
	w.SelectProvider("openai")

	// Phase: ProviderSetup — credentials are explicit, so OpenAI blocks until
	// the operator chooses one.
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase = %v, want ProviderSetup", w.Phase.Get())
	}
	rendered, _ = mountedrender.String(w, 120, 40)
	if !strings.Contains(rendered, "credential") {
		t.Fatalf("expected 'credential' in setup frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "new target · OpenAI") {
		t.Fatalf("expected target config title after provider select:\n%s", rendered)
	}
	if !strings.Contains(rendered, "OpenAI") {
		t.Fatalf("expected 'OpenAI' in setup row:\n%s", rendered)
	}
	if !strings.Contains(rendered, "credential") || !strings.Contains(rendered, "required") || !strings.Contains(rendered, "choose ↵") {
		t.Fatalf("expected credential chooser row in setup frame:\n%s", rendered)
	}

	w.SetSetupReady("env:OPENAI_API_KEY", "https://api.openai.com/v1")
	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase = %v, want LoadingCatalog", w.Phase.Get())
	}
	rendered, _ = mountedrender.String(w, 120, 40)
	if !strings.Contains(rendered, "loading catalog…") {
		t.Fatalf("expected loading row while catalog is pending:\n%s", rendered)
	}
	w.ProbeCatalog()
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase = %v, want ChoosingModel", w.Phase.Get())
	}
	rendered, _ = mountedrender.String(w, 120, 40)
	if !strings.Contains(rendered, "model") {
		t.Fatalf("expected 'model' in frame:\n%s", rendered)
	}
	// RFC Phase 5: the model row is enterable (manual-enter picker) once the
	// catalog loads. The picker body is covered by the model picker tests; this
	// e2e drives the commit through the SelectModel API.
	if !strings.Contains(rendered, "choose ↵") {
		t.Fatalf("expected enterable model row after catalog load:\n%s", rendered)
	}

	// Select model via API.
	for _, d := range w.Catalog.Get().Deployments {
		if d.ID == "gpt-4.1" {
			w.SelectModel(d)
			break
		}
	}
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase = %v, want ReadyToCreate", w.Phase.Get())
	}
	rendered, _ = mountedrender.String(w, 120, 40)
	if !strings.Contains(rendered, "chat_completions") {
		t.Fatalf("expected selected default protocol after model selection:\n%s", rendered)
	}
	rendered, _ = mountedrender.String(w, 120, 40)
	if !strings.Contains(rendered, "new target · OpenAI") {
		t.Fatalf("expected ready-state target config title:\n%s", rendered)
	}
	if !strings.Contains(rendered, "> create") {
		t.Fatalf("expected create row focused in ready state:\n%s", rendered)
	}

	rendered, _ = mountedrender.String(w, 120, 40)
	if !strings.Contains(rendered, "provider") || !strings.Contains(rendered, "model") || !strings.Contains(rendered, "routing") || !strings.Contains(rendered, "create") {
		t.Fatalf("expected summary rows in ReadyToCreate frame:\n%s", rendered)
	}

	// Select balance with step 1. Placement picker rendering is covered by
	// TestTargetConfig_PlacementPickerRender; the e2e drives the commit directly.
	w.SelectPlacement(readmodel.PlacementOptionReadModel{
		Rank: 1, Weight: 1, Kind: readmodel.PlacementBalance,
	})
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase = %v, want ReadyToCreate after routing select", w.Phase.Get())
	}
	rendered, _ = mountedrender.String(w, 120, 40)
	if !strings.Contains(rendered, "balance with step 1") {
		t.Fatalf("expected selected placement summary after picker:\n%s", rendered)
	}
}

// TestTargetConfig_EndToEnd_FirstTargetFixedPlacement walks the same mounted tree
// for an empty route and proves the first target does not open a placement
// picker.
func TestTargetConfig_EndToEnd_FirstTargetFixedPlacement(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	var request ports.SaveTargetRequest
	route := readmodel.RouteReadModel{
		ID: "gpt", ModelName: "gpt",
	}
	w := NewTargetConfig("dev", route, func(_ context.Context, req ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
		request = req
		return readmodel.TargetReadModel{ID: "t-new"}, nil
	}, nil)
	w.Open()

	w.SelectProvider("openai")
	w.SetSetupReady("env:OPENAI_API_KEY", "")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", SupportedProviderProtocols: []string{"chat_completions"}, DefaultProviderProtocol: "chat_completions"},
		},
	})
	for _, d := range w.Catalog.Get().Deployments {
		if d.ID == "gpt-4.1" {
			w.SelectModel(d)
			break
		}
	}

	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase = %v, want ReadyToCreate", w.Phase.Get())
	}
	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
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

	w.Create(context.TODO())
	if request.Draft.Rank != 1 {
		t.Fatalf("create rank = %d, want 1", request.Draft.Rank)
	}
	if request.Draft.Weight != 1 {
		t.Fatalf("create weight = %d, want 1", request.Draft.Weight)
	}
}

// TestTargetConfig_ChatGPTAuthPendingRender proves the interactive-auth branch
// renders the pending auth rows before catalog probing begins.
func TestTargetConfig_ChatGPTAuthPendingRender(t *testing.T) {
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
	}

	w.SelectProvider("chatgpt")
	w.ContinueSetup()

	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if w.Phase.Get() != PhaseAuthPending {
		t.Fatalf("phase = %v, want AuthPending", w.Phase.Get())
	}
	if !strings.Contains(rendered, "browser login") {
		t.Fatalf("expected browser login row in frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "https://auth.openai.com/oauth/authorize") {
		t.Fatalf("expected auth URL in frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "waiting for login") {
		t.Fatalf("expected pending status row in frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "cancel") {
		t.Fatalf("expected cancel row in frame:\n%s", rendered)
	}
	// Fixture snapshot so we have a visual record of this wireframe.
	testkit.AssertVisual("chatgpt_auth_pending").
		Fixture("testdata/target_config_component/fixture/chatgpt_auth_pending.txt").
		Viewport(120, 40).
		Now(t, rendered)
}

// TestTargetConfig_ChatGPTAuthDeviceRender proves device auth shows code + copy.
func TestTargetConfig_ChatGPTAuthDeviceRender(t *testing.T) {
	w := NewTargetConfig("dev", sampleRoute(), nil, nil)
	w.TargetAuthCommands = fakeTargetAuthCommands{
		start: func(_ context.Context, _ ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
			return readmodel.AuthSessionReadModel{
				ProviderSpec: "chatgpt",
				SessionID:    "sess-2",
				State:        "pending",
				AuthorizeURL: "https://auth.openai.com/device",
				UserCode:     "ABCD-1234",
			}, nil
		},
	}

	w.SelectProvider("chatgpt")
	w.ContinueSetup()

	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if w.Phase.Get() != PhaseAuthPending {
		t.Fatalf("phase = %v, want AuthPending", w.Phase.Get())
	}
	// Current implementation labels all interactive auth as "browser login".
	if !strings.Contains(rendered, "browser login") {
		t.Fatalf("expected browser login row in frame:\n%s", rendered)
	}
	// The URL should contain the device endpoint.
	if !strings.Contains(rendered, "https://auth.openai.com/device") {
		t.Fatalf("expected device auth URL in frame:\n%s", rendered)
	}
	// Device flow surfaces the user code as a copyable control.
	if !strings.Contains(rendered, "code") || !strings.Contains(rendered, "ABCD-1234") || !strings.Contains(rendered, "copy ↵") {
		t.Fatalf("expected device user-code copy row in frame:\n%s", rendered)
	}
	// Fixture snapshot for device auth wireframe.
	testkit.AssertVisual("chatgpt_device_auth").
		Fixture("testdata/target_config_component/fixture/chatgpt_device_auth.txt").
		Viewport(120, 40).
		Now(t, rendered)
}

// TestTargetConfig_ChatGPTAuthCompleteRender proves auth success + catalog loading.
func TestTargetConfig_ChatGPTAuthCompleteRender(t *testing.T) {
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

	w.SelectProvider("chatgpt")
	w.ContinueSetup()
	w.RefreshAuthSession()

	if w.Draft.Get().CredentialRef != "chatgpt:acct_a" {
		t.Fatalf("credential ref = %q, want chatgpt:acct_a", w.Draft.Get().CredentialRef)
	}
	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase = %v, want LoadingCatalog", w.Phase.Get())
	}

	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if !strings.Contains(rendered, "signed in") {
		t.Fatalf("expected signed-in state in frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "loading catalog") {
		t.Fatalf("expected catalog loading in frame:\n%s", rendered)
	}
	if strings.Contains(rendered, "credential") {
		t.Fatalf("should not show raw credential row after auth success:\n%s", rendered)
	}
	// Fixture snapshot for auth complete wireframe.
	testkit.AssertVisual("chatgpt_auth_complete").
		Fixture("testdata/target_config_component/fixture/chatgpt_auth_complete.txt").
		Viewport(120, 40).
		Now(t, rendered)
}

// TestTargetConfig_ChatGPTReadyToCreateRender proves the full ChatGPT happy path
// reaches ReadyToCreate with the RFC "ChatGPT Ready" shape: auth signed in,
// model selected, protocol responses_stream fixed as the default (ChatGPT
// exposes exactly one protocol, so no picker is ever shown), first-target fixed
// placement, and a focused create row. Browser/device login is simulated through
// the TargetAuthCommands seam, so no real OpenAI login is required.
func TestTargetConfig_ChatGPTReadyToCreateRender(t *testing.T) {
	route := readmodel.RouteReadModel{ID: "gpt", ModelName: "gpt"}
	w := NewTargetConfig("dev", route, nil, nil)
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

	w.SelectProvider("chatgpt")
	w.ContinueSetup()
	if w.Phase.Get() != PhaseAuthPending {
		t.Fatalf("phase after start = %v, want AuthPending", w.Phase.Get())
	}
	w.RefreshAuthSession()
	if w.Draft.Get().CredentialRef != "chatgpt:acct_a" {
		t.Fatalf("credential ref = %q, want chatgpt:acct_a", w.Draft.Get().CredentialRef)
	}
	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase after auth success = %v, want LoadingCatalog", w.Phase.Get())
	}

	w.SetCatalogResult(readmodel.ModelCatalogReadModel{
		Deployments: []readmodel.ModelDeploymentReadModel{
			{ID: "gpt-5.5", Name: "GPT-5.5", ModelName: "gpt-5.5", SupportedProviderProtocols: []string{"responses_stream"}, DefaultProviderProtocol: "responses_stream"},
		},
		ResolvedProviderProtocol: "responses_stream",
	})
	if w.Phase.Get() != PhaseConfiguring {
		t.Fatalf("phase after catalog = %v, want ChoosingModel", w.Phase.Get())
	}
	w.SelectModel(readmodel.ModelDeploymentReadModel{
		ID: "gpt-5.5", Name: "GPT-5.5", ModelName: "gpt-5.5",
		SupportedProviderProtocols: []string{"responses_stream"}, DefaultProviderProtocol: "responses_stream",
	})

	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase after model select = %v, want ReadyToCreate", w.Phase.Get())
	}
	if got := w.Draft.Get().ProviderProtocol; got != "responses_stream" {
		t.Fatalf("selected protocol = %q, want responses_stream", got)
	}
	if options := w.CurrentProtocolOptions(); len(options) != 1 || options[0].ID != "responses_stream" {
		t.Fatalf("protocol options = %+v, want exactly [responses_stream]", options)
	}

	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if !strings.Contains(rendered, "new target · ChatGPT") {
		t.Fatalf("expected chatgpt title:\n%s", rendered)
	}
	if !strings.Contains(rendered, "signed in") {
		t.Fatalf("expected signed-in auth row:\n%s", rendered)
	}
	if !strings.Contains(rendered, "gpt-5.5") {
		t.Fatalf("expected selected model gpt-5.5:\n%s", rendered)
	}
	if !strings.Contains(rendered, "responses_stream") {
		t.Fatalf("expected responses_stream protocol row:\n%s", rendered)
	}
	// ChatGPT exposes exactly one protocol, so it must never render a picker.
	if strings.Contains(rendered, "6 of 6 shown") {
		t.Fatalf("chatgpt must not render a protocol picker:\n%s", rendered)
	}
	if !strings.Contains(rendered, "step 1") {
		t.Fatalf("expected first-target fixed placement:\n%s", rendered)
	}
	if !strings.Contains(rendered, "> create") {
		t.Fatalf("expected focused create row:\n%s", rendered)
	}

	testkit.AssertVisual("chatgpt_protocol_default").
		Fixture("testdata/target_config_component/fixture/chatgpt_protocol_default.txt").
		Viewport(120, 40).
		Now(t, rendered)
}

// TestTargetConfig_FocusableComponentsWalk verifies that ProviderSetup and
// ChoosingModel phases render focusable action rows.
func TestTargetConfig_FocusableComponentsWalk(t *testing.T) {
	w := newTargetConfigSeededWithProviders("dev", sampleRoute(), nil, nil, []readmodel.ProviderOptionReadModel{
		{ProviderSpec: "openai", DisplayName: "OpenAI", SetupHint: "API key"},
	})

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	w.Open()

	// ChoosingProvider: SearchPicker mounts selectable ChoiceList rows.
	f1 := countFocusables(h.App().Root())

	w.SelectProvider("openai")
	h.Open()
	f2 := countFocusables(h.App().Root())
	// ProviderSetup mounts focusable blocked-credential rows.
	if f2 < f1 || f2 == 0 {
		t.Fatalf("provider setup focusables = %d, want > %d", f2, f1)
	}

	w.SetSetupReady("env:OPENAI_API_KEY", "https://api.openai.com/v1")
	waitForTargetConfigPhase(t, h, w, PhaseConfiguring)
	f3 := countFocusables(h.App().Root())
	// ChoosingModel: SearchPicker is focusable.
	if f3 == 0 {
		t.Fatalf("model picker phase should have focusables, got %d", f3)
	}
}

func waitForTargetConfigPhase(t *testing.T, h *testkit.MockAppHarness, w *TargetConfig, want Phase) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for w.Phase.Get() != want {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for phase %v, got %v", want, w.Phase.Get())
		}
		if !h.App().Step() {
			t.Fatalf("app stopped while waiting for phase %v", want)
		}
		time.Sleep(1 * time.Millisecond)
	}
}

func countFocusables(root *tui.Element) int {
	var count int
	root.WalkFocusables(func(f tui.Focusable) {
		count++
	})
	return count
}
