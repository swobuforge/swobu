package main

import (
	"context"
	"os"

	"github.com/spf13/cobra"
	"github.com/swobuforge/swobu/internal/adapters/inbound/cli"
	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
)

type exitCodeError struct {
	code int
}

func (e exitCodeError) Error() string {
	return "swobu command failed"
}

func main() {
	runner := cli.Runner{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}

	root := &cobra.Command{
		Use:           "swobu",
		Short:         "Swobu local control boundary",
		Version:       controlplane.SwobuVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			// Default invocation keeps existing interactive behavior.
			exitCode := int(runner.Run(context.Background(), nil))
			if exitCode != 0 {
				return exitCodeError{code: exitCode}
			}
			return nil
		},
	}

	delegate := func(prefix []string) func(*cobra.Command, []string) error {
		return func(_ *cobra.Command, args []string) error {
			runArgs := append(append([]string{}, prefix...), args...)
			exitCode := int(runner.Run(context.Background(), runArgs))
			if exitCode != 0 {
				return exitCodeError{code: exitCode}
			}
			return nil
		}
	}

	daemonCmd := &cobra.Command{
		Use:                "daemon [args]",
		Short:              "Start daemon",
		DisableFlagParsing: true,
		RunE:               delegate([]string{"daemon"}),
	}
	statusCmd := &cobra.Command{
		Use:                "status [args]",
		Short:              "Print daemon status",
		DisableFlagParsing: true,
		RunE:               delegate([]string{"status"}),
	}
	downCmd := &cobra.Command{
		Use:                "down [args]",
		Short:              "Request daemon shutdown",
		DisableFlagParsing: true,
		RunE:               delegate([]string{"down"}),
	}
	telemetryCmd := &cobra.Command{
		Use:                "telemetry [args]",
		Short:              "Telemetry controls",
		DisableFlagParsing: true,
		RunE:               delegate([]string{"telemetry"}),
	}
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version",
		RunE:  delegate([]string{"version"}),
	}

	root.AddCommand(daemonCmd, statusCmd, downCmd, telemetryCmd, versionCmd)

	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.SetVersionTemplate("{{.Version}}\n")

	if err := root.Execute(); err != nil {
		if codeErr, ok := err.(exitCodeError); ok {
			os.Exit(codeErr.code)
		}
		os.Exit(1)
	}
}
