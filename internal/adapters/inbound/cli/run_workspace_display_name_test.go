package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestWorkspaceDisplayNameRequiresExactConsentAndOneOriginalRevision(t *testing.T) {
	for _, mode := range []string{"success", "clear", "decline", "eof", "nil-input", "noninteractive", "stale", "lost", "unavailable", "wrong-identity", "missing-revision", "missing-name-flag", "invalid-name"} {
		t.Run(mode, func(t *testing.T) {
			var reads, writes, calls atomic.Int32
			name := "Research 東京"
			if mode == "clear" {
				name = ""
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/api/v1/status" {
					fmt.Fprint(w, `{"state":"healthy","control_plane_protocol":11}`)
					return
				}
				if r.URL.Path != "/api/v1/workspaces/research" || r.URL.RawQuery != "" {
					t.Error("noncanonical Workspace path")
				}
				if r.Method == http.MethodGet {
					reads.Add(1)
					if mode == "unavailable" {
						http.Error(w, "unavailable", 503)
						return
					}
					slug, revision := "research", "reviewed"
					if mode == "wrong-identity" {
						slug = "other"
					}
					if mode == "missing-revision" {
						revision = ""
					}
					fmt.Fprintf(w, `{"slug":%q,"display_name":"Before","management_etag":%q,"management_revision":1}`, slug, revision)
					return
				}
				writes.Add(1)
				var body map[string]string
				if r.Method != http.MethodPatch || r.Header.Get("If-Match") != `"reviewed"` || json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 1 || body["display_name"] != name {
					t.Error("reviewed name or revision changed")
				}
				if mode == "stale" {
					http.Error(w, "stale", 412)
					return
				}
				if mode == "lost" {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
					return
				}
				w.Header().Set("ETag", `"committed"`)
				fmt.Fprintf(w, `{"slug":"research","display_name":%q,"management_etag":"committed","management_revision":2}`, name)
			}))
			defer server.Close()
			input := "NAME research\n"
			if mode == "decline" {
				input = "no\n"
			}
			if mode == "eof" {
				input = "NAME research"
			}
			runner := Runner{Stdin: strings.NewReader(input), IsInteractive: func() bool { return mode != "noninteractive" }}
			if mode == "nil-input" {
				runner.Stdin = nil
			}
			args := []string{"--workspace", "research", "--addr", strings.TrimPrefix(server.URL, "http://")}
			if mode != "missing-name-flag" {
				if mode == "invalid-name" {
					name = "bad\nname"
				}
				args = append(args, "--display-name", name)
			}
			var out, notices strings.Builder
			code := runWorkspaceDisplayName(t.Context(), server.Client(), &out, &notices, args, runner)
			wantWrites := int32(0)
			if mode == "success" || mode == "clear" || mode == "stale" || mode == "lost" {
				wantWrites = 1
			}
			if writes.Load() != wantWrites || reads.Load() > 1 {
				t.Fatal("consent bypass, retry or refreshed revision")
			}
			if mode == "success" || mode == "clear" {
				if code != ExitHealthy || !strings.Contains(out.String(), "Slug, paths, permissions and credentials unchanged") || !strings.Contains(notices.String(), fmt.Sprintf("After: %q", name)) {
					t.Fatal("exact update not disclosed and acknowledged")
				}
			} else if code != ExitDown || out.Len() != 0 {
				t.Fatal("failed update reported success")
			}
			if mode == "noninteractive" || mode == "missing-name-flag" || mode == "invalid-name" {
				if calls.Load() != 0 {
					t.Fatal("invalid or noninteractive intent contacted daemon")
				}
			}
			if mode == "lost" && (!strings.Contains(notices.String(), "do not infer rollback") || !strings.Contains(notices.String(), `reviewed revision="reviewed"`)) {
				t.Fatal("uncertainty discarded original consent")
			}
		})
	}
}
