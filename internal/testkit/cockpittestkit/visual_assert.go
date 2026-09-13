package testkit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/grindlemire/go-tui"

	"github.com/swobuforge/swobu/internal/cockpit/mountedrender"
	"github.com/swobuforge/swobu/internal/testkit/testscreen"
	assert "github.com/swobuforge/swobu/internal/testkit/testscreen/assert"
	"github.com/swobuforge/swobu/internal/testkit/testscreen/fixture"
	"github.com/swobuforge/swobu/internal/testkit/testscreen/testpath"
)

type Predicate = assert.Predicate

var (
	Text    = assert.Text
	TextRE  = assert.TextRE
	All     = assert.All
	Not     = assert.Not
	EvalNow = assert.EvalNow
)

// RenderString renders an element tree to a deterministic string at the given dimensions.
// It runs layout and rendering without an App. The output includes trailing spaces;
// callers that need trimmed lines should use RenderTrimmed.
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

// ScreenFromBuffer is the Go-TUI producer adapter. Shared screen comparison
// and ANSI persistence remain independent of Go-TUI.
func ScreenFromBuffer(b *tui.Buffer) (testscreen.Screen, error) {
	if b == nil {
		return testscreen.New(1, 1), nil
	}
	cols, rows := b.Size()
	screen := testscreen.New(cols, rows)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			cell := b.Cell(x, y)
			rendition := renditionFromTUI(cell.Style)
			if cell.Link != "" {
				return testscreen.Screen{}, fmt.Errorf("go-tui screen contains unsupported hyperlink at column %d row %d", x, y)
			}
			if cell.Combining != "" {
				return testscreen.Screen{}, fmt.Errorf("go-tui screen contains unsupported combining content %q at column %d row %d", cell.Combining, x, y)
			}
			if cell.Width == 0 {
				if x == 0 || b.Cell(x-1, y).Width != 2 || cell.Rune != 0 {
					return testscreen.Screen{}, fmt.Errorf("go-tui screen contains orphan continuation at column %d row %d", x, y)
				}
				screen.Rows[y][x] = testscreen.Cell{Width: 0, Rendition: rendition}
				continue
			}
			if cell.Rune == 0 || (cell.Width != 1 && cell.Width != 2) {
				return testscreen.Screen{}, fmt.Errorf("go-tui screen contains unsupported rune %q with width %d at column %d row %d", cell.Rune, cell.Width, x, y)
			}
			if cell.Width == 2 && (x+1 >= cols || b.Cell(x+1, y).Width != 0) {
				return testscreen.Screen{}, fmt.Errorf("go-tui screen contains width-2 rune %q without a continuation at column %d row %d", cell.Rune, x, y)
			}
			screen.Rows[y][x] = testscreen.Cell{Text: string(cell.Rune), Width: int(cell.Width), Rendition: rendition}
		}
	}
	return screen, nil
}

func renditionFromTUI(style tui.Style) testscreen.Rendition {
	attrs := testscreen.Attrs(0)
	for _, item := range []struct {
		source tui.Attr
		target testscreen.Attrs
	}{
		{tui.AttrBold, testscreen.AttrBold},
		{tui.AttrReverse, testscreen.AttrReverse},
	} {
		if style.Attrs&item.source != 0 {
			attrs |= item.target
		}
	}
	return testscreen.EffectiveRendition(testscreen.Rendition{FG: colorFromTUI(style.Fg), BG: colorFromTUI(style.Bg), Attrs: attrs})
}

func colorFromTUI(color tui.Color) testscreen.Color {
	switch color.Type() {
	case tui.ColorANSI:
		return testscreen.ANSIColor(color.ANSI())
	case tui.ColorRGB:
		r, g, b := color.RGB()
		return testscreen.RGBColor(r, g, b)
	default:
		return testscreen.DefaultColor()
	}
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

// RenderMountedScreen renders the actual styled Go-TUI buffer into the neutral
// terminal screen used by canonical ANSI goldens.
func RenderMountedScreen(t testing.TB, component tui.Component, width, height int) testscreen.Screen {
	t.Helper()
	app, _, err := mountedrender.NewApp(width, height)
	if err != nil {
		t.Fatalf("render mounted component: %v", err)
	}
	defer app.Close()
	app.SetRootComponent(component)
	app.Render()
	screen, err := ScreenFromBuffer(app.Buffer())
	if err != nil {
		t.Fatalf("capture mounted screen: %v", err)
	}
	return screen
}

// AssertNow executes a testscreen predicate against a rendered string.
// Failures surface through t.Fatalf.
func AssertNow(t testing.TB, rendered string, predicate assert.Predicate) {
	t.Helper()
	if err := assert.EvalNow(rendered, predicate); err != nil {
		t.Fatalf("assertion failed: %v\nrendered:\n%s", err, rendered)
	}
}

// VisualAssertBuilder configures one fixture-backed visual assertion.
type VisualAssertBuilder struct {
	fixture fixture.Builder
}

// AssertVisual creates a fixture-backed visual assert using Cockpit testkit
// conventions. Fixture path is derived as:
// testdata/<testid>/fixture/<assertname>.ansi where testid is <testfile>__<testname>.
func AssertVisual(assertName string) VisualAssertBuilder {
	return VisualAssertBuilder{fixture: fixture.BuilderFor(deriveVisualTestID(), assertName)}
}

func (b VisualAssertBuilder) Fixture(path string) VisualAssertBuilder {
	b.fixture = b.fixture.Fixture(path)
	return b
}

func (b VisualAssertBuilder) Viewport(minCols, minRows int) VisualAssertBuilder {
	b.fixture = b.fixture.Viewport(minCols, minRows)
	return b
}

// ExactViewport requires both fixture and actual screen to carry precisely the
// requested terminal geometry. Use it for whole-screen contracts where extra
// blank rows or columns are observable state rather than harmless capacity.
func (b VisualAssertBuilder) ExactViewport(cols, rows int) VisualAssertBuilder {
	b.fixture = b.fixture.ExactViewport(cols, rows)
	return b
}

// Compare checks snapshot against the configured visual fixture.
func (b VisualAssertBuilder) Compare(screen testscreen.Screen) fixture.Report {
	return fixture.CompareScreen(screen, b.fixture.Config())
}

// Now checks snapshot once and fails the test on mismatch.
func (b VisualAssertBuilder) Now(t testing.TB, screen testscreen.Screen) {
	t.Helper()
	report := b.Compare(screen)
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
	msg.WriteString("\nupdate with: SWOBU_UPDATE_FIXTURES=1 go test ./...; review the diff\n")
	return msg.String()
}
