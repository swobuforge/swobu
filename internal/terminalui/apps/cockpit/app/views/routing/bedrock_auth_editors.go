package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

type bedrockAuthProfileEditorSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

func bedrockAuthProfileEditor(spec bedrockAuthProfileEditorSpec) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		model := ctx.Model()
		pc := selectedProvider(model, spec.ProviderConfig, spec.CreateMode)
		if pc == nil || !strings.EqualFold(trimRoutingInput(pc.ProviderSpec), "bedrock") {
			return nil
		}
		if !isBedrockAWSProfileCredentialRef(pc.CredentialRef) {
			return nil
		}
		profile := trimRoutingInput(bedrockProfileFromCredentialRef(pc.CredentialRef))
		return backendURLEditorRow(ctx, "profile", selectorsEmptyOr(profile, "default"), profile, "work-prod", func(value string) []update.Action {
			ref := encodeBedrockProfileCredentialRef(value)
			if spec.CreateMode {
				baseURL := trimRoutingInput(model.CreateDraftProviderConfig.BaseURL)
				if baseURL == "" {
					baseURL = trimRoutingInput(pc.BaseURL)
				}
				return []update.Action{
					state.SetCreateDraftCredentialRef{CredentialRef: ref},
					state.SetCreateDraftModelIDAction{ModelID: ""},
					state.LoadRoutingModelCatalogRequestedAction{
						Scope:         state.RoutingModelCatalogScopeCreateDraft,
						ProviderSpec:  "bedrock",
						BaseURL:       baseURL,
						CredentialRef: ref,
					},
				}
			}
			if spec.ProviderConfig == nil || trimRoutingInput(spec.EndpointName) == "" {
				return nil
			}
			next := *spec.ProviderConfig
			next.CredentialRef = ref
			return routingSaveProviderConfigActions(trimRoutingInput(spec.EndpointName), next, "provider/bedrock/profile")
		})
	})
}

type bedrockAuthRegionEditorSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

func bedrockAuthRegionEditor(spec bedrockAuthRegionEditorSpec) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		model := ctx.Model()
		pc := selectedProvider(model, spec.ProviderConfig, spec.CreateMode)
		if pc == nil || !strings.EqualFold(trimRoutingInput(pc.ProviderSpec), "bedrock") {
			return nil
		}
		region := trimRoutingInput(bedrockResolvedRegion(pc.Region, pc.BaseURL))
		if region == "" && !spec.CreateMode {
			region = bedrockDefaultRegion
		}
		summary := region
		if summary == "" {
			summary = "region missing"
		}
		closeMode := state.InteractionModeManageList
		focusKey := "scope"
		if spec.CreateMode {
			closeMode = state.InteractionModeNAV
		}
		return bedrockRegionPickerRow(ctx, bedrockRegionPickerRowSpec{
			Label:      "region",
			Summary:    summary,
			Current:    region,
			CloseMode:  closeMode,
			FocusKey:   focusKey,
			EditorHint: "eu-west-2",
			OnSave: func(value string) []update.Action {
				nextRegion := trimRoutingInput(value)
				if nextRegion == "" && !spec.CreateMode {
					nextRegion = bedrockDefaultRegion
				}
				baseURL := ""
				if nextRegion != "" {
					baseURL = bedrockBaseURLForRegion(nextRegion)
				}
				if spec.CreateMode {
					credentialRef := trimRoutingInput(model.CreateDraftProviderConfig.CredentialRef)
					if credentialRef == "" {
						credentialRef = trimRoutingInput(pc.CredentialRef)
					}
					return []update.Action{
						state.SetCreateDraftBaseURL{BaseURL: baseURL},
						state.SetCreateDraftModelIDAction{ModelID: ""},
						state.LoadRoutingModelCatalogRequestedAction{
							Scope:         state.RoutingModelCatalogScopeCreateDraft,
							ProviderSpec:  "bedrock",
							BaseURL:       baseURL,
							CredentialRef: credentialRef,
						},
					}
				}
				if spec.ProviderConfig == nil || trimRoutingInput(spec.EndpointName) == "" {
					return nil
				}
				next := *spec.ProviderConfig
				next.Region = nextRegion
				next.BaseURL = baseURL
				return routingSaveProviderConfigActions(trimRoutingInput(spec.EndpointName), next, "provider/bedrock/region")
			},
		})
	})
}

func addModelBedrockAuthProfileEditor(ctx *retained.Context[state.Model], draft state.ProviderConfigSnapshot, panel addModelPanelState) retained.ViewSpec[state.Model] {
	if !strings.EqualFold(trimRoutingInput(draft.ProviderSpec), "bedrock") {
		return nil
	}
	if !isBedrockAWSProfileCredentialRef(draft.CredentialRef) {
		return nil
	}
	profile := trimRoutingInput(bedrockProfileFromCredentialRef(draft.CredentialRef))
	return backendURLEditorRow(ctx, "profile", selectorsEmptyOr(profile, "default"), profile, "work-prod", func(value string) []update.Action {
		next := draft
		next.CredentialRef = encodeBedrockProfileCredentialRef(value)
		next.ModelID = ""
		panel.setDraft(next)
		return []update.Action{
			state.LoadRoutingModelCatalogRequestedAction{
				Scope:         state.RoutingModelCatalogScopeAddModelDraft,
				ProviderSpec:  trimRoutingInput(next.ProviderSpec),
				BaseURL:       trimRoutingInput(next.BaseURL),
				CredentialRef: trimRoutingInput(next.CredentialRef),
			},
			state.SetInteractionMode{Mode: state.InteractionModeManageList},
			interaction.FocusKeyAction{Key: "add-model/profile"},
		}
	})
}

func addModelBedrockAuthRegionEditor(ctx *retained.Context[state.Model], draft state.ProviderConfigSnapshot, panel addModelPanelState) retained.ViewSpec[state.Model] {
	if !strings.EqualFold(trimRoutingInput(draft.ProviderSpec), "bedrock") {
		return nil
	}
	region := trimRoutingInput(bedrockResolvedRegion(draft.Region, draft.BaseURL))
	if region == "" {
		region = bedrockDefaultRegion
	}
	return bedrockRegionPickerRow(ctx, bedrockRegionPickerRowSpec{
		Label:      "region",
		Summary:    region,
		Current:    region,
		CloseMode:  state.InteractionModeManageList,
		FocusKey:   "add-model/region",
		EditorHint: "eu-west-2",
		OnSave: func(value string) []update.Action {
			nextRegion := trimRoutingInput(value)
			if nextRegion == "" {
				nextRegion = bedrockDefaultRegion
			}
			next := draft
			next.Region = nextRegion
			next.BaseURL = bedrockBaseURLForRegion(nextRegion)
			next.ModelID = ""
			panel.setDraft(next)
			return []update.Action{
				state.LoadRoutingModelCatalogRequestedAction{
					Scope:         state.RoutingModelCatalogScopeAddModelDraft,
					ProviderSpec:  trimRoutingInput(next.ProviderSpec),
					BaseURL:       trimRoutingInput(next.BaseURL),
					CredentialRef: trimRoutingInput(next.CredentialRef),
				},
				state.SetInteractionMode{Mode: state.InteractionModeManageList},
				interaction.FocusKeyAction{Key: "add-model/region"},
			}
		},
	})
}

func addModelBedrockAuthEnvEditor(ctx *retained.Context[state.Model], draft state.ProviderConfigSnapshot, panel addModelPanelState) retained.ViewSpec[state.Model] {
	if !strings.EqualFold(trimRoutingInput(draft.ProviderSpec), "bedrock") {
		return nil
	}
	current := trimRoutingInput(envCredentialKey(draft.CredentialRef)) // swobu:io-string source=boundary
	if current == "" {
		current = "AWS_BEARER_TOKEN_BEDROCK"
	}
	return backendURLEditorRow(
		ctx,
		"env",
		current,
		current,
		"AWS_BEARER_TOKEN_BEDROCK",
		func(value string) []update.Action {
			next := draft
			next.CredentialRef = encodeCredentialEnvRef(value)
			next.ModelID = ""
			panel.setDraft(next)
			return []update.Action{
				state.LoadRoutingModelCatalogRequestedAction{
					Scope:         state.RoutingModelCatalogScopeAddModelDraft,
					ProviderSpec:  trimRoutingInput(next.ProviderSpec),
					BaseURL:       trimRoutingInput(next.BaseURL),
					CredentialRef: trimRoutingInput(next.CredentialRef),
				},
				state.SetInteractionMode{Mode: state.InteractionModeManageList},
				interaction.FocusKeyAction{Key: "add-model/env-key"},
			}
		},
	)
}

func selectorsEmptyOr(value, fallback string) string {
	value = trimRoutingInput(value)
	if value == "" {
		return fallback
	}
	return value
}

type bedrockRegionPickerRowSpec struct {
	Label      string
	Summary    string
	Current    string
	CloseMode  string
	FocusKey   string
	EditorHint string
	OnSave     func(string) []update.Action
}

func bedrockRegionPickerRow(ctx *retained.Context[state.Model], spec bedrockRegionPickerRowSpec) retained.ViewSpec[state.Model] {
	regions := bedrockRegions()
	if len(regions) == 0 {
		return backendURLEditorRow(ctx, spec.Label, spec.Summary, spec.Current, spec.EditorHint, spec.OnSave)
	}
	open, setOpen := retained.UseState(ctx, func() bool { return false })
	picker, setPicker := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	parent := views.RowChoiceWithCancel(spec.Label, spec.Summary, func() []update.Action {
		nextOpen := !open
		setOpen(nextOpen)
		if nextOpen {
			views.ResetFilterablePickerState(setPicker)
			return []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModePickOne},
				interaction.FocusKeyAction{Key: views.FilterablePickerFocusKey("bedrock-region-option", 0)},
			}
		}
		return []update.Action{
			state.SetInteractionMode{Mode: spec.CloseMode},
			interaction.FocusKeyAction{Key: spec.FocusKey},
		}
	}, func() []update.Action {
		if !open {
			return nil
		}
		setOpen(false)
		return []update.Action{
			state.SetInteractionMode{Mode: spec.CloseMode},
			interaction.FocusKeyAction{Key: spec.FocusKey},
		}
	})
	if !open {
		return parent
	}
	items := make([]views.FilterablePickerItem, 0, len(regions))
	for _, region := range regions {
		choice := trimRoutingInput(region)
		if choice == "" {
			continue
		}
		items = append(items, views.FilterablePickerItem{
			Label:    choice,
			Search:   choice,
			Selected: strings.EqualFold(choice, trimRoutingInput(spec.Current)),
			OnChoose: func() []update.Action {
				setOpen(false)
				actions := spec.OnSave(choice)
				return append(actions,
					state.SetInteractionMode{Mode: spec.CloseMode},
					interaction.FocusKeyAction{Key: spec.FocusKey},
				)
			},
		})
	}
	if len(items) == 0 {
		return backendURLEditorRow(ctx, spec.Label, spec.Summary, spec.Current, spec.EditorHint, spec.OnSave)
	}
	return views.RenderFilterablePickerDisclosure(ctx, parent, picker, setPicker, items, views.FilterablePickerConfig{
		KeyPrefix:      "bedrock-region-option",
		BuildOptionRow: views.ChoicePickerOptionRow(false),
		WindowSize:     8,
		FindLabel:      "find",
		ShowSelected:   true,
		OnNoMatchFocus: func() []update.Action {
			return []update.Action{interaction.FocusKeyAction{Key: spec.FocusKey}}
		},
		OnCancel: func() []update.Action {
			setOpen(false)
			return []update.Action{
				state.SetInteractionMode{Mode: spec.CloseMode},
				interaction.FocusKeyAction{Key: spec.FocusKey},
			}
		},
	})
}
