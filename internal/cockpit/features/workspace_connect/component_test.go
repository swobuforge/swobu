package workspace_connect

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	cockpitui "github.com/swobuforge/swobu/internal/cockpit/ui"
)

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
		{ID: clientconnect.ClientClaude, Name: "Claude Code", Configured: true},
		{ID: clientconnect.ClientKilo, Name: "Kilo Code"},
		{ID: clientconnect.ClientPi, Name: "pi", Configured: true},
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
		},
	}
	d := New(target, ops)
	return d, ops
}

func TestDiscoveryRunsOnlyOnDeliberateOpenAndReopen(t *testing.T) {
	configured := false
	ops := &fakeOperations{discover: func() []clientconnect.Client {
		return []clientconnect.Client{{ID: clientconnect.ClientCodex, Name: "Codex CLI", Configured: configured}}
	}}
	d := New(connectTarget(t), ops)
	_ = testkit.RenderMountedTrimmed(t, d, 100, 10)
	if ops.discoverCalls != 0 {
		t.Fatalf("collapsed mount discovery calls = %d", ops.discoverCalls)
	}
	d.toggleEndpoint()
	if ops.discoverCalls != 1 || d.Clients.Get()[0].Configured {
		t.Fatalf("first open = calls %d clients %#v", ops.discoverCalls, d.Clients.Get())
	}
	d.toggleEndpoint()
	if ops.discoverCalls != 1 {
		t.Fatalf("close discovery calls = %d", ops.discoverCalls)
	}
	configured = true
	d.toggleEndpoint()
	if ops.discoverCalls != 2 || !d.Clients.Get()[0].Configured {
		t.Fatalf("reopen = calls %d clients %#v", ops.discoverCalls, d.Clients.Get())
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
	for _, want := range []string{"Codex CLI", "configure ↵", "Claude Code", "configured", "Kilo Code", "pi", "Other clients", "setup ↵"} {
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
	d.chooseClient(d.Clients.Get()[0])
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

	// Make discovery return configured after apply
	ops.discover = func() []clientconnect.Client {
		return []clientconnect.Client{
			{ID: clientconnect.ClientCodex, Name: "Codex CLI", Configured: true},
			{ID: clientconnect.ClientClaude, Name: "Claude Code", Configured: true},
		}
	}

	d.applyPlan()
	if ops.applied.ClientID != clientconnect.ClientCodex {
		t.Fatalf("applied = %#v", ops.applied)
	}

	// Browse list returned with Codex CLI configured
	frame = testkit.RenderMountedTrimmed(t, d, 100, 20)
	if !strings.Contains(frame, "Codex CLI") || !strings.Contains(frame, "configured") || strings.Contains(frame, "/tmp/.codex/config.toml") {
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
			frame := testkit.RenderMountedTrimmed(t, PlanActionRow(d, plan), 100, 2)
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
	d.chooseClient(d.Clients.Get()[0])
	d.applyPlan()
	frame := testkit.RenderMountedTrimmed(t, d, 100, 22)
	if !strings.Contains(frame, "nothing was overwritten") || !strings.Contains(frame, "replace ↵") {
		t.Fatalf("error frame:\n%s", frame)
	}
	if !d.Child.Get().hasPlan(clientconnect.ClientCodex) {
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
	d.chooseClient(d.Clients.Get()[0])
	if !d.Child.Get().hasPlan(clientconnect.ClientCodex) {
		t.Fatal("chooseClient did not set plan")
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
	d, ops := connectFixture(t)
	// Start with unconfigured Codex CLI
	configured := false
	ops.discover = func() []clientconnect.Client {
		return []clientconnect.Client{
			{ID: clientconnect.ClientCodex, Name: "Codex CLI", Configured: configured},
		}
	}

	h, err := testkit.NewHarness(&connectRoot{Disclosure: d})
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	// 1. Open endpoint
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyDown})
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyEnter})

	// 2. Focus Codex CLI (first unconfigured client) and press Enter
	for i := 0; i < 5; i++ {
		h.DispatchKey(ui.KeyEvent{Key: ui.KeyDown})
		frame := h.Frame()
		if strings.Contains(frame, "> Codex CLI") {
			break
		}
	}
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyEnter})

	// Plan view is displayed
	frame := h.Frame()
	if !strings.Contains(frame, "config") || !strings.Contains(frame, "replace ↵") {
		t.Fatalf("expected plan view:\n%s", frame)
	}

	// After apply, discover will return configured: true
	configured = true

	// 3. Navigate to config row and press Enter (apply)
	for i := 0; i < 5; i++ {
		h.DispatchKey(ui.KeyEvent{Key: ui.KeyDown})
		frame = h.Frame()
		if strings.Contains(frame, "> config") {
			break
		}
	}
	h.DispatchKey(ui.KeyEvent{Key: ui.KeyEnter})

	// Returns to browse list, Codex CLI is now configured
	frame = h.Frame()
	if !strings.Contains(frame, "Codex CLI") || !strings.Contains(frame, "configured") {
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
	root.mounted.chooseClient(root.mounted.Clients.Get()[0])
	if !root.mounted.Child.Get().hasPlan(clientconnect.ClientCodex) || !root.mounted.EndpointOpen.Get() {
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
	if got := root.mounted.Clients.Get(); len(got) != 1 || got[0].Name != "Claude B" {
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
			d.chooseClient(d.Clients.Get()[0])
			planReplace := testkit.RenderMountedTrimmed(t, d, w, 20)
			if !strings.Contains(planReplace, "Codex CLI") || !strings.Contains(planReplace, "config") || !strings.Contains(planReplace, "replace ↵") {
				t.Fatalf("plan replace fixture failed at width %d:\n%s", w, planReplace)
			}

			// 4. Named-client additive Plan
			dAdditive, _ := connectFixture(t)
			dAdditive.toggleEndpoint()
			dAdditive.Child.Set(childScope{
				kind: childPlan,
				plan: clientconnect.Plan{
					ClientID:   clientconnect.ClientKilo,
					ClientName: "Kilo Code",
					ConfigPath: "/tmp/.kilo/config.json",
					Target:     connectTarget(t),
					Changes:    []clientconnect.Change{{Field: "endpoint", After: connectTarget(t).WorkspaceURL()}},
				},
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
			ops.plans = map[clientconnect.ClientID]clientconnect.Plan{
				clientconnect.ClientCodex: {
					ClientID:   clientconnect.ClientCodex,
					ClientName: "Codex CLI",
					ConfigPath: "/home/alice/.codex/config.toml",
					Target:     connectTarget(t),
					Changes: []clientconnect.Change{
						{Field: "backend", BeforeExists: true, Before: "openai", After: "swobu"},
						{Field: "base URL", BeforeExists: true, Before: "https://api.openai.com/v1", After: connectTarget(t).WorkspaceURL()},
						{Field: "model", BeforeExists: true, Before: "gpt-5-preview", After: "default"},
					},
				},
			}

			surface := &fullWorkspaceSurface{Disclosure: d, Slug: "dev"}
			h, err := testkit.NewHarnessAt(surface, w, 24)
			if err != nil {
				t.Fatal(err)
			}
			h.Open()
			defer h.Close()

			// Open endpoint and choose Codex CLI to expand worst-case Plan
			d.toggleEndpoint()
			d.chooseClient(d.Clients.Get()[0])
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
			for _, want := range []string{"Claude Code", "configured", "Kilo Code", "configure ↵", "pi", "OpenClaw", "Hermes Agent", "Other clients", "setup ↵"} {
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
			for _, want := range []string{"Codex CLI", "configure ↵", "Claude Code", "configured", "Kilo Code", "pi", "OpenClaw", "Hermes Agent"} {
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
	ops.plans = map[clientconnect.ClientID]clientconnect.Plan{
		clientconnect.ClientCodex: {
			ClientID:   clientconnect.ClientCodex,
			ClientName: "Codex CLI",
			ConfigPath: "/home/alice/.codex/config.toml",
			Target:     connectTarget(t),
			Changes: []clientconnect.Change{
				{Field: "backend", BeforeExists: true, Before: "openai", After: "swobu"},
				{Field: "base URL", BeforeExists: true, Before: "https://api.openai.com/v1", After: connectTarget(t).WorkspaceURL()},
			},
		},
	}

	surface := &fullWorkspaceSurface{Disclosure: d, Slug: "dev"}
	h, err := testkit.NewHarnessAt(surface, 60, 24)
	if err != nil {
		t.Fatal(err)
	}
	h.Open()
	defer h.Close()

	// 1. Test stress with Plan open
	d.toggleEndpoint()
	d.chooseClient(d.Clients.Get()[0])
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
		r.mounted = New(r.target, r.ops)
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
