package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	connectapp "github.com/swobuforge/swobu/internal/app/operator/connect"
)

func reviewedConnectFixture(t *testing.T) operatorclient.ClientConfigurationIntent {
	t.Helper()
	intent, err := operatorclient.NewClientConfigurationIntent(connectapp.Plan{Authentication: connectapp.Authentication{Principal: "acc_AAAAAAAAAAAAAAAAAAAAAA", PermissionRevision: "permission-fixture", CredentialID: "acred_AAAAAAAAAAAAAAAAAAAAAA"}, Revision: "connect-" + strings.Repeat("a", 64), Workspace: "research", ModelID: "coding", Client: connectapp.Client{ID: "codex"}, ConfigPaths: []string{"fixture.toml"}, Changes: []connectapp.Change{{Field: "endpoint", Before: "old", BeforeExists: true, After: "new"}}, RequiresReplace: true}, "original-connect-key")
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

func TestReviewedConnectConfirmationRetentionAndRecovery(t *testing.T) {
	for _, mode := range []string{"fresh", "decline", "eof", "noninteractive", "lost", "recovery", "recovery-decline", "conflict", "existing", "wrong-resource"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(privateConnectDirectory(t), "intent.json")
			original := reviewedConnectFixture(t)
			record, _ := original.RecoveryRecord()
			recover := strings.HasPrefix(mode, "recovery") || mode == "wrong-resource"
			if recover || mode == "existing" {
				if err := os.WriteFile(path, record, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var calls, previews, writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				if request.URL.Path == "/api/v1/status" {
					fmt.Fprint(writer, `{"state":"healthy","control_plane_protocol":11}`)
					return
				}
				if request.URL.Path != "/api/v1/client-configurations" || request.Method != http.MethodPost {
					t.Error("escaped canonical Connect")
				}
				if request.URL.RawQuery == "dry_run=true" {
					previews.Add(1)
					plan, _ := original.Review()
					json.NewEncoder(writer).Encode(plan)
					return
				}
				writes.Add(1)
				stored, err := os.ReadFile(path)
				retained, restoreErr := operatorclient.RestoreClientConfigurationIntent(stored)
				if err != nil || restoreErr != nil {
					t.Error("effect sent before retained original intent")
				}
				var envelope struct {
					Request json.RawMessage `json:"request"`
					Key     string          `json:"retry_key"`
				}
				json.Unmarshal(stored, &envelope)
				raw, _ := io.ReadAll(request.Body)
				review, _ := retained.Review()
				if !bytes.Equal(raw, envelope.Request) || request.Header.Get("Idempotency-Key") != envelope.Key || request.Header.Get("If-Match") != `"`+review.Revision+`"` || request.URL.RawQuery != "dry_run=false" {
					t.Error("send changed retained intent")
				}
				if mode == "lost" {
					connection, _, err := writer.(http.Hijacker).Hijack()
					if err == nil {
						connection.Close()
					}
					return
				}
				if mode == "conflict" {
					writer.WriteHeader(http.StatusConflict)
					return
				}
				writer.Header().Set("ETag", `"`+review.Revision+`"`)
				fmt.Fprintf(writer, `{"revision":%q,"workspace":"research","model_id":"coding","client":{"id":"codex"},"authentication":{"principal":"acc_AAAAAAAAAAAAAAAAAAAAAA","permission_revision":"permission-fixture","issue_credential":false,"credential_id":"acred_AAAAAAAAAAAAAAAAAAAAAA"},"config_paths":["fixture.toml"],"changes":[],"already_configured":true,"requires_replace":false,"replayed":%t}`, review.Revision, recover)
			}))
			defer server.Close()
			args := []string{"codex", "--workspace", "research", "--model", "coding", "--principal", "acc_AAAAAAAAAAAAAAAAAAAAAA", "--intent-file", path, "--addr", strings.TrimPrefix(server.URL, "http://")}
			if recover {
				args = append(args, "--recover")
			} else {
				args = append(args, "--apply")
			}
			if mode == "wrong-resource" {
				args[2] = "other"
			}
			input := "CONNECT codex research\n"
			if strings.Contains(mode, "decline") {
				input = "no\n"
			}
			if mode == "eof" {
				input = "CONNECT codex research"
			}
			var stdout, stderr strings.Builder
			code := runConnect(t.Context(), server.Client(), &stdout, &stderr, args, Runner{Stdin: strings.NewReader(input), IsInteractive: func() bool { return mode != "noninteractive" }})
			shouldApply := mode == "fresh" || mode == "recovery" || mode == "lost" || mode == "conflict"
			if (writes.Load() == 1) != shouldApply || writes.Load() > 1 {
				t.Fatal("confirmation failed or local effect/retry occurred")
			}
			if (code == ExitHealthy) != (mode == "fresh" || mode == "recovery") {
				t.Fatal("incorrect terminal result", stderr.String())
			}
			if (recover && previews.Load() != 0) || ((mode == "noninteractive" || mode == "existing" || mode == "wrong-resource") && calls.Load() != 0) {
				t.Fatal("recovery refreshed consent or invalid call contacted Node")
			}
			if shouldApply {
			} else if !recover && mode != "existing" {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("decline retained intent")
				}
			}
			if mode == "recovery" && !strings.Contains(stdout.String(), "current client configuration was not inspected or changed") {
				t.Fatal("replay claimed readiness")
			}
		})
	}
}

func TestReviewedConnectRejectsAmbiguousFlagsBeforeContact(t *testing.T) {
	for _, args := range [][]string{
		{"codex", "--apply"}, {"codex", "--recover"},
		{"codex", "--apply", "--recover", "--intent-file", "/fixture"},
		{"codex", "--apply", "--dry-run", "--intent-file", "/fixture"},
		{"codex", "--apply", "--replace", "--intent-file", "/fixture"},
		{"codex", "--intent-file", "/fixture"},
	} {
		calls := 0
		var stdout, stderr strings.Builder
		if code := runConnect(t.Context(), &http.Client{Transport: unexpectedRequestTransport{calls: &calls}}, &stdout, &stderr, args, Runner{}); code != ExitDown || calls != 0 {
			t.Fatal("ambiguous flags reached Node")
		}
	}
}
