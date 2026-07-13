package effect

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

const (
	SupportAskQuestionURL = "https://github.com/swobuforge/swobu/discussions/new/choose"
	SupportFileIssueURL   = "https://github.com/swobuforge/swobu/issues/new/choose"
)

func TestOpenSupportLinkEffect_ReportsFallbackOnSuccess(t *testing.T) {
	orig := startProcess
	startProcess = func(_ *exec.Cmd) error { return nil }
	t.Cleanup(func() { startProcess = orig })

	action := (OpenSupportLinkEffect{Label: "ask question", URL: SupportAskQuestionURL}).Run(context.Background())
	note, ok := action.(SupportLinkNoted)
	if !ok {
		t.Fatalf("action type=%T want SupportLinkNoted", action)
	}
	if !strings.Contains(note.Message, "opened") || !strings.Contains(note.Message, SupportAskQuestionURL) {
		t.Fatalf("message=%q", note.Message)
	}
}

func TestOpenSupportLinkEffect_ReportsFallbackOnFailure(t *testing.T) {
	orig := startProcess
	startProcess = func(_ *exec.Cmd) error { return errors.New("boom") }
	t.Cleanup(func() { startProcess = orig })

	action := (OpenSupportLinkEffect{Label: "file issue", URL: SupportFileIssueURL}).Run(context.Background())
	note, ok := action.(SupportLinkNoted)
	if !ok {
		t.Fatalf("action type=%T want SupportLinkNoted", action)
	}
	if !strings.Contains(note.Message, "fallback") || !strings.Contains(note.Message, SupportFileIssueURL) {
		t.Fatalf("message=%q", note.Message)
	}
}
