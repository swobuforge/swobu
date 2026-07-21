package target_config

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestEditAzureTargetResumesCatalogProbeAfterMount(t *testing.T) {
	target := readmodel.TargetReadModel{
		ID:               "primary",
		Model:            "gpt-5.6-sol",
		Provider:         string(profile.ProviderSpecAzure),
		ProviderProtocol: "responses_stream",
		BaseURL:          "https://contact-8837-resource.services.ai.azure.com/api/projects/contact-8837",
		CredentialRef:    "secret:azure",
	}
	route := readmodel.RouteReadModel{ID: "openai", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{target}}}}
	probed := make(chan struct{}, 2)
	config := NewEditTargetConfig("personal", route, target, nil, nil)
	config.TargetSetupQueries = targetProbeQueriesFunc(func(context.Context, ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
		probed <- struct{}{}
		return readmodel.ModelCatalogReadModel{Deployments: []readmodel.ModelDeploymentReadModel{{
			ID:                         target.Model,
			Name:                       target.Model,
			ModelName:                  target.Model,
			SupportedProviderProtocols: []string{target.ProviderProtocol},
			DefaultProviderProtocol:    target.ProviderProtocol,
		}}}, nil
	})

	config.Open() // Routes opens edit before GSX mounts the component.
	if got := config.SelectedModel.Get().ModelName; got != target.Model {
		t.Fatalf("selected model after open = %q, want persisted %q", got, target.Model)
	}
	if got := config.Draft.Get().ProviderProtocol; got != target.ProviderProtocol {
		t.Fatalf("protocol after open = %q, want persisted %q", got, target.ProviderProtocol)
	}

	harness, err := testkit.NewHarnessAt(config, 100, 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(harness.Close)
	harness.Open()

	select {
	case <-probed:
	case <-time.After(time.Second):
		t.Fatal("mount did not resume the pending edit catalog probe")
	}
	frame := harness.FrameTrimmed()
	if config.catalogLoading() {
		t.Fatalf("catalog remained loading after probe completion:\n%s", frame)
	}
	if got := config.SelectedModel.Get().ModelName; got != target.Model {
		t.Fatalf("hydrated model = %q, want %q", got, target.Model)
	}
	if !strings.Contains(frame, "deployment") || !strings.Contains(frame, target.Model) || !strings.Contains(frame, "OpenAI · Responses · stream") {
		t.Fatalf("edit did not resolve persisted Azure values:\n%s", frame)
	}
	select {
	case <-probed:
		t.Fatal("mount launched the pending catalog probe more than once")
	default:
	}
}

func TestEditCustomTargetProtocolRemainsChangeable(t *testing.T) {
	t.Parallel()

	target := readmodel.TargetReadModel{
		ID:               "primary",
		Model:            "glm-5.2",
		Provider:         "custom",
		ProviderProtocol: "responses_stream",
		BaseURL:          "https://api.example.test/v1",
	}
	route := readmodel.RouteReadModel{ID: "primary", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{target}}}}
	config := NewEditTargetConfig("dev", route, target, nil, nil)

	frame := testkit.RenderMountedTrimmed(t, TargetConfigTail(config), 100, 12)
	if !strings.Contains(frame, "protocol          OpenAI · Responses · stream") || !strings.Contains(frame, "change ↵") {
		t.Fatalf("existing custom protocol must remain selected and changeable:\n%s", frame)
	}
	if strings.Contains(frame, "protocol          OpenAI · Responses · stream                fixed") {
		t.Fatalf("existing custom protocol was narrowed to a fixed singleton:\n%s", frame)
	}
}

func TestCustomTargetRehydratesWithCanonicalIdentityAndHeader(t *testing.T) {
	target := readmodel.TargetReadModel{
		ID: "primary", Provider: "custom", ProviderProtocol: "messages",
		BaseURL: "https://api.example.test/v1", AuthHeader: "x-api-key",
	}
	draft := TargetDraftFromReadModel("chat", target)
	if draft.ProviderSpec != "custom" || draft.CredentialHeader != "x-api-key" {
		t.Fatalf("draft = %#v, want canonical custom identity and direct credential header", draft)
	}
}

func TestEditCustomTargetPersistsChangedProtocol(t *testing.T) {
	t.Parallel()

	target := readmodel.TargetReadModel{ID: "primary", Model: "glm-5.2", Provider: "custom", ProviderProtocol: "responses_stream", BaseURL: "https://api.example.test/v1"}
	route := readmodel.RouteReadModel{ID: "primary", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{target}}}}
	var saved ports.SaveTargetRequest
	save := func(_ context.Context, request ports.SaveTargetRequest) (ports.SaveTargetResult, error) {
		saved = request
		updated := target
		updated.ProviderProtocol = request.Protocol
		return ports.SaveTargetResult{Target: updated, Route: route}, nil
	}
	config := NewEditTargetConfig("dev", route, target, save, nil)

	config.selectProtocol("messages_stream")

	if saved.Protocol != "messages_stream" {
		t.Fatalf("saved protocol=%q want messages_stream", saved.Protocol)
	}
	if config.Target.ProviderProtocol != "messages_stream" {
		t.Fatalf("committed protocol=%q want messages_stream", config.Target.ProviderProtocol)
	}
}
