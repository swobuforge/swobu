package cockpit

import "github.com/swobuforge/swobu/internal/cockpit/readmodel"

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
		EnvironmentLabel:    "local",
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
					PlanKind:  readmodel.RoutePlanRanked,
					Default:   true,
					Enabled:   true,
					Targets: []readmodel.TargetReadModel{
						{
							ID:            "target-1",
							Name:          "target-1",
							Provider:      "openai_compatible",
							Model:         "gpt-4.1",
							BaseURL:       "https://api.openai.com/v1",
							CredentialRef: "default-key",
							Rank:          1,
							Weight:        1,
						},
						{
							ID:            "target-2",
							Name:          "target-2",
							Provider:      "openai_compatible",
							Model:         "gpt-4o",
							BaseURL:       "https://api.openai.com/v1",
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
					PlanKind:  readmodel.RoutePlanSingle,
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
