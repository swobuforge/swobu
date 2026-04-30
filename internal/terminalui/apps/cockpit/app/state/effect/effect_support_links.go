package effect

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
)

const (
	SupportAskQuestionURL = "https://github.com/metrofun/swobu/discussions/new/choose"
	SupportFileIssueURL   = "https://github.com/metrofun/swobu/issues/new/choose"
)

var startProcess = func(command *exec.Cmd) error {
	return command.Start()
}

type OpenSupportLinkEffect struct {
	Label string
	URL   string
}

func (cmd OpenSupportLinkEffect) Execute(ctx context.Context) []update.Action {
	label := strings.TrimSpace(cmd.Label)
	if label == "" {
		label = "support"
	}
	rawURL := strings.TrimSpace(cmd.URL)
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
	switch runtime.GOOS {
	case "darwin":
		command = exec.CommandContext(ctx, "open", rawURL)
	case "windows":
		command = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		command = exec.CommandContext(ctx, "xdg-open", rawURL)
	}
	return startProcess(command)
}
