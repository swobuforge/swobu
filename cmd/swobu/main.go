package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/swobuforge/swobu/internal/adapters/inbound/cli"
	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/platform/logging"
)

func main() {
	logging.ConfigureDefaultLogger(os.Stderr)

	runner := &cli.Runner{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	root := newRootCommand(runner, os.Stdout, os.Stderr, isInteractiveTTY)

	if err := root.Execute(); err != nil {
		var codeErr *exitCodeError
		if ok := asExitCodeError(err, &codeErr); ok {
			os.Exit(codeErr.code)
		}
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func newRootCommand(runner *cli.Runner, stdout, stderr io.Writer, isInteractive func() bool) *cobra.Command {
	if runner == nil {
		runner = &cli.Runner{}
	}
	if runner.Stdout == nil {
		runner.Stdout = stdout
	}
	if runner.Stderr == nil {
		runner.Stderr = stderr
	}
	if isInteractive == nil {
		isInteractive = isInteractiveTTY
	}

	exitWithRunner := func(args []string) error {
		exitCode := int(runner.Run(context.Background(), args))
		if exitCode == 0 {
			return nil
		}
		return &exitCodeError{code: exitCode}
	}

	root := &cobra.Command{
		Use:   "swobu",
		Short: "Swobu local control boundary",
		Example: strings.Join([]string{
			"  swobu",
			"  swobu --addr 127.0.0.1:9000",
			"  swobu --addr 127.0.0.1:9000 --config ./swobu.yaml",
		}, "\n"),
		Version:       controlplane.SwobuVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !isInteractive() {
				return cmd.Help()
			}
			return exitWithRunner(nil)
		},
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.Flags().StringVar(&runner.Addr, "addr", "", fmt.Sprintf("daemon address for Cockpit attach-or-start (env: %s) (default: %s)", platformconfig.EnvAddr, platformconfig.DefaultAddr()))
	root.Flags().StringVar(&runner.ConfigPath, "config", "", fmt.Sprintf("daemon config path when Cockpit starts it (env: %s) (default: %s)", platformconfig.EnvConfigPath, platformconfig.DefaultConfigPath()))

	delegate := func(prefix ...string) func(*cobra.Command, []string) error {
		return func(_ *cobra.Command, args []string) error {
			runArgs := append(append([]string{}, prefix...), args...)
			return exitWithRunner(runArgs)
		}
	}

	daemonCmd := &cobra.Command{
		Use:                "daemon [args]",
		Short:              "Start daemon",
		DisableFlagParsing: true,
		RunE:               delegate("daemon"),
	}
	statusCmd := &cobra.Command{
		Use:                "status [args]",
		Short:              "Print daemon status",
		DisableFlagParsing: true,
		RunE:               delegate("status"),
	}
	connectCmd := &cobra.Command{
		Use:                "connect <client> [args]",
		Short:              "Configure a supported local client",
		DisableFlagParsing: true,
		RunE:               delegate("connect"),
	}
	downCmd := &cobra.Command{
		Use:                "down [args]",
		Short:              "Request daemon shutdown",
		DisableFlagParsing: true,
		RunE:               delegate("daemon", "down"),
	}
	daemonCmd.AddCommand(downCmd)
	telemetryCmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Telemetry controls",
	}
	telemetryStatusCmd := &cobra.Command{
		Use:                "status [args]",
		Short:              "Show telemetry state",
		DisableFlagParsing: true,
		RunE:               delegate("telemetry", "status"),
	}
	telemetryOnCmd := &cobra.Command{
		Use:                "on [args]",
		Short:              "Enable telemetry",
		DisableFlagParsing: true,
		RunE:               delegate("telemetry", "on"),
	}
	telemetryOffCmd := &cobra.Command{
		Use:                "off [args]",
		Short:              "Disable telemetry",
		DisableFlagParsing: true,
		RunE:               delegate("telemetry", "off"),
	}
	telemetryCmd.AddCommand(telemetryStatusCmd, telemetryOnCmd, telemetryOffCmd)

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version",
		RunE:  delegate("version"),
	}

	root.AddCommand(connectCmd, daemonCmd, statusCmd, telemetryCmd, versionCmd)
	return root
}

type exitCodeError struct {
	code int
}

func (e *exitCodeError) Error() string {
	return "swobu command failed"
}

func asExitCodeError(err error, target **exitCodeError) bool {
	if err == nil {
		return false
	}
	codeErr, ok := err.(*exitCodeError)
	if !ok {
		return false
	}
	*target = codeErr
	return true
}

func isInteractiveTTY() bool {
	statIn, errIn := os.Stdin.Stat()
	statOut, errOut := os.Stdout.Stat()
	if errIn != nil || errOut != nil {
		return false
	}
	return (statIn.Mode()&os.ModeCharDevice) != 0 && (statOut.Mode()&os.ModeCharDevice) != 0
}
