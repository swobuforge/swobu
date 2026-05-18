package cli

import (
	"io"

	"github.com/swobuforge/swobu/internal/app/operator/daemonlifecycle"
	uicli "github.com/swobuforge/swobu/internal/terminalui/apps/cli"
)

func startupReporterFromWriter(out io.Writer) daemonlifecycle.StartupReporter {
	return uicli.NewStartupConsolePresenter(out).DaemonLifecycleReporter()
}
