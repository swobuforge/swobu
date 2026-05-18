package effect

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

const (
	SupportAskQuestionURL = "https://github.com/swobuforge/swobu/discussions/new/choose"
	SupportFileIssueURL   = "https://github.com/swobuforge/swobu/issues/new/choose"
)

var startProcess = func(command *exec.Cmd) error {
	return command.Start()
}

type OpenSupportLinkEffect struct {
	Label string
	URL   string
}

func (cmd OpenSupportLinkEffect) Execute(ctx context.Context) []update.Action {
	label := strings.TrimSpace(cmd.Label) // swobu:io-string source=boundary
	if label == "" {
		label = "support"
	}
	rawURL := strings.TrimSpace(cmd.URL) // swobu:io-string source=boundary
	if rawURL == "" {
		return []update.Action{SupportLinkNoted{Message: "support link is missing"}}
	}
	err := openURL(ctx, rawURL)
	if err != nil {
		return []update.Action{SupportLinkNoted{
			Message: fmt.Sprintf("%s open failed; fallback %s", label, rawURL),
		}}
	}
	return []update.Action{SupportLinkNoted{
		Message: fmt.Sprintf("%s opened; fallback %s", label, rawURL),
	}}
}

type SupportLinkNoted struct{ Message string }

func openURL(ctx context.Context, rawURL string) error {
	var command *exec.Cmd
	if runtime.GOOS == "darwin" {
		command = exec.CommandContext(ctx, "open", rawURL)
	} else if runtime.GOOS == "windows" {
		command = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", rawURL)
	} else {
		command = exec.CommandContext(ctx, "xdg-open", rawURL)
	}
	return startProcess(command)
}
