package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
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
		profiles := bedrockDiscoveredAWSProfiles()
		profile := trimRoutingInput(bedrockProfileFromCredentialRef(pc.CredentialRef))
		return bedrockProfilePickerRow(ctx, bedrockProfilePickerRowSpec{
			Summary:   bedrockProfileSummary(profile),
			Current:   profile,
			Profiles:  profiles,
			CloseMode: state.InteractionModeManageList,
			FocusKey:  "profile",
			OnSave: func(value string) []update.Action {
				ref := encodeBedrockProfileCredentialRef(value)
				if strings.TrimSpace(ref) == "" { // swobu:io-string source=boundary
					return nil
				}
				if spec.CreateMode {
					next := model.CreateDraftProviderConfig
					next.CredentialRef = ref
					baseURL := effectiveDraftBaseURL(next)
					return []update.Action{
						state.SetCreateDraftCredentialRef{CredentialRef: ref},
						state.SetCreateDraftModelIDAction{ModelID: ""},
						state.LoadRoutingModelCatalogRequestedAction{
							Scope:            state.RoutingModelCatalogScopeCreateDraft,
							ProviderSpec:     "bedrock",
							ProviderProtocol: trimRoutingInput(next.ProviderProtocol),
							BaseURL:          baseURL,
							CredentialRef:    ref,
						},
					}
				}
				if spec.ProviderConfig == nil || trimRoutingInput(spec.EndpointName) == "" {
					return nil
				}
				next := *spec.ProviderConfig
				next.CredentialRef = ref
				return routingSaveProviderConfigActions(trimRoutingInput(spec.EndpointName), next, "provider/bedrock/profile")
			},
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
		focusKey := "region"
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
							Scope:            state.RoutingModelCatalogScopeCreateDraft,
							ProviderSpec:     "bedrock",
							ProviderProtocol: trimRoutingInput(pc.ProviderProtocol),
							BaseURL:          baseURL,
							CredentialRef:    credentialRef,
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
	profiles := bedrockDiscoveredAWSProfiles()
	profile := trimRoutingInput(bedrockProfileFromCredentialRef(draft.CredentialRef))
	return bedrockProfilePickerRow(ctx, bedrockProfilePickerRowSpec{
		Summary:   bedrockProfileSummary(profile),
		Current:   profile,
		Profiles:  profiles,
		CloseMode: state.InteractionModeManageList,
		FocusKey:  "add-model/profile",
		OnSave: func(value string) []update.Action {
			next := draft
			next.CredentialRef = encodeBedrockProfileCredentialRef(value)
			next.ModelID = ""
			panel.setDraft(next)
			return []update.Action{
				state.LoadRoutingModelCatalogRequestedAction{
					Scope:            state.RoutingModelCatalogScopeAddModelDraft,
					ProviderSpec:     trimRoutingInput(next.ProviderSpec),
					ProviderProtocol: trimRoutingInput(next.ProviderProtocol),
					BaseURL:          trimRoutingInput(next.BaseURL),
					CredentialRef:    trimRoutingInput(next.CredentialRef),
				},
				state.SetInteractionMode{Mode: state.InteractionModeManageList},
				interaction.FocusKeyAction{Key: "add-model/profile"},
			}
		},
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
					Scope:            state.RoutingModelCatalogScopeAddModelDraft,
					ProviderSpec:     trimRoutingInput(next.ProviderSpec),
					ProviderProtocol: trimRoutingInput(next.ProviderProtocol),
					BaseURL:          trimRoutingInput(next.BaseURL),
					CredentialRef:    trimRoutingInput(next.CredentialRef),
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
					Scope:            state.RoutingModelCatalogScopeAddModelDraft,
					ProviderSpec:     trimRoutingInput(next.ProviderSpec),
					ProviderProtocol: trimRoutingInput(next.ProviderProtocol),
					BaseURL:          trimRoutingInput(next.BaseURL),
					CredentialRef:    trimRoutingInput(next.CredentialRef),
				},
				state.SetInteractionMode{Mode: state.InteractionModeManageList},
				interaction.FocusKeyAction{Key: "add-model/env-key"},
			}
		},
	)
}
