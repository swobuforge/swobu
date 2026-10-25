package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConnectDryRunUsesOnlyCanonicalControlPreview(t *testing.T) {
	for _, machine := range []bool{false, true} {
		t.Run(fmt.Sprint(machine), func(t *testing.T) {
			calls := 0
			node := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				switch request.URL.Path {
				case "/api/v1/status":
					fmt.Fprint(writer, `{"state":"healthy","control_plane_protocol":11}`)
				case "/api/v1/client-configurations":
					if request.Method != http.MethodPost || request.URL.RawQuery != "dry_run=true" {
						t.Error("preview escaped explicit dry run")
					}
					fmt.Fprint(writer, `{"workspace":"research","model_id":"coding","client":{"id":"codex","name":"Codex"},"authentication":{"principal":"acc_AAAAAAAAAAAAAAAAAAAAAA","permission_revision":"permission-fixture","issue_credential":false,"credential_id":"acred_AAAAAAAAAAAAAAAAAAAAAA"},"config_paths":["fixture/config.toml"],"changes":[{"field":"endpoint","before":"old","before_exists":true,"after":"http://127.0.0.1:7926/workspaces/research"}],"already_configured":false,"requires_replace":true}`)
				default:
					t.Error("preview reached another Node operation")
				}
			}))
			defer node.Close()
			var stdout, stderr strings.Builder
			args := []string{"codex", "--dry-run", "--workspace", "research", "--model", "coding", "--principal", "acc_AAAAAAAAAAAAAAAAAAAAAA", "--addr", strings.TrimPrefix(node.URL, "http://")}
			if machine {
				args = append(args, "--json")
			}
			code := runConnect(t.Context(), node.Client(), &stdout, &stderr, args, Runner{})
			if code != ExitHealthy || stderr.Len() != 0 || calls != 2 || !strings.Contains(stdout.String(), "workspaces/research") {
				t.Fatal("preview failed or entered local planning/apply")
			}
			if machine && !strings.HasPrefix(stdout.String(), "{") {
				t.Fatal("JSON decorated")
			}
			if !machine && !strings.Contains(stdout.String(), "Nothing applied. Preview is not approval.") {
				t.Fatal("human preview implies effect")
			}
		})
	}
}

func TestConnectPreviewAllowsBoundedClientInspection(t *testing.T) {
	node := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/status":
			fmt.Fprint(writer, `{"state":"healthy","control_plane_protocol":11}`)
		case "/api/v1/client-configurations":
			if request.URL.RawQuery != "dry_run=true" {
				t.Error("Connect preview did not remain effect-free")
			}
			time.Sleep(2100 * time.Millisecond)
			fmt.Fprint(writer, `{"workspace":"research","model_id":"coding","client":{"id":"openclaw","name":"OpenClaw"},"authentication":{"principal":"acc_AAAAAAAAAAAAAAAAAAAAAA","permission_revision":"permission-fixture","issue_credential":false,"credential_id":"acred_AAAAAAAAAAAAAAAAAAAAAA"},"config_paths":["/tmp/openclaw.json"],"changes":[],"already_configured":true,"requires_replace":false}`)
		default:
			t.Error("preview reached another Node operation")
		}
	}))
	defer node.Close()

	var stdout, stderr strings.Builder
	args := []string{"openclaw", "--dry-run", "--workspace", "research", "--model", "coding", "--principal", "acc_AAAAAAAAAAAAAAAAAAAAAA", "--addr", strings.TrimPrefix(node.URL, "http://")}
	client := &http.Client{Timeout: 2 * time.Second}
	code := runConnect(t.Context(), withClientTimeout(client, connectReviewTimeout), &stdout, &stderr, args, Runner{})
	if code != ExitHealthy || stderr.Len() != 0 || !strings.Contains(stdout.String(), "Preview only:") || !strings.Contains(stdout.String(), "/tmp/openclaw.json") {
		t.Fatalf("bounded local client inspection did not complete its preview: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestConnectPreviewInvalidFlagsRejectBeforeContact(t *testing.T) {
	for _, args := range [][]string{
		{"codex", "--dry-run"}, {"codex", "--dry-run", "--workspace", "Research"},
		{"codex", "--dry-run", "--workspace", "research", "--principal", "acc_AAAAAAAAAAAAAAAAAAAAAA", "--replace"}, {"codex", "--json"},
	} {
		calls := 0
		var stdout, stderr strings.Builder
		code := runConnect(t.Context(), &http.Client{Transport: unexpectedRequestTransport{calls: &calls}}, &stdout, &stderr, args, Runner{})
		if code != ExitDown || calls != 0 || stdout.Len() != 0 {
			t.Fatal("invalid preview reached Node or emitted success")
		}
	}
}
