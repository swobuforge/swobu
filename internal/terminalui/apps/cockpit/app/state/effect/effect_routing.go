package effect

import (
	"context"
	"fmt"
	"strings"
	"time"

	outboundcredentials "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

const authSessionStartTimeout = 20 * time.Second
const authSessionPollTimeout = 10 * time.Second

// SaveSelectedTargetEffect saves the selected provider config ref for an endpoint.
type SaveSelectedTargetEffect struct {
	EndpointName string
	ProviderRef  string
	ErrorAnchor  string
}

func (cmd SaveSelectedTargetEffect) Execute(ctx context.Context) []update.Action {
	c := operatorClientWithTimeout(authSessionStartTimeout)
	ep, err := c.Get(ctx, cmd.EndpointName)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: normalizeOperatorSurfaceError(err), ErrorAnchor: cmd.ErrorAnchor}}
	}
	ref, err := endpointintent.ParseProviderConfigRef(cmd.ProviderRef)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: err.Error(), ErrorAnchor: cmd.ErrorAnchor}}
	}
	newEp, err := endpointintent.NewEndpoint(ep.Name(), ep.ProviderConfigs(), ref)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: err.Error(), ErrorAnchor: cmd.ErrorAnchor}}
	}
	if _, err := c.Put(ctx, newEp); err != nil {
		return []update.Action{RoutingSaveFailed{Message: normalizeOperatorSurfaceError(err), ErrorAnchor: cmd.ErrorAnchor}}
	}
	return []update.Action{RoutingSaveSucceeded{
		EndpointName: cmd.EndpointName,
		ProviderRef:  cmd.ProviderRef,
	}}
}

// SaveProviderConfigEffect saves a provider config mutation for an endpoint.
type SaveProviderConfigEffect struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
	ErrorAnchor    string
}

func (cmd SaveProviderConfigEffect) Execute(ctx context.Context) []update.Action {
	c := operatorClientWithTimeout(authSessionPollTimeout)
	ep, err := c.Get(ctx, cmd.EndpointName)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: normalizeOperatorSurfaceError(err), ErrorAnchor: cmd.ErrorAnchor}}
	}
	pc, err := argsToProviderConfig(cmd.ProviderConfig)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: err.Error(), ErrorAnchor: cmd.ErrorAnchor}}
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
		return []update.Action{RoutingSaveFailed{Message: err.Error(), ErrorAnchor: cmd.ErrorAnchor}}
	}
	if _, err := c.Put(ctx, newEp); err != nil {
		return []update.Action{RoutingSaveFailed{Message: normalizeOperatorSurfaceError(err), ErrorAnchor: cmd.ErrorAnchor}}
	}
	return []update.Action{RoutingMutationSaved{}}
}

// AddProviderConfigEffect appends a new provider config and makes it primary.
type AddProviderConfigEffect struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
	ErrorAnchor    string
}

func (cmd AddProviderConfigEffect) Execute(ctx context.Context) []update.Action {
	c := operatorClient()
	ep, err := c.Get(ctx, cmd.EndpointName)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: normalizeOperatorSurfaceError(err), ErrorAnchor: cmd.ErrorAnchor}}
	}
	allocatedRef, err := endpointintent.NewOpaqueProviderConfigRef(ep.ProviderConfigs())
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: err.Error(), ErrorAnchor: cmd.ErrorAnchor}}
	}
	draft := cmd.ProviderConfig
	draft.Ref = allocatedRef.String()
	pc, err := argsToProviderConfig(draft)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: err.Error(), ErrorAnchor: cmd.ErrorAnchor}}
	}
	configs := ep.ProviderConfigs()
	for i := range configs {
		if configs[i].Ref() == pc.Ref() {
			return []update.Action{RoutingSaveFailed{Message: fmt.Sprintf("provider config %q already exists", pc.Ref().String()), ErrorAnchor: cmd.ErrorAnchor}}
		}
	}
	configs = append(configs, pc)
	newEp, err := endpointintent.NewEndpoint(ep.Name(), configs, pc.Ref())
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: err.Error(), ErrorAnchor: cmd.ErrorAnchor}}
	}
	if _, err := c.Put(ctx, newEp); err != nil {
		return []update.Action{RoutingSaveFailed{Message: normalizeOperatorSurfaceError(err), ErrorAnchor: cmd.ErrorAnchor}}
	}
	return []update.Action{
		RoutingMutationSaved{},
		ProviderConfigAddedSaved{
			EndpointName:   cmd.EndpointName,
			ProviderConfig: draft,
		},
	}
}

// DeleteProviderConfigEffect deletes one provider config while preserving
// endpoint invariants.
type DeleteProviderConfigEffect struct {
	EndpointName string
	ProviderRef  string
	ErrorAnchor  string
}

func (cmd DeleteProviderConfigEffect) Execute(ctx context.Context) []update.Action {
	c := operatorClient()
	ep, err := c.Get(ctx, cmd.EndpointName)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: normalizeOperatorSurfaceError(err), ErrorAnchor: cmd.ErrorAnchor}}
	}
	removeRef := strings.TrimSpace(cmd.ProviderRef) // swobu:io-string source=boundary
	configs := ep.ProviderConfigs()
	if len(configs) <= 1 {
		return []update.Action{RoutingSaveFailed{Message: "at least one model is required", ErrorAnchor: cmd.ErrorAnchor}}
	}
	next := make([]endpointintent.ProviderConfig, 0, len(configs)-1)
	removed := false
	for _, cfg := range configs {
		if strings.TrimSpace(cfg.Ref().String()) == removeRef { // swobu:io-string source=boundary
			removed = true
			continue
		}
		next = append(next, cfg)
	}
	if !removed {
		return []update.Action{RoutingSaveFailed{Message: "model not found", ErrorAnchor: cmd.ErrorAnchor}}
	}
	selectedRef := ep.SelectedProviderConfigRef()
	if strings.TrimSpace(selectedRef.String()) == removeRef { // swobu:io-string source=boundary
		selectedRef = next[0].Ref()
	}
	newEp, err := endpointintent.NewEndpoint(ep.Name(), next, selectedRef)
	if err != nil {
		return []update.Action{RoutingSaveFailed{Message: err.Error(), ErrorAnchor: cmd.ErrorAnchor}}
	}
	if _, err := c.Put(ctx, newEp); err != nil {
		return []update.Action{RoutingSaveFailed{Message: normalizeOperatorSurfaceError(err), ErrorAnchor: cmd.ErrorAnchor}}
	}
	return []update.Action{RoutingMutationSaved{}}
}

// StoreKeychainCredentialEffect persists a keychain secret for provider-scoped use.
type StoreKeychainCredentialEffect struct {
	ProviderSpec string
	KeyName      string
	Secret       string
	ErrorAnchor  string
}

func (cmd StoreKeychainCredentialEffect) Execute(ctx context.Context) []update.Action {
	_ = ctx
	providerSpec := strings.TrimSpace(cmd.ProviderSpec) // swobu:io-string source=boundary
	keyName := strings.TrimSpace(cmd.KeyName)           // swobu:io-string source=boundary
	secret := strings.TrimSpace(cmd.Secret)             // swobu:io-string source=boundary
	if providerSpec == "" || keyName == "" || secret == "" {
		return []update.Action{RoutingSaveFailed{Message: "provider, key name, and key value are required", ErrorAnchor: cmd.ErrorAnchor}}
	}
	if err := outboundcredentials.StoreKeychainCredential(providerSpec, keyName, secret); err != nil {
		return []update.Action{RoutingSaveFailed{Message: err.Error(), ErrorAnchor: cmd.ErrorAnchor}}
	}
	return []update.Action{KeychainCredentialStored{ProviderSpec: providerSpec, KeyName: keyName}}
}

// RoutingSaveFailed reports that a routing save operation failed.
type RoutingSaveFailed struct {
	Message     string
	ErrorAnchor string
}

// RoutingSaveSucceeded reports that a routing save operation succeeded.
type RoutingSaveSucceeded struct {
	EndpointName string
	ProviderRef  string
}

// RoutingMutationSaved reports that a provider config mutation was saved.
type RoutingMutationSaved struct{}

// ProviderConfigAddedSaved reports that add-model mutation committed.
type ProviderConfigAddedSaved struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
}

// KeychainCredentialStored reports that keychain credential write succeeded.
type KeychainCredentialStored struct {
	ProviderSpec string
	KeyName      string
}

// StartProviderAuthSessionEffect starts provider login flow and polls for a
// resolved credential reference.
type StartProviderAuthSessionEffect struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
	OwnerKey       string
	AuthScope      string
}

func (eff StartProviderAuthSessionEffect) Execute(ctx context.Context) []update.Action {
	endpointName := strings.TrimSpace(eff.EndpointName)                // swobu:io-string source=boundary
	providerSpec := strings.TrimSpace(eff.ProviderConfig.ProviderSpec) // swobu:io-string source=boundary
	ownerKey := strings.TrimSpace(eff.OwnerKey)                        // swobu:io-string source=boundary
	authScope := strings.TrimSpace(eff.AuthScope)                      // swobu:io-string source=boundary
	if providerSpec == "" {
		return []update.Action{ProviderAuthSessionFailedAction{
			EndpointName:   endpointName,
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       ownerKey,
			AuthScope:      authScope,
			Message:        "provider is required for login",
		}}
	}
	if authScope == "" {
		return []update.Action{ProviderAuthSessionFailedAction{
			EndpointName:   endpointName,
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       ownerKey,
			AuthScope:      authScope,
			Message:        "auth scope is required for login",
		}}
	}
	if authScope == stateModel.AuthScopeEndpointProvider && endpointName == "" {
		return []update.Action{ProviderAuthSessionFailedAction{
			EndpointName:   endpointName,
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       ownerKey,
			AuthScope:      authScope,
			Message:        "endpoint is required for provider login",
		}}
	}
	c := operatorClient()
	authSubject, err := authSubjectForOwnerKey(ownerKey, authScope)
	if err != nil {
		return []update.Action{ProviderAuthSessionFailedAction{
			EndpointName:   endpointName,
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       ownerKey,
			AuthScope:      authScope,
			Message:        err.Error(),
		}}
	}
	start, err := c.StartAuthSession(
		ctx,
		providerSpec,
		authSubject,
		authModeForCredentialRef(strings.TrimSpace(eff.ProviderConfig.CredentialRef)), // swobu:io-string source=boundary
	)
	if err != nil {
		return []update.Action{ProviderAuthSessionFailedAction{
			EndpointName:   endpointName,
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       ownerKey,
			AuthScope:      authScope,
			Message:        normalizeAuthSessionSurfaceError(err),
		}}
	}
	sessionID := strings.TrimSpace(start.SessionID) // swobu:io-string source=boundary
	return []update.Action{
		ProviderAuthSessionStarted{
			EndpointName:   endpointName,
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       ownerKey,
			AuthScope:      authScope,
			SessionID:      sessionID,
			AuthorizeURL:   strings.TrimSpace(start.AuthorizeURL), // swobu:io-string source=boundary
			UserCode:       strings.TrimSpace(start.UserCode),     // swobu:io-string source=boundary
			State:          "pending",
		},
		PollProviderAuthSessionRequestedAction{
			EndpointName:   endpointName,
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       ownerKey,
			AuthScope:      authScope,
			SessionID:      sessionID,
			AttemptsLeft:   120,
		},
	}
}

func authSubjectForOwnerKey(ownerKey string, authScope string) (string, error) {
	key := stateModel.AuthOwnerKey(strings.TrimSpace(ownerKey)) // swobu:io-string source=boundary
	providerRef := key.ProviderRef()
	endpointName := key.EndpointName()
	if providerRef == "" {
		return "", fmt.Errorf("auth owner key is missing provider ref")
	}
	switch strings.TrimSpace(authScope) { // swobu:io-string source=boundary
	case stateModel.AuthScopeCreateDraft:
		if !key.IsCreateDraft() {
			return "", fmt.Errorf("auth owner key must be create-draft scoped")
		}
		return stateModel.EncodeAuthTransientSubjectLocator("", providerRef), nil
	case stateModel.AuthScopeEndpointProvider:
		if key.IsAddModelDraft() {
			return stateModel.EncodeAuthTransientSubjectLocator(endpointName, providerRef), nil
		}
		if key.IsEndpointProvider() {
			if endpointName == "" {
				return "", fmt.Errorf("auth owner key is missing endpoint name")
			}
			return stateModel.EncodeAuthEndpointProviderLocator(endpointName, providerRef), nil
		}
		return "", fmt.Errorf("auth owner key prefix is incompatible with endpoint-provider scope")
	default:
		return "", fmt.Errorf("auth scope is required for login")
	}
}
