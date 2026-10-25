package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/bootstrap"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

type permissionConfirmationReader struct {
	t          *testing.T
	notices    *bytes.Buffer
	input      string
	beforeRead func()
}

func (r *permissionConfirmationReader) Read(p []byte) (int, error) {
	fields := []string{"Before:", "After:", "Revision:", "Affected credentials:", "Preview is not approval", "Retry key:", "Type "}
	if strings.Contains(r.notices.String(), "Recover original permission") {
		fields = []string{"Original revision:", "Original proposed permission:", "Current state (advisory):", "No current revision will be substituted", "Retry key:", "Type "}
	}
	for _, field := range fields {
		if !strings.Contains(r.notices.String(), field) {
			r.t.Fatalf("consent requested before %s disclosure", field)
		}
	}
	if r.beforeRead != nil {
		r.beforeRead()
		r.beforeRead = nil
	}
	if r.input == "" {
		return 0, io.EOF
	}
	n := copy(p, r.input)
	r.input = r.input[n:]
	return n, nil
}

type permissionApplyTransport struct {
	next                  http.RoundTripper
	lost                  bool
	beforeApply           bool
	inspectionUnavailable bool
	applies               int
	previews              int
	conflictBody          string
}

func TestWorkspacePermissionRejectsInvalidFlagsBeforeDaemonContact(t *testing.T) {
	contacts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contacts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	base := []string{"workspace", "permission", "revoke", "--workspace", "research", "--principal", "acc_AAAAAAAAAAAAAAAAAAAAAA", "--addr", strings.TrimPrefix(server.URL, "http://")}
	for _, extra := range [][]string{{"--yes"}, {"--expires-at", "2099-10-31T00:00:00Z"}, {"--expires-at", "not-an-instant"}, {"surplus"}, {"--retry-key", "original"}, {"--expected-revision", "original"}, {"--retry-key", "bad key", "--expected-revision", "original"}} {
		var out, notices bytes.Buffer
		runner := Runner{Stdin: strings.NewReader("APPLY\n"), Stdout: &out, Stderr: &notices, HTTPClient: server.Client(), IsInteractive: func() bool { return true }}
		args := append(append([]string(nil), base...), extra...)
		if code := runner.Run(t.Context(), args); code != ExitDown || contacts != 0 {
			t.Fatalf("invalid flags contacted daemon: code=%d contacts=%d", code, contacts)
		}
	}
}

func (t *permissionApplyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	apply := strings.Contains(r.URL.Path, "/permissions/") && r.Method != http.MethodGet && r.URL.RawQuery == ""
	if apply {
		t.applies++
		if t.beforeApply {
			return nil, errors.New("connection lost before permission apply")
		}
	}
	if t.inspectionUnavailable && r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/permissions/") {
		return nil, errors.New("permission inspection unavailable")
	}
	if strings.Contains(r.URL.Path, "/permissions/") && r.URL.RawQuery != "" {
		t.previews++
	}
	resp, err := t.next.RoundTrip(r)
	if err == nil && resp.StatusCode == http.StatusConflict {
		raw, readErr := io.ReadAll(resp.Body)
		if readErr == nil {
			t.conflictBody = string(raw)
			resp.Body.Close()
			resp.Body = io.NopCloser(bytes.NewReader(raw))
		}
	}
	if err == nil && apply && t.lost {
		resp.Body.Close()
		return nil, errors.New("permission response lost after commit")
	}
	return resp, err
}

func TestWorkspacePermissionRealDaemonTrustedConfirmation(t *testing.T) {
	for _, variant := range []string{"grant", "revoke", "decline", "eof", "noninteractive", "stale", "cancelled", "lost-response", "lost-grant-response", "before-apply"} {
		t.Run(variant, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv(platformconfig.EnvSwobuHome, dir)
			path := filepath.Join(dir, "swobu.yaml")
			if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			daemon, err := bootstrap.Start(t.Context(), bootstrap.StartInput{ConfigPath: path, StartupConfig: platformconfig.StartupConfig{Addr: "127.0.0.1:0"}})
			if err != nil {
				t.Fatal(err)
			}
			defer daemon.Close()
			create, err := http.NewRequestWithContext(t.Context(), http.MethodPost, daemon.BaseURL()+"/api/v1/workspaces", strings.NewReader(`{"slug":"research"}`))
			if err != nil {
				t.Fatal(err)
			}
			create.Header.Set("Content-Type", "application/json")
			create.Header.Set("Idempotency-Key", "permission-fixture-creation")
			resp, err := http.DefaultClient.Do(create)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 201 {
				t.Fatalf("create: %d", resp.StatusCode)
			}
			api := operatorclient.New(&http.Client{}, daemon.BaseURL())
			identity, _, err := api.CreatePrincipal(t.Context(), "permission fixture", "permission-fixture-create")
			if err != nil {
				t.Fatal(err)
			}
			fixturePreview, err := api.PreviewWorkspacePermission(t.Context(), "research", identity.ID, true, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			fixtureIntent, err := operatorclient.ReviewPermission(fixturePreview, "permission-fixture-grant")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := api.ApplyWorkspacePermission(t.Context(), fixtureIntent); err != nil {
				t.Fatal(err)
			}
			issuance, err := operatorclient.RestoreCredentialIssuanceIntent(identity.ID, "research", "permission-fixture-credential")
			if err != nil {
				t.Fatal(err)
			}
			issued, err := api.ApplyCredentialIssuance(t.Context(), issuance)
			if err != nil {
				t.Fatal(err)
			}
			principal := issued.Access.ID
			var out, notices bytes.Buffer
			operation := "revoke"
			if variant == "grant" || variant == "lost-grant-response" {
				operation = "grant"
			}
			input := &permissionConfirmationReader{t: t, notices: &notices, input: strings.ToUpper(operation) + " " + principal + " research\n"}
			if variant == "decline" {
				input.input = "APPLY\n"
			}
			if variant == "eof" {
				input.input = ""
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if variant == "cancelled" {
				input.beforeRead = cancel
			}
			if variant == "stale" {
				input.beforeRead = func() {
					preview, err := api.PreviewWorkspacePermission(t.Context(), "research", principal, true, time.Now().Add(time.Hour))
					if err != nil {
						t.Fatal(err)
					}
					reviewed, err := operatorclient.ReviewPermission(preview, "competing-human-edit")
					if err != nil {
						t.Fatal(err)
					}
					if _, err := api.ApplyWorkspacePermission(t.Context(), reviewed); err != nil {
						t.Fatal(err)
					}
				}
			}
			transport := &permissionApplyTransport{next: http.DefaultTransport, lost: variant == "lost-response" || variant == "lost-grant-response", beforeApply: variant == "before-apply"}
			runner := Runner{Stdin: input, Stdout: &out, Stderr: &notices, HTTPClient: &http.Client{Transport: transport}, IsInteractive: func() bool { return variant != "noninteractive" }}
			args := []string{"workspace", "permission", operation, "--workspace", "research", "--principal", principal, "--addr", strings.TrimPrefix(daemon.BaseURL(), "http://")}
			if operation == "grant" {
				args = append(args, "--expires-at", "2099-10-31T00:00:00+02:00")
			}
			code := runner.Run(ctx, args)
			if variant == "stale" && !strings.Contains(notices.String(), "Permission revision changed; obtain a fresh preview") {
				t.Fatal("stale consent lost its semantic recovery explanation")
			}
			wantCount := 0
			if variant == "grant" || variant == "revoke" || variant == "stale" || variant == "lost-response" || variant == "lost-grant-response" || variant == "before-apply" {
				wantCount = 1
			}
			if transport.applies != wantCount || transport.previews > 1 || (code == ExitHealthy) != (variant == "grant" || variant == "revoke") {
				t.Fatalf("code=%d applies=%d previews=%d notices=%s", code, transport.applies, transport.previews, notices.String())
			}
			state, err := api.GetWorkspacePermission(t.Context(), "research", principal)
			if err != nil {
				t.Fatal(err)
			}
			wantAllowed := variant != "revoke" && variant != "lost-response"
			if state.Allowed != wantAllowed {
				t.Fatalf("allowed=%t want=%t", state.Allowed, wantAllowed)
			}
			if operation == "grant" && (state.Permission.ExpiresAt == nil || state.Permission.ExpiresAt.Format(time.RFC3339) != "2099-10-30T22:00:00Z") {
				t.Fatal("expiry changed")
			}
			if (transport.lost || transport.beforeApply) && (!strings.Contains(notices.String(), "Do not infer rollback") || out.Len() != 0) {
				t.Fatal("uncertain result reported success or rollback")
			}
			if transport.lost || transport.beforeApply {
				var key, revision string
				for _, line := range strings.Split(notices.String(), "\n") {
					if strings.HasPrefix(line, "Retry key: ") {
						key = strings.TrimPrefix(line, "Retry key: ")
					}
					if strings.HasPrefix(line, "Revision: ") {
						revision = strings.Trim(strings.TrimPrefix(line, "Revision: "), "\"")
					}
				}
				if key == "" || revision == "" {
					t.Fatal("lost original recovery intent")
				}
				args = append(args, "--retry-key", key, "--expected-revision", revision)
				transport.lost = false
				transport.beforeApply = false
				transport.inspectionUnavailable = true
				notices.Reset()
				input.input = strings.ToUpper(operation) + " " + principal + " research\n"
				if code := runner.Run(t.Context(), args); code != ExitDown || transport.applies != 1 || strings.Contains(notices.String(), "Type ") {
					t.Fatal("unavailable inspection allowed recovery consent or apply")
				}
				transport.inspectionUnavailable = false
				notices.Reset()
				input.input = "no\n"
				if code := runner.Run(t.Context(), args); code != ExitDown || transport.applies != 1 {
					t.Fatal("declined recovery sent apply")
				}
				notices.Reset()
				input.input = strings.ToUpper(operation) + " " + principal + " research\n"
				wantReplay := variant != "before-apply"
				if code := runner.Run(t.Context(), args); code != ExitHealthy || transport.applies != 2 || transport.previews != 1 || !strings.Contains(out.String(), fmt.Sprintf("replayed=%t", wantReplay)) {
					t.Fatalf("original recovery failed: code=%d applies=%d previews=%d notices=%s out=%s", code, transport.applies, transport.previews, notices.String(), out.String())
				}
				current, err := api.GetWorkspacePermission(t.Context(), "research", principal)
				if err != nil || (wantReplay && current.Revision != state.Revision) || current.Allowed != (operation == "grant") {
					t.Fatal("receipt replay changed authority")
				}
				// A retained key does not approve a changed revision or deadline.
				changed := append([]string(nil), args...)
				changed[len(changed)-1] = revision + "changed"
				if operation == "grant" {
					changed[len(changed)-1] = revision
					for i := range changed {
						if changed[i] == "--expires-at" {
							changed[i+1] = "2099-11-01T00:00:00Z"
						}
					}
				}
				out.Reset()
				notices.Reset()
				input.input = strings.ToUpper(operation) + " " + principal + " research\n"
				if code := runner.Run(t.Context(), changed); code != ExitDown || !strings.Contains(notices.String(), "idempotency_conflict") || out.Len() != 0 {
					t.Fatalf("changed recovery intent accepted: code=%d response=%s notices=%s", code, transport.conflictBody, notices.String())
				}
				unchanged, err := api.GetWorkspacePermission(t.Context(), "research", principal)
				if err != nil || unchanged.Revision != current.Revision || unchanged.Allowed != current.Allowed {
					t.Fatal("changed recovery intent altered authority")
				}
				if operation == "grant" {
					if strings.Contains(out.String(), issued.Token) || strings.Contains(notices.String(), issued.Token) {
						t.Fatal("credential disclosed during grant recovery")
					}
					return
				}
				preview, err := api.PreviewWorkspacePermission(t.Context(), "research", principal, true, time.Time{})
				if err != nil {
					t.Fatal(err)
				}
				grant, err := operatorclient.ReviewPermission(preview, "fresh-human-regrant")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := api.ApplyWorkspacePermission(t.Context(), grant); err != nil {
					t.Fatal(err)
				}
				out.Reset()
				notices.Reset()
				input.input = "REVOKE " + principal + " research\n"
				if code := runner.Run(t.Context(), args); code != ExitHealthy || !strings.Contains(out.String(), "allowed=true, replayed=true") {
					t.Fatalf("historical revoke replay hid current regrant: %d %s %s", code, out.String(), notices.String())
				}
				current, err = api.GetWorkspacePermission(t.Context(), "research", principal)
				if err != nil || !current.Allowed {
					t.Fatal("historical recovery revoked fresh authority")
				}
			}
			if strings.Contains(out.String(), issued.Token) || strings.Contains(notices.String(), issued.Token) {
				t.Fatal("credential disclosed")
			}
		})
	}
}
