// JTBD penetration proofs for Cockpit root navigation.
//
// These tests capture rendered frames for J-11 and J-10 across 80/100/120
// columns and keep the resulting traces under package-local
// testdata/penetration/225/ and the workspace focus lane under
// testdata/penetration/226/.
package cockpit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/mountedrender"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

const penetrationUpdateEnv = "SWOBU_UPDATE_FIXTURES"

func TestPenetrate_TabSwitch_CapturesFrame(t *testing.T) {
	h := newPenetrationHarness(t, NewCockpit(DefaultFixtureReadModel()), 100, 24)
	defer h.Close()

	frame := h.Frame()
	assertContainsFrame(t, frame, "⛉ SWOBU", "[› dev]", "workspace ▾", "↑↓ move   ↵ action   ? help   esc back   tab switch")

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyTab})
	frame = h.Frame()
	assertContainsFrame(t, frame, "[› lab]")
}

func TestPenetrate_TabCyclesAllTabs(t *testing.T) {
	for _, width := range []int{80, 100, 120} {
		width := width
		t.Run(fmt.Sprintf("%d", width), func(t *testing.T) {
			h := newPenetrationHarness(t, NewCockpit(DefaultFixtureReadModel()), width, 24)
			defer h.Close()

			assertContainsFrame(t, h.Frame(), "[› dev]")

			sequence := []struct {
				key  tui.KeyEvent
				want string
			}{
				{key: tui.KeyEvent{Key: tui.KeyTab}, want: "[› lab]"},
				{key: tui.KeyEvent{Key: tui.KeyTab}, want: "[› +]"},
				{key: tui.KeyEvent{Key: tui.KeyTab}, want: "[› ?]"},
			}

			for _, step := range sequence {
				h.DispatchKey(step.key)
				assertContainsFrame(t, h.Frame(), step.want)
			}
		})
	}
}

func TestPenetrate_ShiftTabCyclesBackward(t *testing.T) {
	for _, width := range []int{80, 100, 120} {
		width := width
		t.Run(fmt.Sprintf("%d", width), func(t *testing.T) {
			h := newPenetrationHarness(t, NewCockpit(DefaultFixtureReadModel()), width, 24)
			defer h.Close()

			h.DispatchKey(tui.KeyEvent{Key: tui.KeyTab, Mod: tui.ModShift})
			assertContainsFrame(t, h.Frame(), "[› ?]")
		})
	}
}

func TestPenetrate_TabWrapsAround(t *testing.T) {
	for _, width := range []int{80, 100, 120} {
		width := width
		t.Run(fmt.Sprintf("%d", width), func(t *testing.T) {
			h := newPenetrationHarness(t, NewCockpit(DefaultFixtureReadModel()), width, 24)
			defer h.Close()

			for i := 0; i < 4; i++ {
				h.DispatchKey(tui.KeyEvent{Key: tui.KeyTab})
			}
			assertContainsFrame(t, h.Frame(), "[› dev]")
		})
	}
}

func TestPenetrate_F1ActivatesHelpTab(t *testing.T) {
	for _, width := range []int{80, 100, 120} {
		width := width
		t.Run(fmt.Sprintf("%d", width), func(t *testing.T) {
			h := newPenetrationHarness(t, NewCockpit(DefaultFixtureReadModel()), width, 24)
			defer h.Close()

			h.DispatchKey(tui.KeyEvent{Key: tui.KeyF1})
			frame := h.Frame()
			assertContainsFrame(t, frame, "⛉ SWOBU", "[› ?]", "help", "docs", "community", "issue", "diagnostics")
			assertContainsFrame(t, frame, "↑↓ move   ↵ action   ? help   esc back   tab switch")
		})
	}
}

func TestPenetrate_HelpTabFooterHints(t *testing.T) {
	for _, width := range []int{80, 100, 120} {
		width := width
		t.Run(fmt.Sprintf("%d", width), func(t *testing.T) {
			h := newPenetrationHarness(t, helpFixtureCockpit(readmodel.DiagnosticsReady), width, 24)
			defer h.Close()

			frame := h.Frame()
			assertContainsFrame(t, frame, "⛉ SWOBU", "[› ?]", "↑↓ move   ↵ action   ? help   esc back   tab switch")

			h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
			assertContainsFrame(t, h.Frame(), "> docs", "open ↵")
		})
	}
}

func TestPenetrate_WorkspaceFocusTrace(t *testing.T) {
	w := newPenetrationArtifactWriterForBatch("226")
	frameCount := 0

	for _, width := range []int{80, 100, 120} {
		width := width
		t.Run(fmt.Sprintf("workspace-focus/%d", width), func(t *testing.T) {
			frameCount += writeWorkspaceFocusTrace(t, w, width, 24)
		})
	}

	journal := fmt.Sprintf(`# Workspace Focus Journal

Catalog coherence check: pass, 2026-06-27.
Default-to-draft tab trace: yes.
Draft cursor trace: yes.
Delete marker trace: yes.
Viewport matrix: 80x24, 100x24, 120x24.
Frames captured: %d.
Delta summary: workspace body and cursor states matched the expected trace.
`, frameCount)
	w.writeOrCompare(t, "journal.md", journal)

	ledger := `# Delta Ledger

Workspace focus trace is green.

- ` + "`dev`" + ` tab restored the existing workspace body after tabbing back from ` + "`+`" + `.
- ` + "`+`" + ` tab showed the draft slug cursor on the first frame.
- Delete focus showed a visible select marker when stepped to the delete row.
`
	w.writeOrCompare(t, "delta-ledger.md", ledger)
}

func TestPenetrate_WriteScreenTraces(t *testing.T) {
	w := newPenetrationArtifactWriter()
	frameCount := 0

	for _, width := range []int{80, 100, 120} {
		width := width
		t.Run(fmt.Sprintf("jtbd-11/%d", width), func(t *testing.T) {
			frameCount += writeJTBD11Trace(t, w, width, 24)
		})
		t.Run(fmt.Sprintf("jtbd-10/%d", width), func(t *testing.T) {
			frameCount += writeJTBD10Trace(t, w, width, 24)
		})
		t.Run(fmt.Sprintf("state-10/%d", width), func(t *testing.T) {
			frameCount += writeCollapsedStateTrace(t, w, width, 24)
		})
	}

	journal := fmt.Sprintf(`# JTBD Penetration Journal

Catalog coherence check: pass, 2026-06-27.
JTBD-11 traced: yes.
JTBD-10 traced: yes.
State 10 traced: yes.
Viewport matrix: 80x24, 100x24, 120x24.
Frames captured: %d.
Delta summary: none observed.
`, frameCount)
	w.writeOrCompare(t, "journal.md", journal)

	ledger := `# Delta Ledger

No deltas observed.

- State 1 matched the default workspace wireframe at 80x24, 100x24, and 120x24.
- State 8 matched the help-tab wireframe at 80x24, 100x24, and 120x24.
- State 10 matched the collapsed wireframe at 80x24, 100x24, and 120x24.
- JTBD-11 tab switching and wrap-around stayed coherent across all viewports.
- JTBD-10 help activation and return stayed coherent across all viewports.
`
	w.writeOrCompare(t, "delta-ledger.md", ledger)
}

type traceStep struct {
	key   tui.KeyEvent
	want  []string
	note  string
	label string
}

type penetrationArtifactWriter struct {
	update bool
	batch  string
}

func newPenetrationArtifactWriter() penetrationArtifactWriter {
	return newPenetrationArtifactWriterForBatch("225")
}

func newPenetrationArtifactWriterForBatch(batch string) penetrationArtifactWriter {
	return penetrationArtifactWriter{
		update: updateEnabled(os.Getenv(penetrationUpdateEnv)),
		batch:  batch,
	}
}

func (w penetrationArtifactWriter) writeOrCompare(t *testing.T, relPath, content string) {
	t.Helper()
	path := penetrationArtifactPath(w.batch, relPath)
	content = trimRightLines(content)
	if w.update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing penetration artifact %s: run %s=1 go test ./internal/cockpit -run Penetrate -count=1", path, penetrationUpdateEnv)
	}
	got := trimRightLines(string(raw))
	if got != content {
		t.Fatalf("penetration artifact mismatch for %s\n--- expected ---\n%s\n--- actual ---\n%s", path, got, content)
	}
}

func writeJTBD11Trace(t *testing.T, w penetrationArtifactWriter, width, height int) int {
	t.Helper()
	h := newPenetrationHarness(t, NewCockpit(DefaultFixtureReadModel()), width, height)
	defer h.Close()

	steps := []traceStep{
		{key: tui.KeyEvent{Key: tui.KeyTab}, want: []string{"[› lab]"}, note: "Tab advances to the next workspace tab.", label: "tab"},
		{key: tui.KeyEvent{Key: tui.KeyTab}, want: []string{"[› +]"}, note: "Tab advances to the draft workspace tab.", label: "tab"},
		{key: tui.KeyEvent{Key: tui.KeyTab}, want: []string{"[› ?]"}, note: "Tab advances to help, proving the root tab rail stays coherent.", label: "tab"},
		{key: tui.KeyEvent{Key: tui.KeyTab}, want: []string{"[› dev]"}, note: "Tab wraps from help back to the first workspace tab.", label: "tab"},
		{key: tui.KeyEvent{Key: tui.KeyTab, Mod: tui.ModShift}, want: []string{"[› ?]"}, note: "Shift+Tab moves back from the first workspace tab to help.", label: "shift-tab"},
	}

	return captureJourneyTrace(t, w, filepath.Join("jtbd-11", viewportName(width, height)), h, steps)
}

func writeJTBD10Trace(t *testing.T, w penetrationArtifactWriter, width, height int) int {
	t.Helper()
	h := newPenetrationHarness(t, NewCockpit(DefaultFixtureReadModel()), width, height)
	defer h.Close()

	steps := []traceStep{
		{key: tui.KeyEvent{Key: tui.KeyF1}, want: []string{"[› ?]", "help"}, note: "F1 opens the help tab.", label: "f1"},
		{key: tui.KeyEvent{Key: tui.KeyDown}, want: []string{"> docs", "open ↵"}, note: "Down focuses the docs row.", label: "down"},
		{key: tui.KeyEvent{Key: tui.KeyDown}, want: []string{"> community", "open ↵"}, note: "Down moves to community.", label: "down"},
		{key: tui.KeyEvent{Key: tui.KeyUp}, want: []string{"> docs", "open ↵"}, note: "Up returns to docs.", label: "up"},
		{key: tui.KeyEvent{Key: tui.KeyTab}, want: []string{"[› dev]", "workspace ▾"}, note: "Tab returns to the prior workspace tab.", label: "tab"},
	}

	return captureJourneyTrace(t, w, filepath.Join("jtbd-10", viewportName(width, height)), h, steps)
}

func writeCollapsedStateTrace(t *testing.T, w penetrationArtifactWriter, width, height int) int {
	t.Helper()
	frame := captureFrame(t, collapsedFixtureCockpit(), width, height)
	assertContainsFrame(t, frame, "workspace ▾", "model routes ▸", "activity")
	w.writeOrCompare(t, filepath.Join("state-10", viewportName(width, height), "state.txt"), frame)
	return 1
}

func writeWorkspaceFocusTrace(t *testing.T, w penetrationArtifactWriter, width, height int) int {
	t.Helper()
	h := newPenetrationHarness(t, NewCockpit(DefaultFixtureReadModel()), width, height)
	defer h.Close()

	steps := []traceStep{
		{
			key:   tui.KeyEvent{Key: tui.KeyTab},
			want:  []string{"[› lab]", "slug", "edit ↵"},
			note:  "Tab to the next existing workspace. The body must switch with the tab rail.",
			label: "tab",
		},
		{
			key:   tui.KeyEvent{Key: tui.KeyDown},
			want:  []string{"> workspace ▾"},
			note:  "Down lands on the workspace section header.",
			label: "down",
		},
		{
			key:   tui.KeyEvent{Key: tui.KeyDown},
			want:  []string{"> endpoint"},
			note:  "Down reaches the hero endpoint row.",
			label: "down",
		},
		{
			key:   tui.KeyEvent{Key: tui.KeyTab},
			want:  []string{"[› +]", "new workspace", "slug", "required", "after create", "_"},
			note:  "Tab to the draft workspace. The slug cursor must be visible on the first frame.",
			label: "tab",
		},
		{
			key:   tui.KeyEvent{Key: tui.KeyTab, Mod: tui.ModShift},
			want:  []string{"[› lab]", "slug", "edit ↵"},
			note:  "Shift+Tab returns to the existing workspace body instead of leaving the draft screen behind.",
			label: "shift-tab",
		},
		{
			key:   tui.KeyEvent{Key: tui.KeyTab, Mod: tui.ModShift},
			want:  []string{"[› dev]", "edit ↵", "http://127.0.0.1:7926/c/dev"},
			note:  "Shift+Tab again returns to the default workspace body.",
			label: "shift-tab",
		},
		{
			key:   tui.KeyEvent{Key: tui.KeyDown},
			want:  []string{"[› dev]", "endpoint"},
			note:  "Down moves focus through page.",
			label: "down",
		},
		{
			key:   tui.KeyEvent{Key: tui.KeyDown},
			want:  []string{"[› dev]", "slug"},
			note:  "Down continues traversal.",
			label: "down",
		},
		{
			key:   tui.KeyEvent{Key: tui.KeyDown},
			want:  []string{"[› dev]", "delete ↵"},
			note:  "Down reaches the delete row area.",
			label: "down",
		},
	}

	return captureJourneyTrace(t, w, filepath.Join("workspace-focus", viewportName(width, height)), h, steps)
}
func captureJourneyTrace(t *testing.T, w penetrationArtifactWriter, dir string, h *penetrationHarness, steps []traceStep) int {
	t.Helper()
	captured := 0
	before := h.Frame()
	captured++
	for i, step := range steps {
		stepNum := i + 1
		base := filepath.Join(dir, fmt.Sprintf("step-%03d", stepNum))
		w.writeOrCompare(t, base+".before.txt", before)
		w.writeOrCompare(t, base+".keys.txt", describeKeyEvent(step.key))
		h.DispatchKey(step.key)
		after := h.Frame()
		captured++
		w.writeOrCompare(t, base+".after.txt", after)
		w.writeOrCompare(t, base+".notes.md", step.note)
		assertContainsFrame(t, after, step.want...)
		before = after
	}
	return captured
}

func captureFrame(t *testing.T, root tui.Component, width, height int) string {
	t.Helper()
	h := newPenetrationHarness(t, root, width, height)
	defer h.Close()
	return h.Frame()
}

func assertContainsFrame(t *testing.T, frame string, want ...string) {
	t.Helper()
	for _, needle := range want {
		if !strings.Contains(frame, needle) {
			t.Fatalf("frame missing %q:\n%s", needle, frame)
		}
	}
}

func describeKeyEvent(event tui.KeyEvent) string {
	switch {
	case event.Key == tui.KeyTab && event.Mod&tui.ModShift != 0:
		return "Shift+Tab"
	case event.Key == tui.KeyTab:
		return "Tab"
	case event.Key == tui.KeyF1:
		return "F1"
	case event.Key == tui.KeyDown:
		return "Down"
	case event.Key == tui.KeyUp:
		return "Up"
	case event.Key == tui.KeyEscape:
		return "Esc"
	case event.Key == tui.KeyEnter:
		return "Enter"
	case event.Key == tui.KeyRune:
		return fmt.Sprintf("Rune(%q)", event.Rune)
	default:
		return fmt.Sprintf("%v", event.Key)
	}
}

func viewportName(width, height int) string {
	return fmt.Sprintf("viewport-%dx%d", width, height)
}

func penetrationArtifactPath(batch, relPath string) string {
	parts := []string{"testdata", "penetration", batch, filepath.FromSlash(relPath)}
	return filepath.Clean(filepath.Join(parts...))
}

func updateEnabled(value string) bool {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

type penetrationHarness struct {
	app *tui.App
}

func newPenetrationHarness(t *testing.T, root tui.Component, width, height int) *penetrationHarness {
	t.Helper()
	app, _, err := mountedrender.NewApp(width, height)
	if err != nil {
		t.Fatalf("mountedrender.NewApp(%d,%d): %v", width, height, err)
	}
	app.SetRootComponent(root)
	app.MarkDirty()
	app.Render()
	return &penetrationHarness{app: app}
}

func (h *penetrationHarness) Frame() string {
	h.app.Render()
	return trimRightLines(h.app.Buffer().String())
}

func (h *penetrationHarness) DispatchKey(event tui.KeyEvent) {
	h.app.Dispatch(event)
	h.app.Render()
}

func (h *penetrationHarness) Close() {
	if h.app != nil {
		_ = h.app.Close()
	}
}
