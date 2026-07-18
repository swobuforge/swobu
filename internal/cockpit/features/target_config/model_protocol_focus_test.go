package target_config

import (
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestModelSelectionAdvancesSelectionToRequiredProtocol(t *testing.T) {
	config := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	config.Draft.Set(readmodel.TargetDraft{
		ProviderSpec:  "openai",
		CredentialRef: "env:OPENAI_API_KEY",
	})
	config.Catalog.Set(readmodel.ModelCatalogReadModel{Deployments: []readmodel.ModelDeploymentReadModel{{
		ID:        "gpt-5.6",
		Name:      "gpt-5.6",
		ModelName: "gpt-5.6",
	}}})
	config.Open()

	h, err := testkit.NewHarness(config)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	if frame := h.Frame(); !strings.Contains(frame, "> model") {
		t.Fatalf("model row must own selection before opening picker:\n%s", frame)
	}

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	frame := h.Frame()
	if !strings.Contains(frame, "> protocol") {
		t.Fatalf("protocol row must own selection after model choice:\n%s", frame)
	}
	if got := strings.Count(frame, ">"); got != 1 {
		t.Fatalf("selection markers = %d, want exactly one on protocol:\n%s", got, frame)
	}
}
