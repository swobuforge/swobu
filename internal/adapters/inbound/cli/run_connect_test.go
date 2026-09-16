package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/clientconnect"
)

type connectWorkspacesStub struct {
	summaries []workspaceapi.WorkspaceSummary
}

func (s connectWorkspacesStub) ListWorkspaces(context.Context) ([]workspaceapi.WorkspaceSummary, error) {
	return append([]workspaceapi.WorkspaceSummary(nil), s.summaries...), nil
}

type connectOperationsStub struct {
	plan       clientconnect.Plan
	verified   clientconnect.Plan
	planErr    error
	applyErr   error
	plannedID  clientconnect.ClientID
	plannedURL string
	applied    bool
}

func (s *connectOperationsStub) Plan(_ context.Context, id clientconnect.ClientID, target clientconnect.Target) (clientconnect.Plan, error) {
	s.plannedID, s.plannedURL = id, target.WorkspaceURL()
	return s.plan, s.planErr
}
func (s *connectOperationsStub) Apply(_ context.Context, plan clientconnect.Plan) (clientconnect.Plan, error) {
	s.applied = true
	if s.applyErr != nil {
		if s.verified.ClientID != "" {
			return s.verified, s.applyErr
		}
		return plan, s.applyErr
	}
	if s.verified.ClientID != "" {
		return s.verified, nil
	}
	plan.Changes = nil
	return plan, nil
}

func TestConnectDistinguishesPlanAndApplyFailureSideEffects(t *testing.T) {
	target, err := clientconnect.NewTarget("personal", "http://127.0.0.1:7926/c/personal")
	if err != nil {
		t.Fatal(err)
	}
	reviewed := clientconnect.Plan{ClientID: clientconnect.ClientCodex, ClientName: "Codex CLI", ConfigPaths: []string{"/tmp/config"}, Target: target, Changes: []clientconnect.Change{{Field: "endpoint", After: target.WorkspaceURL()}}}
	for _, tc := range []struct {
		name string
		ops  *connectOperationsStub
		want bool
	}{
		{name: "plan failure", ops: &connectOperationsStub{planErr: errors.New("inspection failed")}, want: true},
		{name: "apply failure", ops: &connectOperationsStub{plan: reviewed, applyErr: errors.New("mutation failed")}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			runner := Runner{Stdout: &stdout, Stderr: &stderr, HTTPClient: http.DefaultClient, ConnectOperations: tc.ops, ConnectWorkspaces: connectWorkspacesStub{summaries: []workspaceapi.WorkspaceSummary{{Slug: "personal"}}}, ConnectAttach: func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error { return nil }}
			if got := runner.Run(context.Background(), []string{"connect", "codex"}); got != ExitDown {
				t.Fatalf("code = %v", got)
			}
			if strings.Contains(stderr.String(), "Nothing changed.") != tc.want {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}

func TestConnectWorkspaceResolutionMatrix(t *testing.T) {
	for _, tc := range []struct {
		name     string
		slugs    []string
		explicit string
		want     string
		wantErr  string
	}{
		{name: "zero defaults", want: "default"},
		{name: "zero explicit default", explicit: "default", want: "default"},
		{name: "zero rejects another", explicit: "work", wantErr: `workspace "work" is not configured`},
		{name: "one selects", slugs: []string{"personal"}, want: "personal"},
		{name: "one explicit exact", slugs: []string{"personal"}, explicit: "personal", want: "personal"},
		{name: "one explicit wrong", slugs: []string{"personal"}, explicit: "work", wantErr: `workspace "work" is not configured`},
		{name: "many require explicit", slugs: []string{"work", "personal"}, wantErr: "Multiple workspaces are configured: personal, work."},
		{name: "many exact", slugs: []string{"work", "personal"}, explicit: "work", want: "work"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			summaries := make([]workspaceapi.WorkspaceSummary, 0, len(tc.slugs))
			for _, slug := range tc.slugs {
				summaries = append(summaries, workspaceapi.WorkspaceSummary{Slug: slug})
			}
			got, err := resolveConnectWorkspace(summaries, tc.explicit)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("resolve = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestConnectUsesCanonicalPlanAndSemanticReplaceGate(t *testing.T) {
	target, err := clientconnect.NewTarget("personal", "http://127.0.0.1:7926/c/personal")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		plan      clientconnect.Plan
		args      []string
		wantCode  ExitCode
		wantApply bool
		wantText  string
	}{
		{name: "new leaf applies", plan: clientconnect.Plan{ClientID: clientconnect.ClientCodex, ClientName: "Codex CLI", ConfigPaths: []string{"/tmp/config"}, Target: target, Changes: []clientconnect.Change{{Field: "endpoint", After: target.WorkspaceURL()}}}, args: []string{"connect", "codex"}, wantCode: ExitHealthy, wantApply: true, wantText: "configured"},
		{name: "replacement refused", plan: clientconnect.Plan{ClientID: clientconnect.ClientCodex, ClientName: "Codex CLI", ConfigPaths: []string{"/tmp/config"}, Target: target, Changes: []clientconnect.Change{{Field: "endpoint", Before: "https://old", BeforeExists: true, After: target.WorkspaceURL()}}}, args: []string{"connect", "codex"}, wantCode: ExitDown, wantText: "Run again with --replace."},
		{name: "replacement applied", plan: clientconnect.Plan{ClientID: clientconnect.ClientCodex, ClientName: "Codex CLI", ConfigPaths: []string{"/tmp/config"}, Target: target, Changes: []clientconnect.Change{{Field: "endpoint", Before: "https://old", BeforeExists: true, After: target.WorkspaceURL()}}}, args: []string{"connect", "codex", "--replace"}, wantCode: ExitHealthy, wantApply: true, wantText: "configured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops := &connectOperationsStub{plan: tc.plan}
			var stdout, stderr bytes.Buffer
			runner := Runner{Stdout: &stdout, Stderr: &stderr, HTTPClient: http.DefaultClient, ConnectOperations: ops, ConnectWorkspaces: connectWorkspacesStub{summaries: []workspaceapi.WorkspaceSummary{{Slug: "personal"}}}, ConnectAttach: func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error { return nil }}
			got := runner.Run(context.Background(), tc.args)
			if got != tc.wantCode || ops.applied != tc.wantApply {
				t.Fatalf("code/apply = %v/%v", got, ops.applied)
			}
			if text := stdout.String() + stderr.String(); !strings.Contains(text, tc.wantText) {
				t.Fatalf("output missing %q:\n%s", tc.wantText, text)
			}
			if ops.plannedID != clientconnect.ClientCodex || ops.plannedURL != "http://127.0.0.1:7926/c/personal" {
				t.Fatalf("planned = %q %q", ops.plannedID, ops.plannedURL)
			}
		})
	}
}

func TestConnectAntigravityDoesNotStartDaemonOrResolveWorkspace(t *testing.T) {
	plan := clientconnect.Plan{
		ClientID: clientconnect.ClientAntigravity, ClientName: "Antigravity CLI", ConfigPaths: []string{"/tmp/settings.json"},
		Changes: []clientconnect.Change{{Field: "modelProvider", After: "gemini"}},
	}
	ops := &connectOperationsStub{plan: plan}
	attachCalled := false
	var stdout, stderr bytes.Buffer
	runner := Runner{
		Stdout: &stdout, Stderr: &stderr, HTTPClient: http.DefaultClient, ConnectOperations: ops,
		ConnectWorkspaces: connectWorkspacesStub{summaries: []workspaceapi.WorkspaceSummary{{Slug: "work"}, {Slug: "personal"}}},
		ConnectAttach: func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error {
			attachCalled = true
			return errors.New("must not attach")
		},
	}
	if got := runner.Run(context.Background(), []string{"connect", "antigravity"}); got != ExitHealthy {
		t.Fatalf("code=%v stderr=%s", got, stderr.String())
	}
	if attachCalled {
		t.Fatal("global Antigravity configuration started or attached the daemon")
	}
	if ops.plannedID != clientconnect.ClientAntigravity || ops.plannedURL != "" || !ops.applied {
		t.Fatalf("plan/apply=%q %q %t", ops.plannedID, ops.plannedURL, ops.applied)
	}
}

func TestConnectUsageAndClientAuthority(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr}
	if got := runner.Run(context.Background(), []string{"connect"}); got != ExitDown {
		t.Fatalf("code = %v", got)
	}
	for _, want := range []string{"usage: swobu connect", "codex", "claude", "kilo", "pi"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("usage missing %q", want)
		}
	}
	if got := runner.Run(context.Background(), []string{"connect", "Codex"}); got != ExitDown {
		t.Fatalf("alias accepted: %v", got)
	}
	stdout.Reset()
	if got := runner.Run(context.Background(), []string{"connect", "--help"}); got != ExitHealthy {
		t.Fatalf("help code = %v", got)
	}
}

func TestConnectPlanRendersEveryReviewedSemanticChange(t *testing.T) {
	target, err := clientconnect.NewTarget("work", "http://127.0.0.1:7926/c/work")
	if err != nil {
		t.Fatal(err)
	}
	plan := clientconnect.Plan{
		ClientName: "Codex CLI", ConfigPaths: []string{"/tmp/config.toml"}, Target: target,
		Changes: []clientconnect.Change{
			{Field: "backend", Before: "openai/model", BeforeExists: true, After: "swobu/default"},
			{Field: "endpoint", Before: "http://127.0.0.1:7926/c/old", BeforeExists: true, After: target.WorkspaceURL()},
			{Field: "protocol", Before: "chat", BeforeExists: true, After: "responses"},
		},
	}
	var out bytes.Buffer
	renderConnectPlan(&out, plan, target)
	text := out.String()
	for _, want := range []string{"backend", "openai/model", "swobu/default", "endpoint", "/c/old", "/c/work", "protocol", "chat", "responses", "writes", "/tmp/config.toml"} {
		if !strings.Contains(text, want) {
			t.Fatalf("plan output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "API      ") {
		t.Fatalf("semantic plan was collapsed into API endpoint:\n%s", text)
	}
}
