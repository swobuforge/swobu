package cli

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/creack/pty"
	"github.com/mattn/go-runewidth"
)

// runeCount is the test's own width model: one cell per rune. It is deliberately
// independent of the renderer's runewidth-based math, so a width bug in the
// renderer shows up as a mismatch against these expectations rather than as a
// shared assumption. It is exact for the ASCII and box-drawing content used in
// the golden cases; wide runes are covered separately.
func runeCount(s string) int {
	return utf8.RuneCountInString(s)
}

// expectedBox is the test's independent reconstruction of a 68-cell notice box.
// It hardcodes the spec (outer 68, inner 66, content 64, one-space side padding,
// "─ Title " label) rather than calling the renderer, so any drift in the
// renderer's geometry fails the comparison.
func expectedBox(t *testing.T, title string, rows []string) string {
	t.Helper()
	const inner = 66
	const contentW = 64
	var b strings.Builder
	label := "─ " + title + " "
	b.WriteString("╭")
	b.WriteString(label)
	b.WriteString(strings.Repeat("─", inner-runeCount(label)))
	b.WriteString("╮")
	for _, row := range rows {
		b.WriteString("\r\n│ ")
		b.WriteString(row)
		b.WriteString(strings.Repeat(" ", contentW-runeCount(row)))
		b.WriteString(" │")
	}
	b.WriteString("\r\n╰")
	b.WriteString(strings.Repeat("─", inner))
	b.WriteString("╯\r\n")
	return b.String()
}

// requireClosedNotice asserts that stdout contains a fully closed notice box for
// title: a top border and a bottom border after it, enclosing every listed row.
// This is the property the original substring assertions ("╭─ Title ") missed —
// a half-drawn header with no closure passed those checks. The expected bottom
// border is built here from the documented geometry, independent of
// renderNoticeBlock, so the assertion stays non-circular. A nil rows slice
// checks only that the box is closed.
func requireClosedNotice(t *testing.T, stdout, title string, rows []string) {
	t.Helper()
	const inner = 66
	topMarker := "╭─ " + title + " "
	bottomBorder := "╰" + strings.Repeat("─", inner) + "╯"

	topIdx := strings.Index(stdout, topMarker)
	if topIdx < 0 {
		t.Fatalf("stdout missing notice top %q:\n%s", topMarker, stdout)
	}
	rest := stdout[topIdx:]
	botIdx := strings.Index(rest, bottomBorder)
	if botIdx < 0 {
		t.Fatalf("notice %q missing its closing bottom border (half-drawn box regression):\n%s", title, rest)
	}
	block := rest[:botIdx+len(bottomBorder)]
	for _, row := range rows {
		if !strings.Contains(block, row) {
			t.Fatalf("notice %q missing row %q; block:\n%s", title, row, block)
		}
	}
}

func TestWriteNoticeBlock_UpdateAvailableGolden(t *testing.T) {
	rows := []string{
		"versions: dev → 0.9.1",
		"update: curl -fsSL https://swobu.com/install.sh | sh",
		"hide: export SWOBU_SKIP_VERSION_NOTICE=1",
	}
	var out strings.Builder
	writeNoticeBlock(&out, "Update Available", rows)

	want := expectedBox(t, "Update Available", rows)
	if got := out.String(); got != want {
		t.Fatalf("update-available box mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestWriteNoticeBlock_TelemetryDisclosureGolden(t *testing.T) {
	rows := []string{
		"swobu collects anonymous usage data to fix bugs and guide work.",
		"no prompts, secrets, or request content are sent.",
		"disable: export SWOBU_DO_NOT_TRACK=1",
	}
	var out strings.Builder
	writeNoticeBlock(&out, "Telemetry Disclosure", rows)

	want := expectedBox(t, "Telemetry Disclosure", rows)
	if got := out.String(); got != want {
		t.Fatalf("telemetry box mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestWriteNoticeBlock_DaemonRuntimeGolden(t *testing.T) {
	rows := []string{
		"config path: /home/metrofun/.config/swobu/swobu.yaml",
		"address: 127.0.0.1:7926",
	}
	var out strings.Builder
	writeNoticeBlock(&out, "Daemon Runtime", rows)

	want := expectedBox(t, "Daemon Runtime", rows)
	if got := out.String(); got != want {
		t.Fatalf("daemon-runtime box mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestWriteNoticeBlock_StartupFailedGolden(t *testing.T) {
	rows := []string{
		"listen tcp 127.0.0.1:7926: bind: address already in use",
		"next: stop existing daemon or run `swobu daemon down`",
		"next: run `swobu status`",
	}
	var out strings.Builder
	writeNoticeBlock(&out, "Startup Failed", rows)

	want := expectedBox(t, "Startup Failed", rows)
	if got := out.String(); got != want {
		t.Fatalf("startup-failed box mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestRenderNoticeBlock_AllLinesAreUniformWidth(t *testing.T) {
	lines := renderNoticeBlock("Update Available", []string{
		"versions: dev → 0.9.1",
		"update: curl -fsSL https://swobu.com/install.sh | sh",
	})
	const wantWidth = 68
	for i, line := range lines {
		if w := runewidth.StringWidth(line); w != wantWidth {
			t.Fatalf("line %d has display width %d, want %d: %q", i, w, wantWidth, line)
		}
	}
}

func TestRenderNoticeBlockAtWidth_StaysInsideNarrowTerminal(t *testing.T) {
	const terminalWidth = 60
	lines := renderNoticeBlockAtWidth("Update Available", []string{
		"versions: dev → 1.0.0-rc.10",
		"update: curl -fsSL https://swobu.com/install.sh | sh",
		"hide: export SWOBU_SKIP_VERSION_NOTICE=1",
	}, terminalWidth)

	for i, line := range lines {
		if width := runewidth.StringWidth(line); width > terminalWidth {
			t.Fatalf("line %d width = %d, exceeds terminal width %d: %q", i, width, terminalWidth, line)
		}
		if !strings.HasSuffix(line, "╮") && !strings.HasSuffix(line, "│") && !strings.HasSuffix(line, "╯") {
			t.Fatalf("line %d lost its right border before terminal wrapping: %q", i, line)
		}
	}
}

func TestNoticeWidth_UsesRealTerminalColumns(t *testing.T) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatalf("open pty: %v", err)
	}
	t.Cleanup(func() {
		_ = ptmx.Close()
		_ = tty.Close()
	})
	if err := pty.Setsize(ptmx, &pty.Winsize{Cols: 60, Rows: 24}); err != nil {
		t.Fatalf("set pty size: %v", err)
	}

	if got := noticeWidth(tty); got != 60 {
		t.Fatalf("notice width = %d, want terminal width 60", got)
	}
	if got := noticeWidth(os.Stdout); got > noticeBoxMaxWidth {
		t.Fatalf("stdout notice width = %d, exceeds compact maximum %d", got, noticeBoxMaxWidth)
	}
}

func TestRenderNoticeBlock_BordersComplete(t *testing.T) {
	lines := renderNoticeBlock("Startup Failed", []string{"one row", "two row"})
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (top, body, bottom), got %d: %v", len(lines), lines)
	}
	top, bottom := lines[0], lines[len(lines)-1]
	if !strings.HasPrefix(top, "╭") || !strings.HasSuffix(top, "╮") {
		t.Fatalf("top border malformed: %q", top)
	}
	if !strings.HasPrefix(bottom, "╰") || !strings.HasSuffix(bottom, "╯") {
		t.Fatalf("bottom border malformed: %q", bottom)
	}
	if !strings.HasPrefix(top, "╭─ Startup Failed ") {
		t.Fatalf("top border missing title segment: %q", top)
	}
	for i, line := range lines[1 : len(lines)-1] {
		if !strings.HasPrefix(line, "│") || !strings.HasSuffix(line, "│") {
			t.Fatalf("body line %d missing vertical borders: %q", i, line)
		}
	}
}

func TestRenderNoticeBlock_EmptyRowsRenderAsBlankBorderedLines(t *testing.T) {
	lines := renderNoticeBlock("Notice", []string{"kept", "", "also kept"})
	// top, kept, blank, also kept, bottom
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines preserving the empty row, got %d: %v", len(lines), lines)
	}
	blank := lines[2]
	wantBlank := "│" + strings.Repeat(" ", 66) + "│"
	if blank != wantBlank {
		t.Fatalf("empty row should render as %q, got %q", wantBlank, blank)
	}
	if runewidth.StringWidth(blank) != 68 {
		t.Fatalf("blank row has width %d, want 68: %q", runewidth.StringWidth(blank), blank)
	}
}

func TestRenderNoticeBlock_WrapsLongRowAndHardBreaksTokens(t *testing.T) {
	longRow := strings.Repeat("word ", 30) + "overflowtoken" + strings.Repeat("z", 200)
	lines := renderNoticeBlock("Notice", []string{longRow})

	if len(lines) <= 3 {
		t.Fatalf("expected wrapping to produce multiple body lines, got %d: %v", len(lines), lines)
	}
	for i, line := range lines {
		if runewidth.StringWidth(line) != 68 {
			t.Fatalf("wrapped line %d has width %d, want 68: %q", i, runewidth.StringWidth(line), line)
		}
		if strings.Contains(line, "…") {
			t.Fatalf("row was ellipsized; wrapping must hard-break, never truncate: %q", line)
		}
	}
	joined := strings.Join(lines, "")
	if !strings.Contains(joined, "z") {
		t.Fatalf("hard-break dropped the over-wide token content; output:\n%s", joined)
	}
}

func TestRenderNoticeBlock_UnicodeWidthKeepsBorderAligned(t *testing.T) {
	// 中 is a width-2 rune. A correct renderer pads by display width so the right
	// border stays at column 68 regardless of multi-cell content.
	lines := renderNoticeBlock("Notice", []string{"command 中 line"})
	for i, line := range lines {
		if w := runewidth.StringWidth(line); w != 68 {
			t.Fatalf("unicode line %d has display width %d, want 68: %q", i, w, line)
		}
	}
}

func TestRenderNoticeBlock_SanitizesControlCharacters(t *testing.T) {
	lines := renderNoticeBlock("A\x1bB", []string{"a\tb\rc\nd", "esc\x1b[31mred"})
	for i, line := range lines {
		for _, bad := range []string{"\n", "\r", "\t", "\x1b"} {
			if strings.Contains(line, bad) {
				t.Fatalf("control character %q survived sanitization on line %d: %q", bad, i, line)
			}
		}
	}
	// ESC drops, printable neighbors survive: "A\x1bB" -> "AB".
	if !strings.HasPrefix(lines[0], "╭─ AB ") {
		t.Fatalf("title control chars not sanitized as expected: %q", lines[0])
	}
}

func TestWriteNoticeBlock_EmitsCRLFOnly(t *testing.T) {
	var out strings.Builder
	writeNoticeBlock(&out, "Notice", []string{"one", "two"})
	text := out.String()
	if !strings.HasSuffix(text, "\r\n") {
		t.Fatalf("output must end with CRLF: %q", text)
	}
	if strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\n") {
		t.Fatalf("output contains a bare LF; every line must be CRLF: %q", text)
	}
}
