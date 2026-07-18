package target_config

import (
	"context"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestEditCustomTargetProtocolRemainsChangeable(t *testing.T) {
	t.Parallel()

	target := readmodel.TargetReadModel{
		ID:               "primary",
		Model:            "glm-5.2",
		Provider:         "openai_compatible",
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

func TestEditCustomTargetPersistsChangedProtocol(t *testing.T) {
	t.Parallel()

	target := readmodel.TargetReadModel{ID: "primary", Model: "glm-5.2", Provider: "openai_compatible", ProviderProtocol: "responses_stream", BaseURL: "https://api.example.test/v1"}
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
