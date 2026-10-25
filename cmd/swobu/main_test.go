package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/inbound/cli"
)

func TestNoObsoleteCommandSurface(t *testing.T) {
	var out, notices bytes.Buffer
	root := newRootCommand(&cli.Runner{Stdout: &out, Stderr: &notices}, &out, &notices)
	for _, obsolete := range []string{"share", "exposure", "access", "assist"} {
		for _, cmd := range root.Commands() {
			if cmd.Name() == obsolete {
				t.Fatalf("obsolete command %q survives", obsolete)
			}
		}
	}
}

func TestCredentialIssuanceIsReachableFromBinaryCommandTree(t *testing.T) {
	var out, notices bytes.Buffer
	runner := &cli.Runner{Stdout: &out, Stderr: &notices}
	root := newRootCommand(runner, &out, &notices)
	root.SetArgs([]string{"credential", "issue", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notices.String(), "swobu credential issue") || !strings.Contains(notices.String(), "--retry-key") {
		t.Fatal("canonical credential recovery help absent")
	}
}

func TestPrincipalCreationIsReachableFromBinaryCommandTree(t *testing.T) {
	var out, notices bytes.Buffer
	runner := &cli.Runner{Stdout: &out, Stderr: &notices}
	root := newRootCommand(runner, &out, &notices)
	root.SetArgs([]string{"principal", "create", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notices.String(), "--label") || !strings.Contains(notices.String(), "--retry-key") {
		t.Fatal("Principal creation/recovery help absent")
	}
}

func TestPrincipalLabelIsReachableFromBinaryCommandTree(t *testing.T) {
	var out, notices bytes.Buffer
	runner := &cli.Runner{Stdout: &out, Stderr: &notices}
	root := newRootCommand(runner, &out, &notices)
	root.SetArgs([]string{"principal", "label", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notices.String(), "swobu principal label") || !strings.Contains(notices.String(), "--principal") {
		t.Fatal("Principal label help absent")
	}
}

func TestWorkspaceDeletionIsReachableFromBinaryCommandTree(t *testing.T) {
	var out, notices bytes.Buffer
	runner := &cli.Runner{Stdout: &out, Stderr: &notices}
	root := newRootCommand(runner, &out, &notices)
	root.SetArgs([]string{"workspace", "delete", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notices.String(), "swobu workspace delete") || !strings.Contains(notices.String(), "--workspace") {
		t.Fatalf("deletion help absent: %s%s", out.String(), notices.String())
	}
}

func TestWorkspacePermissionIsReachableFromBinaryCommandTree(t *testing.T) {
	var out, notices bytes.Buffer
	runner := &cli.Runner{Stdout: &out, Stderr: &notices}
	root := newRootCommand(runner, &out, &notices)
	root.SetArgs([]string{"workspace", "permission", "grant", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notices.String(), "swobu workspace permission grant") || !strings.Contains(notices.String(), "--principal") {
		t.Fatalf("permission help absent: %s%s", out.String(), notices.String())
	}
}

func TestWebOperatorIsRootAndExplicitOpenCommand(t *testing.T) {
	t.Setenv("SWOBU_SKIP_VERSION_NOTICE", "1")
	t.Setenv("DO_NOT_TRACK", "1")
	const token = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.URL.Path != "/api/v1/browser-launches" {
			t.Errorf("unexpected launch request: %s %s", req.Method, req.URL)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"launch_path":"/open#`+token+`"}`)
	}))
	defer server.Close()
	for _, tc := range []struct {
		name, command string
	}{
		{"bare root (non-TTY)", ""},
		{"explicit open", "open"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			var opened string
			runner := &cli.Runner{
				Stdout: &out, Stderr: &errOut,
				HTTPClient:    server.Client(),
				Addr:          strings.TrimPrefix(server.URL, "http://"),
				AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error { return nil },
				OpenBrowser:   func(url string) error { opened = url; return nil },
			}
			root := newRootCommand(runner, &out, &errOut)
			if tc.command != "" {
				root.SetArgs([]string{tc.command, "--addr", strings.TrimPrefix(server.URL, "http://")})
			} else {
				root.SetArgs([]string{"--addr", strings.TrimPrefix(server.URL, "http://")})
			}
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if opened != server.URL+"/open#"+token {
				t.Fatalf("browser target = %s", opened)
			}
			if strings.Contains(out.String()+errOut.String(), token) {
				t.Fatal("browser bearer leaked to shell")
			}
		})
	}
}

func TestCommandTreeEnumeratesActualSupportedLeaves(t *testing.T) {
	for _, tc := range []struct {
		group  string
		leaves []string
	}{
		{"config", []string{"validate"}},
		{"credential", []string{"issue", "revoke"}},
		{"principal", []string{"list", "create", "label", "revoke"}},
		{"workspace", []string{"create", "display-name", "delete", "permission", "limits"}},
		{"telemetry", []string{"status", "on", "off", "inspect"}},
		{"daemon", []string{"down"}},
		{"workspace permission", []string{"grant", "revoke"}},
	} {
		t.Run(tc.group, func(t *testing.T) {
			root := newRootCommand(&cli.Runner{}, io.Discard, io.Discard)
			group, _, err := root.Find(strings.Fields(tc.group))
			if err != nil || group == root || group.Name() != strings.Fields(tc.group)[len(strings.Fields(tc.group))-1] {
				t.Fatalf("missing command group %q: %v", tc.group, err)
			}
			for _, name := range tc.leaves {
				leaf, _, err := root.Find(append(strings.Fields(tc.group), name))
				if err != nil || leaf == group || leaf.Name() != name {
					t.Errorf("missing CLI operation %s %s", tc.group, name)
				}
			}
		})
	}
}

func TestBinaryHelpExposesWorkspaceAndPrincipalOperations(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"workspace", "--help"}, []string{"create", "display-name", "permission", "limits", "delete"}},
		{[]string{"principal", "--help"}, []string{"list", "create", "label", "revoke"}},
		{[]string{"telemetry", "--help"}, []string{"inspect", "status", "off"}},
		{[]string{"workspace", "permission", "--help"}, []string{"grant", "revoke"}},
	} {
		var stdout, stderr bytes.Buffer
		root := newRootCommand(&cli.Runner{Stdout: &stdout, Stderr: &stderr}, &stdout, &stderr)
		root.SetArgs(tc.args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		for _, word := range tc.want {
			if !strings.Contains(stdout.String()+stderr.String(), word) {
				t.Errorf("%v help missing %q: %q", tc.args, word, stdout.String()+stderr.String())
			}
		}
	}
}

func TestBinaryPrincipalListForwardsAddressAndMachineFlag(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/api/v1/status":
			_, _ = io.WriteString(w, `{"state":"healthy","control_plane_protocol":11}`)
		case "/api/v1/principals":
			_, _ = io.WriteString(w, `[{"id":"acc_1","label":"from selected node","kind":"managed","status":"active","permissions":[]}]`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	root := newRootCommand(&cli.Runner{Stdout: &stdout, Stderr: &stderr, HTTPClient: server.Client()}, &stdout, &stderr)
	root.SetArgs([]string{"principal", "list", "--addr", strings.TrimPrefix(server.URL, "http://"), "--json"})
	if err := root.Execute(); err != nil || calls != 2 || !strings.Contains(stdout.String(), `"label":"from selected node"`) {
		t.Fatalf("binary delegated to wrong command: err=%v calls=%d out=%q notices=%q", err, calls, stdout.String(), stderr.String())
	}
}
