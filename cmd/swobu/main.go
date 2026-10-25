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
	if err := logging.ConfigureDefault(os.Stderr); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "invalid logging configuration: %v\n", err)
		os.Exit(2)
	}

	runner := &cli.Runner{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	root := newRootCommand(runner, os.Stdout, os.Stderr)

	if err := root.Execute(); err != nil {
		var codeErr *exitCodeError
		if ok := asExitCodeError(err, &codeErr); ok {
			os.Exit(codeErr.code)
		}
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func newRootCommand(runner *cli.Runner, stdout, stderr io.Writer) *cobra.Command {
	if runner == nil {
		runner = &cli.Runner{}
	}
	if runner.Stdout == nil {
		runner.Stdout = stdout
	}
	if runner.Stderr == nil {
		runner.Stderr = stderr
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
		RunE: func(_ *cobra.Command, _ []string) error {
			return exitWithRunner(nil)
		},
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.Flags().StringVar(&runner.Addr, "addr", "", fmt.Sprintf("daemon address for local Web Operator attach-or-start (env: %s) (default: %s)", platformconfig.EnvAddr, platformconfig.DefaultAddr()))
	root.Flags().StringVar(&runner.ConfigPath, "config", "", fmt.Sprintf("daemon config path when starting the local Web Operator (env: %s) (default: %s)", platformconfig.EnvConfigPath, platformconfig.DefaultConfigPath()))

	delegate := func(prefix ...string) func(*cobra.Command, []string) error {
		return func(_ *cobra.Command, args []string) error {
			runArgs := append(append([]string{}, prefix...), args...)
			return exitWithRunner(runArgs)
		}
	}

	// The binary's command tree and leaf help must reflect the existing Runner
	// dispatch. Leaves pass their flags through to the single standard-library
	// parser that enforces original review, recovery and local-node authority.
	leaf := func(use, short string, path ...string) *cobra.Command {
		return &cobra.Command{Use: use, Short: short, DisableFlagParsing: true, RunE: delegate(path...)}
	}

	openCmd := &cobra.Command{Use: "open [args]", Short: "Open an authenticated local Web Operator session", DisableFlagParsing: true, RunE: delegate("open")}
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
	credentialCmd := &cobra.Command{Use: "credential", Short: "Confirm credential issuance or selected revocation", RunE: delegate("credential")}
	credentialCmd.AddCommand(
		leaf("issue", "Issue a scoped credential after local confirmation", "credential", "issue"),
		leaf("revoke", "Revoke one credential after local confirmation", "credential", "revoke"),
	)
	principalCmd := &cobra.Command{Use: "principal", Short: "Inspect and manage Principal identity and authority", RunE: delegate("principal")}
	principalCmd.AddCommand(
		leaf("list", "List managed Principals and their Workspace permissions", "principal", "list"),
		leaf("create", "Create a Principal with reviewed attribution", "principal", "create"),
		leaf("label", "Change a Principal label after local confirmation", "principal", "label"),
		leaf("revoke", "Revoke a Principal and its credentials", "principal", "revoke"),
	)
	workspaceCmd := &cobra.Command{Use: "workspace", Short: "Review and apply Workspace mutations and inspect limits", RunE: delegate("workspace")}
	permissionCmd := &cobra.Command{Use: "permission", Short: "Review a Principal's Workspace authority", RunE: delegate("workspace", "permission")}
	permissionCmd.AddCommand(
		leaf("grant", "Grant Workspace use after local confirmation", "workspace", "permission", "grant"),
		leaf("revoke", "Revoke Workspace use after local confirmation", "workspace", "permission", "revoke"),
	)
	workspaceCmd.AddCommand(
		leaf("create", "Create an empty Workspace after local confirmation", "workspace", "create"),
		leaf("display-name", "Change a Workspace display name", "workspace", "display-name"),
		leaf("delete", "Delete a Workspace after local confirmation", "workspace", "delete"),
		permissionCmd,
		leaf("limits", "Inspect Workspace accounting limits and estimated spend", "workspace", "limits"),
	)
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
	telemetryInspectCmd := leaf("inspect", "Inspect local Node product telemetry without uploading", "telemetry", "inspect")
	telemetryCmd.AddCommand(telemetryStatusCmd, telemetryOnCmd, telemetryOffCmd, telemetryInspectCmd)

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Args:  cobra.NoArgs,
		RunE:  delegate("version"),
	}
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update a standalone Swobu installation",
		Args:  cobra.NoArgs,
		RunE:  delegate("update"),
	}

	configCmd := &cobra.Command{Use: "config", Short: "Validate Node authority offline", RunE: delegate("config")}
	configCmd.AddCommand(leaf("validate", "Validate existing configuration without starting the daemon", "config", "validate"))
	root.AddCommand(openCmd, configCmd, credentialCmd, principalCmd, workspaceCmd, connectCmd, daemonCmd, statusCmd, telemetryCmd, updateCmd, versionCmd)
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
