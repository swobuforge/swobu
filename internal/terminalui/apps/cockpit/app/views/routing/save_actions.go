package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func routingSaveSelectedTargetActions(endpointName, providerRef, rowKey string) []update.Action {
	return []update.Action{
		state.RoutingSaveStartedAction{},
		state.SaveSelectedTargetRequested{
			EndpointName: strings.TrimSpace(endpointName), // trimlowerlint:allow boundary canonicalization
			ProviderRef:  strings.TrimSpace(providerRef),  // trimlowerlint:allow boundary canonicalization
			ErrorAnchor:  views.ScopedErrorAnchor("routing", rowKey),
		},
	}
}

func routingSaveProviderConfigActions(endpointName string, providerConfig state.ProviderConfigSnapshot, rowKey string) []update.Action {
	return []update.Action{
		state.RoutingSaveStartedAction{},
		state.SaveProviderConfigRequested{
			EndpointName:   strings.TrimSpace(endpointName), // trimlowerlint:allow boundary canonicalization
			ProviderConfig: providerConfig,
			ErrorAnchor:    views.ScopedErrorAnchor("routing", rowKey),
		},
	}
}

func routingAddProviderConfigActions(endpointName string, providerConfig state.ProviderConfigSnapshot, rowKey string) []update.Action {
	return []update.Action{
		state.RoutingSaveStartedAction{},
		state.AddProviderConfigRequested{
			EndpointName:   strings.TrimSpace(endpointName), // trimlowerlint:allow boundary canonicalization
			ProviderConfig: providerConfig,
			ErrorAnchor:    views.ScopedErrorAnchor("routing", rowKey),
		},
	}
}

func routingDeleteProviderConfigActions(endpointName, providerRef, rowKey string) []update.Action {
	return []update.Action{
		state.RoutingSaveStartedAction{},
		state.DeleteProviderConfigRequested{
			EndpointName: strings.TrimSpace(endpointName), // trimlowerlint:allow boundary canonicalization
			ProviderRef:  strings.TrimSpace(providerRef),  // trimlowerlint:allow boundary canonicalization
			ErrorAnchor:  views.ScopedErrorAnchor("routing", rowKey),
		},
	}
}

func routingStoreKeychainCredentialActions(providerSpec, keyName, secret, rowKey string) []update.Action {
	return []update.Action{
		state.StoreKeychainCredentialRequested{
			ProviderSpec: strings.TrimSpace(providerSpec), // trimlowerlint:allow boundary canonicalization
			KeyName:      strings.TrimSpace(keyName),      // trimlowerlint:allow boundary canonicalization
			Secret:       strings.TrimSpace(secret),       // trimlowerlint:allow boundary canonicalization
			ErrorAnchor:  views.ScopedErrorAnchor("routing", rowKey),
		},
	}
}
