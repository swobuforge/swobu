package cockpit

import (
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// DefaultFixtureReadModel returns fixed Cockpit data for the first visual
// fixture. It is intentionally adapter-free so layout proof cannot depend on
// daemon or operator-client state.
func DefaultFixtureReadModel() readmodel.CockpitReadModel {
	dev := readmodel.WorkspaceID("dev")
	return readmodel.CockpitReadModel{
		Tabs: []readmodel.WorkspaceTabReadModel{
			{ID: dev, Slug: "dev", Kind: readmodel.WorkspaceTabExisting, Selected: true},
			{ID: "lab", Slug: "lab", Kind: readmodel.WorkspaceTabExisting},
			{ID: "+", Kind: readmodel.WorkspaceTabDraft},
			{ID: "?", Kind: readmodel.WorkspaceTabHelp},
		},
		SelectedWorkspaceID: dev,
		Help: readmodel.HelpReadModel{
			Version:        "swobu dev",
			CockpitVersion: "cockpit v2",
			Commit:         "",
			DocsURL:        "swobu.com/docs",
			CommunityURL:   "https://discord.gg/swobu",
			IssueURL:       "https://github.com/swobuforge/swobu/issues/new",
		},
		SelectedWorkspace: readmodel.WorkspaceReadModel{
			ID:            dev,
			Slug:          "dev",
			State:         readmodel.WorkspaceExisting,
			ClientBaseURL: "http://127.0.0.1:7926/c/dev",
			RunCommands: []readmodel.RunCommandReadModel{
				{
					ID:          "codex",
					ClientID:    "codex",
					Label:       "Codex",
					CommandName: "codex",
					Effect:      readmodel.RunCommandOpensClient,
				},
			},
			Routes: []readmodel.RouteReadModel{
				{
					ID:        "gpt",
					ModelName: "gpt",
					State:     readmodel.RouteNormal,
					Default:   true,
					Enabled:   true,
					Targets: []readmodel.TargetReadModel{
						{
							ID:            "target-1",
							Name:          "target-1",
							Provider:      "openai",
							Model:         "gpt-4.1",
							BaseURL:       "https://api.openai.com/v1",
							CredentialRef: "default-key",
							Rank:          1,
							Weight:        1,
						},
						{
							ID:            "target-2",
							Name:          "target-2",
							Provider:      "anthropic",
							Model:         "claude-sonnet",
							BaseURL:       "https://api.anthropic.com/v1",
							CredentialRef: "default-key",
							Rank:          2,
							Weight:        1,
						},
					},
				},
				{
					ID:        "local",
					ModelName: "local",
					State:     readmodel.RouteNormal,
					Enabled:   true,
					Targets: []readmodel.TargetReadModel{
						{
							ID:            "local-1",
							Name:          "local-1",
							Provider:      "openai_compatible",
							Model:         "llama3.2",
							BaseURL:       "http://127.0.0.1:11434/v1",
							CredentialRef: "local-key",
							Rank:          1,
							Weight:        1,
						},
					},
				},
			},
		},
	}
}

func TestDefaultFixtureReadModel_ModelRoutes(t *testing.T) {
	model := DefaultFixtureReadModel()
	if got, want := len(model.SelectedWorkspace.Routes), 2; got != want {
		t.Fatalf("route count = %d, want %d", got, want)
	}

	gpt := model.SelectedWorkspace.Routes[0]
	if got, want := gpt.ID, readmodel.RouteID("gpt"); got != want {
		t.Fatalf("route id = %q, want %q", got, want)
	}
	if got, want := len(gpt.Targets), 2; got != want {
		t.Fatalf("target count = %d, want %d", got, want)
	}
	if got, want := gpt.Targets[0].Provider, "openai"; got != want {
		t.Fatalf("first target provider = %q, want %q", got, want)
	}
	if got, want := gpt.Targets[0].Model, "gpt-4.1"; got != want {
		t.Fatalf("first target model = %q, want %q", got, want)
	}
	if got, want := gpt.Targets[1].Provider, "anthropic"; got != want {
		t.Fatalf("second target provider = %q, want %q", got, want)
	}
	if got, want := gpt.Targets[1].Model, "claude-sonnet"; got != want {
		t.Fatalf("second target model = %q, want %q", got, want)
	}
}
