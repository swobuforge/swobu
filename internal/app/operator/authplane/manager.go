package authplane

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type Manager struct {
	driver AuthMethodDriver
	store  CredentialStore

	mu       sync.RWMutex
	sessions map[string]StartInput
}

func NewManager(driver AuthMethodDriver, store CredentialStore) (*Manager, error) {
	if driver == nil {
		return nil, fmt.Errorf("authplane driver is required")
	}
	return &Manager{
		driver:   driver,
		store:    store,
		sessions: map[string]StartInput{},
	}, nil
}

func (m *Manager) Start(ctx context.Context, in StartInput) (StartOutput, error) {
	provider := strings.TrimSpace(strings.ToLower(in.ProviderSpec))
	if provider == "" {
		return StartOutput{}, fmt.Errorf("provider spec is required")
	}
	endpointRef := strings.TrimSpace(in.EndpointRef)
	if endpointRef == "" {
		return StartOutput{}, fmt.Errorf("endpoint ref is required")
	}
	out, err := m.driver.Start(ctx, StartInput{
		ProviderSpec: provider,
		EndpointRef:  endpointRef,
		AuthMode:     strings.TrimSpace(in.AuthMode),
	})
	if err != nil {
		return StartOutput{}, err
	}
	sessionID := strings.TrimSpace(out.SessionID)
	if sessionID == "" {
		return StartOutput{}, fmt.Errorf("auth method driver returned empty session id")
	}
	m.mu.Lock()
	m.sessions[sessionID] = StartInput{
		ProviderSpec: provider,
		EndpointRef:  endpointRef,
		AuthMode:     strings.TrimSpace(in.AuthMode),
	}
	m.mu.Unlock()
	return StartOutput{
		SessionID:    sessionID,
		State:        SessionStatePending,
		AuthorizeURL: strings.TrimSpace(out.AuthorizeURL),
		UserCode:     strings.TrimSpace(out.UserCode),
	}, nil
}

func (m *Manager) Poll(ctx context.Context, sessionID string) (SessionOutput, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionOutput{}, fmt.Errorf("session id is required")
	}
	input, ok := m.lookupSession(sessionID)
	if !ok {
		return SessionOutput{}, fmt.Errorf("auth session is unknown")
	}
	pollOut, err := m.driver.Poll(ctx, sessionID)
	if err != nil {
		return SessionOutput{}, err
	}
	if pollOut.State == SessionStateSucceeded {
		if strings.TrimSpace(pollOut.CredentialRef) == "" {
			return SessionOutput{}, fmt.Errorf("auth session succeeded without credential ref")
		}
		if m.store != nil {
			persistedRef, err := m.store.UpsertCredentialRef(ctx, input.ProviderSpec, input.EndpointRef, strings.TrimSpace(pollOut.CredentialRef))
			if err != nil {
				return SessionOutput{}, err
			}
			if strings.TrimSpace(persistedRef) != "" {
				pollOut.CredentialRef = strings.TrimSpace(persistedRef)
			}
		}
	}
	return SessionOutput{
		ProviderSpec:  input.ProviderSpec,
		SessionID:     sessionID,
		State:         pollOut.State,
		CredentialRef: strings.TrimSpace(pollOut.CredentialRef),
		ErrorMessage:  strings.TrimSpace(pollOut.ErrorMessage),
	}, nil
}

func (m *Manager) Cancel(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if _, ok := m.lookupSession(sessionID); !ok {
		return fmt.Errorf("auth session is unknown")
	}
	if err := m.driver.Cancel(ctx, sessionID); err != nil {
		return err
	}
	return nil
}

func (m *Manager) Retry(ctx context.Context, sessionID string) (StartOutput, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return StartOutput{}, fmt.Errorf("session id is required")
	}
	input, ok := m.lookupSession(sessionID)
	if !ok {
		return StartOutput{}, fmt.Errorf("auth session is unknown")
	}
	return m.Start(ctx, input)
}

func (m *Manager) lookupSession(sessionID string) (StartInput, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	in, ok := m.sessions[sessionID]
	return in, ok
}
