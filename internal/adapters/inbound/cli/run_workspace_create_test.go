package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestWorkspaceCreationRequiresConsentAndRetainsRecoveryIntent(t *testing.T) {
	for _, mode := range []string{"fresh", "decline", "eof", "noninteractive", "lost", "recovery", "recovery-decline", "conflict", "invalid", "missing-original-name"} {
		t.Run(mode, func(t *testing.T) {
			var calls, previews, reads, writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/api/v1/status" {
					fmt.Fprint(w, `{"state":"healthy","control_plane_protocol":11}`)
					return
				}
				if r.Method == http.MethodGet {
					reads.Add(1)
					fmt.Fprint(w, `{"slug":"research","display_name":"Later","management_revision":3,"management_etag":"later"}`)
					return
				}
				if r.URL.Path != "/api/v1/workspaces" {
					t.Error("noncanonical creation")
				}
				if r.URL.RawQuery == "dry_run=true" {
					previews.Add(1)
					fmt.Fprint(w, `{"slug":"research","display_name":"Research","route_count":0,"target_count":0,"readiness":{"llm":"not_configured","mcp":"not_configured"},"approval_class":"confirmation","diff":"Create","warning":"No reservation"}`)
					return
				}
				writes.Add(1)
				if r.Header.Get("Idempotency-Key") == "" {
					t.Error("key missing")
				}
				if mode == "recovery" && r.Header.Get("Idempotency-Key") != "original-key" {
					t.Error("recovery key changed")
				}
				if mode == "lost" {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
					return
				}
				if mode == "conflict" {
					http.Error(w, "changed intent", 409)
					return
				}
				w.Header().Set("ETag", `"original-1"`)
				w.Header().Set("Location", "/api/v1/workspaces/research")
				if mode == "recovery" {
					w.WriteHeader(200)
				} else {
					w.WriteHeader(201)
				}
				fmt.Fprint(w, `{"slug":"research","display_name":"Research","management_revision":1,"management_etag":"original-1","routes":null,"readiness":{"llm":"not_configured","mcp":"not_configured"}}`)
			}))
			defer server.Close()
			args := []string{"--workspace", "research", "--display-name", "Research", "--addr", strings.TrimPrefix(server.URL, "http://")}
			if strings.HasPrefix(mode, "recovery") || mode == "conflict" || mode == "missing-original-name" {
				args = append(args, "--retry-key", "original-key")
			}
			if mode == "missing-original-name" {
				args = []string{"--workspace", "research", "--retry-key", "original-key"}
			}
			if mode == "invalid" {
				args = []string{"--workspace", "Bad Slug"}
			}
			input := "CREATE research\n"
			if mode == "decline" || mode == "recovery-decline" {
				input = "no\n"
			}
			if mode == "eof" {
				input = "CREATE research"
			}
			var out, notices strings.Builder
			code := runWorkspaceCreate(t.Context(), server.Client(), &out, &notices, args, Runner{Stdin: strings.NewReader(input), IsInteractive: func() bool { return mode != "noninteractive" }})
			shouldApply := mode == "fresh" || mode == "lost" || mode == "recovery" || mode == "conflict"
			expected := int32(0)
			if shouldApply {
				expected = 1
			}
			if writes.Load() != expected || previews.Load() > 1 || reads.Load() > 1 {
				t.Fatal("consent bypass, retry or repeated inspection")
			}
			if mode == "fresh" || mode == "recovery" {
				if code != ExitHealthy || !strings.Contains(out.String(), "original display_name") {
					t.Fatal("success not acknowledged")
				}
			} else if code != ExitDown || out.Len() != 0 {
				t.Fatal("failed creation claimed success")
			}
			if mode == "lost" && !strings.Contains(notices.String(), "--retry-key") {
				t.Fatal("uncertainty discarded recovery")
			}
			if mode == "noninteractive" || mode == "invalid" || mode == "missing-original-name" {
				if calls.Load() != 0 {
					t.Fatal("invalid intent contacted daemon")
				}
			}
			if mode == "recovery" && (previews.Load() != 0 || reads.Load() != 1 || !strings.Contains(notices.String(), "not original consent")) {
				t.Fatal("recovery replaced original consent")
			}
		})
	}
}
