package effect

import (
	"context"
	"fmt"
	"strings"

	outboundcredentials "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

// SaveSelectedTargetEffect saves the selected provider config ref for an endpoint.
type SaveSelectedTargetEffect struct {
	EndpointName string
	ProviderRef  string
}

func (cmd SaveSelectedTargetEffect) Execute(ctx context.Context) []update.Action {
	c := operatorClient()
	ep, err := c.Get(ctx, cmd.EndpointName)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	ref, err := endpointintent.ParseProviderConfigRef(cmd.ProviderRef)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: err.Error()}}
	}
	newEp, err := endpointintent.NewEndpoint(ep.Name(), ep.ProviderConfigs(), ref)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: err.Error()}}
	}
	if _, err := c.Put(ctx, newEp); err != nil {
		return []update.Action{RoutingSaveFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	return []update.Action{RoutingSaveSucceeded(cmd)}
}

// SaveProviderConfigEffect saves a provider config mutation for an endpoint.
type SaveProviderConfigEffect struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
}

func (cmd SaveProviderConfigEffect) Execute(ctx context.Context) []update.Action {
	c := operatorClient()
	ep, err := c.Get(ctx, cmd.EndpointName)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	pc, err := argsToProviderConfig(cmd.ProviderConfig)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: err.Error()}}
	}
	configs := ep.ProviderConfigs()
	for i := range configs {
		if configs[i].Ref() == pc.Ref() {
			configs[i] = pc
			break
		}
	}
	newEp, err := endpointintent.NewEndpoint(ep.Name(), configs, ep.SelectedProviderConfigRef())
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: err.Error()}}
	}
	if _, err := c.Put(ctx, newEp); err != nil {
		return []update.Action{RoutingSaveFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	return []update.Action{RoutingMutationSaved{}}
}

// AddProviderConfigEffect appends a new provider config and makes it primary.
type AddProviderConfigEffect struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
}

func (cmd AddProviderConfigEffect) Execute(ctx context.Context) []update.Action {
	c := operatorClient()
	ep, err := c.Get(ctx, cmd.EndpointName)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	pc, err := argsToProviderConfig(cmd.ProviderConfig)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: err.Error()}}
	}
	configs := ep.ProviderConfigs()
	for i := range configs {
		if configs[i].Ref() == pc.Ref() {
			return []update.Action{RoutingSaveFailed{Message: fmt.Sprintf("provider config %q already exists", pc.Ref().String())}}
		}
	}
	configs = append(configs, pc)
	newEp, err := endpointintent.NewEndpoint(ep.Name(), configs, pc.Ref())
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: err.Error()}}
	}
	if _, err := c.Put(ctx, newEp); err != nil {
		return []update.Action{RoutingSaveFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	return []update.Action{RoutingMutationSaved{}}
}

// DeleteProviderConfigEffect deletes one provider config while preserving
// endpoint invariants.
type DeleteProviderConfigEffect struct {
	EndpointName string
	ProviderRef  string
}

func (cmd DeleteProviderConfigEffect) Execute(ctx context.Context) []update.Action {
	c := operatorClient()
	ep, err := c.Get(ctx, cmd.EndpointName)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	removeRef := strings.TrimSpace(cmd.ProviderRef)
	configs := ep.ProviderConfigs()
	if len(configs) <= 1 {
		return []update.Action{RoutingSaveFailed{Message: "at least one model is required"}}
	}
	next := make([]endpointintent.ProviderConfig, 0, len(configs)-1)
	removed := false
	for _, cfg := range configs {
		if strings.TrimSpace(cfg.Ref().String()) == removeRef {
			removed = true
			continue
		}
		next = append(next, cfg)
	}
	if !removed {
		return []update.Action{RoutingSaveFailed{Message: "model not found"}}
	}
	selectedRef := ep.SelectedProviderConfigRef()
	if strings.TrimSpace(selectedRef.String()) == removeRef {
		selectedRef = next[0].Ref()
	}
	newEp, err := endpointintent.NewEndpoint(ep.Name(), next, selectedRef)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: err.Error()}}
	}
	if _, err := c.Put(ctx, newEp); err != nil {
		return []update.Action{RoutingSaveFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	return []update.Action{RoutingMutationSaved{}}
}

// StoreKeychainCredentialEffect persists a keychain secret for provider-scoped use.
type StoreKeychainCredentialEffect struct {
	ProviderSpec string
	KeyName      string
	Secret       string
}

func (cmd StoreKeychainCredentialEffect) Execute(ctx context.Context) []update.Action {
	_ = ctx
	providerSpec := strings.TrimSpace(cmd.ProviderSpec)
	keyName := strings.TrimSpace(cmd.KeyName)
	secret := strings.TrimSpace(cmd.Secret)
	if providerSpec == "" || keyName == "" || secret == "" {
		return []update.Action{RoutingSaveFailed{Message: "provider, key name, and key value are required"}}
	}
	if err := outboundcredentials.StoreKeychainCredential(providerSpec, keyName, secret); err != nil {
		return []update.Action{RoutingSaveFailed{Message: err.Error()}}
	}
	return []update.Action{KeychainCredentialStored{ProviderSpec: providerSpec, KeyName: keyName}}
}

// RoutingSaveFailed reports that a routing save operation failed.
type RoutingSaveFailed struct{ Message string }

// RoutingSaveSucceeded reports that a routing save operation succeeded.
type RoutingSaveSucceeded struct {
	EndpointName string
	ProviderRef  string
}

// RoutingMutationSaved reports that a provider config mutation was saved.
type RoutingMutationSaved struct{}

// KeychainCredentialStored reports that keychain credential write succeeded.
type KeychainCredentialStored struct {
	ProviderSpec string
	KeyName      string
}
