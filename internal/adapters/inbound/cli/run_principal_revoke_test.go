package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPrincipalRevocationConsentAndOriginalRecovery(t *testing.T) {
	const id = "acc_AAAAAAAAAAAAAAAAAAAAAA"
	for _, mode := range []string{"success", "decline", "eof", "noninteractive", "stale", "lost", "recovery", "recovery-decline"} {
		t.Run(mode, func(t *testing.T) {
			var writes, previews atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/status" {
					fmt.Fprint(w, `{"state":"healthy","control_plane_protocol":11}`)
					return
				}
				if r.URL.Path != "/api/v1/principals/"+id {
					t.Error("noncanonical path")
				}
				principal := `{"id":"` + id + `","label":"Retained","kind":"managed","status":"active","revision":"reviewed","permissions":[]}`
				if r.Method == http.MethodGet {
					if strings.HasPrefix(mode, "recovery") {
						principal = strings.Replace(principal, "reviewed", "advisory", 1)
					}
					fmt.Fprint(w, principal)
					return
				}
				if r.Header.Get("If-Match") != `"reviewed"` {
					t.Error("consent revision refreshed")
				}
				if r.URL.RawQuery == "dry_run=true" {
					previews.Add(1)
					w.Header().Set("ETag", `"reviewed"`)
					fmt.Fprintf(w, `{"principal":%s,"credential_count":2,"replayed":false}`, principal)
					return
				}
				writes.Add(1)
				if r.Header.Get("Idempotency-Key") == "" {
					t.Error("missing retry key")
				}
				if strings.HasPrefix(mode, "recovery") && r.Header.Get("Idempotency-Key") != "original" {
					t.Error("original key replaced")
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
					conn.Close()
					return
				}
				principal = strings.Replace(strings.Replace(principal, "active", "revoked", 1), "reviewed", "committed", 1)
				w.Header().Set("ETag", `"committed"`)
				fmt.Fprintf(w, `{"principal":%s,"credential_count":0,"replayed":%t}`, principal, mode == "recovery")
			}))
			defer server.Close()
			args := []string{"revoke", "--principal", id, "--addr", strings.TrimPrefix(server.URL, "http://")}
			if strings.HasPrefix(mode, "recovery") {
				args = append(args, "--retry-key", "original", "--expected-revision", "reviewed")
			}
			input := "REVOKE " + id + "\n"
			if strings.Contains(mode, "decline") {
				input = "no\n"
			}
			if mode == "eof" {
				input = "REVOKE " + id
			}
			var out, notices strings.Builder
			code := runPrincipal(t.Context(), server.Client(), &out, &notices, args, Runner{Stdin: strings.NewReader(input), IsInteractive: func() bool { return mode != "noninteractive" }})
			wantWrites := int32(0)
			if mode == "success" || mode == "stale" || mode == "lost" || mode == "recovery" {
				wantWrites = 1
			}
			if writes.Load() != wantWrites || previews.Load() > 1 {
				t.Fatal("consent bypass or automatic retry")
			}
			if strings.HasPrefix(mode, "recovery") && previews.Load() != 0 {
				t.Fatal("recovery replaced original preview")
			}
			if mode == "success" || mode == "recovery" {
				if code != ExitHealthy || !strings.Contains(out.String(), "status=revoked") {
					t.Fatal("revocation not acknowledged")
				}
			} else if code != ExitDown || out.Len() != 0 {
				t.Fatal("failed consent reported success")
			}
			if mode == "lost" && (!strings.Contains(notices.String(), "Do not infer rollback") || !strings.Contains(notices.String(), "--expected-revision reviewed")) {
				t.Fatal("uncertainty lost original recovery intent")
			}
		})
	}
}
