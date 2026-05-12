package authplane

import (
	"context"
	"errors"
	"testing"
)

type driverStub struct {
	startFn  func(context.Context, StartInput) (DriverStartResult, error)
	pollFn   func(context.Context, string) (DriverPollResult, error)
	cancelFn func(context.Context, string) error
}

func (d driverStub) Start(ctx context.Context, in StartInput) (DriverStartResult, error) {
	if d.startFn == nil {
		return DriverStartResult{}, nil
	}
	return d.startFn(ctx, in)
}

func (d driverStub) Poll(ctx context.Context, sessionID string) (DriverPollResult, error) {
	if d.pollFn == nil {
		return DriverPollResult{}, nil
	}
	return d.pollFn(ctx, sessionID)
}

func (d driverStub) Cancel(ctx context.Context, sessionID string) error {
	if d.cancelFn == nil {
		return nil
	}
	return d.cancelFn(ctx, sessionID)
}

type storeStub struct {
	provider  string
	endpoint  string
	ref       string
	returnRef string
	err       error
}

func (s *storeStub) UpsertCredentialRef(_ context.Context, providerSpec string, endpointRef string, credentialRef string) (string, error) {
	s.provider = providerSpec
	s.endpoint = endpointRef
	s.ref = credentialRef
	if s.err != nil {
		return "", s.err
	}
	return s.returnRef, nil
}

func TestManagerStartAndPollSuccessPersistsCredentialRef(t *testing.T) {
	t.Parallel()
	store := &storeStub{}
	manager, err := NewManager(driverStub{
		startFn: func(_ context.Context, in StartInput) (DriverStartResult, error) {
			if in.ProviderSpec != "chatgpt" {
				t.Fatalf("provider spec = %q", in.ProviderSpec)
			}
			return DriverStartResult{SessionID: "sess-1", AuthorizeURL: "https://example/login"}, nil
		},
		pollFn: func(_ context.Context, sessionID string) (DriverPollResult, error) {
			if sessionID != "sess-1" {
				t.Fatalf("session id = %q", sessionID)
			}
			return DriverPollResult{State: SessionStateSucceeded, CredentialRef: "keychain:chatgpt/default"}, nil
		},
	}, store)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	start, err := manager.Start(context.Background(), StartInput{ProviderSpec: " ChatGPT ", EndpointRef: "acme"})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	if start.State != SessionStatePending {
		t.Fatalf("start state = %q", start.State)
	}

	session, err := manager.Poll(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("Poll error: %v", err)
	}
	if session.State != SessionStateSucceeded {
		t.Fatalf("session state = %q", session.State)
	}
	if session.CredentialRef != "keychain:chatgpt/default" {
		t.Fatalf("session credential ref = %q", session.CredentialRef)
	}
	if store.provider != "chatgpt" || store.endpoint != "acme" || store.ref != "keychain:chatgpt/default" {
		t.Fatalf("store write mismatch provider=%q endpoint=%q ref=%q", store.provider, store.endpoint, store.ref)
	}
}

func TestManagerPollUsesPersistedCredentialRefOverride(t *testing.T) {
	t.Parallel()
	store := &storeStub{returnRef: "memory:chatgpt/acme"}
	manager, err := NewManager(driverStub{
		startFn: func(_ context.Context, _ StartInput) (DriverStartResult, error) {
			return DriverStartResult{SessionID: "sess-2"}, nil
		},
		pollFn: func(_ context.Context, _ string) (DriverPollResult, error) {
			return DriverPollResult{State: SessionStateSucceeded, CredentialRef: "keychain:chatgpt/default"}, nil
		},
	}, store)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}
	if _, err := manager.Start(context.Background(), StartInput{ProviderSpec: "chatgpt", EndpointRef: "acme"}); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	session, err := manager.Poll(context.Background(), "sess-2")
	if err != nil {
		t.Fatalf("Poll error: %v", err)
	}
	if session.CredentialRef != "memory:chatgpt/acme" {
		t.Fatalf("session credential ref = %q, want memory override", session.CredentialRef)
	}
}

func TestManagerPollUnknownSessionFails(t *testing.T) {
	t.Parallel()
	manager, err := NewManager(driverStub{}, nil)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}
	if _, err := manager.Poll(context.Background(), "missing"); err == nil {
		t.Fatal("expected unknown session error")
	}
}

func TestManagerCancelPropagatesDriverError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("cancel failed")
	manager, err := NewManager(driverStub{
		startFn: func(_ context.Context, _ StartInput) (DriverStartResult, error) {
			return DriverStartResult{SessionID: "sess-1"}, nil
		},
		cancelFn: func(_ context.Context, _ string) error { return wantErr },
	}, nil)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}
	if _, err := manager.Start(context.Background(), StartInput{ProviderSpec: "chatgpt", EndpointRef: "acme"}); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	if err := manager.Cancel(context.Background(), "sess-1"); !errors.Is(err, wantErr) {
		t.Fatalf("Cancel error = %v, want %v", err, wantErr)
	}
}

func TestManagerRetryReusesStartInput(t *testing.T) {
	t.Parallel()
	var seen []StartInput
	manager, err := NewManager(driverStub{
		startFn: func(_ context.Context, in StartInput) (DriverStartResult, error) {
			seen = append(seen, in)
			if len(seen) == 1 {
				return DriverStartResult{SessionID: "sess-1"}, nil
			}
			return DriverStartResult{SessionID: "sess-2"}, nil
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}
	_, err = manager.Start(context.Background(), StartInput{
		ProviderSpec: "chatgpt",
		EndpointRef:  "main#cfg-a",
	})
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	if _, err := manager.Retry(context.Background(), "sess-1"); err != nil {
		t.Fatalf("Retry error: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("start count = %d", len(seen))
	}
	if seen[1].ProviderSpec != "chatgpt" || seen[1].EndpointRef != "main#cfg-a" {
		t.Fatalf("retry input lost fields: %#v", seen[1])
	}
}
