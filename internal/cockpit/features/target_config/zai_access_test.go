package target_config

import (
	"context"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestZAICreateFlow(t *testing.T) {
	w := NewTargetConfig("personal", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.Open()
	w.SelectProvider(string(profile.ProviderSpecZAI))

	if got := w.setupState().Status; got != setupMissingLocator {
		t.Fatalf("setup status = %v, want missing access", got)
	}
	frame := testkit.RenderMountedTrimmed(t, w, 100, 24)
	for _, required := range []string{"provider", "Z.AI", "access", "required"} {
		if !strings.Contains(frame, required) {
			t.Fatalf("Z.AI form missing %q:\n%s", required, frame)
		}
	}
	for _, forbidden := range []string{"backend URL", "credential header", "authentication mode", "protocol"} {
		if strings.Contains(frame, forbidden) {
			t.Fatalf("Z.AI form exposes %q:\n%s", forbidden, frame)
		}
	}

	w.SelectZAIAccess(string(routing.ZAIAccessCodingPlan))
	frame = testkit.RenderMountedTrimmed(t, w, 100, 24)
	if !strings.Contains(frame, "Coding Plan") || !strings.Contains(frame, "credential") {
		t.Fatalf("selected Z.AI access did not reveal credential setup:\n%s", frame)
	}
	if got := w.setupState().Status; got != setupMissingCredential {
		t.Fatalf("setup status = %v, want missing credential", got)
	}
	w.CredentialCommands = targetCredentialCommandsFunc(func(_ context.Context, req ports.StorePastedCredentialRequest) (ports.StorePastedCredentialResult, error) {
		if req.ProviderSpec != string(profile.ProviderSpecZAI) {
			t.Fatalf("credential provider = %q, want zai", req.ProviderSpec)
		}
		return ports.StorePastedCredentialResult{CredentialRef: "secretfile:" + req.Name}, nil
	})
	ref, err := w.storePastedCredential(context.Background(), "zai-secret")
	if err != nil {
		t.Fatal(err)
	}
	w.changeCredentialRef(ref)
	w.SelectModel(readmodel.ModelAuthoringOptionReadModel{ID: "manual-model", ModelName: "manual-model"})

	if draft := w.Draft.Get(); draft.ProviderProtocol != "" || draft.ZAIAccess != string(routing.ZAIAccessCodingPlan) || draft.CredentialRef != ref {
		t.Fatalf("Z.AI create draft = %#v", draft)
	}
	if !w.readyToCreate() {
		t.Fatal("manual Z.AI model did not satisfy create readiness")
	}
	var saved ports.SaveTargetRequest
	w.SaveTarget = func(_ context.Context, req ports.SaveTargetRequest) (ports.SaveTargetResult, error) {
		saved = req
		return ports.SaveTargetResult{}, nil
	}
	w.Create(context.Background())
	if saved.Protocol != "" {
		t.Fatalf("Z.AI save protocol = %q, want omitted", saved.Protocol)
	}
	connection, ok := saved.Connection.(routing.ZAIConnection)
	if !ok || connection.Access() != routing.ZAIAccessCodingPlan || connection.Credential().String() != ref || saved.ModelID != "manual-model" {
		t.Fatalf("saved Z.AI target = %#v, connection = %#v", saved, saved.Connection)
	}
}

func TestMountedZAIModelIsPlainInputAndSubmitsTypedName(t *testing.T) {
	w := authoringConfig(t, profile.ProviderSpecZAI, "", "env:ZAI_API_KEY")
	w.SelectZAIAccess(string(routing.ZAIAccessCodingPlan))
	h, err := testkit.NewHarnessAt(w, 100, 18)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()

	frame := h.FrameTrimmed()
	if !strings.Contains(frame, "> model") || !strings.Contains(frame, "required") {
		t.Fatalf("Z.AI model input did not own the required field:\n%s", frame)
	}
	if strings.Contains(frame, "search") || strings.Contains(frame, "shown") {
		t.Fatalf("Z.AI model field mounted catalog-picker chrome:\n%s", frame)
	}

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	for _, key := range runeKeys("glm-5") {
		h.DispatchKey(key)
	}
	editing := h.FrameTrimmed()
	if !strings.Contains(editing, "glm-5") || !strings.Contains(editing, "save ↵") {
		t.Fatalf("Z.AI model row did not expose a plain text editor:\n%s", editing)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if got := w.SelectedModel.Get().ModelName; got != "glm-5" {
		t.Fatalf("submitted Z.AI model = %q, want glm-5", got)
	}
	if !w.readyToCreate() {
		t.Fatal("submitted Z.AI model did not make the target ready")
	}
	if frame := h.FrameTrimmed(); strings.Contains(frame, "search") || strings.Contains(frame, "shown") {
		t.Fatalf("submitted Z.AI model switched to picker chrome:\n%s", frame)
	}
}

func TestMountedZAIModelEscapeCancelsInputWithoutClosingTargetConfig(t *testing.T) {
	w := authoringConfig(t, profile.ProviderSpecZAI, "", "env:ZAI_API_KEY")
	w.SelectZAIAccess(string(routing.ZAIAccessGeneralAPI))
	h, err := testkit.NewHarnessAt(w, 100, 18)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	for _, key := range runeKeys("discard-me") {
		h.DispatchKey(key)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if !w.IsOpen() {
		t.Fatal("Escape from Z.AI model input closed target configuration")
	}
	if got := w.SelectedModel.Get().ModelName; got != "" {
		t.Fatalf("Escape published Z.AI model draft %q", got)
	}
	frame := h.FrameTrimmed()
	if !strings.Contains(frame, "> model") || strings.Contains(frame, "discard-me") || strings.Contains(frame, "search") {
		t.Fatalf("Escape did not return to the plain model row:\n%s", frame)
	}
}

func TestZAIEditFlow(t *testing.T) {
	target := readmodel.TargetReadModel{
		ID:               "zai",
		Model:            "manual-model",
		Provider:         string(profile.ProviderSpecZAI),
		ProviderProtocol: routing.ZAIProviderProtocol,
		ZAIAccess:        string(routing.ZAIAccessCodingPlan),
		CredentialRef:    "secretfile:cockpit/target/personal/zai/target",
	}
	route := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{target}}}}
	w := NewEditTargetConfig("personal", route, target, nil, nil)

	w.Open()

	if w.catalogLoading() {
		t.Fatal("Z.AI manual edit scheduled a catalog probe")
	}
	if draft := w.Draft.Get(); draft.ProviderSpec != string(profile.ProviderSpecZAI) ||
		draft.ZAIAccess != string(routing.ZAIAccessCodingPlan) ||
		draft.CredentialRef != target.CredentialRef ||
		draft.ProviderProtocol != "" {
		t.Fatalf("reloaded Z.AI draft = %#v", draft)
	}
	w.SelectZAIAccess(string(routing.ZAIAccessGeneralAPI))
	if got := w.SelectedModel.Get(); got.ModelName != "manual-model" {
		t.Fatalf("access change cleared manual model: %#v", got)
	}
	draft := w.Draft.Get()
	if draft.ZAIAccess != string(routing.ZAIAccessGeneralAPI) || draft.ProviderProtocol != "" {
		t.Fatalf("draft after access change = %#v", draft)
	}
	connection, err := connectionFromDraft(draft)
	if err != nil {
		t.Fatal(err)
	}
	zai, ok := connection.(routing.ZAIConnection)
	if !ok || zai.Access() != routing.ZAIAccessGeneralAPI || zai.Credential().String() != target.CredentialRef {
		t.Fatalf("reconstructed Z.AI connection = %#v", connection)
	}
}
