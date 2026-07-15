// Package testscreen/testpath owns canonical test-id tokenization and path
// derivation shared across testscreen consumers.
//
// Boundary law:
//   - no test runtime concerns (retry, timing, session, pty)
//   - no fixture IO
//   - pure string/pathname helpers only
package testpath

import (
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var (
	unsafePattern = regexp.MustCompile(`[^a-z0-9_\-]+`)
	dupSepPattern = regexp.MustCompile(`[_\-]+`)
)

// Token normalizes arbitrary labels into stable filesystem-safe tokens.
func Token(raw string) string {
	v := strings.TrimSpace(strings.ToLower(raw))
	v = strings.ReplaceAll(v, "/", "_")
	v = strings.ReplaceAll(v, "\\", "_")
	v = strings.ReplaceAll(v, ":", "_")
	v = strings.ReplaceAll(v, " ", "_")
	v = unsafePattern.ReplaceAllString(v, "_")
	v = dupSepPattern.ReplaceAllString(v, "_")
	v = strings.Trim(v, "_")
	if v == "" {
		return "unnamed"
	}
	return v
}

// TestID is the canonical <testfile>__<testname> identity token.
func TestID(testFileStem, testName string) string {
	return Token(testFileStem) + "__" + Token(testName)
}

// TestIDToken normalizes a caller-supplied test identity while preserving the
// canonical double-underscore separator between identity parts.
func TestIDToken(raw string) string {
	parts := strings.Split(strings.TrimSpace(raw), "__")
	if len(parts) < 2 {
		return Token(raw)
	}
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		tokens = append(tokens, Token(part))
	}
	return strings.Join(tokens, "__")
}

// CallerTestFrame locates the nearest non-excluded *_test.go caller frame.
func CallerTestFrame(skipContains []string) (file string, fn string, line int, ok bool) {
	pcs := make([]uintptr, 64)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if strings.HasSuffix(frame.File, "_test.go") && !containsAny(frame.File, skipContains) {
			return frame.File, frame.Function, frame.Line, true
		}
		if !more {
			break
		}
	}
	return "", "", 0, false
}

// FunctionNameToken normalizes a Go function name into a stable filesystem
// token without caller line-number entropy.
func FunctionNameToken(function string) string {
	return FunctionToken(function, 0)
}

// FunctionToken normalizes a Go function name into a filesystem token.
// Pass line=0 for stable test identities. Non-zero line numbers are reserved
// for rare call-site disambiguation and must not be used for visual fixtures.
func FunctionToken(function string, line int) string {
	fn := strings.TrimSpace(function)
	if idx := strings.LastIndex(fn, "."); idx >= 0 && idx+1 < len(fn) {
		fn = fn[idx+1:]
	}
	if line > 0 {
		return Token(fmt.Sprintf("%s_l%d", fn, line))
	}
	return Token(fn)
}

// CallerTestID locates the nearest non-excluded *_test.go caller frame and
// returns the canonical <testfile>__<testname> identity without line numbers.
func CallerTestID(skipContains []string) (string, bool) {
	file, fn, _, ok := CallerTestFrame(skipContains)
	if !ok {
		return "", false
	}
	return TestID(FileStem(file), FunctionNameToken(fn)), true
}

// FileStem extracts the normalized test-file stem from a path.
func FileStem(path string) string {
	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	stem = strings.TrimSuffix(stem, "_test")
	return Token(stem)
}

func containsAny(path string, needles []string) bool {
	for _, n := range needles {
		n = strings.TrimSpace(n)
		if n != "" && strings.Contains(path, n) {
			return true
		}
	}
	return false
}
