package workspace_connect

import (
	"errors"
	"strings"
	"testing"

	ui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
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
func (f *fakeOperations) Apply(plan clientconnect.Plan) error { f.applied = plan; return f.applyErr }

func connectTarget(t *testing.T) clientconnect.Target {
	t.Helper()
	target, err := clientconnect.NewTarget("work", "http://127.0.0.1:7926/c/work")
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func connectFixture(t *testing.T) (*Disclosure, *fakeOperations, *[]readmodel.Notice) {
	t.Helper()
	ops := &fakeOperations{
		clients: []clientconnect.Client{{ID: clientconnect.ClientCodex, Name: "Codex CLI"}, {ID: clientconnect.ClientClaude, Name: "Claude Code", Configured: true}, {ID: clientconnect.ClientKilo, Name: "Kilo Code"}, {ID: clientconnect.ClientPi, Name: "pi"}},
		plans: map[clientconnect.ClientID]clientconnect.Plan{
			clientconnect.ClientCodex: {ClientID: clientconnect.ClientCodex, ClientName: "Codex CLI", ConfigPath: "/tmp/.codex/config.toml", Target: connectTarget(t), Changes: []clientconnect.Change{{Field: "endpoint", After: connectTarget(t).WorkspaceURL()}}},
		},
	}
	notices := &[]readmodel.Notice{}
	d := New(connectTarget(t), ops, func(notice readmodel.Notice) { *notices = append(*notices, notice) })
	return d, ops, notices
}

func TestDiscoveryRunsOnlyOnDeliberateOpenAndReopen(t *testing.T) {
	configured := false
	ops := &fakeOperations{discover: func() []clientconnect.Client {
		return []clientconnect.Client{{ID: clientconnect.ClientCodex, Name: "Codex CLI", Configured: configured}}
	}}
	d := New(connectTarget(t), ops, nil)
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
	d, _, _ := connectFixture(t)
	resting := testkit.RenderMountedTrimmed(t, d, 100, 20)
	for _, want := range []string{"endpoint", "http://127.0.0.1:7926/c/work", "connect ↵", "OpenAI · Anthropic"} {
		if !strings.Contains(resting, want) {
			t.Fatalf("resting frame missing %q:\n%s", want, resting)
		}
	}
	if strings.Contains(resting, "Codex CLI") || strings.Contains(resting, "Other clients") {
		t.Fatalf("resting frame leaked disclosure:\n%s", resting)
	}
	d.toggleEndpoint()
	expanded := testkit.RenderMountedTrimmed(t, d, 100, 20)
	for _, want := range []string{"Codex CLI", "configure ↵", "Claude Code", "configured", "Kilo Code", "pi", "Other clients", "copy ↵"} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("expanded frame missing %q:\n%s", want, expanded)
		}
	}
}

func TestDisclosurePlanIsNestedAndApplyReportsConfigured(t *testing.T) {
	d, ops, _ := connectFixture(t)
	d.toggleEndpoint()
	d.chooseClient(d.Clients.Get()[0])
	frame := testkit.RenderMountedTrimmed(t, d, 100, 20)
	for _, want := range []string{"Codex CLI", "endpoint", "→ /c/work", "writes", "/tmp/.codex/config.toml", "apply ↵"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("plan frame missing %q:\n%s", want, frame)
		}
	}
	d.applyPlan()
	if ops.applied.ClientID != clientconnect.ClientCodex {
		t.Fatalf("applied = %#v", ops.applied)
	}
	frame = testkit.RenderMountedTrimmed(t, d, 100, 20)
	if !strings.Contains(frame, "Codex CLI") || !strings.Contains(frame, "configured") || strings.Contains(frame, "/tmp/.codex/config.toml") {
		t.Fatalf("success frame:\n%s", frame)
	}
}

func TestPlanActionGrammarDistinguishesInsertFromOverwrite(t *testing.T) {
	d, _, _ := connectFixture(t)
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
	d, ops, _ := connectFixture(t)
	ops.applyErr = errors.New("client configuration changed; nothing was overwritten")
	d.toggleEndpoint()
	d.chooseClient(d.Clients.Get()[0])
	d.applyPlan()
	frame := testkit.RenderMountedTrimmed(t, d, 100, 22)
	if !strings.Contains(frame, "nothing was overwritten") || !strings.Contains(frame, "apply ↵") {
		t.Fatalf("error frame:\n%s", frame)
	}
}

func TestDisclosureOtherClientsCopiesCanonicalWorkspaceURLWithoutAnotherDisclosure(t *testing.T) {
	d, _, notices := connectFixture(t)
	d.toggleEndpoint()
	var copied []string
	cleanup := cockpitui.RegisterEffectHooks(nil, func(value string) (bool, error) { copied = append(copied, value); return true, nil }, nil)
	defer cleanup()
	frame := testkit.RenderMountedTrimmed(t, d, 100, 24)
	for _, want := range []string{"Other clients", "copy ↵"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("Other clients frame missing %q:\n%s", want, frame)
		}
	}
	if strings.Count(frame, "OpenAI · Anthropic") != 1 {
		t.Fatalf("ecosystem summary must appear only on endpoint:\n%s", frame)
	}
	for _, forbidden := range []string{"Responses · Chat", "Messages", "Models", "/c/work/v1"} {
		if strings.Contains(frame, forbidden) {
			t.Fatalf("Other clients leaked protocol-specific URL %q:\n%s", forbidden, frame)
		}
	}
	d.copyWorkspaceURL()
	if len(copied) != 1 || copied[0] != d.Target.WorkspaceURL() {
		t.Fatalf("copied = %#v", copied)
	}
	if len(*notices) != 1 || (*notices)[0].Message != "Workspace URL copied." {
		t.Fatalf("copy notices = %#v", *notices)
	}
	stable := testkit.RenderMountedTrimmed(t, d, 100, 24)
	if strings.Count(stable, "copy ↵") != 1 || strings.Contains(stable, "copied") || strings.Contains(stable, "saved to") {
		t.Fatalf("copy result changed structural rows:\n%s", stable)
	}
	if strings.Contains(frame, "model = default") || strings.Contains(frame, "API key") {
		t.Fatalf("Other clients frame invented configuration:\n%s", frame)
	}
}

func TestCopyFallbackNoticeIncludesConcretePathWithoutChangingRow(t *testing.T) {
	result := cockpitui.CopyResult{Status: cockpitui.CopySavedFile, Path: "/tmp/swobu-url-123.txt"}
	notice := copyNotice("Workspace", result)
	if notice.Kind != readmodel.NoticeWarning || !strings.Contains(notice.Message, "/tmp/swobu-url-123.txt") {
		t.Fatalf("fallback notice = %#v", notice)
	}
}

func TestDisclosureBackClosesNearestScope(t *testing.T) {
	d, _, notices := connectFixture(t)
	d.toggleEndpoint()
	*notices = append(*notices, readmodel.Notice{Kind: readmodel.NoticeWarning, Message: "shell-owned"})
	if !d.Back() || d.EndpointOpen.Get() {
		t.Fatal("Back did not close endpoint")
	}
	if len(*notices) != 1 || (*notices)[0].Message != "shell-owned" {
		t.Fatalf("closing Connect changed shared notice: %#v", *notices)
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
}

func TestDisclosureAppLoopEnterDisclosesEndpoint(t *testing.T) {
	d, _, _ := connectFixture(t)
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
	if root.mounted.Plan.Get().ClientID != clientconnect.ClientCodex || !root.mounted.EndpointOpen.Get() {
		t.Fatal("workspace A disclosure was not open")
	}

	root.key = "workspace-connect:other:" + targetB.WorkspaceURL()
	root.target = targetB
	root.ops = &fakeOperations{clients: []clientconnect.Client{{ID: clientconnect.ClientClaude, Name: "Claude B"}}}
	h.App().MarkDirty()
	h.Frame()
	if root.mounted.Target.WorkspaceSlug() != "other" || root.mounted.EndpointOpen.Get() || root.mounted.Plan.Get().ClientID != "" {
		t.Fatalf("workspace B reused A state: target=%q open=%v plan=%#v", root.mounted.Target.WorkspaceSlug(), root.mounted.EndpointOpen.Get(), root.mounted.Plan.Get())
	}
	root.mounted.toggleEndpoint()
	if got := root.mounted.Clients.Get(); len(got) != 1 || got[0].Name != "Claude B" {
		t.Fatalf("workspace B discovery = %#v", got)
	}
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
		r.mounted = New(r.target, r.ops, nil)
		return r.mounted
	}))
	return root
}

func (r *connectRoot) KeyMap() ui.KeyMap {
	keys := ui.KeyMap{
		ui.OnStop(ui.KeyDown, cockpitui.SelectNext),
		ui.OnStop(ui.KeyEnter, cockpitui.ActivateCurrentSelection),
	}
	return keys
}
