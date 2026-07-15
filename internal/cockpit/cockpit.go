package cockpit

import (
	"context"
	"io"
	"os"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/adapters"
	helppage "github.com/swobuforge/swobu/internal/cockpit/pages/help"
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

	app, err := tui.NewApp(
		tui.WithRootComponent(NewCockpitWithContext(model, ctx, adapter, adapter)),
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

func renderCockpitSnapshot(stdout io.Writer, model readmodel.CockpitReadModel) error {
	model = applyCockpitDefaults(model)
	root := NewCockpit(model).Render(nil)
	buffer := tui.NewBuffer(snapshotWidth, snapshotHeight)
	root.Render(buffer, snapshotWidth, snapshotHeight)
	_, err := io.WriteString(stdout, buffer.StringTrimmed()+cockpitSnapshotNewline)
	return err
}

func applyCockpitDefaults(model readmodel.CockpitReadModel) readmodel.CockpitReadModel {
	if model.Help == (readmodel.HelpReadModel{}) {
		model.Help = helppage.DefaultModel()
	}
	return model
}
