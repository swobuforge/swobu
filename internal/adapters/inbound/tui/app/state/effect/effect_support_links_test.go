package effect

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestOpenSupportLinkEffect_ReportsFallbackOnSuccess(t *testing.T) {
	orig := startProcess
	startProcess = func(_ *exec.Cmd) error { return nil }
	t.Cleanup(func() { startProcess = orig })

	actions := (OpenSupportLinkEffect{Label: "ask question", URL: SupportAskQuestionURL}).Execute(context.Background())
	if len(actions) != 1 {
		t.Fatalf("len(actions)=%d want 1", len(actions))
	}
	note, ok := actions[0].(SupportLinkNoted)
	if !ok {
		t.Fatalf("action type=%T want SupportLinkNoted", actions[0])
	}
	if !strings.Contains(note.Message, "opened") || !strings.Contains(note.Message, SupportAskQuestionURL) {
		t.Fatalf("message=%q", note.Message)
	}
}

func TestOpenSupportLinkEffect_ReportsFallbackOnFailure(t *testing.T) {
	orig := startProcess
	startProcess = func(_ *exec.Cmd) error { return errors.New("boom") }
	t.Cleanup(func() { startProcess = orig })

	actions := (OpenSupportLinkEffect{Label: "file issue", URL: SupportFileIssueURL}).Execute(context.Background())
	if len(actions) != 1 {
		t.Fatalf("len(actions)=%d want 1", len(actions))
	}
	note, ok := actions[0].(SupportLinkNoted)
	if !ok {
		t.Fatalf("action type=%T want SupportLinkNoted", actions[0])
	}
	if !strings.Contains(note.Message, "fallback") || !strings.Contains(note.Message, SupportFileIssueURL) {
		t.Fatalf("message=%q", note.Message)
	}
}
