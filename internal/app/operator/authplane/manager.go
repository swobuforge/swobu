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
	store  CredentialStore

	mu       sync.RWMutex
	sessions map[string]StartInput
}

func NewAuthSessionManager(driver AuthMethodDriver, store CredentialStore) (*AuthSessionManager, error) {
	if driver == nil {
		return nil, fmt.Errorf("authplane driver is required")
	}
	return &AuthSessionManager{
		driver:   driver,
		store:    store,
		sessions: map[string]StartInput{},
	}, nil
}

func (m *AuthSessionManager) Start(ctx context.Context, in StartInput) (StartOutput, error) {
	provider := strings.TrimSpace(strings.ToLower(in.ProviderSpec)) // swobu:io-string source=boundary
	slog.Debug("auth session start requested",
		"component", "authplane",
		"provider_spec", provider,
		"has_endpoint_ref", strings.TrimSpace(in.EndpointRef) != "", // swobu:io-string source=boundary
		"auth_mode", strings.TrimSpace(in.AuthMode), // swobu:io-string source=boundary
	)
	if provider == "" {
		return StartOutput{}, fmt.Errorf("provider spec is required")
	}
	endpointRef := strings.TrimSpace(in.EndpointRef) // swobu:io-string source=boundary
	if endpointRef == "" {
		return StartOutput{}, fmt.Errorf("endpoint ref is required")
	}
	out, err := m.driver.Start(ctx, StartInput{
		ProviderSpec: provider,
		EndpointRef:  endpointRef,
		AuthMode:     strings.TrimSpace(in.AuthMode), // swobu:io-string source=boundary
	})
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
	m.sessions[sessionID] = StartInput{
		ProviderSpec: provider,
		EndpointRef:  endpointRef,
		AuthMode:     strings.TrimSpace(in.AuthMode), // swobu:io-string source=boundary
	}
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
		if m.store != nil {
			persistedRef, err := m.store.UpsertCredentialRef(ctx, input.ProviderSpec, input.EndpointRef, strings.TrimSpace(pollOut.CredentialRef)) // swobu:io-string source=boundary
			if err != nil {
				slog.Warn("auth session credential ref persistence failed",
					"component", "authplane",
					"session_id", sessionID,
					"provider_spec", input.ProviderSpec,
					"error", err.Error(),
				)
				return SessionOutput{}, err
			}
			if strings.TrimSpace(persistedRef) != "" { // swobu:io-string source=boundary
				pollOut.CredentialRef = strings.TrimSpace(persistedRef) // swobu:io-string source=boundary
			}
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

func (m *AuthSessionManager) Retry(ctx context.Context, sessionID string) (StartOutput, error) {
	sessionID = strings.TrimSpace(sessionID) // swobu:io-string source=boundary
	slog.Debug("auth session retry requested",
		"component", "authplane",
		"session_id", sessionID,
	)
	if sessionID == "" {
		return StartOutput{}, fmt.Errorf("session id is required")
	}
	input, ok := m.lookupSession(sessionID)
	if !ok {
		return StartOutput{}, fmt.Errorf("auth session is unknown")
	}
	return m.Start(ctx, input)
}

func (m *AuthSessionManager) lookupSession(sessionID string) (StartInput, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	in, ok := m.sessions[sessionID]
	return in, ok
}
