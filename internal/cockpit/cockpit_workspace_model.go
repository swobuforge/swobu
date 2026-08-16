package cockpit

import "github.com/swobuforge/swobu/internal/cockpit/readmodel"

func workspaceForTab(model readmodel.CockpitReadModel, tab readmodel.WorkspaceTabReadModel) readmodel.WorkspaceReadModel {
	if tab.Kind == readmodel.WorkspaceTabBootstrap {
		workspace := readmodel.NewConventionalFirstWorkspace(
			model.SelectedWorkspace.WorkspaceURL,
			model.SelectedWorkspace.ProviderOptions,
		)
		workspace.ID = tab.ID
		workspace.Slug = tab.Slug
		if model.SelectedWorkspace.IsBootstrap() && model.SelectedWorkspace.ID == tab.ID {
			workspace = mergeWorkspaceProjection(model.SelectedWorkspace, workspace)
		}
		return workspace
	}
	if tab.Kind == readmodel.WorkspaceTabDraft {
		draft := readmodel.NewDraftWorkspace(model.SelectedWorkspace.ProviderOptions)
		draft.ID = tab.ID
		draft.Slug = tab.Slug
		if model.SelectedWorkspace.IsDraft() && model.SelectedWorkspace.ID == tab.ID {
			draft = mergeWorkspaceProjection(model.SelectedWorkspace, draft)
		}
		return draft
	}
	if tab.ID == model.SelectedWorkspaceID {
		return model.SelectedWorkspace
	}
	return readmodel.WorkspaceReadModel{
		ID:    tab.ID,
		Slug:  tab.Slug,
		State: readmodel.WorkspaceExisting,
	}
}

func selectWorkspace(model readmodel.CockpitReadModel, id readmodel.WorkspaceID) readmodel.CockpitReadModel {
	for i := range model.Tabs {
		selected := model.Tabs[i].ID == id
		model.Tabs[i].Selected = selected
		if selected {
			workspace := workspaceForTab(model, model.Tabs[i])
			model.SelectedWorkspaceID = model.Tabs[i].ID
			model.ActivePage = readmodel.CockpitWorkspacePage
			model.SelectedWorkspace = workspace
		}
	}
	return model
}

func updateWorkspaceInModel(model readmodel.CockpitReadModel, workspace readmodel.WorkspaceReadModel) readmodel.CockpitReadModel {
	replaced := false
	for i := range model.Tabs {
		if model.Tabs[i].ID != workspace.ID {
			continue
		}
		model.Tabs[i].Slug = workspace.Slug
		if workspace.IsPersisted() && model.Tabs[i].Kind == readmodel.WorkspaceTabBootstrap {
			model.Tabs[i].Kind = readmodel.WorkspaceTabExisting
			if _, ok := draftTabIndex(model.Tabs); !ok {
				model.Tabs = insertDraftBeforeHelp(model.Tabs)
			}
		}
		replaced = true
		break
	}
	if !replaced {
		model.Tabs = append(model.Tabs, readmodel.WorkspaceTabReadModel{
			ID:   workspace.ID,
			Slug: workspace.Slug,
			Kind: readmodel.WorkspaceTabExisting,
		})
	}
	model.SelectedWorkspaceID = workspace.ID
	model.SelectedWorkspace = workspace
	return model
}

func insertDraftBeforeHelp(tabs []readmodel.WorkspaceTabReadModel) []readmodel.WorkspaceTabReadModel {
	draft := readmodel.WorkspaceTabReadModel{ID: "+", Kind: readmodel.WorkspaceTabDraft}
	if helpIndex, ok := helpTabIndex(tabs); ok {
		tabs = append(tabs, readmodel.WorkspaceTabReadModel{})
		copy(tabs[helpIndex+1:], tabs[helpIndex:])
		tabs[helpIndex] = draft
		return tabs
	}
	return append(tabs, draft)
}

func removeWorkspaceFromModel(model readmodel.CockpitReadModel, deleted readmodel.WorkspaceID) readmodel.CockpitReadModel {
	projectionSource := model.SelectedWorkspace
	tabs := make([]readmodel.WorkspaceTabReadModel, 0, len(model.Tabs))
	for _, tab := range model.Tabs {
		if tab.ID != deleted {
			tab.Selected = false
			tabs = append(tabs, tab)
		}
	}
	model.Tabs = tabs
	for i := range model.Tabs {
		if model.Tabs[i].Kind != readmodel.WorkspaceTabExisting {
			continue
		}
		model.Tabs[i].Selected = true
		workspace := workspaceForTab(model, model.Tabs[i])
		model.SelectedWorkspaceID = model.Tabs[i].ID
		model.ActivePage = readmodel.CockpitWorkspacePage
		model.SelectedWorkspace = workspace
		return model
	}
	model.SelectedWorkspaceID = ""
	model.SelectedWorkspace = readmodel.WorkspaceReadModel{}
	model.ActivePage = readmodel.CockpitWorkspacePage
	if index, ok := bootstrapTabIndex(model.Tabs); ok {
		model.Tabs[index].Selected = true
		model.SelectedWorkspace = projectionSource
		workspace := workspaceForTab(model, model.Tabs[index])
		model.SelectedWorkspaceID = model.Tabs[index].ID
		model.SelectedWorkspace = workspace
		return model
	}
	workspace := readmodel.NewConventionalFirstWorkspace(
		derivedWorkspaceURL(readmodel.CockpitReadModel{
			SelectedWorkspace: projectionSource,
			HeaderRight:       model.HeaderRight,
		}, readmodel.ConventionalFirstWorkspaceSlug),
		projectionSource.ProviderOptions,
	)
	model.Tabs = []readmodel.WorkspaceTabReadModel{
		{ID: workspace.ID, Slug: workspace.Slug, Kind: readmodel.WorkspaceTabBootstrap, Selected: true},
		{ID: "?", Kind: readmodel.WorkspaceTabHelp},
	}
	model.SelectedWorkspaceID = workspace.ID
	model.SelectedWorkspace = workspace
	return model
}

func selectedTabIndex(tabs []readmodel.WorkspaceTabReadModel) int {
	for i, tab := range tabs {
		if tab.Selected {
			return i
		}
	}
	return 0
}

func helpTabIndex(tabs []readmodel.WorkspaceTabReadModel) (int, bool) {
	for i, tab := range tabs {
		if tab.Kind == readmodel.WorkspaceTabHelp {
			return i, true
		}
	}
	return 0, false
}

func draftTabIndex(tabs []readmodel.WorkspaceTabReadModel) (int, bool) {
	for i, tab := range tabs {
		if tab.Kind == readmodel.WorkspaceTabDraft {
			return i, true
		}
	}
	return 0, false
}

func bootstrapTabIndex(tabs []readmodel.WorkspaceTabReadModel) (int, bool) {
	for i, tab := range tabs {
		if tab.Kind == readmodel.WorkspaceTabBootstrap {
			return i, true
		}
	}
	return 0, false
}

func wrapTabIndex(index int, tabCount int) int {
	if tabCount <= 0 {
		return 0
	}
	index %= tabCount
	if index < 0 {
		index += tabCount
	}
	return index
}
