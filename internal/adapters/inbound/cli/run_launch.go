package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/clientconnect"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

func runLaunch(ctx context.Context, httpClient *http.Client, stdout, stderr io.Writer, args []string, runner Runner) ExitCode {
	if len(args) == 0 || args[0] != "antigravity" {
		_, _ = fmt.Fprintln(stderr, "usage: swobu launch antigravity [--workspace <name>] [--addr <host:port>] -- [agy args...]")
		return ExitDown
	}
	fs := flag.NewFlagSet("launch antigravity", flag.ContinueOnError)
	fs.SetOutput(stderr)
	workspace := fs.String("workspace", "", "workspace name")
	addr := fs.String("addr", "", "daemon address")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	childArgs := fs.Args()
	startup, err := platformconfig.ResolveStartupConfig(*addr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return ExitDown
	}
	attach := runner.ConnectAttach
	if attach == nil {
		attach = defaultAttachOrStart
	}
	if err := attach(ctx, stdout, stderr, httpClient, startup.Addr, platformconfig.ResolveConfigPath(runner.ConfigPath)); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return ExitDown
	}
	lister := runner.ConnectWorkspaces
	if lister == nil {
		lister = operatorclient.New(httpClient, platformconfig.BaseURL(startup.Addr))
	}
	summaries, err := lister.ListWorkspaces(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return ExitDown
	}
	slug, err := resolveConnectWorkspace(summaries, *workspace)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return ExitDown
	}
	target, err := clientconnect.NewTarget(slug, platformconfig.BaseURL(startup.Addr)+"/c/"+slug)
	if err != nil || !target.IsLocal() {
		_, _ = fmt.Fprintln(stderr, "Antigravity launch requires a loopback Swobu workspace")
		return ExitDown
	}
	run := runner.RunAntigravity
	if run == nil {
		run = execAntigravity
	}
	env := childEnvironment(os.Environ(), target.WorkspaceURL())
	var versionOutput bytes.Buffer
	if err := run(ctx, []string{"--version"}, env, nil, &versionOutput, &versionOutput); err != nil {
		_, _ = fmt.Fprintln(stderr, "Could not determine Antigravity version:", err)
		return ExitDown
	}
	if err := requireSupportedAntigravityVersion(versionOutput.String()); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return ExitDown
	}
	ops := runner.ConnectOperations
	if ops == nil {
		ops = clientconnect.NewService()
	}
	plan, err := ops.Plan(ctx, clientconnect.ClientAntigravity, target)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return ExitDown
	}
	if plan.RequiresReplace() {
		_, _ = fmt.Fprintln(stderr, "Existing Antigravity configuration conflicts with Gemini mode. Run `swobu connect antigravity --replace` first.")
		return ExitDown
	}
	if !plan.AlreadyConfigured() {
		verified, applyErr := ops.Apply(ctx, plan)
		if applyErr != nil {
			_, _ = fmt.Fprintln(stderr, applyErr)
			return ExitDown
		}
		if !verified.AlreadyConfigured() {
			_, _ = fmt.Fprintln(stderr, "Antigravity did not converge to Gemini mode")
			return ExitDown
		}
	}
	if err := run(ctx, childArgs, env, runner.Stdin, stdout, stderr); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return ExitCode(exitError.ExitCode())
		}
		return ExitDown
	}
	return ExitHealthy
}

var antigravityVersionPattern = regexp.MustCompile(`^\s*(?:agy[ \t]+)?(\d+)\.(\d+)\.(\d+)[ \t\r\n]*$`)

func requireSupportedAntigravityVersion(output string) error {
	match := antigravityVersionPattern.FindStringSubmatch(output)
	if match == nil {
		return fmt.Errorf("Antigravity version output is unrecognized; install Antigravity 1.2.3 or newer")
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	patch, _ := strconv.Atoi(match[3])
	if major < 1 || major == 1 && (minor < 2 || minor == 2 && patch < 3) {
		return fmt.Errorf("Antigravity %s is unsupported; install Antigravity 1.2.3 or newer", strings.TrimSpace(match[0]))
	}
	return nil
}

func execAntigravity(ctx context.Context, args, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, "agy", args...)
	command.Env = env
	command.Stdin, command.Stdout, command.Stderr = stdin, stdout, stderr
	return command.Run()
}
func childEnvironment(env []string, workspaceURL string) []string {
	return replaceEnvironment(env, "GEMINI_API_KEY", "swobu-local", "GOOGLE_GEMINI_BASE_URL", workspaceURL)
}
func replaceEnvironment(env []string, pairs ...string) []string {
	replacements := map[string]string{}
	for i := 0; i < len(pairs); i += 2 {
		replacements[pairs[i]] = pairs[i+1]
	}
	out := make([]string, 0, len(env)+len(replacements))
	for _, value := range env {
		key, _, _ := strings.Cut(value, "=")
		if _, replaced := replacements[key]; !replaced {
			out = append(out, value)
		}
	}
	for key, value := range replacements {
		out = append(out, key+"="+value)
	}
	return out
}
