package routing

import "strings"

type interactiveAuthStatusComponent struct {
	providerSpec  string
	credentialRef string
	sessionState  string
	sessionError  string
}

func newInteractiveAuthStatusComponent(providerSpec string, credentialRef string, sessionState string, sessionError string) interactiveAuthStatusComponent {
	return interactiveAuthStatusComponent{
		providerSpec:  strings.TrimSpace(providerSpec),  // swobu:io-string source=boundary
		credentialRef: strings.TrimSpace(credentialRef), // swobu:io-string source=boundary
		sessionState:  strings.TrimSpace(sessionState),  // swobu:io-string source=boundary
		sessionError:  strings.TrimSpace(sessionError),  // swobu:io-string source=boundary
	}
}

func (c interactiveAuthStatusComponent) Resolved() bool {
	return isResolvedInteractiveCredential(c.providerSpec, c.credentialRef)
}

func (c interactiveAuthStatusComponent) SignedInSummary() string {
	if c.Resolved() && strings.EqualFold(c.sessionState, "pending") {
		return "signed in (re-auth in progress)"
	}
	return "signed in"
}

func (c interactiveAuthStatusComponent) LoginSummary() string {
	if c.Resolved() {
		return c.SignedInSummary()
	}
	summary := "pending browser auth"
	if c.sessionState != "" {
		summary = "login " + c.sessionState
	}
	if c.sessionError != "" {
		summary = "login error"
	}
	return summary
}
