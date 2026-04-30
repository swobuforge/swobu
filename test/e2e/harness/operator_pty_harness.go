package harness

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/metrofun/swobu/test/framework/ptykit"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[()#][0-9A-Za-z]`)
var spaceRunPattern = regexp.MustCompile(`\s+`)

// OperatorPTYJourney is a declarative wrapper over a real PTY harness.
//
// Journeys call intent-level methods while this helper still sends real keys
// and reads real terminal output.
type OperatorPTYJourney struct {
	t   *testing.T
	ctx context.Context
	run *ptykit.HarnessCloser
}

func StartSwobuOperatorPTY(t *testing.T, cols int, rows int) *ptykit.HarnessCloser {
	t.Helper()

	cmd := exec.Command(SwobuBinaryPath(t))
	// Keep test-scoped daemon URL and related env overrides explicit for child PTY
	// processes instead of relying on implicit inheritance behavior.
	cmd.Env = os.Environ()
	if daemonURL := strings.TrimSpace(os.Getenv("SWOBU_DAEMON_URL")); daemonURL != "" {
		cmd.Env = append(cmd.Env, "SWOBU_DAEMON_URL="+daemonURL)
	}
	if configPath := strings.TrimSpace(os.Getenv("SWOBU_CONFIG_PATH")); configPath != "" {
		cmd.Env = append(cmd.Env, "SWOBU_CONFIG_PATH="+configPath)
	}
	run, err := ptykit.StartCommandWithSize(cmd, cols, rows)
	if err != nil {
		t.Skipf("pty unavailable: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		_ = run.Shutdown(shutdownCtx)
	})
	return run
}

func StartSwobuOperatorPTYJourney(ctx context.Context, t *testing.T, cols int, rows int) OperatorPTYJourney {
	t.Helper()
	if ctx == nil {
		ctx = context.Background()
	}
	return OperatorPTYJourney{
		t:   t,
		ctx: ctx,
		run: StartSwobuOperatorPTY(t, cols, rows),
	}
}

func (j OperatorPTYJourney) WaitVisible(needle string) {
	j.t.Helper()
	WaitForVisibleTerminalContains(j.ctx, j.t, j.run, needle)
}

func (j OperatorPTYJourney) WaitVisibleAny(needles ...string) {
	j.t.Helper()
	WaitForVisibleTerminalContainsAny(j.ctx, j.t, j.run, needles...)
}

func (j OperatorPTYJourney) FocusClientAccessRow() {
	j.t.Helper()
	j.EnsureClientsSectionOpen()
	chooseToken := compactVisibleText("choose")
	notSetToken := compactVisibleText("not set")
	for i := 0; i < 120; i++ {
		focused := focusedLineToken(j.run.VisibleOutput())
		if strings.HasPrefix(focused, compactVisibleText("client")) &&
			(strings.Contains(focused, chooseToken) || strings.Contains(focused, notSetToken)) {
			return
		}
		SendOperatorKey(j.t, j.run, "down")
		time.Sleep(15 * time.Millisecond)
	}
	j.t.Fatalf("could not focus client access row; visible=%q", normalizeVisibleText(j.run.VisibleOutput()))
}

func (j OperatorPTYJourney) ActivateFocusedRow() {
	j.t.Helper()
	SendOperatorKey(j.t, j.run, "enter")
}

func (j OperatorPTYJourney) SendKey(key string) {
	j.t.Helper()
	SendOperatorKey(j.t, j.run, key)
}

func (j OperatorPTYJourney) VisibleOutput() string {
	j.t.Helper()
	return j.run.VisibleOutput()
}

// TypeText sends text as raw PTY input for deterministic inline-editor typing.
func (j OperatorPTYJourney) TypeText(value string) {
	j.t.Helper()
	if err := j.run.SendRaw(value); err != nil {
		j.t.Fatalf("type text %q: %v", value, err)
	}
}

// CopyClientBaseURLFromClientAccess selects Other and triggers copy via the
// explicit inline copy action after payload disclosure.
func (j OperatorPTYJourney) CopyClientBaseURLFromClientAccess() {
	j.t.Helper()
	j.FocusClientAccessRow()
	j.ActivateFocusedRow()
	j.WaitVisible("Other")
	j.FocusRow("Other")
	j.ActivateFocusedRow()
	j.WaitVisible("copy values")
	j.FocusRowDown("copy values")
	j.ActivateFocusedRow()
}

// RefreshLatestTraffic drives four rows up from clients/check-access and
// activates the traffic/latest refresh action.
func (j OperatorPTYJourney) RefreshLatestTraffic() {
	j.t.Helper()
	j.FocusRowDown("latest")
	j.ActivateFocusedRow()
}

func (j OperatorPTYJourney) EnsureClientsSectionOpen() {
	j.t.Helper()
	visible := strings.ToLower(j.VisibleOutput())
	if strings.Contains(visible, "client") && strings.Contains(visible, "choose") {
		return
	}
	j.FocusRow("clients")
	j.ActivateFocusedRow()
	j.WaitVisible("choose")
}

func (j OperatorPTYJourney) OpenClientChooser() {
	j.t.Helper()
	j.FocusClientAccessRow()
	j.ActivateFocusedRow()
}

func (j OperatorPTYJourney) SelectClient(label string) {
	j.t.Helper()
	j.OpenClientChooser()
	labelToken := compactVisibleText(label)
	found := false
	for i := 0; i < 180; i++ {
		visibleToken := compactVisibleText(j.run.VisibleOutput())
		if strings.Contains(visibleToken, labelToken) {
			found = true
			break
		}
		SendOperatorKey(j.t, j.run, "down")
		time.Sleep(15 * time.Millisecond)
	}
	if !found {
		j.t.Fatalf("could not find client %q in chooser; visible=%q", label, normalizeVisibleText(j.run.VisibleOutput()))
	}
	j.FocusRow(label)
	j.ActivateFocusedRow()
	j.WaitVisible(label)
	j.WaitVisible("choose")
}

func (j OperatorPTYJourney) FocusRow(label string) {
	j.t.Helper()
	j.focusRowAny(label)
}

func (j OperatorPTYJourney) FocusRowDown(label string) {
	j.t.Helper()
	j.focusRow(label, "down")
}

func (j OperatorPTYJourney) FocusRowUp(label string) {
	j.t.Helper()
	j.focusRow(label, "up")
}

func (j OperatorPTYJourney) focusRow(label string, direction string) {
	j.t.Helper()
	label = strings.TrimSpace(label)
	labelToken := compactVisibleText(label)
	if labelToken == "" {
		j.t.Fatalf("empty row label")
	}
	key := strings.ToLower(strings.TrimSpace(direction))
	if key != "up" && key != "down" {
		j.t.Fatalf("unsupported focus direction %q", direction)
	}
	if labelToken == compactVisibleText("name") {
		visible := j.run.VisibleOutput()
		if strings.Contains(visible, "choose a workspace name") && visibleHasFocusedLabel(visible, compactVisibleText("workspace")) {
			for i := 0; i < 4; i++ {
				if visibleHasFocusedLabel(j.run.VisibleOutput(), labelToken) {
					return
				}
				SendOperatorKey(j.t, j.run, "down")
				time.Sleep(15 * time.Millisecond)
			}
		}
	}
	for i := 0; i < 120; i++ {
		if visibleHasFocusedLabel(j.run.VisibleOutput(), labelToken) {
			time.Sleep(10 * time.Millisecond)
			if visibleHasFocusedLabel(j.run.VisibleOutput(), labelToken) {
				return
			}
		}
		SendOperatorKey(j.t, j.run, key)
		time.Sleep(15 * time.Millisecond)
	}
	for i := 0; i < 120; i++ {
		compact := compactVisibleText(j.run.VisibleOutput())
		if strings.Contains(compact, ">"+labelToken) || strings.Contains(compact, "›"+labelToken) {
			return
		}
		SendOperatorKey(j.t, j.run, key)
		time.Sleep(15 * time.Millisecond)
	}
	j.t.Fatalf("could not focus row %q; visible=%q", label, normalizeVisibleText(j.run.VisibleOutput()))
}

func (j OperatorPTYJourney) focusRowAny(label string) {
	j.t.Helper()
	label = strings.TrimSpace(label)
	labelToken := compactVisibleText(label)
	if labelToken == "" {
		j.t.Fatalf("empty row label")
	}
	visibleRaw := j.run.VisibleOutput()
	visibleToken := compactVisibleText(visibleRaw)
	if labelToken == "name" &&
		strings.Contains(visibleToken, labelToken) &&
		(strings.Contains(visibleRaw, "saving") || strings.Contains(visibleRaw, "saved")) {
		return
	}
	if visibleHasFocusedLabel(visibleRaw, labelToken) {
		return
	}
	if labelToken == compactVisibleText("name") &&
		strings.Contains(visibleRaw, "choose a workspace name") &&
		visibleHasFocusedLabel(visibleRaw, compactVisibleText("workspace")) {
		SendOperatorKey(j.t, j.run, "down")
		time.Sleep(15 * time.Millisecond)
		if visibleHasFocusedLabel(j.run.VisibleOutput(), labelToken) {
			return
		}
	}
	if labelToken == compactVisibleText("routing") &&
		(visibleHasFocusedLabel(visibleRaw, compactVisibleText("workspace")) ||
			strings.Contains(compactVisibleText(visibleRaw), ">workspace")) {
		for i := 0; i < 8; i++ {
			SendOperatorKey(j.t, j.run, "down")
			time.Sleep(15 * time.Millisecond)
			if visibleHasFocusedLabel(j.run.VisibleOutput(), labelToken) {
				return
			}
		}
	}
	// Try both directions because disclosure rows may be inserted above or below
	// the current focus depending on prior interactions.
	for _, key := range []string{"down", "up"} {
		for i := 0; i < 120; i++ {
			if visibleHasFocusedLabel(j.run.VisibleOutput(), labelToken) {
				time.Sleep(10 * time.Millisecond)
				if visibleHasFocusedLabel(j.run.VisibleOutput(), labelToken) {
					return
				}
			}
			SendOperatorKey(j.t, j.run, key)
			time.Sleep(15 * time.Millisecond)
		}
	}
	// Fallback for PTY frames that collapse line breaks: detect focused-row token
	// in compact output and drive focus linearly.
	for _, key := range []string{"down", "up"} {
		for i := 0; i < 120; i++ {
			compact := compactVisibleText(j.run.VisibleOutput())
			if strings.Contains(compact, ">"+labelToken) || strings.Contains(compact, "›"+labelToken) {
				return
			}
			SendOperatorKey(j.t, j.run, key)
			time.Sleep(15 * time.Millisecond)
		}
	}
	j.t.Fatalf("could not focus row %q; visible=%q", label, normalizeVisibleText(j.run.VisibleOutput()))
}

func (j OperatorPTYJourney) AssertOutputContains(needle string) {
	j.t.Helper()
	AssertTerminalOutputContains(j.t, j.run, needle)
}

func (j OperatorPTYJourney) AssertVisibleContains(needle string) {
	j.t.Helper()
	AssertVisibleTerminalOutputContains(j.t, j.run, needle)
}

func (j OperatorPTYJourney) AssertVisibleOmits(needle string) {
	j.t.Helper()
	AssertVisibleTerminalOutputOmits(j.t, j.run, needle)
}

func WaitForTerminalContains(ctx context.Context, t *testing.T, run *ptykit.HarnessCloser, needle string) {
	t.Helper()
	if err := run.WaitForContains(ctx, needle); err != nil {
		t.Fatalf("wait for %q: %v", needle, err)
	}
}

func WaitForVisibleTerminalContains(ctx context.Context, t *testing.T, run *ptykit.HarnessCloser, needle string) {
	t.Helper()
	WaitForVisibleTerminalContainsAny(ctx, t, run, needle)
}

func WaitForVisibleTerminalContainsAny(ctx context.Context, t *testing.T, run *ptykit.HarnessCloser, needles ...string) {
	t.Helper()

	needleNorm := make([]string, 0, len(needles))
	needleCompact := make([]string, 0, len(needles))
	for _, needle := range needles {
		normalized := normalizeVisibleText(needle)
		compact := compactVisibleText(needle)
		if normalized != "" {
			needleNorm = append(needleNorm, normalized)
		}
		if compact != "" {
			needleCompact = append(needleCompact, compact)
		}
	}
	if len(needleNorm) == 0 && len(needleCompact) == 0 {
		return
	}

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		visible := normalizeVisibleText(run.VisibleOutput())
		compactVisible := compactVisibleText(visible)
		if containsAny(visible, needleNorm) || containsAny(compactVisible, needleCompact) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for visible one of %q: %v\nvisible:\n%s\nraw:\n%s", needles, ctx.Err(), run.VisibleOutput(), StripANSIEscapeSequences(run.Output()))
		case <-ticker.C:
		}
	}
}

func SendOperatorKey(t *testing.T, run *ptykit.HarnessCloser, key string) {
	t.Helper()
	normalized := normalizePTYKeyName(key)
	if normalized == " " {
		if err := run.SendRaw(" "); err != nil {
			t.Fatalf("send key %q: %v", key, err)
		}
		return
	}
	if err := run.SendKey(normalized); err == nil {
		return
	}
	if len([]rune(strings.TrimSpace(key))) == 1 {
		if err := run.SendRaw(key); err == nil {
			return
		}
	}
	if err := run.SendKey(normalized); err != nil {
		t.Fatalf("send key %q: %v", key, err)
	}
}

func SendOperatorKeyTimes(t *testing.T, run *ptykit.HarnessCloser, key string, times int) {
	t.Helper()
	for i := 0; i < times; i++ {
		SendOperatorKey(t, run, key)
	}
}

func AssertTerminalOutputContains(t *testing.T, run *ptykit.HarnessCloser, needle string) {
	t.Helper()
	output := run.Output()
	if !strings.Contains(output, needle) {
		t.Fatalf("terminal output missing %q: %q", needle, output)
	}
}

func AssertVisibleTerminalOutputContains(t *testing.T, run *ptykit.HarnessCloser, needle string) {
	t.Helper()
	output := normalizeVisibleText(run.VisibleOutput())
	needleNorm := normalizeVisibleText(needle)
	if (needleNorm != "" && strings.Contains(output, needleNorm)) ||
		(compactVisibleText(needle) != "" && strings.Contains(compactVisibleText(output), compactVisibleText(needle))) {
		return
	}
	t.Fatalf("visible terminal output missing %q: %q", needle, output)
}

func AssertVisibleTerminalOutputOmits(t *testing.T, run *ptykit.HarnessCloser, needle string) {
	t.Helper()
	output := normalizeVisibleText(run.VisibleOutput())
	needleNorm := normalizeVisibleText(needle)
	if needleNorm != "" && strings.Contains(output, needleNorm) {
		t.Fatalf("visible terminal output unexpectedly contains %q: %q", needle, output)
	}
	needleCompact := compactVisibleText(needle)
	if needleCompact != "" && strings.Contains(compactVisibleText(output), needleCompact) {
		t.Fatalf("visible terminal output unexpectedly contains %q: %q", needle, output)
	}
}

func StripANSIEscapeSequences(value string) string {
	return ansiEscapePattern.ReplaceAllString(value, "")
}

func normalizePTYKeyName(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "enter":
		return "Enter"
	case "down":
		return "Down"
	case "up":
		return "Up"
	case "left":
		return "Left"
	case "right":
		return "Right"
	case "tab":
		return "Tab"
	case "shift+tab", "shifttab":
		return "ShiftTab"
	case "esc":
		return "Esc"
	case "backspace":
		return "Backspace"
	case "space":
		return " "
	default:
		return key
	}
}

func normalizeVisibleText(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if unicode.IsSpace(r) {
			b.WriteRune(' ')
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(spaceRunPattern.ReplaceAllString(b.String(), " "))
}

func compactVisibleText(value string) string {
	normalized := normalizeVisibleText(value)
	if normalized == "" {
		return ""
	}
	return strings.ReplaceAll(normalized, " ", "")
}

func containsAny(haystack string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}

func visibleHasFocusedLabel(visible string, labelToken string) bool {
	for _, line := range strings.Split(visible, "\n") {
		focused := focusedLineToken(line)
		if focused == "" {
			continue
		}
		if focusedLabelMatches(focused, labelToken) {
			return true
		}
	}
	compact := compactVisibleText(visible)
	if compact == "" || labelToken == "" {
		return false
	}
	return strings.Contains(compact, ">"+labelToken) || strings.Contains(compact, "›"+labelToken)
}

func focusedLabelMatches(focused string, labelToken string) bool {
	if labelToken == "" {
		return false
	}
	return strings.HasPrefix(focused, labelToken)
}

func focusedLineToken(line string) string {
	runes := []rune(line)
	for i, r := range runes {
		if r != '>' {
			continue
		}
		if i > 0 {
			prev := runes[i-1]
			if prev == '-' || prev == '[' {
				continue
			}
		}
		j := i + 1
		for j < len(runes) && unicode.IsSpace(runes[j]) {
			j++
		}
		if j < len(runes) && runes[j] == '-' {
			j++
			for j < len(runes) && unicode.IsSpace(runes[j]) {
				j++
			}
		}
		if j >= len(runes) || !unicode.IsLetter(runes[j]) {
			continue
		}
		return compactVisibleText(string(runes[j:]))
	}
	return ""
}
