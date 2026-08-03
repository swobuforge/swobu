package target_config

import (
	"context"
	"strings"
	"testing"

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
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: "manual-model", ModelName: "manual-model"})

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
