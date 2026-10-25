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

func TestManagerStartAndPollSuccessReturnsDriverCredentialRef(t *testing.T) {
	t.Parallel()
	manager, err := NewAuthSessionManager(driverStub{
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
			return DriverPollResult{State: SessionStateSucceeded, CredentialRef: "secret:chatgpt/default"}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewAuthSessionManager error: %v", err)
	}

	start, err := manager.Start(context.Background(), StartInput{ProviderSpec: " ChatGPT ", DraftSubject: "subject:acme"})
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
	if session.CredentialRef != "secret:chatgpt/default" {
		t.Fatalf("session credential ref = %q", session.CredentialRef)
	}
}

func TestManagerPollUnknownSessionFails(t *testing.T) {
	t.Parallel()
	manager, err := NewAuthSessionManager(driverStub{})
	if err != nil {
		t.Fatalf("NewAuthSessionManager error: %v", err)
	}
	if _, err := manager.Poll(context.Background(), "missing"); err == nil {
		t.Fatal("expected unknown session error")
	}
}

func TestManagerCancelPropagatesDriverError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("cancel failed")
	manager, err := NewAuthSessionManager(driverStub{
		startFn: func(_ context.Context, _ StartInput) (DriverStartResult, error) {
			return DriverStartResult{SessionID: "sess-1"}, nil
		},
		cancelFn: func(_ context.Context, _ string) error { return wantErr },
	})
	if err != nil {
		t.Fatalf("NewAuthSessionManager error: %v", err)
	}
	if _, err := manager.Start(context.Background(), StartInput{ProviderSpec: "chatgpt", DraftSubject: "subject:acme"}); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	if err := manager.Cancel(context.Background(), "sess-1"); !errors.Is(err, wantErr) {
		t.Fatalf("Cancel error = %v, want %v", err, wantErr)
	}
}
