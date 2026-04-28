package tui_test

import (
	"testing"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/test/e2e/harness"
)

func TestCockpitAcceptance_HeaderAndFooterUseNormativeVocabulary(t *testing.T) {
	journey := startJourney(t, 160, 50)
	journey.WaitVisible("Swobu")
	journey.AssertVisibleOmits("as you command")
	// First-run omits tab hints: there is nothing to cycle yet
	journey.AssertVisibleOmits("tab next")
	journey.AssertVisibleOmits("q quit")
	journey.AssertVisibleOmits("space toggle")
}

func TestCockpitAcceptance_CreateLaneDefaultPreviewIsDerivedEndpoint(t *testing.T) {
	journey := startJourney(t, 160, 50)
	journey.WaitVisible("[ + new workspace ]")
	journey.WaitVisible("/c/<slug>/")
	journey.AssertVisibleOmits("endpoint        invalid")
}

func TestCockpitAcceptance_TabCyclesWorkspaceRail(t *testing.T) {
	upstream := newChatCompletionsUpstream(t, 200, "")
	defer upstream.Close()

	daemon := harness.StartDaemonProcess(t, harness.DaemonProcessConfig{
		Endpoints: []endpointintent.Endpoint{
			harness.NewEndpoint(t, "acme", "backend-a", harness.NewProviderConfig(t, "backend-a", "custom", upstream.URL+"/v1", "", protocolsurface.ChatCompletions)),
			harness.NewEndpoint(t, "staging", "backend-b", harness.NewProviderConfig(t, "backend-b", "custom", upstream.URL+"/v1", "", protocolsurface.ChatCompletions)),
		},
	})

	journey := startJourneyWithDaemonAndWorkspaceRail(t, daemon.BaseURL, 160, 50, "[› acme]", "[› staging]")
	journey.SendKey("tab")
	journey.WaitVisible("[› staging]")
	journey.SendKey("shift+tab")
	journey.WaitVisible("[› acme]")
}
