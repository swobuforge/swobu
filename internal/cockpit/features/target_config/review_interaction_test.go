package target_config

import (
	"context"
	"errors"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestBedrockRegionSelectionLaunchesFirstProbe(t *testing.T) {
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.Open()
	w.SelectProvider(string(profile.ProviderSpecBedrock))
	probes := 0
	w.TargetSetupQueries = targetProbeQueriesFunc(func(context.Context, ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
		probes++
		return readmodel.ModelCatalogReadModel{Deployments: []readmodel.ModelDeploymentReadModel{{ID: "model", ModelName: "model"}}}, nil
	})
	w.SelectBedrockRegion("eu-west-2")
	if !w.catalogLoading() {
		t.Fatal("region commit did not enter probe loading state")
	}
	w.ProbeCatalog()
	if probes != 1 {
		t.Fatalf("probes = %d, want 1", probes)
	}
}

func TestBedrockRetryLaunchesAnotherProbe(t *testing.T) {
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.Open()
	w.SelectProvider(string(profile.ProviderSpecBedrock))
	probes := 0
	w.TargetSetupQueries = targetProbeQueriesFunc(func(context.Context, ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
		probes++
		return readmodel.ModelCatalogReadModel{}, errors.New("unavailable")
	})
	w.SelectBedrockRegion("eu-west-2")
	w.ProbeCatalog()
	w.RetryCatalog()
	w.ProbeCatalog()
	if probes != 2 {
		t.Fatalf("probes = %d, want 2", probes)
	}
}

func TestCredentialChangeInvalidatesCreate(t *testing.T) {
	w := readyBedrockConfig(t)
	if !w.readyToCreate() {
		t.Fatal("fixture is not initially creatable")
	}
	newCredentialRow(w, false).props.Apply("env:MISSING")
	w.SetCatalogResult(readmodel.ModelCatalogReadModel{}, errors.New("missing credential"))
	if w.readyToCreate() {
		t.Fatal("failed current probe retained stale create authorization")
	}
}

func TestCustomManualModelEditStillSchedulesBestEffortCatalogProbe(t *testing.T) {
	target := readmodel.TargetReadModel{
		ID:               "custom",
		Model:            "manual-model",
		Provider:         string(profile.ProviderSpecCustom),
		ProviderProtocol: "chat_completions_stream",
		BaseURL:          "http://127.0.0.1:11434/v1",
	}
	route := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{target}}}}
	w := NewEditTargetConfig("personal", route, target, nil, nil)

	w.Open()

	if !w.catalogLoading() {
		t.Fatal("Custom Endpoint edit did not schedule best-effort catalog probe")
	}
}

func TestCredentialPasteUsesOwnerContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.operationContext = ctx
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft { d.ProviderSpec = "openai"; return d })
	received := make(chan context.Context, 1)
	w.CredentialCommands = targetCredentialCommandsFunc(func(commandCtx context.Context, _ ports.StorePastedCredentialRequest) (ports.StorePastedCredentialResult, error) {
		received <- commandCtx
		return ports.StorePastedCredentialResult{CredentialRef: "secret:stored"}, nil
	})
	r := newCredentialRow(w, true)
	r.open()
	h, err := testkit.NewHarnessAt(CredentialChooser(r), 100, 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	h.FocusNext()
	h.FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // mounted paste editor
	for _, char := range "secret" {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: char})
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	select {
	case commandCtx := <-received:
		if !errors.Is(commandCtx.Err(), context.Canceled) {
			t.Fatalf("command context error = %v, want canceled owner context", commandCtx.Err())
		}
	default:
		t.Fatalf("mounted paste submit did not call TargetCredentialCommands; stage=%v frame:\n%s", r.stage.Get(), h.FrameTrimmed())
	}
}

func TestMountedBedrockNestedCredentialEscapeRetreatsOneLevelAtATime(t *testing.T) {
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.ProviderSpec = string(profile.ProviderSpecBedrock)
		return d
	})
	field := BedrockAuthenticationField(w)
	h, err := testkit.NewHarnessAt(field, 100, 12)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	h.FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // authentication menu
	h.FocusNext()
	h.FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // credential source menu
	h.App().BlurFocused()
	h.FocusNext()                                  // authentication header
	h.FocusNext()                                  // environment source
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // environment editor

	frame := h.FrameTrimmed()
	if !strings.Contains(frame, "variable") || !field.IsEntered() {
		t.Fatalf("environment editor not mounted:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	frame = h.FrameTrimmed()
	if !strings.Contains(frame, "environment variable") || strings.Contains(frame, "variable            _") || !field.IsEntered() {
		t.Fatalf("first Escape did not return to credential source menu:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	frame = h.FrameTrimmed()
	if !strings.Contains(frame, "refresh identity") || strings.Contains(frame, "environment variable") || !field.IsEntered() {
		t.Fatalf("second Escape did not return to Bedrock authentication menu:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	frame = h.FrameTrimmed()
	if field.IsEntered() || strings.Contains(frame, "refresh identity") {
		t.Fatalf("third Escape did not close authentication row:\n%s", frame)
	}
}

func TestCredentialPasteStoreFailurePreservesEditorAndSecret(t *testing.T) {
	r := newCredentialField(CredentialFieldProps{ID: "credential", Store: func(string) (string, error) {
		return "", errors.New("store unavailable")
	}})
	r.stage.Set(credStagePaste)
	r.secret.Set("painful-to-retype-secret")
	r.savePasted()
	if got := r.secret.Get(); got != "painful-to-retype-secret" {
		t.Fatalf("secret = %q, want preserved input", got)
	}
	if got := r.stage.Get(); got != credStagePaste {
		t.Fatalf("stage = %v, want paste editor", got)
	}
	if got := r.localError.Get(); got != "store unavailable" {
		t.Fatalf("local error = %q", got)
	}
	if frame := testkit.RenderMountedTrimmed(t, r, 100, 8); !strings.Contains(frame, "store unavailable") {
		t.Fatalf("store failure is not visible:\n%s", frame)
	}
}

func TestCredentialRepasteUsesOneFormOwnedSlot(t *testing.T) {
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft { d.ProviderSpec = "openai"; return d })
	var names []string
	w.CredentialCommands = targetCredentialCommandsFunc(func(_ context.Context, req ports.StorePastedCredentialRequest) (ports.StorePastedCredentialResult, error) {
		names = append(names, req.Name)
		return ports.StorePastedCredentialResult{CredentialRef: "secret:" + req.Name}, nil
	})
	if _, err := w.storePastedCredential(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.storePastedCredential(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] == "" || names[0] != names[1] {
		t.Fatalf("paste slots = %#v", names)
	}
}

type targetCredentialCommandsFunc func(context.Context, ports.StorePastedCredentialRequest) (ports.StorePastedCredentialResult, error)

func (f targetCredentialCommandsFunc) StorePastedCredential(ctx context.Context, req ports.StorePastedCredentialRequest) (ports.StorePastedCredentialResult, error) {
	return f(ctx, req)
}

func TestOptionalCredentialMenuInitiallyFocusesEnvironmentNeverRemove(t *testing.T) {
	r := newCredentialField(CredentialFieldProps{ID: "credential", Optional: true, Ref: "secret:configured"})
	if !CredentialEnvOption(r).AutoFocus {
		t.Fatal("environment option must own initial menu focus")
	}
	if CredentialRemoveOption(r).AutoFocus {
		t.Fatal("remove credential must never own initial menu focus")
	}
}

func TestFailedSaveRetryInvokesSaveImmediately(t *testing.T) {
	w := readyBedrockConfig(t)
	attempts := 0
	w.SaveTarget = func(context.Context, ports.SaveTargetRequest) (ports.SaveTargetResult, error) {
		attempts++
		if attempts == 1 {
			return ports.SaveTargetResult{}, errors.New("save failed")
		}
		return ports.SaveTargetResult{}, nil
	}
	w.Create(context.Background())
	w.RetryCreate()
	if attempts != 2 {
		t.Fatalf("save attempts = %d, want 2", attempts)
	}
}

type targetProbeQueriesFunc func(context.Context, ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error)

func (f targetProbeQueriesFunc) ProbeProviderModels(ctx context.Context, req ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
	return f(ctx, req)
}

func readyBedrockConfig(t *testing.T) *TargetConfig {
	t.Helper()
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.Open()
	w.SelectProvider(string(profile.ProviderSpecBedrock))
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.Locator = "eu-west-2"
		d.CredentialRef = "secret:target"
		return d
	})
	w.Catalog.Set(catalogOperationState{Result: readmodel.ModelCatalogReadModel{Deployments: []readmodel.ModelDeploymentReadModel{{ID: "model-1", Name: "model-1", ModelName: "model-1"}}, BedrockAuthentication: readmodel.BedrockAuthenticationEvidence{Authentication: readmodel.BedrockAuthenticationExplicitAPIKey}}})
	w.SelectedModel.Set(readmodel.ModelDeploymentReadModel{ID: "model-1", Name: "model-1", ModelName: "model-1", SupportedProviderProtocols: []string{"responses"}})
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.ModelID = "model-1"
		d.ProviderProtocol = "responses"
		return d
	})
	return w
}

func TestRemovingBedrockCredentialReprobesWithoutTargetOverride(t *testing.T) {
	w := readyBedrockConfig(t)
	var probed routing.Connection
	w.TargetSetupQueries = targetProbeQueriesFunc(func(_ context.Context, req ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
		probed = req.Connection
		return readmodel.ModelCatalogReadModel{Deployments: []readmodel.ModelDeploymentReadModel{{ID: "model-1", ModelName: "model-1"}}, BedrockAuthentication: readmodel.BedrockAuthenticationEvidence{Authentication: readmodel.BedrockAuthenticationAWSIdentity}}, nil
	})
	row := newCredentialRow(w, false)
	row.props.Apply("")
	w.ProbeCatalog()
	connection, ok := probed.(routing.BedrockConnection)
	if !ok || connection.Credential().String() != "" {
		t.Fatalf("probe connection = %#v", probed)
	}
	if got := w.catalogResult().BedrockAuthentication.Authentication; got != readmodel.BedrockAuthenticationAWSIdentity {
		t.Fatalf("authentication = %q", got)
	}
}

func TestBedrockRefreshPreservesSelectionWhenStillAvailable(t *testing.T) {
	w := readyBedrockConfig(t)
	deployments := []readmodel.ModelDeploymentReadModel{{ID: "model-1", ModelName: "model-1", SupportedProviderProtocols: []string{"responses"}}}
	w.TargetSetupQueries = targetProbeQueriesFunc(func(context.Context, ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
		return readmodel.ModelCatalogReadModel{Deployments: deployments}, nil
	})
	w.RefreshBedrockIdentity()
	w.ProbeCatalog()
	if w.SelectedModel.Get().ModelName != "model-1" || w.Draft.Get().ProviderProtocol != "responses" {
		t.Fatal("refresh discarded a selection still supported by the refreshed catalog")
	}
	deployments = nil
	w.RefreshBedrockIdentity()
	w.ProbeCatalog()
	if w.SelectedModel.Get().ModelName != "" || w.Draft.Get().ProviderProtocol != "" {
		t.Fatalf("stale selection survived refresh: %#v", w.Draft.Get())
	}
}

func TestMountedBedrockRefreshStartsExactlyOneProbeAndReconcilesSelection(t *testing.T) {
	w := readyBedrockConfig(t)
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft { d.CredentialRef = ""; return d })
	probes := 0
	probed := make(chan struct{}, 2)
	deployments := []readmodel.ModelDeploymentReadModel{{ID: "model-1", ModelName: "model-1", SupportedProviderProtocols: []string{"responses"}}}
	w.TargetSetupQueries = targetProbeQueriesFunc(func(context.Context, ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
		probes++
		probed <- struct{}{}
		return readmodel.ModelCatalogReadModel{Deployments: deployments}, nil
	})
	menu := newBedrockAuthenticationMenu(w, func() {})
	h, err := testkit.NewHarnessAt(menu, 100, 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	w.BindApp(h.App())
	h.Open()
	h.FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	<-probed
	h.Frame()
	if probes != 1 || w.catalogLoading() {
		t.Fatalf("probes=%d loading=%v", probes, w.catalogLoading())
	}
	if w.SelectedModel.Get().ModelName != "model-1" {
		t.Fatal("valid model was cleared")
	}
	deployments = nil
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	<-probed
	h.Frame()
	if probes != 2 || w.catalogLoading() {
		t.Fatalf("probes=%d loading=%v", probes, w.catalogLoading())
	}
	if w.SelectedModel.Get().ModelName != "" || w.Draft.Get().ProviderProtocol != "" {
		t.Fatal("vanished model survived refresh")
	}
}

func TestCustomCredentialHeaderFollowsCredentialPresence(t *testing.T) {
	w := authoringConfig(t, profile.ProviderSpecCustom, "https://example.test/v1", "")
	if shouldRenderCredentialHeaderRow(w) {
		t.Fatal("header row visible without credential")
	}
	row := newCredentialRow(w, false)
	row.props.Apply("env:CUSTOM_KEY")
	if !shouldRenderCredentialHeaderRow(w) {
		t.Fatal("header row missing after credential selection")
	}
	row = newCredentialRow(w, false)
	row.props.Apply("")
	if shouldRenderCredentialHeaderRow(w) {
		t.Fatal("header row survived credential removal")
	}
}

func TestCustomEndpointSubmitLeavesOneSelectionAndNoStaleCaret(t *testing.T) {
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.Open()
	w.SelectProvider(string(profile.ProviderSpecCustom))
	h, err := testkit.NewHarnessAt(w, 100, 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	for _, key := range runeKeys("https://api.z.ai/api/anthropic") {
		h.DispatchKey(key)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	frame := h.FrameTrimmed()
	if got := strings.Count(frame, ">"); got != 1 {
		t.Fatalf("selected markers = %d, want 1 after endpoint submit:\n%s", got, frame)
	}
	if strings.Contains(frame, "anthropic_") {
		t.Fatalf("stale endpoint caret survived submit:\n%s", frame)
	}
	if !strings.Contains(frame, "> credential") {
		t.Fatalf("credential row did not receive selection:\n%s", frame)
	}
}

func TestCustomEndpointWithoutModelCatalogAcceptsTypedModel(t *testing.T) {
	w := authoringConfig(t, profile.ProviderSpecCustom, "https://api.z.ai/api/anthropic", "secret:zai")
	w.Catalog.Set(catalogOperationState{})
	picker := ModelPicker(w, nil)
	if picker.Mode != ui.SearchPickerOpen {
		t.Fatal("Custom Endpoint model picker did not offer the typed model")
	}
	picker.OnSelect(ui.Selection{Value: "glm-4.5", Source: ui.SelectionQuery})
	if got := w.SelectedModel.Get().ModelName; got != "glm-4.5" {
		t.Fatalf("model = %q", got)
	}
	options := w.CurrentProtocolOptions()
	if len(options) == 0 {
		t.Fatal("typed custom model has no protocol choices")
	}
	w.selectProtocol(options[0].ID)
	if !w.readyToCreate() {
		t.Fatal("typed custom model remained blocked by optional catalog discovery")
	}
}

func TestMountedCustomModelRowEntersOpenPickerWithoutReprobing(t *testing.T) {
	w := authoringConfig(t, profile.ProviderSpecCustom, "https://api.z.ai/api/anthropic", "secret:zai")
	w.Catalog.Set(catalogOperationState{})
	probes := 0
	w.TargetSetupQueries = targetProbeQueriesFunc(func(context.Context, ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
		probes++
		return readmodel.ModelCatalogReadModel{}, errors.New("unsupported")
	})
	h, err := testkit.NewHarnessAt(w, 100, 18)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	for _, key := range runeKeys("glm-4.5") {
		h.DispatchKey(key)
	}
	frame := h.FrameTrimmed()
	if probes != 0 {
		t.Fatalf("entering manual model picker issued %d probes", probes)
	}
	if !strings.Contains(frame, "glm-4.5") || !strings.Contains(frame, "use ↵") {
		t.Fatalf("manual model picker did not remain entered:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	if got := w.SelectedModel.Get().ModelName; got != "glm-4.5" {
		t.Fatalf("selected model = %q", got)
	}
}

func TestAzureCatalogOperationRowsUseDeploymentNoun(t *testing.T) {
	w := authoringConfig(t, profile.ProviderSpecAzure, "https://example.services.ai.azure.com/api/projects/demo", "env:AZURE_KEY")
	w.Catalog.Set(catalogOperationState{Loading: true})
	loading := testkit.RenderMountedTrimmed(t, TargetConfigTail(w), 100, 8)
	if !strings.Contains(loading, "deployment") || strings.Contains(loading, "model             loading") {
		t.Fatalf("Azure loading copy is not deployment-specific:\n%s", loading)
	}
	w.Catalog.Set(catalogOperationState{Err: "unauthorized"})
	failed := testkit.RenderMountedTrimmed(t, TargetConfigTail(w), 100, 8)
	if !strings.Contains(failed, "deployment") || strings.Contains(failed, "model             unauthorized") {
		t.Fatalf("Azure failure copy is not deployment-specific:\n%s", failed)
	}
}
