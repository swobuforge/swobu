package tui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/test/e2e/harness"
)

func TestB074_EndpointAffordance_RailShowsTabsAndCreateEntry(t *testing.T) {
	upstream := newChatCompletionsUpstream(t, 200, "")
	defer upstream.Close()

	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(t, "acme", "backend-a", harness.NewProviderConfig(t, "backend-a", "custom", upstream.URL+"/v1", "", protocolsurface.ChatCompletions)),
			harness.NewEndpoint(t, "staging", "backend-b", harness.NewProviderConfig(t, "backend-b", "custom", upstream.URL+"/v1", "", protocolsurface.ChatCompletions)),
		},
	})

	journey := startJourneyWithDaemonAndWorkspaceRail(t, daemon.BaseURL, 160, 50, "[› acme]", "[› staging]")
	journey.AssertVisibleContains("[ + ]")

	waitForRailSelectionAfterTab(t, journey, "[› staging]")
	waitForRailSelectionAfterTab(t, journey, "[› +]")
}

func TestB074_EndpointAffordance_CanCreateSecondWorkspaceFromRailPlus(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-token")
	daemon := startDaemonWithOneCustomEndpoint(t, "acme")
	journey := startJourneyWithDaemonAndWorkspaceRail(t, daemon.BaseURL, 160, 50, "[› acme]")
	journey.SendKey("tab")
	journey.WaitVisibleAny("[› +]", "[ + ]")

	journey.FocusRowDown("name")
	journey.ActivateFocusedRow()
	journey.WaitVisibleAny("↵ save", "save ↵")
	journey.TypeText("beta")
	journey.SendKey("enter")
	journey.WaitVisible("beta")
	selectFirstRunProvider(t, journey)
	chooseFirstRunKeyAndModel(t, journey)
	journey.FocusRow("create")
	journey.ActivateFocusedRow()
	journey.WaitVisible("[› beta]")
}

func waitForRailSelectionAfterTab(t *testing.T, journey harness.OperatorPTYJourney, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		visible := journey.VisibleOutput()
		if strings.Contains(visible, want) {
			return
		}
		journey.SendKey("tab")
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("tab traversal did not reach %q; visible=%q", want, journey.VisibleOutput())
}

func TestB074_EndpointAffordance_DeleteWorkspaceActionWorks(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-token")
	daemon := startDaemonWithOneCustomEndpoint(t, "acme")
	journey := startJourneyWithDaemonAndWorkspaceRail(t, daemon.BaseURL, 160, 50, "acme")

	journey.WaitVisible("[› acme]")
	journey.FocusRowDown("delete workspace")
	journey.ActivateFocusedRow()
	journey.WaitVisible("[ + new workspace ]")
	journey.WaitVisible("choose a workspace name")
}
