package authplane

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

type AuthSessionManager struct {
	driver AuthMethodDriver

	mu       sync.RWMutex
	sessions map[string]StartInput
}

func NewAuthSessionManager(driver AuthMethodDriver) (*AuthSessionManager, error) {
	if driver == nil {
		return nil, fmt.Errorf("authplane driver is required")
	}
	return &AuthSessionManager{
		driver:   driver,
		sessions: map[string]StartInput{},
	}, nil
}

func (m *AuthSessionManager) Start(ctx context.Context, in StartInput) (StartOutput, error) {
	normalized, err := normalizeStartInput(in)
	if err != nil {
		return StartOutput{}, err
	}
	provider := normalized.ProviderSpec
	slog.Debug("auth session start requested",
		"component", "authplane",
		"provider_spec", provider,
		"has_target", strings.TrimSpace(in.TargetID) != "", // swobu:io-string source=boundary
		"auth_mode", strings.TrimSpace(in.AuthMode), // swobu:io-string source=boundary
	)
	out, err := m.driver.Start(ctx, normalized)
	if err != nil {
		slog.Warn("auth session driver start failed",
			"component", "authplane",
			"provider_spec", provider,
			"error", err.Error(),
		)
		return StartOutput{}, err
	}
	sessionID := strings.TrimSpace(out.SessionID) // swobu:io-string source=boundary
	if sessionID == "" {
		return StartOutput{}, fmt.Errorf("auth method driver returned empty session id")
	}
	m.mu.Lock()
	m.sessions[sessionID] = normalized
	m.mu.Unlock()
	slog.Debug("auth session started",
		"component", "authplane",
		"provider_spec", provider,
		"session_id", sessionID,
		"has_authorize_url", strings.TrimSpace(out.AuthorizeURL) != "", // swobu:io-string source=boundary
		"has_user_code", strings.TrimSpace(out.UserCode) != "", // swobu:io-string source=boundary
	)
	return StartOutput{
		SessionID:    sessionID,
		State:        SessionStatePending,
		AuthorizeURL: strings.TrimSpace(out.AuthorizeURL), // swobu:io-string source=boundary
		UserCode:     strings.TrimSpace(out.UserCode),     // swobu:io-string source=boundary
		ExpiresAt:    strings.TrimSpace(out.ExpiresAt),    // swobu:io-string source=boundary
	}, nil
}

// normalizeStartInput is shared by retained admission and driver dispatch so
// the marker cannot describe a different subject or mode from the actual effect.
func normalizeStartInput(in StartInput) (StartInput, error) {
	in = StartInput{ProviderSpec: strings.ToLower(strings.TrimSpace(in.ProviderSpec)), Workspace: strings.TrimSpace(in.Workspace), Route: strings.TrimSpace(in.Route), TargetID: strings.TrimSpace(in.TargetID), DraftSubject: strings.TrimSpace(in.DraftSubject), AuthMode: strings.TrimSpace(in.AuthMode)}
	if in.ProviderSpec == "" {
		return StartInput{}, fmt.Errorf("provider spec is required")
	}
	if in.DraftSubject == "" && (in.Workspace == "" || in.Route == "" || in.TargetID == "") {
		return StartInput{}, fmt.Errorf("workspace, route, and target ID are required")
	}
	return in, nil
}

func (m *AuthSessionManager) Poll(ctx context.Context, sessionID string) (SessionOutput, error) {
	sessionID = strings.TrimSpace(sessionID) // swobu:io-string source=boundary
	slog.Debug("auth session poll requested",
		"component", "authplane",
		"session_id", sessionID,
	)
	if sessionID == "" {
		return SessionOutput{}, fmt.Errorf("session id is required")
	}
	input, ok := m.lookupSession(sessionID)
	if !ok {
		return SessionOutput{}, fmt.Errorf("auth session is unknown")
	}
	pollOut, err := m.driver.Poll(ctx, sessionID)
	if err != nil {
		slog.Warn("auth session driver poll failed",
			"component", "authplane",
			"session_id", sessionID,
			"error", err.Error(),
		)
		return SessionOutput{}, err
	}
	if pollOut.State == SessionStateSucceeded {
		if strings.TrimSpace(pollOut.CredentialRef) == "" { // swobu:io-string source=boundary
			return SessionOutput{}, fmt.Errorf("auth session succeeded without credential ref")
		}
	}
	slog.Debug("auth session poll completed",
		"component", "authplane",
		"session_id", sessionID,
		"provider_spec", input.ProviderSpec,
		"state", string(pollOut.State),
		"has_credential_ref", strings.TrimSpace(pollOut.CredentialRef) != "", // swobu:io-string source=boundary
		"has_error_message", strings.TrimSpace(pollOut.ErrorMessage) != "", // swobu:io-string source=boundary
	)
	return SessionOutput{
		ProviderSpec:  input.ProviderSpec,
		SessionID:     sessionID,
		State:         pollOut.State,
		CredentialRef: strings.TrimSpace(pollOut.CredentialRef), // swobu:io-string source=boundary
		ErrorMessage:  strings.TrimSpace(pollOut.ErrorMessage),  // swobu:io-string source=boundary
	}, nil
}

func (m *AuthSessionManager) Cancel(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID) // swobu:io-string source=boundary
	slog.Debug("auth session cancel requested",
		"component", "authplane",
		"session_id", sessionID,
	)
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if _, ok := m.lookupSession(sessionID); !ok {
		return fmt.Errorf("auth session is unknown")
	}
	if err := m.driver.Cancel(ctx, sessionID); err != nil {
		slog.Warn("auth session cancel failed",
			"component", "authplane",
			"session_id", sessionID,
			"error", err.Error(),
		)
		return err
	}
	slog.Debug("auth session cancel completed",
		"component", "authplane",
		"session_id", sessionID,
	)
	return nil
}

func (m *AuthSessionManager) lookupSession(sessionID string) (StartInput, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	in, ok := m.sessions[sessionID]
	return in, ok
}
