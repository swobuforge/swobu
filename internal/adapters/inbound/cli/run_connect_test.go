package cli

import (
	"bytes"
	"context"
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
	planErr    error
	applyErr   error
	plannedID  clientconnect.ClientID
	plannedURL string
	applied    bool
}

func (s *connectOperationsStub) Plan(id clientconnect.ClientID, target clientconnect.Target) (clientconnect.Plan, error) {
	s.plannedID, s.plannedURL = id, target.WorkspaceURL()
	return s.plan, s.planErr
}
func (s *connectOperationsStub) Apply(clientconnect.Plan) error { s.applied = true; return s.applyErr }

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
		{name: "new leaf applies", plan: clientconnect.Plan{ClientID: clientconnect.ClientCodex, ClientName: "Codex CLI", ConfigPath: "/tmp/config", Target: target, Changes: []clientconnect.Change{{Field: "endpoint", After: target.WorkspaceURL()}}}, args: []string{"connect", "codex"}, wantCode: ExitHealthy, wantApply: true, wantText: "configured"},
		{name: "replacement refused", plan: clientconnect.Plan{ClientID: clientconnect.ClientCodex, ClientName: "Codex CLI", ConfigPath: "/tmp/config", Target: target, Changes: []clientconnect.Change{{Field: "endpoint", Before: "https://old", BeforeExists: true, After: target.WorkspaceURL()}}}, args: []string{"connect", "codex"}, wantCode: ExitDown, wantText: "Run again with --replace."},
		{name: "replacement applied", plan: clientconnect.Plan{ClientID: clientconnect.ClientCodex, ClientName: "Codex CLI", ConfigPath: "/tmp/config", Target: target, Changes: []clientconnect.Change{{Field: "endpoint", Before: "https://old", BeforeExists: true, After: target.WorkspaceURL()}}}, args: []string{"connect", "codex", "--replace"}, wantCode: ExitHealthy, wantApply: true, wantText: "configured"},
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
		ClientName: "Codex CLI", ConfigPath: "/tmp/config.toml", Target: target,
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
