package effect

import (
	"context"
	"fmt"
	"regexp"
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
	removeRef := strings.TrimSpace(cmd.ProviderRef) // trimlowerlint:allow boundary canonicalization
	configs := ep.ProviderConfigs()
	if len(configs) <= 1 {
		return []update.Action{RoutingSaveFailed{Message: "at least one model is required", ErrorAnchor: cmd.ErrorAnchor}}
	}
	next := make([]endpointintent.ProviderConfig, 0, len(configs)-1)
	removed := false
	for _, cfg := range configs {
		if strings.TrimSpace(cfg.Ref().String()) == removeRef { // trimlowerlint:allow boundary canonicalization
			removed = true
			continue
		}
		next = append(next, cfg)
	}
	if !removed {
		return []update.Action{RoutingSaveFailed{Message: "model not found", ErrorAnchor: cmd.ErrorAnchor}}
	}
	selectedRef := ep.SelectedProviderConfigRef()
	if strings.TrimSpace(selectedRef.String()) == removeRef { // trimlowerlint:allow boundary canonicalization
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
	providerSpec := strings.TrimSpace(cmd.ProviderSpec) // trimlowerlint:allow boundary canonicalization
	keyName := strings.TrimSpace(cmd.KeyName)           // trimlowerlint:allow boundary canonicalization
	secret := strings.TrimSpace(cmd.Secret)             // trimlowerlint:allow boundary canonicalization
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
	endpointName := strings.TrimSpace(eff.EndpointName)                // trimlowerlint:allow boundary canonicalization
	providerSpec := strings.TrimSpace(eff.ProviderConfig.ProviderSpec) // trimlowerlint:allow boundary canonicalization
	ownerKey := strings.TrimSpace(eff.OwnerKey)                        // trimlowerlint:allow boundary canonicalization
	authScope := strings.TrimSpace(eff.AuthScope)                      // trimlowerlint:allow boundary canonicalization
	if providerSpec == "" {
		return []update.Action{ProviderAuthSessionFailed{
			EndpointName:   endpointName,
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       ownerKey,
			AuthScope:      authScope,
			Message:        "provider is required for login",
		}}
	}
	if authScope == "" {
		return []update.Action{ProviderAuthSessionFailed{
			EndpointName:   endpointName,
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       ownerKey,
			AuthScope:      authScope,
			Message:        "auth scope is required for login",
		}}
	}
	if authScope == stateModel.AuthScopeEndpointProvider && endpointName == "" {
		return []update.Action{ProviderAuthSessionFailed{
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
		return []update.Action{ProviderAuthSessionFailed{
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
		authModeForCredentialRef(strings.TrimSpace(eff.ProviderConfig.CredentialRef)), // trimlowerlint:allow boundary canonicalization
	)
	if err != nil {
		return []update.Action{ProviderAuthSessionFailed{
			EndpointName:   endpointName,
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       ownerKey,
			AuthScope:      authScope,
			Message:        normalizeAuthSessionSurfaceError(err),
		}}
	}
	sessionID := strings.TrimSpace(start.SessionID) // trimlowerlint:allow boundary canonicalization
	return []update.Action{
		ProviderAuthSessionStarted{
			EndpointName:   endpointName,
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       ownerKey,
			AuthScope:      authScope,
			SessionID:      sessionID,
			AuthorizeURL:   strings.TrimSpace(start.AuthorizeURL), // trimlowerlint:allow boundary canonicalization
			UserCode:       strings.TrimSpace(start.UserCode),     // trimlowerlint:allow boundary canonicalization
			State:          "pending",
		},
		PollProviderAuthSessionRequested{
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
	key := stateModel.AuthOwnerKey(strings.TrimSpace(ownerKey)) // trimlowerlint:allow boundary canonicalization
	providerRef := key.ProviderRef()
	endpointName := key.EndpointName()
	if providerRef == "" {
		return "", fmt.Errorf("auth owner key is missing provider ref")
	}
	switch strings.TrimSpace(authScope) { // trimlowerlint:allow boundary canonicalization
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

type PollProviderAuthSessionRequested struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
	OwnerKey       string
	AuthScope      string
	SessionID      string
	AttemptsLeft   int
}

type ProviderAuthSessionStarted struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
	OwnerKey       string
	AuthScope      string
	SessionID      string
	AuthorizeURL   string
	UserCode       string
	State          string
}

type PollProviderAuthSessionEffect struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
	OwnerKey       string
	AuthScope      string
	SessionID      string
	AttemptsLeft   int
}

func (eff PollProviderAuthSessionEffect) Execute(ctx context.Context) []update.Action {
	sessionID := strings.TrimSpace(eff.SessionID) // trimlowerlint:allow boundary canonicalization
	if sessionID == "" {
		return []update.Action{ProviderAuthSessionFailed{
			EndpointName:   strings.TrimSpace(eff.EndpointName), // trimlowerlint:allow boundary canonicalization
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       strings.TrimSpace(eff.OwnerKey),  // trimlowerlint:allow boundary canonicalization
			AuthScope:      strings.TrimSpace(eff.AuthScope), // trimlowerlint:allow boundary canonicalization
			Message:        "login session id is required",
		}}
	}
	c := operatorClient()
	status, err := c.GetAuthSessionStatus(ctx, sessionID)
	if err != nil {
		return []update.Action{ProviderAuthSessionFailed{
			EndpointName:   strings.TrimSpace(eff.EndpointName), // trimlowerlint:allow boundary canonicalization
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       strings.TrimSpace(eff.OwnerKey),  // trimlowerlint:allow boundary canonicalization
			AuthScope:      strings.TrimSpace(eff.AuthScope), // trimlowerlint:allow boundary canonicalization
			Message:        normalizeAuthSessionSurfaceError(err),
		}}
	}
	stateValue := strings.ToLower(strings.TrimSpace(status.State)) // trimlowerlint:allow boundary canonicalization
	actions := []update.Action{ProviderAuthSessionPolled{
		EndpointName:   strings.TrimSpace(eff.EndpointName), // trimlowerlint:allow boundary canonicalization
		ProviderConfig: eff.ProviderConfig,
		OwnerKey:       strings.TrimSpace(eff.OwnerKey),  // trimlowerlint:allow boundary canonicalization
		AuthScope:      strings.TrimSpace(eff.AuthScope), // trimlowerlint:allow boundary canonicalization
		SessionID:      sessionID,
		State:          stateValue,
		ErrorMessage:   strings.TrimSpace(status.ErrorMessage), // trimlowerlint:allow boundary canonicalization
	}}
	switch stateValue {
	case "succeeded":
		credentialRef := strings.TrimSpace(status.CredentialRef) // trimlowerlint:allow boundary canonicalization
		if credentialRef == "" {
			return append(actions, ProviderAuthSessionFailed{
				EndpointName:   strings.TrimSpace(eff.EndpointName), // trimlowerlint:allow boundary canonicalization
				ProviderConfig: eff.ProviderConfig,
				OwnerKey:       strings.TrimSpace(eff.OwnerKey),  // trimlowerlint:allow boundary canonicalization
				AuthScope:      strings.TrimSpace(eff.AuthScope), // trimlowerlint:allow boundary canonicalization
				Message:        "login completed without credential reference",
			})
		}
		return append(actions, ProviderAuthSessionCredentialResolved{
			EndpointName:   strings.TrimSpace(eff.EndpointName), // trimlowerlint:allow boundary canonicalization
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       strings.TrimSpace(eff.OwnerKey),  // trimlowerlint:allow boundary canonicalization
			AuthScope:      strings.TrimSpace(eff.AuthScope), // trimlowerlint:allow boundary canonicalization
			CredentialRef:  credentialRef,
		})
	case "failed", "expired", "canceled":
		msg := strings.TrimSpace(status.ErrorMessage) // trimlowerlint:allow boundary canonicalization
		if msg == "" {
			msg = fmt.Sprintf("%s login %s", strings.TrimSpace(status.ProviderSpec), stateValue) // trimlowerlint:allow boundary canonicalization
		}
		return append(actions, ProviderAuthSessionFailed{
			EndpointName:   strings.TrimSpace(eff.EndpointName), // trimlowerlint:allow boundary canonicalization
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       strings.TrimSpace(eff.OwnerKey),  // trimlowerlint:allow boundary canonicalization
			AuthScope:      strings.TrimSpace(eff.AuthScope), // trimlowerlint:allow boundary canonicalization
			Message:        msg,
		})
	default:
		if eff.AttemptsLeft <= 1 {
			return append(actions, ProviderAuthSessionFailed{
				EndpointName:   strings.TrimSpace(eff.EndpointName), // trimlowerlint:allow boundary canonicalization
				ProviderConfig: eff.ProviderConfig,
				OwnerKey:       strings.TrimSpace(eff.OwnerKey),  // trimlowerlint:allow boundary canonicalization
				AuthScope:      strings.TrimSpace(eff.AuthScope), // trimlowerlint:allow boundary canonicalization
				Message:        "login timed out; retry",
			})
		}
		timer := time.NewTimer(1 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		return append(actions, PollProviderAuthSessionRequested{
			EndpointName:   strings.TrimSpace(eff.EndpointName), // trimlowerlint:allow boundary canonicalization
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       strings.TrimSpace(eff.OwnerKey),  // trimlowerlint:allow boundary canonicalization
			AuthScope:      strings.TrimSpace(eff.AuthScope), // trimlowerlint:allow boundary canonicalization
			SessionID:      sessionID,
			AttemptsLeft:   eff.AttemptsLeft - 1,
		})
	}
}

type ProviderAuthSessionCredentialResolved struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
	OwnerKey       string
	AuthScope      string
	CredentialRef  string
}

type ProviderAuthSessionPolled struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
	OwnerKey       string
	AuthScope      string
	SessionID      string
	State          string
	ErrorMessage   string
}

type ProviderAuthSessionFailed struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
	OwnerKey       string
	AuthScope      string
	Message        string
}

func normalizeAuthSessionSurfaceError(err error) string {
	raw := strings.TrimSpace(err.Error()) // trimlowerlint:allow boundary canonicalization
	lower := strings.ToLower(raw)         // trimlowerlint:allow boundary canonicalization
	// Preserve backend-auth specific failures instead of collapsing into daemon
	// unavailable hints.
	if strings.Contains(lower, "auth session") && strings.Contains(lower, "code=") {
		return sanitizeAuthSessionErrorMessage(strings.TrimSpace(strings.TrimPrefix(raw, "operator client:"))) // trimlowerlint:allow boundary canonicalization
	}
	return sanitizeAuthSessionErrorMessage(normalizeOperatorSurfaceError(err))
}

var (
	authReturnedStatusPattern = regexp.MustCompile(`(?i)returned status\s+(\d{3})`)
	authCodePattern           = regexp.MustCompile(`(?i)\(code=([A-Z_]+)\)`)
)

func sanitizeAuthSessionErrorMessage(message string) string {
	trimmed := strings.TrimSpace(message) // trimlowerlint:allow boundary canonicalization
	if trimmed == "" {
		return trimmed
	}
	lower := strings.ToLower(trimmed) // trimlowerlint:allow boundary canonicalization
	if strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype html") {
		status := ""
		if match := authReturnedStatusPattern.FindStringSubmatch(trimmed); len(match) == 2 {
			status = strings.TrimSpace(match[1]) // trimlowerlint:allow boundary canonicalization
		}
		code := ""
		if match := authCodePattern.FindStringSubmatch(trimmed); len(match) == 2 {
			code = strings.TrimSpace(match[1]) // trimlowerlint:allow boundary canonicalization
		}
		summary := "auth start failed: upstream returned an HTML challenge page"
		if status != "" {
			summary = fmt.Sprintf("auth start failed: upstream returned status %s with an HTML challenge page", status)
		}
		if code != "" {
			summary = summary + " (code=" + code + ")"
		}
		return summary
	}
	if len(trimmed) > 240 {
		return strings.TrimSpace(trimmed[:240]) + "…" // trimlowerlint:allow boundary canonicalization
	}
	return trimmed
}

func authModeForCredentialRef(ref string) string {
	if strings.EqualFold(strings.TrimSpace(ref), "chatgpt_device_auth") { // trimlowerlint:allow boundary canonicalization
		return "device"
	}
	return "browser"
}
