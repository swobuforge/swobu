package target_config

import (
	"context"
	"errors"
	"strings"
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
	start   func(context.Context, ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error)
	poll    func(context.Context, string) (readmodel.AuthSessionReadModel, error)
	cancel  func(context.Context, string) error
}

func (s *authCommandsStub) StartAuthSession(ctx context.Context, req ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
	select {
	case <-s.started:
	default:
		close(s.started)
	}
	if s.start != nil {
		return s.start(ctx, req)
	}
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

func (s *authCommandsStub) CancelAuthSession(ctx context.Context, sessionID string) error {
	if s.cancel != nil {
		return s.cancel(ctx, sessionID)
	}
	return nil
}

func (*authCommandsStub) RetryAuthSession(context.Context, string) (readmodel.AuthSessionReadModel, error) {
	return readmodel.AuthSessionReadModel{}, nil
}

func TestMountedChatGPTDeviceChoiceStartsDeviceSession(t *testing.T) {
	startedMode := make(chan string, 1)
	commands := &authCommandsStub{
		started: make(chan struct{}),
		polled:  make(chan struct{}, 1),
		start: func(_ context.Context, req ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
			startedMode <- req.AuthMode
			return readmodel.AuthSessionReadModel{
				ProviderSpec: string(profile.ProviderSpecChatGPT),
				SessionID:    "sess-device",
				AuthorizeURL: "https://auth.openai.com/codex/device",
				UserCode:     "ABCD-EFGH",
				State:        "pending",
			}, nil
		},
		poll: func(context.Context, string) (readmodel.AuthSessionReadModel, error) {
			return readmodel.AuthSessionReadModel{State: "pending"}, nil
		},
	}
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.TargetAuthCommands = commands
	w.Open()
	w.SelectProvider(string(profile.ProviderSpecChatGPT))

	h, err := testkit.NewHarnessAt(w, 100, 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	if frame := h.Frame(); !strings.Contains(frame, "device code") {
		t.Fatalf("authentication choice did not expose device code:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	select {
	case mode := <-startedMode:
		if mode != "device" {
			t.Fatalf("auth mode = %q, want device", mode)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("device login did not start")
	}
	frame := h.Frame()
	for _, want := range []string{"authentication", "device code", "change ↵", "https://auth.openai.com/codex/device", "ABCD-EFGH", "copy ↵"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("pending device frame missing %q:\n%s", want, frame)
		}
	}
}

func TestMountedPendingChatGPTAuthenticationSwitchesToDevice(t *testing.T) {
	events := make(chan string, 4)
	commands := &authCommandsStub{
		started: make(chan struct{}),
		polled:  make(chan struct{}, 1),
		start: func(_ context.Context, req ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
			events <- "start:" + req.AuthMode
			result := readmodel.AuthSessionReadModel{
				ProviderSpec: string(profile.ProviderSpecChatGPT),
				SessionID:    "sess-" + req.AuthMode,
				AuthorizeURL: "https://auth.example.test/" + req.AuthMode,
				State:        "pending",
			}
			if req.AuthMode == "device" {
				result.UserCode = "ABCD-EFGH"
			}
			return result, nil
		},
		poll: func(context.Context, string) (readmodel.AuthSessionReadModel, error) {
			return readmodel.AuthSessionReadModel{State: "pending"}, nil
		},
		cancel: func(_ context.Context, sessionID string) error {
			events <- "cancel:" + sessionID
			return nil
		},
	}
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.TargetAuthCommands = commands
	w.Open()
	w.SelectProvider(string(profile.ProviderSpecChatGPT))

	h, err := testkit.NewHarnessAt(w, 100, 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	if event := <-events; event != "start:browser" {
		t.Fatalf("first event = %q, want browser start", event)
	}

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyUp})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyUp})
	if frame := h.Frame(); !strings.Contains(frame, "> authentication") || !strings.Contains(frame, "change ↵") {
		t.Fatalf("pending authentication is not selectable:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	for _, want := range []string{"cancel:sess-browser", "start:device"} {
		select {
		case event := <-events:
			if event != want {
				t.Fatalf("event = %q, want %q", event, want)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("missing event %q", want)
		}
	}
	frame := h.Frame()
	for _, want := range []string{"authentication", "device code", "change ↵", "ABCD-EFGH", "copy ↵"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("replacement device frame missing %q:\n%s", want, frame)
		}
	}
}

func TestMountedPendingChatGPTAuthenticationCancelFailurePreventsReplacement(t *testing.T) {
	startCount := 0
	cancelCount := 0
	commands := &authCommandsStub{
		started: make(chan struct{}),
		polled:  make(chan struct{}, 1),
		start: func(_ context.Context, req ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
			startCount++
			return readmodel.AuthSessionReadModel{
				ProviderSpec: string(profile.ProviderSpecChatGPT),
				SessionID:    "sess-" + req.AuthMode,
				AuthorizeURL: "https://auth.example.test/" + req.AuthMode,
				State:        "pending",
			}, nil
		},
		poll: func(context.Context, string) (readmodel.AuthSessionReadModel, error) {
			return readmodel.AuthSessionReadModel{State: "pending"}, nil
		},
		cancel: func(context.Context, string) error {
			cancelCount++
			return errors.New("cancel denied")
		},
	}
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.TargetAuthCommands = commands
	w.Open()
	w.SelectProvider(string(profile.ProviderSpecChatGPT))

	h, err := testkit.NewHarnessAt(w, 100, 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	for _, key := range []tui.Key{
		tui.KeyEnter, tui.KeyEnter,
		tui.KeyUp, tui.KeyUp, tui.KeyEnter,
		tui.KeyDown, tui.KeyEnter,
	} {
		h.DispatchKey(tui.KeyEvent{Key: key})
	}

	if startCount != 1 || cancelCount != 1 {
		t.Fatalf("start count = %d, cancel count = %d; want 1, 1", startCount, cancelCount)
	}
	if got := w.AuthSession.Get().SessionID; got != "sess-browser" {
		t.Fatalf("active session = %q, want original browser session", got)
	}
	frame := h.Frame()
	for _, want := range []string{"browser login", "change ↵", "cancel denied"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("cancel failure frame missing %q:\n%s", want, frame)
		}
	}
}

func TestMountedChatGPTBrowserLoginCompletesWithoutManualRefresh(t *testing.T) {
	commands := &authCommandsStub{
		started: make(chan struct{}),
		polled:  make(chan struct{}, 1),
		start: func(_ context.Context, req ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
			if req.AuthMode != "browser" {
				t.Fatalf("auth mode = %q, want browser", req.AuthMode)
			}
			return readmodel.AuthSessionReadModel{
				ProviderSpec: string(profile.ProviderSpecChatGPT),
				SessionID:    "sess-browser",
				AuthorizeURL: "https://auth.example.test/authorize",
				State:        "pending",
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
