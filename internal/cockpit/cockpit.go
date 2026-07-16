package cockpit

import (
	"context"
	"io"
	"os"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/adapters"
	"github.com/swobuforge/swobu/internal/cockpit/mountedrender"
	helppage "github.com/swobuforge/swobu/internal/cockpit/pages/help"
	workspace_page "github.com/swobuforge/swobu/internal/cockpit/pages/workspace"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"golang.org/x/term"
)

const (
	snapshotWidth          = 100
	snapshotHeight         = 24
	cockpitSnapshotNewline = "\n"
)

// Run launches the go-tui Cockpit over the daemon-backed operator read model.
// Non-interactive callers receive a deterministic rendered snapshot of the
// loaded Cockpit screen so tests and transcript contexts do not enter raw mode.
func Run(ctx context.Context, daemonURL string, stdin io.Reader, stdout, stderr io.Writer) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	adapter := adapters.NewLiveOperatorAdapter(nil, daemonURL)
	model, err := adapter.LoadCockpit(ctx)
	if err != nil {
		return err
	}
	model = applyCockpitDefaults(model)

	if !isInteractiveTerminal(stdin, stdout) {
		return renderCockpitSnapshot(stdout, model)
	}

	cockpit := NewCockpitWithContext(model, ctx, adapter, adapter)
	app, err := tui.NewApp(
		tui.WithRootComponent(cockpit),
		tui.WithOnResume(func() {
			if page := currentWorkspacePage(cockpit); page != nil {
				page.ActivitySection.Refresh()
			}
		}),
		tui.WithLegacyKeyboard(),
		tui.WithoutMouse(),
	)
	if err != nil {
		return err
	}
	if ctx.Done() != nil {
		go func() {
			<-ctx.Done()
			app.Stop()
		}()
	}
	return app.Run()
}

func isInteractiveTerminal(stdin io.Reader, stdout io.Writer) bool {
	in, inOK := stdin.(*os.File)
	out, outOK := stdout.(*os.File)
	if !inOK || !outOK {
		return false
	}
	return term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd()))
}

func currentWorkspacePage(c *Cockpit) *workspace_page.PageView {
	if c == nil {
		return nil
	}
	model := c.activeModel()
	if model.ActivePage != readmodel.CockpitWorkspacePage {
		return nil
	}
	return c.activeWorkspacePage(model)
}

func renderCockpitSnapshot(stdout io.Writer, model readmodel.CockpitReadModel) error {
	model = applyCockpitDefaults(model)
	snapshot, err := mountedrender.Trimmed(NewCockpit(model), snapshotWidth, snapshotHeight)
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdout, snapshot+cockpitSnapshotNewline)
	return err
}

func applyCockpitDefaults(model readmodel.CockpitReadModel) readmodel.CockpitReadModel {
	defaultHelp := helppage.DefaultModel()
	if model.Help.Version == "" {
		model.Help.Version = defaultHelp.Version
	}
	if model.Help.CockpitVersion == "" {
		model.Help.CockpitVersion = defaultHelp.CockpitVersion
	}
	if model.Help.DocsURL == "" {
		model.Help.DocsURL = defaultHelp.DocsURL
	}
	if model.Help.CommunityURL == "" {
		model.Help.CommunityURL = defaultHelp.CommunityURL
	}
	if model.Help.IssueURL == "" {
		model.Help.IssueURL = defaultHelp.IssueURL
	}
	return model
}
