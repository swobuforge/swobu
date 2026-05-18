package effect

import (
	"context"
	"fmt"
	"strings"
	"time"

	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
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

func (eff PollProviderAuthSessionEffect) Execute(ctx context.Context) []update.Action {
	sessionID := strings.TrimSpace(eff.SessionID) // swobu:io-string source=boundary
	if sessionID == "" {
		return []update.Action{ProviderAuthSessionFailedAction{
			EndpointName:   strings.TrimSpace(eff.EndpointName), // swobu:io-string source=boundary
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       strings.TrimSpace(eff.OwnerKey),  // swobu:io-string source=boundary
			AuthScope:      strings.TrimSpace(eff.AuthScope), // swobu:io-string source=boundary
			Message:        "login session id is required",
		}}
	}
	c := operatorClient()
	status, err := c.GetAuthSessionStatus(ctx, sessionID)
	if err != nil {
		return []update.Action{ProviderAuthSessionFailedAction{
			EndpointName:   strings.TrimSpace(eff.EndpointName), // swobu:io-string source=boundary
			ProviderConfig: eff.ProviderConfig,
			OwnerKey:       strings.TrimSpace(eff.OwnerKey),  // swobu:io-string source=boundary
			AuthScope:      strings.TrimSpace(eff.AuthScope), // swobu:io-string source=boundary
			Message:        normalizeAuthSessionSurfaceError(err),
		}}
	}
	stateValue := strings.ToLower(strings.TrimSpace(status.State)) // swobu:io-string source=boundary
	actions := []update.Action{ProviderAuthSessionPolledAction{
		EndpointName:   strings.TrimSpace(eff.EndpointName), // swobu:io-string source=boundary
		ProviderConfig: eff.ProviderConfig,
		OwnerKey:       strings.TrimSpace(eff.OwnerKey),  // swobu:io-string source=boundary
		AuthScope:      strings.TrimSpace(eff.AuthScope), // swobu:io-string source=boundary
		SessionID:      sessionID,
		State:          stateValue,
		ErrorMessage:   strings.TrimSpace(status.ErrorMessage), // swobu:io-string source=boundary
	}}
	switch stateValue {
	case "succeeded":
		credentialRef := strings.TrimSpace(status.CredentialRef) // swobu:io-string source=boundary
		if credentialRef == "" {
			return append(actions, ProviderAuthSessionFailedAction{EndpointName: strings.TrimSpace(eff.EndpointName), ProviderConfig: eff.ProviderConfig, OwnerKey: strings.TrimSpace(eff.OwnerKey), AuthScope: strings.TrimSpace(eff.AuthScope), Message: "login completed without credential reference"})
		}
		return append(actions, ProviderAuthSessionCredentialResolvedAction{EndpointName: strings.TrimSpace(eff.EndpointName), ProviderConfig: eff.ProviderConfig, OwnerKey: strings.TrimSpace(eff.OwnerKey), AuthScope: strings.TrimSpace(eff.AuthScope), CredentialRef: credentialRef})
	case "failed", "expired", "canceled":
		msg := strings.TrimSpace(status.ErrorMessage) // swobu:io-string source=boundary
		if msg == "" {
			msg = fmt.Sprintf("%s login %s", strings.TrimSpace(status.ProviderSpec), stateValue) // swobu:io-string source=boundary
		}
		return append(actions, ProviderAuthSessionFailedAction{EndpointName: strings.TrimSpace(eff.EndpointName), ProviderConfig: eff.ProviderConfig, OwnerKey: strings.TrimSpace(eff.OwnerKey), AuthScope: strings.TrimSpace(eff.AuthScope), Message: msg})
	default:
		if eff.AttemptsLeft <= 1 {
			return append(actions, ProviderAuthSessionFailedAction{EndpointName: strings.TrimSpace(eff.EndpointName), ProviderConfig: eff.ProviderConfig, OwnerKey: strings.TrimSpace(eff.OwnerKey), AuthScope: strings.TrimSpace(eff.AuthScope), Message: "login timed out; retry"})
		}
		timer := time.NewTimer(1 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		return append(actions, PollProviderAuthSessionRequestedAction{EndpointName: strings.TrimSpace(eff.EndpointName), ProviderConfig: eff.ProviderConfig, OwnerKey: strings.TrimSpace(eff.OwnerKey), AuthScope: strings.TrimSpace(eff.AuthScope), SessionID: sessionID, AttemptsLeft: eff.AttemptsLeft - 1})
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
