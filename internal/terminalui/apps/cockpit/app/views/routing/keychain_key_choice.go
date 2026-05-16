package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// providerKeychainKeyNameRowSpec owns keychain key-name selection when keychain-backed credentials are selected.
type providerKeychainKeyNameRowSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

func providerKeychainKeyNameRow(spec providerKeychainKeyNameRowSpec) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return buildProviderKeychainKeyNameRow(ctx, spec)
	})
}

func buildProviderKeychainKeyNameRow(ctx *retained.Context[state.Model], spec providerKeychainKeyNameRowSpec) retained.ViewSpec[state.Model] {
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
			return routingStoreKeychainCredentialActions(strings.TrimSpace(pc.ProviderSpec), effectiveName, strings.TrimSpace(value), "provider/keychain") // trimlowerlint:allow boundary canonicalization
		},
	)
	return retained.VStack(ctx, keyNameRow, keyValueRow)
}

func keychainValueSummary(model state.Model, providerSpec string, keySlot string) string {
	if strings.EqualFold(model.LastStoredKeyProviderSpec, strings.TrimSpace(providerSpec)) && // trimlowerlint:allow boundary canonicalization
		model.LastStoredKeySlotName == strings.TrimSpace(keySlot) { // trimlowerlint:allow boundary canonicalization
		return "stored"
	}
	return "missing"
}

func keychainEffectiveName(providerSpec string, current string) string {
	name := strings.TrimSpace(current) // trimlowerlint:allow boundary canonicalization
	if name != "" {
		return name
	}
	return defaultKeychainKeyName(providerSpec)
}

func defaultKeychainKeyName(providerSpec string) string {
	spec := strings.TrimSpace(strings.ToLower(providerSpec)) // trimlowerlint:allow boundary canonicalization
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
	if providerConfig == nil || strings.TrimSpace(endpointName) == "" { // trimlowerlint:allow boundary canonicalization
		return nil
	}
	next := *providerConfig
	next.CredentialRef = ref
	return routingSaveProviderConfigActions(strings.TrimSpace(endpointName), next, "provider/auth") // trimlowerlint:allow boundary canonicalization
}
