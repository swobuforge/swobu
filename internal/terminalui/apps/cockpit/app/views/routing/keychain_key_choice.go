package routing

import (
	"strings"

	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/view"
)

// providerKeychainKeyNameRowSpec owns keychain key-name selection when keychain-backed credentials are selected.
type providerKeychainKeyNameRowSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

func providerKeychainKeyNameRow(spec providerKeychainKeyNameRowSpec) view.ViewSpec[state.Model] {
	return view.Build[state.Model](func(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
		return buildProviderKeychainKeyNameRow(ctx, spec)
	})
}

func buildProviderKeychainKeyNameRow(ctx *view.Context[state.Model], spec providerKeychainKeyNameRowSpec) view.ViewSpec[state.Model] {
	model := ctx.Model()
	pc := selectedProvider(model, spec.ProviderConfig, spec.CreateMode)
	if pc == nil || !strings.EqualFold(credentialSource(pc.CredentialRef), "keychain") {
		return nil
	}
	currentName := keychainCredentialName(pc.CredentialRef)
	effectiveName := keychainEffectiveName(pc.ProviderSpec, currentName)
	keyNameRow := backendURLEditorRow(
		ctx,
		"key slot",
		effectiveName,
		effectiveName,
		"provider/default",
		func(value string) []update.Action {
			return applyProviderKeychainKeyNameSelection(value, spec.ProviderConfig, spec.EndpointName, spec.CreateMode)
		},
	)
	keyValueRow := backendURLEditorRow(
		ctx,
		"key value",
		keychainValueSummary(model, pc.ProviderSpec, effectiveName),
		"",
		"paste key value",
		func(value string) []update.Action {
			return []update.Action{state.StoreKeychainCredentialRequested{
				ProviderSpec: strings.TrimSpace(pc.ProviderSpec),
				KeyName:      effectiveName,
				Secret:       strings.TrimSpace(value),
			}}
		},
	)
	return view.VStack(ctx, keyNameRow, keyValueRow)
}

func keychainValueSummary(model state.Model, providerSpec string, keySlot string) string {
	if strings.EqualFold(model.LastStoredKeyProviderSpec, strings.TrimSpace(providerSpec)) &&
		model.LastStoredKeySlotName == strings.TrimSpace(keySlot) {
		return "stored"
	}
	return "missing"
}

func keychainEffectiveName(providerSpec string, current string) string {
	name := strings.TrimSpace(current)
	if name != "" {
		return name
	}
	return defaultKeychainKeyName(providerSpec)
}

func defaultKeychainKeyName(providerSpec string) string {
	spec := strings.TrimSpace(strings.ToLower(providerSpec))
	if spec == "" {
		return "default"
	}
	return spec + "/default"
}

func applyProviderKeychainKeyNameSelection(keyName string, providerConfig *state.ProviderConfigSnapshot, endpointName string, createMode bool) []update.Action {
	ref := encodeCredentialKeychainRef(keyName)
	if createMode {
		return []update.Action{state.SetCreateDraftCredentialRef{CredentialRef: ref}}
	}
	if providerConfig == nil || strings.TrimSpace(endpointName) == "" {
		return nil
	}
	next := *providerConfig
	next.CredentialRef = ref
	return []update.Action{
		state.RoutingSaveStartedAction{},
		state.SaveProviderConfigRequested{
			EndpointName:   strings.TrimSpace(endpointName),
			ProviderConfig: next,
		},
	}
}
