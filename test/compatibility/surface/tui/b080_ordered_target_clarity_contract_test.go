package tui_test

import (
	"strings"
	"testing"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/test/e2e/harness"
)

func TestB080_OrderedTargetClarity_ProviderDisclosureIsDeterministicAndShowsSelectedTarget(t *testing.T) {
	upstream := newChatCompletionsUpstream(t, 200, "")
	defer upstream.Close()

	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(
				t,
				"jobs",
				"backend-b",
				harness.NewProviderConfig(t, "backend-a", "openai", "https://api.openai.com/v1", "cred-openai", protocolsurface.ChatCompletions),
				harness.NewProviderConfig(t, "backend-b", "custom", upstream.URL+"/v1", "", protocolsurface.ChatCompletions),
			),
		},
	})

	journey := startJourneyWithDaemonAndWorkspaceRail(t, daemon.BaseURL, 160, 50, "[› jobs]")
	journey.WaitVisible("/c/jobs/")
	journey.FocusRowDown("routing")
	journey.ActivateFocusedRow()
	journey.WaitVisible("models")
	journey.FocusRowDown("models")
	journey.ActivateFocusedRow()
	journey.WaitVisible("OpenAI")
	journey.WaitVisible("Custom")

	visible := journey.VisibleOutput()
	disclosureStart := strings.Index(visible, "routing")
	if disclosureStart < 0 {
		t.Fatalf("provider disclosure missing routing row: %q", visible)
	}
	disclosure := visible[disclosureStart:]
	idxOpenAI := strings.Index(disclosure, "OpenAI")
	idxCustom := strings.Index(disclosure, "Custom                                                      close ↵")
	if idxOpenAI < 0 || idxCustom < 0 {
		t.Fatalf("provider disclosure missing ordered targets: %q", visible)
	}
	if idxOpenAI > idxCustom {
		t.Fatalf("provider disclosure order drift: OpenAI index %d after Custom index %d; visible=%q", idxOpenAI, idxCustom, visible)
	}
}
