// Package cockpit is the interactive operator-surface launch seam.
//
// The subtree below it is intentionally split into:
//   - `engine/` for the extractable retained runtime framework
//   - `toolkit/` for generic batteries-included views
//   - `app/` for the Swobu cockpit itself
package cockpit

import (
	"context"
	"errors"
	"io"

	"github.com/gdamore/tcell/v2"

	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state"
	stateeffect "github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state/effect"
	rootviews "github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/views/root"
	"github.com/metrofun/swobu/internal/terminalui/apps/shared/daemonstate"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/host"
)

// Run is the interactive cockpit entry seam.
func Run(ctx context.Context, stdin io.Reader, stdout io.Writer, _ io.Writer) error {
	_ = stdin
	_ = stdout
	screen, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	runner := host.New(screen, rootviews.Root(), state.Model{
		HeaderStatus:  daemonstate.HeaderOfflineStale,
		DaemonState:   daemonstate.DaemonStateUnreachable,
		StreamEnabled: true,
	}, state.Reduce)
	restoreForegroundRunner := stateeffect.SetForegroundClientRunner(func(ctx context.Context, executable string, args []string, env map[string]string) (int, error) {
		exitCode, err := host.RunForegroundClient(ctx, executable, args, env)
		if err == nil {
			return exitCode, nil
		}
		if errors.Is(err, host.ErrForegroundHandoffActive) {
			return 0, stateeffect.ErrForegroundClientActive
		}
		if errors.Is(err, host.ErrForegroundHandoffUnavailable) {
			return 0, stateeffect.ErrForegroundClientUnavailable
		}
		return 0, err
	})
	defer restoreForegroundRunner()
	return runner.Run(ctx)
}
