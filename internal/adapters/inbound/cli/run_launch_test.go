package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/clientconnect"
)

func configuredLaunchRunner(t *testing.T, run func(context.Context, []string, []string, io.Reader, io.Writer, io.Writer) error) Runner {
	t.Helper()
	target, _ := clientconnect.NewTarget("work", "http://127.0.0.1:7926/c/work")
	return Runner{
		ConnectAttach:     func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error { return nil },
		ConnectWorkspaces: connectWorkspacesStub{summaries: []workspaceapi.WorkspaceSummary{{Slug: "work"}}},
		ConnectOperations: &connectOperationsStub{plan: clientconnect.Plan{ClientID: clientconnect.ClientAntigravity, ClientName: "Antigravity CLI", Target: target, Changes: nil}},
		RunAntigravity: func(ctx context.Context, args, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
			if len(args) == 1 && args[0] == "--version" {
				_, _ = io.WriteString(stdout, "agy 1.2.3\n")
				return nil
			}
			return run(ctx, args, env, stdin, stdout, stderr)
		},
	}
}

func TestLaunchAntigravityPassesArgsEnvironmentAndNormalizedStdin(t *testing.T) {
	var gotArgs, gotEnv []string
	var gotStdin io.Reader
	runner := configuredLaunchRunner(t, func(_ context.Context, args, env []string, stdin io.Reader, _, _ io.Writer) error {
		gotArgs, gotEnv, gotStdin = args, env, stdin
		return nil
	})
	var stdout, stderr bytes.Buffer
	runner.Stdout, runner.Stderr = &stdout, &stderr
	if code := runner.Run(context.Background(), []string{"launch", "antigravity", "--workspace", "work", "--", "chat", "--safe"}); code != ExitHealthy {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.Join(gotArgs, " ") != "chat --safe" || gotStdin == nil {
		t.Fatalf("args=%q stdin=%v", gotArgs, gotStdin)
	}
	env := strings.Join(gotEnv, "\n")
	if !strings.Contains(env, "GEMINI_API_KEY=swobu-local") || !strings.Contains(env, "GOOGLE_GEMINI_BASE_URL=http://127.0.0.1:7926/c/work") {
		t.Fatalf("env missing bindings")
	}
}

func TestLaunchAntigravityAppliesMissingConfiguration(t *testing.T) {
	ran := false
	ops := &connectOperationsStub{plan: clientconnect.Plan{ClientID: clientconnect.ClientAntigravity, ClientName: "Antigravity CLI", Changes: []clientconnect.Change{{Field: "modelProvider", After: "gemini"}}}}
	runner := configuredLaunchRunner(t, func(context.Context, []string, []string, io.Reader, io.Writer, io.Writer) error {
		ran = true
		return nil
	})
	runner.ConnectOperations = ops
	var stderr bytes.Buffer
	runner.Stderr = &stderr
	if code := runner.Run(context.Background(), []string{"launch", "antigravity"}); code != ExitHealthy || !ran || !ops.applied {
		t.Fatalf("code=%d ran=%v applied=%v", code, ran, ops.applied)
	}
}

func TestLaunchAntigravityRejectsOldVersion(t *testing.T) {
	runner := configuredLaunchRunner(t, func(context.Context, []string, []string, io.Reader, io.Writer, io.Writer) error {
		t.Fatal("child ran after version rejection")
		return nil
	})
	runner.RunAntigravity = func(_ context.Context, args, _ []string, _ io.Reader, stdout, _ io.Writer) error {
		if len(args) == 1 && args[0] == "--version" {
			_, _ = io.WriteString(stdout, "agy 1.2.2\n")
			return nil
		}
		t.Fatal("child ran after version rejection")
		return nil
	}
	var stderr bytes.Buffer
	runner.Stderr = &stderr
	if code := runner.Run(context.Background(), []string{"launch", "antigravity"}); code != ExitDown || !strings.Contains(stderr.String(), "1.2.3 or newer") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestSupportedAntigravityVersionHasCertifiedMinimum(t *testing.T) {
	if err := requireSupportedAntigravityVersion("agy 1.2.3"); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{"agy 1.2.4", "agy 1.3.0", "agy 2.0.0"} {
		if err := requireSupportedAntigravityVersion(output); err != nil {
			t.Fatalf("rejected supported version %q: %v", output, err)
		}
	}
	for _, output := range []string{"agy 1.2.2", "agy 1.2.3-beta", "agy 1.2.3\nagy 2.0.0"} {
		if err := requireSupportedAntigravityVersion(output); err == nil {
			t.Fatalf("accepted unsupported version %q", output)
		}
	}
}

func TestLaunchAntigravityPreservesChildExitCode(t *testing.T) {
	runner := configuredLaunchRunner(t, func(context.Context, []string, []string, io.Reader, io.Writer, io.Writer) error {
		command := exec.Command("sh", "-c", "exit 7")
		return command.Run()
	})
	if code := runner.Run(context.Background(), []string{"launch", "antigravity"}); code != 7 {
		t.Fatalf("code=%d", code)
	}
}
