package workspace_connect

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	cockpitui "github.com/swobuforge/swobu/internal/cockpit/ui"
)

func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func observationFor(t *testing.T, obs []clientObservation, clientID clientconnect.ClientID) clientObservation {
	t.Helper()
	for _, o := range obs {
		if o.Client.ID == clientID {
			return o
		}
	}
	t.Fatalf("client %s not found in observations: %#v", clientID, obs)
	return clientObservation{}
}

type fakeOperations struct {
	clients       []clientconnect.Client
	discover      func() []clientconnect.Client
	discoverCalls int
	plans         map[clientconnect.ClientID]clientconnect.Plan
	applyErr      error
	applied       clientconnect.Plan
}

func (f *fakeOperations) Discover(clientconnect.Target) []clientconnect.Client {
	f.discoverCalls++
	if f.discover != nil {
		return f.discover()
	}
	return append([]clientconnect.Client(nil), f.clients...)
}

func (f *fakeOperations) Plan(id clientconnect.ClientID, _ clientconnect.Target) (clientconnect.Plan, error) {
	plan, ok := f.plans[id]
	if !ok {
		return clientconnect.Plan{}, errors.New("plan failed. Nothing changed.")
	}
	return plan, nil
}

func (f *fakeOperations) Apply(plan clientconnect.Plan) error {
	f.applied = plan
	return f.applyErr
}

func connectTarget(t *testing.T) clientconnect.Target {
	t.Helper()
	target, err := clientconnect.NewTarget("work", "http://127.0.0.1:7926/c/work")
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func allSixClients() []clientconnect.Client {
	return []clientconnect.Client{
		{ID: clientconnect.ClientCodex, Name: "Codex CLI"},
		{ID: clientconnect.ClientClaude, Name: "Claude Code"},
		{ID: clientconnect.ClientKilo, Name: "Kilo Code"},
		{ID: clientconnect.ClientPi, Name: "pi"},
		{ID: clientconnect.ClientOpenClaw, Name: "OpenClaw"},
		{ID: clientconnect.ClientHermes, Name: "Hermes Agent"},
	}
}

func connectFixture(t *testing.T) (*Disclosure, *fakeOperations) {
	t.Helper()
	target := connectTarget(t)
	ops := &fakeOperations{
		clients: allSixClients(),
		plans: map[clientconnect.ClientID]clientconnect.Plan{
			clientconnect.ClientCodex: {
				ClientID:   clientconnect.ClientCodex,
				ClientName: "Codex CLI",
				ConfigPath: "/tmp/.codex/config.toml",
				Target:     target,
				Changes: []clientconnect.Change{
					{Field: "backend", BeforeExists: true, Before: "openai", After: "swobu"},
					{Field: "base URL", BeforeExists: true, Before: "https://api.openai.com/v1", After: target.WorkspaceURL()},
					{Field: "model", BeforeExists: true, Before: "gpt-5-preview", After: "default"},
				},
			},
			clientconnect.ClientClaude: {
				ClientID:   clientconnect.ClientClaude,
				ClientName: "Claude Code",
				ConfigPath: "/tmp/.claude/settings.json",
				Target:     target,
				Changes:    nil, // AlreadyConfigured
			},
			clientconnect.ClientKilo: {
				ClientID:   clientconnect.ClientKilo,
				ClientName: "Kilo Code",
				ConfigPath: "/tmp/.kilo/config.json",
				Target:     target,
				Changes: []clientconnect.Change{
					{Field: "endpoint", BeforeExists: false, After: target.WorkspaceURL()},
				},
			},
			clientconnect.ClientPi: {
				ClientID:   clientconnect.ClientPi,
				ClientName: "pi",
				ConfigPath: "/tmp/.pi/config.toml",
				Target:     target,
				Changes:    nil, // AlreadyConfigured
			},
			clientconnect.ClientOpenClaw: {
				ClientID:   clientconnect.ClientOpenClaw,
				ClientName: "OpenClaw",
				ConfigPath: "/tmp/.openclaw/config.json",
				Target:     target,
				Changes: []clientconnect.Change{
					{Field: "endpoint", BeforeExists: false, After: target.WorkspaceURL()},
				},
			},
			clientconnect.ClientHermes: {
				ClientID:   clientconnect.ClientHermes,
				ClientName: "Hermes Agent",
				ConfigPath: "/tmp/.hermes/config.yaml",
				Target:     target,
				Changes: []clientconnect.Change{
					{Field: "endpoint", BeforeExists: false, After: target.WorkspaceURL()},
				},
			},
		},
	}
	d := New(target, ops)
	return d, ops
}

func TestDiscoveryRunsOnlyOnDeliberateOpenAndReopen(t *testing.T) {
	ops := &fakeOperations{
		clients: []clientconnect.Client{{ID: clientconnect.ClientCodex, Name: "Codex CLI"}},
		plans: map[clientconnect.ClientID]clientconnect.Plan{
			clientconnect.ClientCodex: {
				ClientID:   clientconnect.ClientCodex,
				ClientName: "Codex CLI",
				ConfigPath: "/tmp/.codex/config.toml",
				Changes: []clientconnect.Change{
					{Field: "backend", BeforeExists: false, After: "swobu"},
				},
			},
		},
	}
	d := New(connectTarget(t), ops)
	_ = testkit.RenderMountedTrimmed(t, d, 100, 10)
	if ops.discoverCalls != 0 {
		t.Fatalf("collapsed mount discovery calls = %d", ops.discoverCalls)
	}
	d.toggleEndpoint()
	if ops.discoverCalls != 1 || len(d.Observations.Get()) != 1 {
		t.Fatalf("first open = calls %d observations %#v", ops.discoverCalls, d.Observations.Get())
	}
	d.toggleEndpoint()
	if ops.discoverCalls != 1 {
		t.Fatalf("close discovery calls = %d", ops.discoverCalls)
	}
	d.toggleEndpoint()
	if ops.discoverCalls != 2 || len(d.Observations.Get()) != 1 {
		t.Fatalf("reopen = calls %d observations %#v", ops.discoverCalls, d.Observations.Get())
	}
}

func TestDisclosureRestingAndExpandedFrames(t *testing.T) {
	d, _ := connectFixture(t)
	resting := testkit.RenderMountedTrimmed(t, d, 100, 20)
	for _, want := range []string{"endpoint", "http://127.0.0.1:7926/c/work", "clients ↵", "OpenAI · Anthropic"} {
		if !strings.Contains(resting, want) {
			t.Fatalf("resting frame missing %q:\n%s", want, resting)
		}
	}
	if strings.Contains(resting, "Codex CLI") || strings.Contains(resting, "Other clients") {
		t.Fatalf("resting frame leaked disclosure:\n%s", resting)
	}
	d.toggleEndpoint()
	expanded := testkit.RenderMountedTrimmed(t, d, 100, 20)
	for _, want := range []string{"Codex CLI", "configure ↵", "Claude Code", "configured ↵", "Kilo Code", "pi", "Other clients", "setup ↵"} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("expanded frame missing %q:\n%s", want, expanded)
		}
	}
	// Ecosystem summary line is omitted in expanded state to save height
	if strings.Contains(expanded, "OpenAI · Anthropic") {
		t.Fatalf("expanded frame should omit OpenAI · Anthropic ecosystem line:\n%s", expanded)
	}
}

func TestDisclosurePlanChildScopeAndApplyConfiguresClientWithoutToast(t *testing.T) {
	d, ops := connectFixture(t)
	d.toggleEndpoint()
	d.chooseClient(clientconnect.ClientCodex)
	frame := testkit.RenderMountedTrimmed(t, d, 100, 20)
	for _, want := range []string{"Codex CLI", "close ↵", "backend", "openai → swobu", "base URL", "https://api.openai.com/v1 → /c/work", "model", "gpt-5-preview → default", "config", "/tmp/.codex/config.toml", "replace ↵"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("plan frame missing %q:\n%s", want, frame)
		}
	}
	// Sibling client rows remain visible inline in the browse list
	if !strings.Contains(frame, "Kilo Code") || !strings.Contains(frame, "Other clients") {
		t.Fatalf("plan frame should keep sibling browse rows visible:\n%s", frame)
	}
	// Label must be 'config', not 'writes'
	if strings.Contains(frame, "writes") {
		t.Fatalf("plan frame used 'writes' instead of 'config':\n%s", frame)
	}

	d.applyPlan(clientconnect.ClientCodex)
	if ops.applied.ClientID != clientconnect.ClientCodex {
		t.Fatalf("applied = %#v", ops.applied)
	}

	// Browse list returned with Codex CLI configured ↵ and zero extra Discover calls
	frame = testkit.RenderMountedTrimmed(t, d, 100, 20)
	if !strings.Contains(frame, "Codex CLI") || !strings.Contains(frame, "configured ↵") || strings.Contains(frame, "/tmp/.codex/config.toml") {
		t.Fatalf("success frame:\n%s", frame)
	}
}

func TestPlanActionGrammarDistinguishesInsertFromOverwrite(t *testing.T) {
	d, _ := connectFixture(t)
	for _, tc := range []struct {
		name       string
		overwrites bool
		want       string
		forbidden  string
	}{
		{name: "new binding", want: "apply ↵", forbidden: "replace ↵"},
		{name: "existing binding", overwrites: true, want: "replace ↵", forbidden: "apply ↵"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			change := clientconnect.Change{Field: "endpoint", After: connectTarget(t).WorkspaceURL()}
			if tc.overwrites {
				change.BeforeExists, change.Before = true, "https://old"
			}
			plan := clientconnect.Plan{ClientID: clientconnect.ClientCodex, ConfigPath: "/tmp/config", Target: connectTarget(t), Changes: []clientconnect.Change{change}}
			obs := clientObservation{Client: clientconnect.Client{ID: clientconnect.ClientCodex, Name: "Codex CLI"}, Kind: observationNeedsChange, Plan: plan}
			frame := testkit.RenderMountedTrimmed(t, PlanActionRow(d, obs), 100, 2)
			if !strings.Contains(frame, tc.want) || strings.Contains(frame, tc.forbidden) {
				t.Fatalf("action grammar frame:\n%s", frame)
			}
		})
	}
}

func TestDisclosureApplyErrorStatesNothingChangedAndKeepsPlan(t *testing.T) {
	d, ops := connectFixture(t)
	ops.applyErr = errors.New("client configuration changed; nothing was overwritten")
	d.toggleEndpoint()
	d.chooseClient(clientconnect.ClientCodex)
	d.applyPlan(clientconnect.ClientCodex)
	frame := testkit.RenderMountedTrimmed(t, d, 100, 22)
	if !strings.Contains(frame, "nothing was") || !strings.Contains(frame, "overwritten") || !strings.Contains(frame, "replace ↵") {
		t.Fatalf("error frame:\n%s", frame)
	}
	if !d.Child.Get().isClient(clientconnect.ClientCodex) {
		t.Fatalf("error dismissed plan state: %#v", d.Child.Get())
	}
}

func TestDisclosureOtherClientsManualSetupChildScope(t *testing.T) {
	d, _ := connectFixture(t)
	d.toggleEndpoint()

	var copied []string
	cleanup := cockpitui.RegisterEffectHooks(nil, func(value string) (bool, error) {
		copied = append(copied, value)
		return true, nil
	}, nil)
	defer cleanup()

	// In browse, row is 'Other clients setup ↵'
	browseFrame := testkit.RenderMountedTrimmed(t, d, 100, 24)
	if !strings.Contains(browseFrame, "Other clients") || !strings.Contains(browseFrame, "setup ↵") {
		t.Fatalf("browse frame missing 'Other clients setup ↵':\n%s", browseFrame)
	}

	// Open manual setup
	d.openManualSetup()
	manualFrame := testkit.RenderMountedTrimmed(t, d, 100, 24)
	for _, want := range []string{
		"Other clients", "close ↵",
		"API", "OpenAI · Anthropic",
		"Base URL", "http://127.0.0.1:7926/c/work", "copy ↵",
		"Model", "default", "copy ↵",
		"Models URL", "http://127.0.0.1:7926/c/work/models", "copy ↵",
		"API key", "swobu · placeholder", "copy ↵",
	} {
		if !strings.Contains(manualFrame, want) {
			t.Fatalf("manual setup frame missing %q:\n%s", want, manualFrame)
		}
	}
	// Sibling client rows remain visible inline above Other clients
	if !strings.Contains(manualFrame, "Codex CLI") || !strings.Contains(manualFrame, "Kilo Code") {
		t.Fatalf("manual setup frame should keep sibling browse rows visible:\n%s", manualFrame)
	}

	// 1. Copy Base URL
	d.copyItem("base-url", d.Target.WorkspaceURL())
	if len(copied) != 1 || copied[0] != "http://127.0.0.1:7926/c/work" {
		t.Fatalf("copied base-url = %#v", copied)
	}
	frameAfterCopyBase := testkit.RenderMountedTrimmed(t, d, 100, 24)
	if !strings.Contains(frameAfterCopyBase, "Base URL") || !strings.Contains(frameAfterCopyBase, "copied") {
		t.Fatalf("frame after copy base URL did not show 'copied':\n%s", frameAfterCopyBase)
	}
	// Only Base URL shows copied; Model, Models URL, API key still show copy ↵
	if strings.Count(frameAfterCopyBase, "copied") != 1 || strings.Count(frameAfterCopyBase, "copy ↵") != 3 {
		t.Fatalf("copy feedback count unexpected:\n%s", frameAfterCopyBase)
	}

	// 2. Copy Model
	d.copyItem("model", "default")
	if len(copied) != 2 || copied[1] != "default" {
		t.Fatalf("copied model = %#v", copied)
	}
	frameAfterCopyModel := testkit.RenderMountedTrimmed(t, d, 100, 24)
	// Base URL reverted to copy ↵, Model shows copied
	if strings.Count(frameAfterCopyModel, "copied") != 1 || strings.Count(frameAfterCopyModel, "copy ↵") != 3 {
		t.Fatalf("copy feedback did not move to model row:\n%s", frameAfterCopyModel)
	}

	// 3. Copy Models URL
	d.copyItem("models-url", d.Target.WorkspaceURL()+"/models")
	if len(copied) != 3 || copied[2] != "http://127.0.0.1:7926/c/work/models" {
		t.Fatalf("copied models-url = %#v", copied)
	}

	// 4. Copy API key (copies "swobu")
	d.copyItem("api-key", "swobu")
	if len(copied) != 4 || copied[3] != "swobu" {
		t.Fatalf("copied api-key = %#v", copied)
	}

	// Closing manual setup clears copy feedback
	d.closeChildScope()
	if d.Child.Get().isManual() || d.Feedback.Get().key != "" {
		t.Fatalf("closeChildScope did not reset state")
	}
}

func TestDisclosureManualSetupFallbackSavedFileAndError(t *testing.T) {
	d, _ := connectFixture(t)
	d.toggleEndpoint()
	d.openManualSetup()

	// 1. Fallback temp file copy
	cleanup := cockpitui.RegisterEffectHooks(
		nil,
		func(string) (bool, error) { return false, errors.New("no clipboard") },
		func(dir, prefix, text string) (string, error) { return "/tmp/swobu-fallback-456.txt", nil },
	)
	defer cleanup()

	d.copyItem("base-url", d.Target.WorkspaceURL())
	frame := testkit.RenderMountedTrimmed(t, d, 100, 24)
	if !strings.Contains(frame, "saved") || !strings.Contains(frame, "/tmp/swobu-fallback-456.txt") {
		t.Fatalf("fallback file frame:\n%s", frame)
	}

	// 2. Hard failure
	cleanup2 := cockpitui.RegisterEffectHooks(
		nil,
		func(string) (bool, error) { return false, errors.New("no clipboard") },
		func(dir, prefix, text string) (string, error) { return "", errors.New("disk full") },
	)
	defer cleanup2()

	d.copyItem("model", "default")
	frameErr := testkit.RenderMountedTrimmed(t, d, 100, 24)
	if !strings.Contains(frameErr, "copy failed") || !strings.Contains(frameErr, "swobu doctor --copy") {
		t.Fatalf("copy failure frame:\n%s", frameErr)
	}
}

func TestDisclosureBackScopeGrammar(t *testing.T) {
	d, _ := connectFixture(t)

	// Collapsed: Back() is false
	if d.Back() {
		t.Fatal("Back() returned true on collapsed disclosure")
	}

	// Open endpoint -> browse list
	d.toggleEndpoint()
	if !d.EndpointOpen.Get() {
		t.Fatal("toggleEndpoint did not open")
	}

	// Open manual setup
	d.openManualSetup()
	if !d.Child.Get().isManual() {
		t.Fatal("openManualSetup did not open")
	}

	// Esc / Back() from manual setup -> returns to browse (endpoint stays open)
	if !d.Back() || d.Child.Get().isManual() || !d.EndpointOpen.Get() {
		t.Fatalf("Back() from manual setup failed: child=%#v endpoint=%v", d.Child.Get(), d.EndpointOpen.Get())
	}

	// Open named-client plan
	d.chooseClient(clientconnect.ClientCodex)
	if !d.Child.Get().isClient(clientconnect.ClientCodex) {
		t.Fatal("chooseClient did not set client child")
	}

	// Esc / Back() from plan -> returns to browse (endpoint stays open)
	if !d.Back() || d.Child.Get().kind != childNone || !d.EndpointOpen.Get() {
		t.Fatalf("Back() from plan failed: child=%#v endpoint=%v", d.Child.Get(), d.EndpointOpen.Get())
	}

	// Esc / Back() from browse -> closes endpoint
	if !d.Back() || d.EndpointOpen.Get() {
		t.Fatalf("Back() from browse failed: endpoint=%v", d.EndpointOpen.Get())
	}
}

func TestDisplayChangeShortensOnlyCanonicalWorkspaceOrigin(t *testing.T) {
	target := connectTarget(t)
	if got := displayChange(target, clientconnect.Change{After: target.WorkspaceURL()}); got != "→ /c/work" {
		t.Fatalf("same-origin display = %q", got)
	}
	if got := displayChange(target, clientconnect.Change{BeforeExists: true, Before: "", After: target.WorkspaceURL()}); got != " → /c/work" {
		t.Fatalf("present-empty display = %q", got)
	}
	if got := displayChange(target, clientconnect.Change{BeforeExists: true, Before: "openai", After: "swobu"}); got != "openai → swobu" {
		t.Fatalf("named values display = %q", got)
	}
}

func TestDisclosureAppLoopEnterDisclosesEndpoint(t *testing.T) {
	d, _ := connectFixture(t)
	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyDown})
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyEnter})
	waitFor(t, func() bool {
		h.Frame()
		return len(d.Observations.Get()) > 0
	})
	if !strings.Contains(h.Frame(), "Codex CLI") {
		t.Fatalf("Enter did not disclose endpoint:\n%s", h.Frame())
	}
}

func TestDisclosureAppLoopOtherClientsManualSetupFlow(t *testing.T) {
	d, _ := connectFixture(t)
	var copied []string
	cleanup := cockpitui.RegisterEffectHooks(nil, func(value string) (bool, error) {
		copied = append(copied, value)
		return true, nil
	}, nil)
	defer cleanup()

	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	// 1. Open endpoint
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyDown})
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyEnter})
	waitFor(t, func() bool {
		h.Frame()
		return len(d.Observations.Get()) > 0
	})
	if !strings.Contains(h.Frame(), "Other clients") {
		t.Fatalf("expected Other clients row:\n%s", h.Frame())
	}

	// 2. Navigate to Other clients row and press Enter
	for i := 0; i < 10; i++ {
		h.DispatchKey(ui.KeyEvent{Key: ui.KeyDown})
		frame := h.Frame()
		if strings.Contains(frame, "> Other clients") {
			break
		}
	}
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyEnter})

	// Manual setup view is visible
	frame := h.Frame()
	if !strings.Contains(frame, "API") || !strings.Contains(frame, "Base URL") {
		t.Fatalf("expected manual setup rows after Enter on Other clients:\n%s", frame)
	}

	// 3. Navigate down to Base URL and press Enter
	for i := 0; i < 5; i++ {
		h.DispatchKey(ui.KeyEvent{Key: ui.KeyDown})
		frame = h.Frame()
		if strings.Contains(frame, "> Base URL") {
			break
		}
	}
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyEnter})

	// Local row changed to 'copied'
	frame = h.Frame()
	if !strings.Contains(frame, "copied") {
		t.Fatalf("expected 'copied' on Base URL row:\n%s", frame)
	}
	if len(copied) != 1 || copied[0] != "http://127.0.0.1:7926/c/work" {
		t.Fatalf("copied = %#v", copied)
	}

	// 4. Press Escape to return to browse
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyEscape})
	frame = h.Frame()
	if !strings.Contains(frame, "Other clients") || !strings.Contains(frame, "setup ↵") || strings.Contains(frame, "Base URL") {
		t.Fatalf("expected browse list after Escape:\n%s", frame)
	}
}

func TestDisclosureAppLoopNamedClientPlanFlow(t *testing.T) {
	d, _ := connectFixture(t)

	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	// 1. Open endpoint
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyDown})
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyEnter})
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) > 0 && observationFor(t, obs, clientconnect.ClientCodex).Kind == observationNeedsChange
	})

	// 2. Focus Codex CLI and press Enter
	for i := 0; i < 5; i++ {
		h.DispatchKey(ui.KeyEvent{Key: ui.KeyDown})
		frame := h.Frame()
		if strings.Contains(frame, "> Codex CLI") {
			break
		}
	}
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyEnter})

	// Plan view is displayed
	waitFor(t, func() bool {
		h.Frame()
		return d.Child.Get().isClient(clientconnect.ClientCodex)
	})
	frame := h.Frame()
	if !strings.Contains(frame, "config") || !strings.Contains(frame, "replace ↵") {
		t.Fatalf("expected plan view:\n%s", frame)
	}

	// 3. Navigate to config row and press Enter (apply)
	for i := 0; i < 5; i++ {
		h.DispatchKey(ui.KeyEvent{Key: ui.KeyDown})
		frame = h.Frame()
		if strings.Contains(frame, "> config") {
			break
		}
	}
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyEnter})

	// Returns to browse list, Codex CLI is now configured ↵
	waitFor(t, func() bool {
		h.Frame()
		return d.Child.Get().kind == childNone
	})
	frame = h.Frame()
	if !strings.Contains(frame, "Codex CLI") || !strings.Contains(frame, "configured ↵") {
		t.Fatalf("expected configured row:\n%s", frame)
	}
}

func TestWorkspaceIdentityKeyRemountsConnectDisclosure(t *testing.T) {
	targetA := connectTarget(t)
	targetB, err := clientconnect.NewTarget("other", "http://127.0.0.1:7926/c/other")
	if err != nil {
		t.Fatal(err)
	}
	root := &remountRoot{
		key:    "workspace-connect:work:" + targetA.WorkspaceURL(),
		target: targetA,
		ops: &fakeOperations{
			clients: []clientconnect.Client{{ID: clientconnect.ClientCodex, Name: "Codex A"}},
			plans: map[clientconnect.ClientID]clientconnect.Plan{clientconnect.ClientCodex: {
				ClientID: clientconnect.ClientCodex, ClientName: "Codex A",
				Target: targetA,
			}},
		},
	}
	h, err := testkit.NewHarness(root)
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()
	root.mounted.toggleEndpoint()
	waitFor(t, func() bool {
		h.Frame()
		return len(root.mounted.Observations.Get()) > 0
	})
	root.mounted.chooseClient(clientconnect.ClientCodex)
	waitFor(t, func() bool {
		h.Frame()
		return root.mounted.Child.Get().isClient(clientconnect.ClientCodex)
	})
	if !root.mounted.EndpointOpen.Get() {
		t.Fatal("workspace A disclosure was not open")
	}

	root.key = "workspace-connect:other:" + targetB.WorkspaceURL()
	root.target = targetB
	root.ops = &fakeOperations{clients: []clientconnect.Client{{ID: clientconnect.ClientClaude, Name: "Claude B"}}}
	h.App().MarkDirty()
	h.Frame()
	if root.mounted.Target.WorkspaceSlug() != "other" || root.mounted.EndpointOpen.Get() || root.mounted.Child.Get().kind != childNone {
		t.Fatalf("workspace B reused A state: target=%q open=%v child=%#v", root.mounted.Target.WorkspaceSlug(), root.mounted.EndpointOpen.Get(), root.mounted.Child.Get())
	}
	root.mounted.toggleEndpoint()
	waitFor(t, func() bool {
		h.Frame()
		return len(root.mounted.Observations.Get()) > 0
	})
	if got := root.mounted.Observations.Get(); len(got) != 1 || observationFor(t, got, clientconnect.ClientClaude).Client.Name != "Claude B" {
		t.Fatalf("workspace B discovery = %#v", got)
	}
}

func TestFullFrameMultiWidthFixtures(t *testing.T) {
	widths := []int{80, 100, 120}

	for _, w := range widths {
		t.Run("Width_"+string(rune('0'+w/100))+string(rune('0'+(w/10)%10))+string(rune('0'+w%10)), func(t *testing.T) {
			// 1. Resting collapsed
			d, _ := connectFixture(t)
			resting := testkit.RenderMountedTrimmed(t, d, w, 20)
			if !strings.Contains(resting, "endpoint") || !strings.Contains(resting, "clients ↵") || !strings.Contains(resting, "OpenAI · Anthropic") {
				t.Fatalf("resting fixture failed at width %d:\n%s", w, resting)
			}

			// 2. Browse with all 6 automatic clients
			d.toggleEndpoint()
			browse := testkit.RenderMountedTrimmed(t, d, w, 20)
			for _, client := range allSixClients() {
				if !strings.Contains(browse, client.Name) {
					t.Fatalf("browse fixture missing %s at width %d:\n%s", client.Name, w, browse)
				}
			}
			if !strings.Contains(browse, "Other clients") || !strings.Contains(browse, "setup ↵") {
				t.Fatalf("browse fixture missing Other clients at width %d:\n%s", w, browse)
			}

			// 3. Named-client replacement Plan
			d.chooseClient(clientconnect.ClientCodex)
			planReplace := testkit.RenderMountedTrimmed(t, d, w, 20)
			if !strings.Contains(planReplace, "Codex CLI") || !strings.Contains(planReplace, "config") || !strings.Contains(planReplace, "replace ↵") {
				t.Fatalf("plan replace fixture failed at width %d:\n%s", w, planReplace)
			}

			// 4. Named-client additive Plan
			dAdditive, _ := connectFixture(t)
			dAdditive.toggleEndpoint()
			dAdditive.Child.Set(childScope{
				kind:     childClient,
				clientID: clientconnect.ClientKilo,
			})
			planApply := testkit.RenderMountedTrimmed(t, dAdditive, w, 20)
			if !strings.Contains(planApply, "Kilo Code") || !strings.Contains(planApply, "config") || !strings.Contains(planApply, "apply ↵") {
				t.Fatalf("plan apply fixture failed at width %d:\n%s", w, planApply)
			}

			// 5. Other clients Manual Setup
			d.openManualSetup()
			manual := testkit.RenderMountedTrimmed(t, d, w, 20)
			for _, row := range []string{"Other clients", "close ↵", "API", "OpenAI · Anthropic", "Base URL", "Model", "Models URL", "API key"} {
				if !strings.Contains(manual, row) {
					t.Fatalf("manual setup fixture missing %s at width %d:\n%s", row, w, manual)
				}
			}

			// 6. Manual Setup with local copied result
			cleanup := cockpitui.RegisterEffectHooks(nil, func(string) (bool, error) { return true, nil }, nil)
			d.copyItem("base-url", d.Target.WorkspaceURL())
			copiedFrame := testkit.RenderMountedTrimmed(t, d, w, 20)
			if !strings.Contains(copiedFrame, "copied") {
				t.Fatalf("copied fixture missing 'copied' at width %d:\n%s", w, copiedFrame)
			}
			cleanup()

			// 7. Manual Setup with fallback file saved
			cleanupFallback := cockpitui.RegisterEffectHooks(
				nil,
				func(string) (bool, error) { return false, errors.New("fail") },
				func(dir, prefix, text string) (string, error) { return "/tmp/saved-url.txt", nil },
			)
			d.copyItem("base-url", d.Target.WorkspaceURL())
			savedFrame := testkit.RenderMountedTrimmed(t, d, w, 20)
			if !strings.Contains(savedFrame, "saved") || !strings.Contains(savedFrame, "/tmp/saved-url.txt") {
				t.Fatalf("saved fixture missing 'saved' or path at width %d:\n%s", w, savedFrame)
			}
			cleanupFallback()
		})
	}
}

func TestStructuralStressFixture60Columns(t *testing.T) {
	d, _ := connectFixture(t)
	d.toggleEndpoint()
	d.openManualSetup()

	// 60x24 structural stress test
	frame := testkit.RenderMountedTrimmed(t, d, 60, 24)
	lines := strings.Split(frame, "\n")
	for i, line := range lines {
		// No line should exceed 60 printable character width or wrap unexpectedly
		if len([]rune(line)) > 60 {
			t.Fatalf("line %d exceeded 60 columns (%d chars):\n%s", i, len([]rune(line)), line)
		}
	}

	// Essential actions must survive truncation
	for _, want := range []string{"Other clients", "close ↵", "Base URL", "copy ↵", "Model", "Models URL", "API key"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("60-col stress frame dropped action or label %q:\n%s", want, frame)
		}
	}
}

func TestShortLocusPathBoundaries(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("user home not available")
	}

	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{name: "exact home", path: home, want: "~"},
		{name: "subpath in home", path: filepath.Join(home, ".codex", "config.toml"), want: filepath.Join("~", ".codex", "config.toml")},
		{name: "prefix collision not in home", path: home + "2" + string(os.PathSeparator) + "foo", want: home + "2" + string(os.PathSeparator) + "foo"},
		{name: "external path", path: "/etc/config.toml", want: "/etc/config.toml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shortLocus(tc.path); got != tc.want {
				t.Fatalf("shortLocus(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestCockpitSurfaceWorstCasePlanAtAllWidths(t *testing.T) {
	widths := []int{80, 100, 120}

	for _, w := range widths {
		t.Run("Width_"+string(rune('0'+w/100))+string(rune('0'+(w/10)%10))+string(rune('0'+w%10)), func(t *testing.T) {
			d, ops := connectFixture(t)
			ops.clients = allSixClients()

			surface := &fullWorkspaceSurface{Disclosure: d, Slug: "dev"}
			h, err := testkit.NewHarnessAt(surface, w, 32)
			if err != nil {
				t.Fatal(err)
			}
			h.Open()
			defer h.Close()

			// Open endpoint and choose Codex CLI to expand worst-case Plan
			d.toggleEndpoint()
			waitFor(t, func() bool {
				h.Frame()
				return len(d.Observations.Get()) > 0
			})
			d.chooseClient(clientconnect.ClientCodex)
			waitFor(t, func() bool {
				h.Frame()
				return d.Child.Get().isClient(clientconnect.ClientCodex)
			})
			h.App().MarkDirty()

			frame := h.FrameTrimmed()
			lines := strings.Split(frame, "\n")
			for i, line := range lines {
				if len([]rune(line)) > w {
					t.Fatalf("line %d exceeded %d columns (%d chars):\n%s", i, w, len([]rune(line)), line)
				}
			}

			// 1. Header and footer present
			for _, want := range []string{"⛉ SWOBU", "[› dev]", "healthy", "select · enter activate · esc back · q exit"} {
				if !strings.Contains(frame, want) {
					t.Fatalf("surface missing header/footer element %q:\n%s", want, frame)
				}
			}

			// 2. Persisted workspace identity & delete
			for _, want := range []string{"workspace ▾", "name", "dev", "delete", "delete ↵"} {
				if !strings.Contains(frame, want) {
					t.Fatalf("surface missing workspace element %q:\n%s", want, frame)
				}
			}

			// 3. Expanded Plan under Codex CLI
			for _, want := range []string{"Codex CLI", "close ↵", "backend", "openai → swobu", "base URL", "model", "config", "replace ↵"} {
				if !strings.Contains(frame, want) {
					t.Fatalf("surface missing plan element %q:\n%s", want, frame)
				}
			}

			// 4. All sibling client rows remain visible in the browse list
			for _, want := range []string{"Claude Code", "configured ↵", "Kilo Code", "configure ↵", "pi", "OpenClaw", "Hermes Agent", "Other clients", "setup ↵"} {
				if !strings.Contains(frame, want) {
					t.Fatalf("surface missing sibling client element %q:\n%s", want, frame)
				}
			}

			// 5. Routes and Activity sections remain present
			for _, want := range []string{"model routes ▾", "default", "1 target", "add model route", "activity ▾", "no requests yet"} {
				if !strings.Contains(frame, want) {
					t.Fatalf("surface missing routes/activity element %q:\n%s", want, frame)
				}
			}
		})
	}
}

func TestCockpitSurfaceOtherClientsManualSetupAtAllWidths(t *testing.T) {
	widths := []int{80, 100, 120}

	for _, w := range widths {
		t.Run("Width_"+string(rune('0'+w/100))+string(rune('0'+(w/10)%10))+string(rune('0'+w%10)), func(t *testing.T) {
			d, ops := connectFixture(t)
			ops.clients = allSixClients()

			surface := &fullWorkspaceSurface{Disclosure: d, Slug: "dev"}
			h, err := testkit.NewHarnessAt(surface, w, 24)
			if err != nil {
				t.Fatal(err)
			}
			h.Open()
			defer h.Close()

			// Open endpoint and open Other clients manual setup
			d.toggleEndpoint()
			waitFor(t, func() bool {
				h.Frame()
				return len(d.Observations.Get()) > 0
			})
			d.openManualSetup()
			h.App().MarkDirty()

			frame := h.FrameTrimmed()
			lines := strings.Split(frame, "\n")
			for i, line := range lines {
				if len([]rune(line)) > w {
					t.Fatalf("line %d exceeded %d columns (%d chars):\n%s", i, w, len([]rune(line)), line)
				}
			}

			// 1. Header and footer present
			for _, want := range []string{"⛉ SWOBU", "[› dev]", "healthy", "select · enter activate · esc back · q exit"} {
				if !strings.Contains(frame, want) {
					t.Fatalf("surface missing header/footer element %q:\n%s", want, frame)
				}
			}

			// 2. Persisted workspace identity & delete
			for _, want := range []string{"workspace ▾", "name", "dev", "delete", "delete ↵"} {
				if !strings.Contains(frame, want) {
					t.Fatalf("surface missing workspace element %q:\n%s", want, frame)
				}
			}

			// 3. Sibling client rows remain visible
			for _, want := range []string{"Codex CLI", "configure ↵", "Claude Code", "configured ↵", "Kilo Code", "pi", "OpenClaw", "Hermes Agent"} {
				if !strings.Contains(frame, want) {
					t.Fatalf("surface missing sibling client element %q:\n%s", want, frame)
				}
			}

			// 4. Manual setup child scope elements
			for _, want := range []string{"Other clients", "close ↵", "API", "OpenAI · Anthropic", "Base URL", "copy ↵", "Model", "default", "Models URL", "API key", "swobu · placeholder"} {
				if !strings.Contains(frame, want) {
					t.Fatalf("surface missing manual setup element %q:\n%s", want, frame)
				}
			}

			// 5. Routes and Activity sections remain present
			for _, want := range []string{"model routes ▾", "default", "1 target", "add model route", "activity ▾", "no requests yet"} {
				if !strings.Contains(frame, want) {
					t.Fatalf("surface missing routes/activity element %q:\n%s", want, frame)
				}
			}
		})
	}
}

func TestCockpitSurfaceStructuralStress60Columns(t *testing.T) {
	d, ops := connectFixture(t)
	ops.clients = allSixClients()

	surface := &fullWorkspaceSurface{Disclosure: d, Slug: "dev"}
	h, err := testkit.NewHarnessAt(surface, 60, 32)
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	// 1. Test stress with Plan open
	d.toggleEndpoint()
	waitFor(t, func() bool {
		h.Frame()
		return len(d.Observations.Get()) > 0
	})
	d.chooseClient(clientconnect.ClientCodex)
	waitFor(t, func() bool {
		h.Frame()
		return d.Child.Get().isClient(clientconnect.ClientCodex)
	})
	h.App().MarkDirty()

	frame := h.FrameTrimmed()
	for i, line := range strings.Split(frame, "\n") {
		if len([]rune(line)) > 60 {
			t.Fatalf("60-col plan stress line %d exceeded 60 columns (%d chars):\n%s", i, len([]rune(line)), line)
		}
	}
	for _, want := range []string{"Codex CLI", "close ↵", "config", "replace ↵", "Claude Code", "Other clients"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("60-col plan stress dropped essential action/label %q:\n%s", want, frame)
		}
	}

	// 2. Test stress with Manual Setup open
	d.openManualSetup()
	h.App().MarkDirty()

	frame = h.FrameTrimmed()
	for i, line := range strings.Split(frame, "\n") {
		if len([]rune(line)) > 60 {
			t.Fatalf("60-col manual setup stress line %d exceeded 60 columns (%d chars):\n%s", i, len([]rune(line)), line)
		}
	}
	for _, want := range []string{"Other clients", "close ↵", "Base URL", "copy ↵", "API key", "Codex CLI"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("60-col manual setup stress dropped essential action/label %q:\n%s", want, frame)
		}
	}
}

type fullWorkspaceSurface struct {
	Disclosure *Disclosure
	Slug       string
}

func (s *fullWorkspaceSurface) Render(app *ui.App) *ui.Element {
	root := ui.New(ui.WithDisplay(ui.DisplayFlex), ui.WithDirection(ui.Column), ui.WithWidthPercent(100))

	// Header
	header := ui.New(ui.WithDisplay(ui.DisplayFlex), ui.WithDirection(ui.Row), ui.WithWidthPercent(100))
	header.AddChild(ui.New(ui.WithText("⛉ SWOBU"), ui.WithWidth(9)))
	header.AddChild(ui.New(ui.WithText("[› "+s.Slug+"] [+] [?]"), ui.WithFlexGrow(1)))
	header.AddChild(ui.New(ui.WithText("healthy")))
	root.AddChild(header)

	// Body
	body := ui.New(ui.WithDisplay(ui.DisplayFlex), ui.WithDirection(ui.Column), ui.WithWidthPercent(100), ui.WithFlexGrow(1))

	// Overview Section Header & Rows
	body.AddChild(cockpitui.NewSelectableRow("section:overview", "workspace ▾", "", "", nil).Render(app))
	body.AddChild(cockpitui.NewSelectableRow("workspace:name", "name", s.Slug, "edit ↵", nil).Render(app))

	// Connect Disclosure
	body.AddChild(app.Mount(s, ui.MountKey(0, "workspace-connect:"+s.Slug), func() ui.Component {
		return s.Disclosure
	}))

	// Delete
	body.AddChild(cockpitui.NewSelectableRow("workspace:delete", "delete", "workspace", "delete ↵", nil).Render(app))

	// Routes Section
	body.AddChild(cockpitui.NewSelectableRow("section:routes", "model routes ▾", "", "", nil).Render(app))
	body.AddChild(cockpitui.NewSelectableRow("route:default", "default", "1 target", "edit ↵", nil).Render(app))
	body.AddChild(cockpitui.NewSelectableRow("route:add", "add model route", "", "add ↵", nil).Render(app))

	// Activity Section
	body.AddChild(cockpitui.NewSelectableRow("section:activity", "activity ▾", "", "", nil).Render(app))
	body.AddChild(ui.New(ui.WithText("  activity\n                                   no requests yet")))

	root.AddChild(body)

	// Footer
	footer := ui.New(ui.WithDisplay(ui.DisplayFlex), ui.WithDirection(ui.Row), ui.WithWidthPercent(100))
	footer.AddChild(ui.New(ui.WithText("↑/↓ select · enter activate · esc back · q exit")))
	root.AddChild(footer)

	return root
}

func (s *fullWorkspaceSurface) KeyMap() ui.KeyMap {
	keys := s.Disclosure.KeyMap()
	keys = append(keys,
		ui.OnStop(ui.KeyUp, func(event ui.KeyEvent) {
			if app := event.App(); app != nil {
				app.FocusPrev()
			}
		}),
		ui.OnStop(ui.KeyDown, func(event ui.KeyEvent) {
			if app := event.App(); app != nil {
				app.FocusNext()
			}
		}),
		ui.OnStop(ui.KeyEnter, cockpitui.ActivateCurrentSelection),
	)
	return keys
}

type connectRoot struct{ *Disclosure }

type remountRoot struct {
	key     string
	target  clientconnect.Target
	ops     *fakeOperations
	mounted *Disclosure
}

func (r *remountRoot) Render(app *ui.App) *ui.Element {
	root := ui.New(ui.WithDisplay(ui.DisplayFlex), ui.WithDirection(ui.Column), ui.WithWidthPercent(100))
	root.AddChild(app.Mount(r, ui.MountKey(0, r.key), func() ui.Component {
		if r.mounted == nil || r.mounted.Target.WorkspaceURL() != r.target.WorkspaceURL() {
			r.mounted = New(r.target, r.ops)
		}
		return r.mounted
	}))
	return root
}

func (r *connectRoot) KeyMap() ui.KeyMap {
	keys := r.Disclosure.KeyMap()
	keys = append(keys,
		ui.OnStop(ui.KeyUp, func(event ui.KeyEvent) {
			if app := event.App(); app != nil {
				app.FocusPrev()
			}
		}),
		ui.OnStop(ui.KeyDown, func(event ui.KeyEvent) {
			if app := event.App(); app != nil {
				app.FocusNext()
			}
		}),
		ui.OnStop(ui.KeyEnter, cockpitui.ActivateCurrentSelection),
	)
	return keys
}

func TestAsyncDiscoveryDeliversResultsWithoutBlockingMountedApp(t *testing.T) {
	d, ops := connectFixture(t)
	unblock := make(chan struct{})
	ops.discover = func() []clientconnect.Client {
		<-unblock
		return []clientconnect.Client{{ID: clientconnect.ClientCodex, Name: "Codex CLI"}}
	}

	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	d.toggleEndpoint()
	if !d.EndpointOpen.Get() {
		t.Fatal("endpoint was not opened immediately")
	}
	if len(d.Observations.Get()) != 0 {
		t.Fatalf("observations populated before discovery unblocked: %#v", d.Observations.Get())
	}

	// Pre-unblock frame must contain 'finding installed clients…', 'wait', and 'Other clients'
	frame := h.Frame()
	if !strings.Contains(frame, "finding installed clients…") || !strings.Contains(frame, "wait") || !strings.Contains(frame, "Other clients") {
		t.Fatalf("expected pre-unblock finding installed clients frame:\n%s", frame)
	}

	close(unblock)
	waitFor(t, func() bool {
		h.Frame()
		return len(d.Observations.Get()) == 1
	})
	if observationFor(t, d.Observations.Get(), clientconnect.ClientCodex).Client.Name != "Codex CLI" {
		t.Fatalf("unexpected discovered client: %#v", d.Observations.Get())
	}
}

func TestAsyncDiscoveryStaleResultIgnoredOnRapidReopen(t *testing.T) {
	d, ops := connectFixture(t)
	firstStarted := make(chan struct{})
	firstUnblock := make(chan struct{})
	callCount := 0
	ops.discover = func() []clientconnect.Client {
		callCount++
		if callCount == 1 {
			close(firstStarted)
			<-firstUnblock
			return []clientconnect.Client{{ID: clientconnect.ClientClaude, Name: "Stale First"}}
		}
		return []clientconnect.Client{{ID: clientconnect.ClientCodex, Name: "Fresh Second"}}
	}

	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	// 1. First open (slow)
	d.toggleEndpoint()
	<-firstStarted

	// 2. Close and reopen (fresh)
	d.toggleEndpoint()
	d.toggleEndpoint()

	// Wait for fresh second discovery
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) == 1 && observationFor(t, obs, clientconnect.ClientCodex).Client.Name == "Fresh Second"
	})

	// 3. Unblock first discovery (must be ignored due to endpointGeneration)
	close(firstUnblock)
	h.Frame()
	time.Sleep(10 * time.Millisecond)
	h.Frame()

	obs := d.Observations.Get()
	if len(obs) != 1 || observationFor(t, obs, clientconnect.ClientCodex).Client.Name != "Fresh Second" {
		t.Fatalf("stale discovery overwritten fresh result: %#v", obs)
	}
}

func TestAsyncPlanInspectionCompletesAndUpdatesParentRowAfterChildScopeClose(t *testing.T) {
	d, ops := connectFixture(t)
	ops.clients = []clientconnect.Client{{ID: clientconnect.ClientCodex, Name: "Codex CLI"}}
	planUnblock := make(chan struct{})

	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	// Wrap ops.plans with blocking behavior
	customOps := &delayedPlanOperations{
		fakeOperations: ops,
		planUnblock:    planUnblock,
	}
	d.Ops = customOps

	d.toggleEndpoint()
	waitFor(t, func() bool {
		h.Frame()
		return len(d.Observations.Get()) == 1
	})

	d.chooseClient(clientconnect.ClientCodex)

	// While Plan inspection is in flight, user backs out to parent browse list
	d.Back()
	if d.Child.Get().kind != childNone {
		t.Fatalf("child scope not closed on Back: %#v", d.Child.Get())
	}

	// Unblock delayed plan: according to RFC §17, closing child scope does not cancel in-flight inspection;
	// completion updates the parent row's observation state.
	close(planUnblock)
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) == 1 && observationFor(t, obs, clientconnect.ClientCodex).Kind == observationNeedsChange
	})

	// Child scope remains closed, and parent row displays configure ↵
	if d.Child.Get().kind != childNone {
		t.Fatalf("child scope opened unexpectedly: %#v", d.Child.Get())
	}
	frame := h.Frame()
	if !strings.Contains(frame, "Codex CLI") || !strings.Contains(frame, "configure ↵") {
		t.Fatalf("parent row not updated with inspection result:\n%s", frame)
	}
}

type delayedPlanOperations struct {
	*fakeOperations
	planUnblock chan struct{}
}

func (d *delayedPlanOperations) Plan(id clientconnect.ClientID, target clientconnect.Target) (clientconnect.Plan, error) {
	<-d.planUnblock
	return d.fakeOperations.Plan(id, target)
}

func TestBlockedApplyKeepsEventLoopResponsiveWhileGatingDuplicateMutation(t *testing.T) {
	d, ops := connectFixture(t)
	ops.clients = []clientconnect.Client{{ID: clientconnect.ClientCodex, Name: "Codex CLI"}}
	ops.plans = map[clientconnect.ClientID]clientconnect.Plan{
		clientconnect.ClientCodex: {
			ClientID:   clientconnect.ClientCodex,
			ClientName: "Codex CLI",
			ConfigPath: "/tmp/codex.toml",
			Target:     connectTarget(t),
			Changes: []clientconnect.Change{
				{Field: "backend", BeforeExists: false, After: "swobu"},
			},
		},
	}

	applyUnblock := make(chan struct{})
	var applyCalls atomic.Int32
	delayedOps := &delayedApplyOperations{
		fakeOperations: ops,
		applyUnblock:   applyUnblock,
		onApply:        func() { applyCalls.Add(1) },
	}
	d.Ops = delayedOps

	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	d.toggleEndpoint()
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) == 1 && observationFor(t, obs, clientconnect.ClientCodex).Kind == observationNeedsChange
	})

	d.chooseClient(clientconnect.ClientCodex)
	waitFor(t, func() bool {
		h.Frame()
		return d.Child.Get().isClient(clientconnect.ClientCodex)
	})

	// Reset discover call counter before Apply
	ops.discoverCalls = 0

	// 1. Press Apply: UI immediately sets Applying and renders "configuring…"
	d.applyPlan(clientconnect.ClientCodex)
	if !observationFor(t, d.Observations.Get(), clientconnect.ClientCodex).Applying {
		t.Fatal("applyPlan did not set Applying on observation")
	}
	inFlightFrame := h.Frame()
	if !strings.Contains(inFlightFrame, "configuring…") {
		t.Fatalf("expected configuring… during in-flight apply:\n%s", inFlightFrame)
	}

	// 2. While Apply is blocked: TUI event loop is fully responsive to navigation & rendering
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyDown})
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyUp})
	renderedFrame := h.Frame()
	if !strings.Contains(renderedFrame, "Codex CLI") || !strings.Contains(renderedFrame, "configuring…") {
		t.Fatalf("TUI rendering broken while apply is blocked:\n%s", renderedFrame)
	}

	// 3. Duplicate Apply attempt while in flight is gated and does nothing
	d.applyPlan(clientconnect.ClientCodex)
	if got := applyCalls.Load(); got != 1 {
		t.Fatalf("duplicate Apply was not gated: applyCalls = %d, want 1", got)
	}

	// 4. Unblock Apply: finishes and updates observation to observationMatch
	close(applyUnblock)

	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		if len(obs) != 1 {
			return false
		}
		cObs := observationFor(t, obs, clientconnect.ClientCodex)
		return !cObs.Applying && cObs.Kind == observationMatch && d.Child.Get().kind == childNone
	})

	if got := applyCalls.Load(); got != 1 {
		t.Fatalf("applyCalls = %d, want 1", got)
	}
	// Zero post-Apply Discover calls required by Connect RFC
	if ops.discoverCalls != 0 {
		t.Fatalf("Apply invoked Discover %d times; want 0 post-Apply Discover calls", ops.discoverCalls)
	}
	obs := d.Observations.Get()
	if len(obs) != 1 || observationFor(t, obs, clientconnect.ClientCodex).Kind != observationMatch {
		t.Fatalf("client observation was not updated to observationMatch: %#v", obs)
	}
}

type delayedApplyOperations struct {
	*fakeOperations
	applyUnblock chan struct{}
	onApply      func()
}

func (d *delayedApplyOperations) Apply(plan clientconnect.Plan) error {
	if d.onApply != nil {
		d.onApply()
	}
	<-d.applyUnblock
	return d.fakeOperations.Apply(plan)
}

func TestDisclosurePlanErrorOpensClientChildScopeAndEscCloses(t *testing.T) {
	d, ops := connectFixture(t)
	ops.clients = []clientconnect.Client{
		{ID: clientconnect.ClientCodex, Name: "Codex CLI"},
		{ID: clientconnect.ClientOpenClaw, Name: "OpenClaw"},
	}
	delete(ops.plans, clientconnect.ClientOpenClaw)

	d.toggleEndpoint()
	waitFor(t, func() bool {
		return len(d.Observations.Get()) == 2
	})
	d.chooseClient(clientconnect.ClientOpenClaw) // OpenClaw, which has no plan in ops.plans -> fails

	if !d.Child.Get().isClient(clientconnect.ClientOpenClaw) {
		t.Fatalf("expected childClient for OpenClaw, got %#v", d.Child.Get())
	}

	frame := testkit.RenderMountedTrimmed(t, d, 100, 20)
	if !strings.Contains(frame, "OpenClaw") || !strings.Contains(frame, "close ↵") {
		t.Fatalf("expected OpenClaw close ↵ header:\n%s", frame)
	}
	if !strings.Contains(frame, "plan failed. Nothing changed.") {
		t.Fatalf("expected error detail in frame:\n%s", frame)
	}
	openClawIdx := strings.Index(frame, "OpenClaw")
	errIdx := strings.Index(frame, "plan failed")
	otherIdx := strings.Index(frame, "Other clients")
	if !(openClawIdx < errIdx && errIdx < otherIdx) {
		t.Fatalf("layout ordering violated: openclaw=%d, err=%d, other=%d\nFrame:\n%s", openClawIdx, errIdx, otherIdx, frame)
	}

	// Back / Esc closes the error scope
	if !d.Back() {
		t.Fatal("Back() returned false on error child scope")
	}
	if d.Child.Get().kind != childNone {
		t.Fatalf("expected childNone after Back, got %#v", d.Child.Get())
	}
	frameAfterBack := testkit.RenderMountedTrimmed(t, d, 100, 20)
	if strings.Contains(frameAfterBack, "plan failed") {
		t.Fatalf("error remained after Back():\n%s", frameAfterBack)
	}
	if !strings.Contains(frameAfterBack, "OpenClaw") || !strings.Contains(frameAfterBack, "retry ↵") {
		t.Fatalf("OpenClaw should show retry ↵ after Back():\n%s", frameAfterBack)
	}
}

func TestDetailRowMultilineSemanticAlignment(t *testing.T) {
	errText := "OpenClaw is not automatically wireable: SyntaxError: Unexpected token.\nNothing changed."
	rendered := testkit.RenderMountedTrimmed(t, DetailRow(errText), 100, 4)
	lines := strings.Split(rendered, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d:\n%s", len(lines), rendered)
	}
	for i := 0; i < 2; i++ {
		line := lines[i]
		if !strings.HasPrefix(line, strings.Repeat(" ", 20)) {
			t.Fatalf("line %d not aligned to column 20: %q", i, line)
		}
	}
}

func TestDetailRowWordWrapping(t *testing.T) {
	longError := "OpenClaw is not automatically wireable: Config path not found: agents.defaults.model.primary. Nothing changed."
	rendered := testkit.RenderMountedTrimmed(t, DetailRow(longError), 80, 6)
	lines := strings.Split(rendered, "\n")
	nonEmpty := 0
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		nonEmpty++
		if !strings.HasPrefix(line, strings.Repeat(" ", 20)) {
			t.Fatalf("line %d not aligned to column 20: %q", i, line)
		}
		if len(line) > 80 {
			t.Fatalf("line %d exceeded width 80 (got %d chars): %q", i, len(line), line)
		}
	}
	if nonEmpty < 2 {
		t.Fatalf("expected at least 2 wrapped lines, got %d:\n%s", nonEmpty, rendered)
	}
	if !strings.Contains(rendered, "Nothing") || !strings.Contains(rendered, "changed.") {
		t.Fatalf("expected full content to survive word wrapping:\n%s", rendered)
	}
}

func TestInFlightCheckingClientCanBeOpenedAndRetainsSinglePlanCall(t *testing.T) {
	d, ops := connectFixture(t)
	ops.clients = []clientconnect.Client{{ID: clientconnect.ClientOpenClaw, Name: "OpenClaw"}}
	planUnblock := make(chan struct{})
	var planCalls atomic.Int32

	customOps := &countingDelayedPlanOps{
		fakeOperations: ops,
		planUnblock:    planUnblock,
		onPlan:         func() { planCalls.Add(1) },
	}
	d.Ops = customOps

	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	d.toggleEndpoint()
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) == 1 && observationFor(t, obs, clientconnect.ClientOpenClaw).Kind == observationChecking
	})

	if got := planCalls.Load(); got != 1 {
		t.Fatalf("planCalls after discovery = %d, want 1", got)
	}

	// While checking, activate the checking client row
	d.chooseClient(clientconnect.ClientOpenClaw)

	// Child scope opened immediately
	if !d.Child.Get().isClient(clientconnect.ClientOpenClaw) {
		t.Fatalf("child scope not opened for in-flight client: %#v", d.Child.Get())
	}

	// Plan call count must remain 1 (no duplicate Plan launched)
	if got := planCalls.Load(); got != 1 {
		t.Fatalf("planCalls after chooseClient = %d, want 1", got)
	}

	// Child view presents "checking configuration…" and "wait"
	frame := h.Frame()
	for _, want := range []string{"OpenClaw", "close ↵", "checking configuration…", "wait"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("in-flight child frame missing %q:\n%s", want, frame)
		}
	}

	// Unblock Plan inspection
	close(planUnblock)
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) == 1 && observationFor(t, obs, clientconnect.ClientOpenClaw).Kind == observationNeedsChange
	})

	// Frame transitions smoothly to complete Plan view
	planFrame := h.Frame()
	for _, want := range []string{"OpenClaw", "close ↵", "endpoint", "config", "apply ↵"} {
		if !strings.Contains(planFrame, want) {
			t.Fatalf("completed plan frame missing %q:\n%s", want, planFrame)
		}
	}
}

type countingDelayedPlanOps struct {
	*fakeOperations
	planUnblock chan struct{}
	onPlan      func()
}

func (c *countingDelayedPlanOps) Plan(id clientconnect.ClientID, target clientconnect.Target) (clientconnect.Plan, error) {
	if c.onPlan != nil {
		c.onPlan()
	}
	<-c.planUnblock
	return c.fakeOperations.Plan(id, target)
}

func TestApplyingParentRowStateAndSelectiveChildScopeClose(t *testing.T) {
	d, ops := connectFixture(t)
	ops.clients = []clientconnect.Client{
		{ID: clientconnect.ClientCodex, Name: "Codex CLI"},
		{ID: clientconnect.ClientOpenClaw, Name: "OpenClaw"},
	}

	applyUnblock := make(chan struct{})
	applyCalls := 0
	delayedOps := &delayedApplyOperations{
		fakeOperations: ops,
		applyUnblock:   applyUnblock,
		onApply:        func() { applyCalls++ },
	}
	d.Ops = delayedOps

	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	d.toggleEndpoint()
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) == 2 && observationFor(t, obs, clientconnect.ClientCodex).Kind == observationNeedsChange
	})

	// Open Codex CLI plan and launch Apply
	d.chooseClient(clientconnect.ClientCodex)
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return d.Child.Get().isClient(clientconnect.ClientCodex) && observationFor(t, obs, clientconnect.ClientCodex).Kind == observationNeedsChange
	})
	d.applyPlan(clientconnect.ClientCodex)

	if !observationFor(t, d.Observations.Get(), clientconnect.ClientCodex).Applying {
		t.Fatal("Codex CLI observation not marked Applying")
	}

	// User closes child scope with Back / Esc
	d.Back()
	if d.Child.Get().kind != childNone {
		t.Fatalf("expected childNone after Back, got %#v", d.Child.Get())
	}

	// Parent browse list renders "configuring…" for Codex CLI
	browseFrame := h.Frame()
	if !strings.Contains(browseFrame, "Codex CLI") || !strings.Contains(browseFrame, "configuring…") {
		t.Fatalf("parent browse list did not show configuring…:\n%s", browseFrame)
	}

	// User opens Other clients manual setup child while Codex is still applying
	d.openManualSetup()
	if !d.Child.Get().isManual() {
		t.Fatalf("failed to open manual setup: %#v", d.Child.Get())
	}

	// Unblock Codex Apply
	close(applyUnblock)

	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) == 2 && !observationFor(t, obs, clientconnect.ClientCodex).Applying && observationFor(t, obs, clientconnect.ClientCodex).Kind == observationMatch
	})

	// Apply completion must NOT close the unrelated (manual setup) child scope!
	if !d.Child.Get().isManual() {
		t.Fatalf("apply completion closed unrelated child scope: %#v", d.Child.Get())
	}

	// Codex CLI is now observationMatch
	if observationFor(t, d.Observations.Get(), clientconnect.ClientCodex).Kind != observationMatch {
		t.Fatalf("Codex CLI observation = %v, want observationMatch", observationFor(t, d.Observations.Get(), clientconnect.ClientCodex).Kind)
	}
}

func TestSlowClientInspectionDoesNotDelayFastClientObservation(t *testing.T) {
	d, ops := connectFixture(t)
	ops.clients = []clientconnect.Client{
		{ID: clientconnect.ClientCodex, Name: "Codex CLI"},
		{ID: clientconnect.ClientOpenClaw, Name: "OpenClaw"},
	}

	openClawUnblock := make(chan struct{})
	d.Ops = &selectiveDelayedPlanOps{
		fakeOperations: ops,
		delayFor:       clientconnect.ClientOpenClaw,
		unblock:        openClawUnblock,
	}

	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	d.toggleEndpoint()

	// Wait for Codex CLI inspection to complete and show configure ↵,
	// while OpenClaw is still blocked in checking…
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) == 2 && observationFor(t, obs, clientconnect.ClientCodex).Kind == observationNeedsChange && observationFor(t, obs, clientconnect.ClientOpenClaw).Kind == observationChecking
	})

	frame := h.Frame()
	if !strings.Contains(frame, "Codex CLI") || !strings.Contains(frame, "configure ↵") {
		t.Fatalf("fast client not resolved before slow client:\n%s", frame)
	}
	if !strings.Contains(frame, "OpenClaw") || !strings.Contains(frame, "checking…") {
		t.Fatalf("slow client not showing checking…:\n%s", frame)
	}

	// Unblock OpenClaw
	close(openClawUnblock)
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) == 2 && observationFor(t, obs, clientconnect.ClientOpenClaw).Kind == observationNeedsChange
	})
}

type selectiveDelayedPlanOps struct {
	*fakeOperations
	delayFor clientconnect.ClientID
	unblock  chan struct{}
}

func (s *selectiveDelayedPlanOps) Plan(id clientconnect.ClientID, target clientconnect.Target) (clientconnect.Plan, error) {
	if id == s.delayFor {
		<-s.unblock
	}
	return s.fakeOperations.Plan(id, target)
}

func TestConfiguredClientRemainsActionable(t *testing.T) {
	d, ops := connectFixture(t)
	ops.clients = []clientconnect.Client{{ID: clientconnect.ClientPi, Name: "pi"}}

	plan2Unblock := make(chan struct{})
	var planCalls atomic.Int32
	d.Ops = &callCountingPlanOps{
		fakeOperations: ops,
		onPlan: func(id clientconnect.ClientID) {
			if planCalls.Add(1) == 2 {
				<-plan2Unblock
			}
		},
	}

	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	d.toggleEndpoint()
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) == 1 && observationFor(t, obs, clientconnect.ClientPi).Kind == observationMatch
	})

	if got := planCalls.Load(); got != 1 {
		t.Fatalf("expected 1 initial planCall, got %d", got)
	}

	// Pi is rendered as configured ↵ in resting browse
	frame := h.Frame()
	if !strings.Contains(frame, "pi") || !strings.Contains(frame, "configured ↵") {
		t.Fatalf("expected configured ↵:\n%s", frame)
	}

	// Activating the configured row initiates fresh Plan
	d.chooseClient(clientconnect.ClientPi)

	// Child is opened immediately, fresh inspection is launched (planCalls == 2),
	// and UI visibly renders "checking configuration…" and "wait"
	if !d.Child.Get().isClient(clientconnect.ClientPi) {
		t.Fatalf("chooseClient on configured client failed to open child: %#v", d.Child.Get())
	}
	waitFor(t, func() bool {
		return planCalls.Load() == 2
	})
	inFlightFrame := h.Frame()
	for _, want := range []string{"pi", "close ↵", "checking configuration…", "wait"} {
		if !strings.Contains(inFlightFrame, want) {
			t.Fatalf("in-flight child frame missing %q:\n%s", want, inFlightFrame)
		}
	}

	// Unblock the second Plan
	close(plan2Unblock)
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) == 1 && observationFor(t, obs, clientconnect.ClientPi).Kind == observationMatch
	})

	// Completed child frame renders status current
	childFrame := h.Frame()
	if !strings.Contains(childFrame, "pi") || !strings.Contains(childFrame, "close ↵") || !strings.Contains(childFrame, "status") || !strings.Contains(childFrame, "current") {
		t.Fatalf("expected child frame for configured client:\n%s", childFrame)
	}
}

type callCountingPlanOps struct {
	*fakeOperations
	onPlan func(id clientconnect.ClientID)
}

func (c *callCountingPlanOps) Plan(id clientconnect.ClientID, target clientconnect.Target) (clientconnect.Plan, error) {
	if c.onPlan != nil {
		c.onPlan(id)
	}
	return c.fakeOperations.Plan(id, target)
}

func TestConfiguredObservationRefreshesToNeedsChangeWithoutRestart(t *testing.T) {
	d, ops := connectFixture(t)
	ops.clients = []clientconnect.Client{{ID: clientconnect.ClientPi, Name: "pi"}}

	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	// 1. Initial state: Pi matches Swobu
	d.toggleEndpoint()
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) == 1 && observationFor(t, obs, clientconnect.ClientPi).Kind == observationMatch
	})
	if !strings.Contains(h.Frame(), "configured ↵") {
		t.Fatalf("expected configured ↵:\n%s", h.Frame())
	}

	// 2. External change: Pi configuration is modified/deleted outside Swobu
	ops.plans[clientconnect.ClientPi] = clientconnect.Plan{
		ClientID:   clientconnect.ClientPi,
		ClientName: "pi",
		ConfigPath: "/tmp/.pi/config.toml",
		Target:     connectTarget(t),
		Changes: []clientconnect.Change{
			{Field: "backend", BeforeExists: false, After: "swobu"},
		},
	}

	// 3. Without restart or closing endpoint, activate Pi
	d.chooseClient(clientconnect.ClientPi)

	// 4. Fresh inspection runs and presents new Plan
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) == 1 && observationFor(t, obs, clientconnect.ClientPi).Kind == observationNeedsChange && d.Child.Get().isClient(clientconnect.ClientPi)
	})

	frame := h.Frame()
	if !strings.Contains(frame, "backend") || !strings.Contains(frame, "apply ↵") {
		t.Fatalf("recovered frame missing plan changes:\n%s", frame)
	}
}

func TestRealPiConfigurationExternalDeletionAndRecovery(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	binDir := filepath.Join(tempHome, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "pi"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	piDir := filepath.Join(tempHome, ".pi", "agent")
	if err := os.MkdirAll(piDir, 0755); err != nil {
		t.Fatal(err)
	}

	target := connectTarget(t)

	// Initial matching Pi settings & models files
	settingsJSON := `{"defaultProvider": "swobu", "defaultModel": "default"}`
	if err := os.WriteFile(filepath.Join(piDir, "settings.json"), []byte(settingsJSON), 0644); err != nil {
		t.Fatal(err)
	}
	modelsJSON := `{"providers": {"swobu": {"baseUrl": "` + target.WorkspaceURL() + `", "api": "openai-completions", "apiKey": "swobu", "models": [{"id": "default", "name": "Swobu default"}]}}}`
	if err := os.WriteFile(filepath.Join(piDir, "models.json"), []byte(modelsJSON), 0644); err != nil {
		t.Fatal(err)
	}

	svc := clientconnect.NewService()

	d := New(target, svc)
	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	// 1. Initial inspection: Pi matches Swobu -> configured ↵
	d.toggleEndpoint()
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) >= 1 && observationFor(t, obs, clientconnect.ClientPi).Kind == observationMatch
	})
	if !strings.Contains(h.Frame(), "configured ↵") {
		t.Fatalf("expected configured ↵:\n%s", h.Frame())
	}

	// 2. Real external deletion on disk: remove models.json
	if err := os.Remove(filepath.Join(piDir, "models.json")); err != nil {
		t.Fatal(err)
	}

	// 3. Activate Pi without restart or reopening endpoint
	d.chooseClient(clientconnect.ClientPi)

	// 4. Fresh Plan inspection runs against real disk -> detects missing provider -> needs change
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) >= 1 && observationFor(t, obs, clientconnect.ClientPi).Kind == observationNeedsChange && d.Child.Get().isClient(clientconnect.ClientPi)
	})

	childFrame := h.Frame()
	if !strings.Contains(childFrame, ".pi") || !strings.Contains(childFrame, "apply ↵") {
		t.Fatalf("recovered frame missing real plan changes:\n%s", childFrame)
	}

	// 5. Apply the plan to repair the deleted configuration
	d.applyPlan(clientconnect.ClientPi)
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) >= 1 && observationFor(t, obs, clientconnect.ClientPi).Kind == observationMatch && d.Child.Get().kind == childNone
	})

	// 6. Verify file restored on disk
	restoredModels, err := os.ReadFile(filepath.Join(piDir, "models.json"))
	if err != nil {
		t.Fatalf("failed to read restored models.json: %v", err)
	}
	if !strings.Contains(string(restoredModels), target.WorkspaceURL()) {
		t.Fatalf("restored models.json missing target workspace URL:\n%s", string(restoredModels))
	}
}

func TestRealPiConfigurationCreatesMissingGlobalFiles(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	binDir := filepath.Join(tempHome, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "pi"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	target := connectTarget(t)
	d := New(target, clientconnect.NewService())
	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	d.toggleEndpoint()
	waitFor(t, func() bool {
		h.Frame()
		observations := d.Observations.Get()
		return len(observations) >= 1 && observationFor(t, observations, clientconnect.ClientPi).Kind != observationChecking
	})
	observation := observationFor(t, d.Observations.Get(), clientconnect.ClientPi)
	if observation.Kind != observationNeedsChange {
		t.Fatalf("Pi observation = %#v\n%s", observation, h.Frame())
	}
	d.chooseClient(clientconnect.ClientPi)
	waitFor(t, func() bool {
		h.Frame()
		return d.Child.Get().isClient(clientconnect.ClientPi)
	})
	observation = observationFor(t, d.Observations.Get(), clientconnect.ClientPi)
	if frame := h.Frame(); !strings.Contains(frame, "apply ↵") || !strings.Contains(observation.Plan.ConfigPath, "settings.json") || !strings.Contains(observation.Plan.ConfigPath, "models.json") {
		t.Fatalf("fresh Pi plan missing:\n%s", frame)
	}
	d.applyPlan(clientconnect.ClientPi)
	waitFor(t, func() bool {
		h.Frame()
		observations := d.Observations.Get()
		return len(observations) >= 1 && observationFor(t, observations, clientconnect.ClientPi).Kind == observationMatch
	})
	for _, name := range []string{"settings.json", "models.json"} {
		if _, err := os.Stat(filepath.Join(tempHome, ".pi", "agent", name)); err != nil {
			t.Fatalf("%s not created: %v", name, err)
		}
	}
}

func TestStaleRenderedCallbackActivationReusesInFlightInspectionAndGatesDuplicatePlan(t *testing.T) {
	d, ops := connectFixture(t)
	ops.clients = []clientconnect.Client{{ID: clientconnect.ClientPi, Name: "pi"}}

	var planCalls atomic.Int32
	var blocker chan struct{}
	countingOps := &callCountingPlanOps{
		fakeOperations: ops,
		onPlan: func(id clientconnect.ClientID) {
			if planCalls.Add(1) == 2 && blocker != nil {
				<-blocker
			}
		},
	}
	d.Ops = countingOps

	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	// 1. Initial discovery & Plan (call 1) -> Match
	d.toggleEndpoint()
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) == 1 && observationFor(t, obs, clientconnect.ClientPi).Kind == observationMatch
	})

	if got := planCalls.Load(); got != 1 {
		t.Fatalf("expected 1 initial Plan call, got %d", got)
	}

	// Block the 2nd Plan call in flight
	blocker = make(chan struct{})

	// 2. First activation starts Plan 2
	d.chooseClient(clientconnect.ClientPi)

	// Wait until Plan 2 is in flight
	waitFor(t, func() bool {
		return planCalls.Load() == 2
	})

	// 3. Second activation with the SAME client ID while in flight before rerender
	d.chooseClient(clientconnect.ClientPi)

	// 4. Verify total Plan calls is still 2 (initial + exactly one refresh)
	if got := planCalls.Load(); got != 2 {
		t.Fatalf("expected 2 total Plan calls (1 initial + 1 refresh), got %d", got)
	}

	// Unblock Plan 2 and let it resolve
	close(blocker)
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) == 1 && observationFor(t, obs, clientconnect.ClientPi).Kind == observationMatch
	})

	if got := planCalls.Load(); got != 2 {
		t.Fatalf("expected planCalls to remain 2 after completion, got %d", got)
	}
}

func TestChooseClientWithAbsentClientIDDoesNotSetChildScopeOrLaunchPlan(t *testing.T) {
	d, ops := connectFixture(t)
	ops.clients = []clientconnect.Client{{ID: clientconnect.ClientPi, Name: "pi"}}

	var planCalls int
	d.Ops = &callCountingPlanOps{
		fakeOperations: ops,
		onPlan: func(id clientconnect.ClientID) {
			planCalls++
		},
	}

	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	d.toggleEndpoint()
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return len(obs) == 1 && observationFor(t, obs, clientconnect.ClientPi).Kind == observationMatch
	})

	if planCalls != 1 {
		t.Fatalf("expected 1 initial Plan call, got %d", planCalls)
	}

	// Try activating an obsolete client ID that is not present in observations
	d.chooseClient("non-existent-client")

	// Child scope must NOT be opened, and no plan launched
	if d.Child.Get().kind != childNone {
		t.Fatalf("child scope opened for absent client: %#v", d.Child.Get())
	}
	if planCalls != 1 {
		t.Fatalf("plan launched for absent client: planCalls = %d, want 1", planCalls)
	}
}

func TestOtherClientsManualSetupDoesNotCancelDiscovery(t *testing.T) {
	d, ops := connectFixture(t)
	unblock := make(chan struct{})
	ops.discover = func() []clientconnect.Client {
		<-unblock
		return []clientconnect.Client{{ID: clientconnect.ClientCodex, Name: "Codex CLI"}}
	}

	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	// 1. Open endpoint (discovery starts and is blocked)
	d.toggleEndpoint()
	if !d.DiscoveryPending.Get() {
		t.Fatal("expected DiscoveryPending")
	}

	// 2. Open Other clients manual setup while discovery is in flight
	d.openManualSetup()
	if !d.Child.Get().isManual() {
		t.Fatalf("manual setup not open: %#v", d.Child.Get())
	}

	// 3. Complete discovery while manual setup is still open
	close(unblock)
	waitFor(t, func() bool {
		h.Frame()
		obs := d.Observations.Get()
		return !d.DiscoveryPending.Get() && len(obs) == 1 && observationFor(t, obs, clientconnect.ClientCodex).Kind == observationNeedsChange
	})

	// 4. Close manual setup -> discovered clients are present in browse list without reopening endpoint
	d.closeChildScope()
	frame := h.Frame()
	if !strings.Contains(frame, "Codex CLI") || !strings.Contains(frame, "configure ↵") {
		t.Fatalf("discovered client missing after manual setup closed:\n%s", frame)
	}
}
