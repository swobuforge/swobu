package help

import "github.com/swobuforge/swobu/internal/cockpit/readmodel"

// DefaultModel returns the support-orientation copy used when the live Cockpit
// model does not supply one.
func DefaultModel() readmodel.HelpReadModel {
	return readmodel.HelpReadModel{
		Version:        "swobu",
		CockpitVersion: "cockpit v2",
		DocsURL:        "swobu.com/docs",
		CommunityURL:   "https://discord.gg/swobu",
		IssueURL:       "https://github.com/swobuforge/swobu/issues/new",
	}
}
