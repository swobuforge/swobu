package cockpit

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// Run is the temporary launch entrypoint for the go-tui Cockpit.
// It is intentionally minimal: it prints a plain placeholder message so the
// CLI compiles and the launch seam is wired, without resurrecting any
// retained terminalui interactive path.
//
// TODO(epic-05-tui-cockpit-v2): replace with real go-tui app loop once the
// harness and fixture baseline are proven (task 10+).
func Run(ctx context.Context, daemonURL string, stdin io.Reader, stdout, stderr io.Writer) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	_, _ = fmt.Fprintln(stdout, "Cockpit placeholder — go-tui app loop not yet implemented.")
	return errors.New("not implemented yet")
}
