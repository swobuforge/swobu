package target_config

import (
	"context"
	"testing"
	"time"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/profile"
)

type authCommandsStub struct {
	started chan struct{}
	polled  chan struct{}
	poll    func(context.Context, string) (readmodel.AuthSessionReadModel, error)
}

func (s *authCommandsStub) StartAuthSession(context.Context, ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
	close(s.started)
	return readmodel.AuthSessionReadModel{
		ProviderSpec: string(profile.ProviderSpecChatGPT),
		SessionID:    "sess-browser",
		AuthorizeURL: "https://auth.example.test/authorize",
		State:        "pending",
	}, nil
}

func (s *authCommandsStub) PollAuthSession(ctx context.Context, sessionID string) (readmodel.AuthSessionReadModel, error) {
	select {
	case s.polled <- struct{}{}:
	default:
	}
	if s.poll != nil {
		return s.poll(ctx, sessionID)
	}
	return readmodel.AuthSessionReadModel{
		ProviderSpec:  string(profile.ProviderSpecChatGPT),
		SessionID:     "sess-browser",
		State:         "succeeded",
		CredentialRef: "secret:chatgpt/sess-browser",
	}, nil
}

func TestChatGPTManualRefreshPreservesPendingBrowserURL(t *testing.T) {
	commands := &authCommandsStub{
		started: make(chan struct{}),
		polled:  make(chan struct{}, 1),
		poll: func(context.Context, string) (readmodel.AuthSessionReadModel, error) {
			return readmodel.AuthSessionReadModel{
				ProviderSpec: string(profile.ProviderSpecChatGPT),
				SessionID:    "sess-browser",
				State:        "pending",
			}, nil
		},
	}
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.TargetAuthCommands = commands
	w.AuthSession.Set(readmodel.AuthSessionReadModel{
		ProviderSpec: string(profile.ProviderSpecChatGPT),
		SessionID:    "sess-browser",
		AuthorizeURL: "https://auth.example.test/authorize",
		State:        "pending",
	})

	w.RefreshAuthSession()

	if got := w.AuthSession.Get().AuthorizeURL; got != "https://auth.example.test/authorize" {
		t.Fatalf("authorize URL = %q after pending refresh", got)
	}
}

func TestMountedChatGPTObserverStopsWhenFormCloses(t *testing.T) {
	pollCanceled := make(chan struct{})
	commands := &authCommandsStub{
		started: make(chan struct{}),
		polled:  make(chan struct{}, 1),
		poll: func(ctx context.Context, _ string) (readmodel.AuthSessionReadModel, error) {
			<-ctx.Done()
			close(pollCanceled)
			return readmodel.AuthSessionReadModel{}, ctx.Err()
		},
	}
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.TargetAuthCommands = commands
	w.Open()
	w.SelectProvider(string(profile.ProviderSpecChatGPT))

	h, err := testkit.NewHarnessAt(w, 100, 12)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	select {
	case <-commands.polled:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("pending browser login was not polled")
	}

	w.Close()
	select {
	case <-pollCanceled:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("closing target form did not cancel auth observer")
	}
	h.Frame()
	if got := w.Lifecycle.Get(); got != LifecycleClosed {
		t.Fatalf("lifecycle = %v, want closed", got)
	}
}

func (*authCommandsStub) CancelAuthSession(context.Context, string) error { return nil }

func (*authCommandsStub) RetryAuthSession(context.Context, string) (readmodel.AuthSessionReadModel, error) {
	return readmodel.AuthSessionReadModel{}, nil
}

func TestMountedChatGPTBrowserLoginCompletesWithoutManualRefresh(t *testing.T) {
	commands := &authCommandsStub{started: make(chan struct{}), polled: make(chan struct{}, 1)}
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.TargetAuthCommands = commands
	w.Open()
	w.SelectProvider(string(profile.ProviderSpecChatGPT))

	h, err := testkit.NewHarnessAt(w, 100, 12)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	select {
	case <-commands.started:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("browser login did not start")
	}
	select {
	case <-commands.polled:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("pending browser login was not observed automatically")
	}
	h.Frame()

	if got := w.AuthSession.Get().State; got != "succeeded" {
		t.Fatalf("auth state = %q, want succeeded", got)
	}
	if got := w.Draft.Get().CredentialRef; got != "secret:chatgpt/sess-browser" {
		t.Fatalf("credential ref = %q", got)
	}
}

func TestMountedChatGPTBrowserLoginProjectsFailureWithoutManualRefresh(t *testing.T) {
	commands := &authCommandsStub{
		started: make(chan struct{}),
		polled:  make(chan struct{}, 1),
		poll: func(context.Context, string) (readmodel.AuthSessionReadModel, error) {
			return readmodel.AuthSessionReadModel{
				ProviderSpec: string(profile.ProviderSpecChatGPT),
				SessionID:    "sess-browser",
				State:        "failed",
				ErrorMessage: "token exchange rejected",
			}, nil
		},
	}
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.TargetAuthCommands = commands
	w.Open()
	w.SelectProvider(string(profile.ProviderSpecChatGPT))

	h, err := testkit.NewHarnessAt(w, 100, 12)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	select {
	case <-commands.polled:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("pending browser login was not observed automatically")
	}
	h.Frame()

	if got := w.AuthSession.Get().State; got != "failed" {
		t.Fatalf("auth state = %q, want failed", got)
	}
	if got := w.Error.Get(); got != "token exchange rejected" {
		t.Fatalf("operator error = %q", got)
	}
}
