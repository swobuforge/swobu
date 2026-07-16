package testkit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/grindlemire/go-tui"

	"github.com/swobuforge/swobu/internal/cockpit/mountedrender"
	assert "github.com/swobuforge/swobu/testscreen/assert"
	"github.com/swobuforge/swobu/testscreen/buf"
	"github.com/swobuforge/swobu/testscreen/fixture"
	"github.com/swobuforge/swobu/testscreen/testpath"
)

// --- Re-exports from testscreen/assert kernel (Rule 1: re-export, don't re-implement) ---

type Expr = assert.Expr
type Predicate = assert.Predicate

var (
	Text        = assert.Text
	TextRE      = assert.TextRE
	All         = assert.All
	Not         = assert.Not
	Box         = assert.Box
	Within      = assert.Within
	EvalNow     = assert.EvalNow
	EvalNowView = assert.EvalNowView
)

// --- Cockpit surface helpers (Rule 3: surface-specific, named to surface) ---

// RenderString renders an element tree to a deterministic string at the given dimensions.
// It runs layout and rendering without an App, which is the strongest deterministic
// lane available. The output includes trailing spaces; callers that need trimmed
// lines should use RenderTrimmed or normalize in fixture.Compare.
func RenderString(el *tui.Element, width, height int) string {
	b := tui.NewBuffer(width, height)
	el.Render(b, width, height)
	return b.String()
}

// RenderTrimmed renders an element tree and strips trailing spaces from each line.
func RenderTrimmed(el *tui.Element, width, height int) string {
	b := tui.NewBuffer(width, height)
	el.Render(b, width, height)
	return b.StringTrimmed()
}

// RenderBuffer renders an element tree into a buf.View for spatial assertions.
func RenderBuffer(el *tui.Element, width, height int) buf.View {
	b := tui.NewBuffer(width, height)
	el.Render(b, width, height)
	return buf.FromString(b.String())
}

// RenderMountedString renders a component through a mounted go-tui App.
// Use it for any Cockpit component that may own KeyMap, focus, or app.Mount
// descendants. Do not call component.Render(nil) in Cockpit tests.
func RenderMountedString(t testing.TB, component tui.Component, width, height int) string {
	t.Helper()
	rendered, err := mountedrender.String(component, width, height)
	if err != nil {
		t.Fatalf("render mounted component: %v", err)
	}
	return rendered
}

// RenderMountedTrimmed renders a mounted component and strips trailing spaces.
func RenderMountedTrimmed(t testing.TB, component tui.Component, width, height int) string {
	t.Helper()
	rendered, err := mountedrender.Trimmed(component, width, height)
	if err != nil {
		t.Fatalf("render mounted component: %v", err)
	}
	return rendered
}

// RenderMountedBuffer renders a mounted component into a testscreen view.
func RenderMountedBuffer(t testing.TB, component tui.Component, width, height int) buf.View {
	t.Helper()
	rendered := RenderMountedString(t, component, width, height)
	return buf.FromString(rendered)
}

// AssertNow executes a testscreen predicate against a rendered string.
// Failures surface through t.Fatalf.
func AssertNow(t testing.TB, rendered string, predicate assert.Predicate) {
	t.Helper()
	if err := assert.EvalNow(rendered, predicate); err != nil {
		t.Fatalf("assertion failed: %v\nrendered:\n%s", err, rendered)
	}
}

// AssertNowView executes a testscreen predicate against a buf.View.
// Failures surface through t.Fatalf.
func AssertNowView(t testing.TB, view buf.View, predicate assert.Predicate) {
	t.Helper()
	if err := assert.EvalNowView(view, predicate); err != nil {
		if view != nil {
			_, rows := view.Size()
			var sb strings.Builder
			for y := 0; y < rows; y++ {
				sb.WriteString(view.Line(y))
				sb.WriteByte('\n')
			}
			t.Fatalf("assertion failed: %v\nrendered:\n%s", err, sb.String())
		}
		t.Fatalf("assertion failed: %v", err)
	}
}

// --- Visual fixture helpers ---

// VisualAssertBuilder configures one fixture-backed visual assertion.
type VisualAssertBuilder struct {
	fixture fixture.Builder
}

// AssertVisual creates a fixture-backed visual assert using Cockpit testkit
// conventions. Fixture path is derived as:
// testdata/<testid>/fixture/<assertname>.txt where testid is <testfile>__<testname>.
func AssertVisual(assertName string) VisualAssertBuilder {
	return VisualAssertBuilder{fixture: fixture.BuilderFor(deriveVisualTestID(), assertName)}
}

func (b VisualAssertBuilder) Normalize(fn func(string) string) VisualAssertBuilder {
	b.fixture = b.fixture.Normalize(fn)
	return b
}

func (b VisualAssertBuilder) Fixture(path string) VisualAssertBuilder {
	b.fixture = b.fixture.Fixture(path)
	return b
}

func (b VisualAssertBuilder) Viewport(minCols, minRows int) VisualAssertBuilder {
	b.fixture = b.fixture.Viewport(minCols, minRows)
	return b
}

// Compare checks snapshot against the configured visual fixture.
func (b VisualAssertBuilder) Compare(snapshot string) fixture.Report {
	return fixture.CompareSnapshot(snapshot, b.fixture.Config())
}

// Now checks snapshot once and fails the test on mismatch.
func (b VisualAssertBuilder) Now(t testing.TB, snapshot string) {
	t.Helper()
	report := b.Compare(snapshot)
	if report.Err != nil {
		t.Fatal(formatVisualReport(report))
	}
}

func deriveVisualTestID() string {
	testID := testpath.TestID("unknown_testfile", "unknown_testname")
	id, ok := testpath.CallerTestID(nil)
	if ok {
		testID = id
	}
	return testID
}

func formatVisualReport(report fixture.Report) string {
	msg := strings.Builder{}
	msg.WriteString(fmt.Sprintf("visual fixture mismatch: %v\n", report.Err))
	if report.Diff != "" {
		msg.WriteString("\n--- diff ---\n")
		msg.WriteString(report.Diff)
	}
	if report.Actual != "" {
		msg.WriteString("\n--- actual ---\n")
		msg.WriteString(report.Actual)
	}
	if report.Expected != "" {
		msg.WriteString("\n--- expected ---\n")
		msg.WriteString(report.Expected)
	}
	msg.WriteString("\npromote with: SWOBU_UPDATE_FIXTURES=\"I generated the fixture candidate, opened it in my editor, compared every row to the RFC wireframe, edited by hand wherever rendering diverged from intent, and verified the fixture matches the intended operator experience before committing.\" go test ./...\n")
	return msg.String()
}
