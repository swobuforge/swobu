package effect

import (
	"context"
	"fmt"
	"strings"
	"time"

	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

type PollProviderAuthSessionRequestedAction struct {
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

// TODO(v2-effect-split): PollProviderAuthSessionEffect previously emitted 1-3
// actions; now it emits one.  The reducer that receives the result must
// schedule follow-up polls or terminal transitions itself.
func (eff PollProviderAuthSessionEffect) Run(ctx context.Context) any {
	sessionID := strings.TrimSpace(eff.SessionID) // swobu:io-string source=boundary
	if sessionID == "" {
		return ProviderAuthSessionFailedAction{
			EndpointName:   strings.TrimSpace(eff.EndpointName), // swobu:io-string source=boundary
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       strings.TrimSpace(eff.OwnerKey),  // swobu:io-string source=boundary
			AuthScope:      strings.TrimSpace(eff.AuthScope), // swobu:io-string source=boundary
			Message:        "login session id is required",
		}
	}
	c := operatorClient()
	status, err := c.GetAuthSessionStatus(ctx, sessionID)
	if err != nil {
		return ProviderAuthSessionFailedAction{
			EndpointName:   strings.TrimSpace(eff.EndpointName), // swobu:io-string source=boundary
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       strings.TrimSpace(eff.OwnerKey),  // swobu:io-string source=boundary
			AuthScope:      strings.TrimSpace(eff.AuthScope), // swobu:io-string source=boundary
			Message:        normalizeAuthSessionSurfaceError(err),
		}
	}
	stateValue := strings.ToLower(strings.TrimSpace(status.State)) // swobu:io-string source=boundary
	epName := strings.TrimSpace(eff.EndpointName)                  // swobu:io-string source=boundary
	ownerKey := strings.TrimSpace(eff.OwnerKey)                    // swobu:io-string source=boundary
	authScope := strings.TrimSpace(eff.AuthScope)                  // swobu:io-string source=boundary

	switch stateValue {
	case "succeeded":
		credentialRef := strings.TrimSpace(status.CredentialRef) // swobu:io-string source=boundary
		if credentialRef == "" {
			return ProviderAuthSessionFailedAction{
				EndpointName:   epName,
				ProviderConfig: eff.ProviderConfig,
				OwnerKey:       ownerKey,
				AuthScope:      authScope,
				Message:        "login completed without credential reference",
			}
		}
		return ProviderAuthSessionCredentialResolvedAction{
			EndpointName:   epName,
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       ownerKey,
			AuthScope:      authScope,
			CredentialRef:  credentialRef,
		}
	case "failed", "expired", "canceled":
		msg := strings.TrimSpace(status.ErrorMessage) // swobu:io-string source=boundary
		if msg == "" {
			msg = fmt.Sprintf("%s login %s", strings.TrimSpace(status.ProviderSpec), stateValue) // swobu:io-string source=boundary
		}
		return ProviderAuthSessionFailedAction{
			EndpointName:   epName,
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       ownerKey,
			AuthScope:      authScope,
			Message:        msg,
		}
	default:
		if eff.AttemptsLeft <= 1 {
			return ProviderAuthSessionFailedAction{
				EndpointName:   epName,
				ProviderConfig: eff.ProviderConfig,
				OwnerKey:       ownerKey,
				AuthScope:      authScope,
				Message:        "login timed out; retry",
			}
		}
		timer := time.NewTimer(1 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		return PollProviderAuthSessionRequestedAction{
			EndpointName:   epName,
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       ownerKey,
			AuthScope:      authScope,
			SessionID:      sessionID,
			AttemptsLeft:   eff.AttemptsLeft - 1,
		}
	}
}

type ProviderAuthSessionCredentialResolvedAction struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
	OwnerKey       string
	AuthScope      string
	CredentialRef  string
}

type ProviderAuthSessionPolledAction struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
	OwnerKey       string
	AuthScope      string
	SessionID      string
	State          string
	ErrorMessage   string
}

type ProviderAuthSessionFailedAction struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
	OwnerKey       string
	AuthScope      string
	Message        string
}
