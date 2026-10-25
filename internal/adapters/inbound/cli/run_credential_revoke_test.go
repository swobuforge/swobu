package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCredentialRevocationConsentAndOriginalRecovery(t *testing.T) {
	const principal = "acc_AAAAAAAAAAAAAAAAAAAAAA"
	const credential = "acred_AAAAAAAAAAAAAAAAAAAAAA"
	for _, mode := range []string{"success", "decline", "eof", "noninteractive", "stale", "lost", "recovery", "recovery-decline", "incomplete-recovery"} {
		t.Run(mode, func(t *testing.T) {
			var writes, previews, requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				requests.Add(1)
				if req.URL.Path == "/api/v1/status" {
					fmt.Fprint(w, `{"state":"healthy","control_plane_protocol":11}`)
					return
				}
				identity := `{"id":"` + principal + `","label":"Retained","kind":"managed","status":"active","revision":"advisory","permissions":[]}`
				if req.URL.Path == "/api/v1/principals/"+principal && req.Method == http.MethodGet {
					fmt.Fprint(w, identity)
					return
				}
				if req.URL.Path != "/api/v1/principals/"+principal+"/credentials/"+credential || req.Method != http.MethodDelete {
					t.Error("noncanonical operation")
				}
				if req.URL.RawQuery == "dry_run=true" {
					previews.Add(1)
					if req.Header.Get("Idempotency-Key") != "" {
						t.Error("preview reserved key")
					}
					w.Header().Set("ETag", `"reviewed"`)
					fmt.Fprintf(w, `{"principal":"%s","credential":{"id":"%s","workspace":"prod","created_at":"2026-10-11T00:00:00Z"},"revision":"reviewed","approval_class":"trusted_confirmation","warning":"Preview is not approval"}`, principal, credential)
					return
				}
				writes.Add(1)
				if req.Header.Get("If-Match") != `"reviewed"` || req.Header.Get("Idempotency-Key") == "" {
					t.Error("original consent lost")
				}
				if strings.HasPrefix(mode, "recovery") && req.Header.Get("Idempotency-Key") != "original" {
					t.Error("recovery changed key")
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
				w.Header().Set("ETag", `"advisory"`)
				fmt.Fprintf(w, `{"principal":%s,"credential":"%s","revoked":true,"replayed":%t}`, identity, credential, mode == "recovery")
			}))
			defer server.Close()
			args := []string{"revoke", "--principal", principal, "--credential", credential, "--addr", strings.TrimPrefix(server.URL, "http://")}
			if strings.HasPrefix(mode, "recovery") {
				args = append(args, "--retry-key", "original", "--expected-revision", "reviewed")
			}
			if mode == "incomplete-recovery" {
				args = append(args, "--retry-key", "original")
			}
			input := "REVOKE " + principal + " " + credential + "\n"
			if strings.Contains(mode, "decline") {
				input = "no\n"
			}
			if mode == "eof" {
				input = strings.TrimSuffix(input, "\n")
			}
			var out, notices strings.Builder
			code := runCredential(t.Context(), server.Client(), &out, &notices, args, Runner{Stdin: strings.NewReader(input), IsInteractive: func() bool { return mode != "noninteractive" }})
			wantWrites := int32(0)
			if mode == "success" || mode == "stale" || mode == "lost" || mode == "recovery" {
				wantWrites = 1
			}
			if writes.Load() != wantWrites || previews.Load() > 1 {
				t.Fatal("consent bypass or automatic retry")
			}
			if strings.HasPrefix(mode, "recovery") && previews.Load() != 0 {
				t.Fatal("recovery refreshed preview")
			}
			if (mode == "noninteractive" || mode == "incomplete-recovery") && requests.Load() != 0 {
				t.Fatal("invalid consent reached daemon")
			}
			if mode == "success" || mode == "recovery" {
				if code != ExitHealthy || !strings.Contains(out.String(), "revoked=true") || !strings.Contains(notices.String(), "Permissions and sibling credentials remain") {
					t.Fatal("valid consent not acknowledged")
				}
			} else if code != ExitDown || out.Len() != 0 {
				t.Fatal("failed consent reported success")
			}
			if mode == "lost" && (!strings.Contains(notices.String(), "Do not infer rollback") || !strings.Contains(notices.String(), "--expected-revision reviewed")) {
				t.Fatal("uncertain recovery lost original intent")
			}
			if strings.HasPrefix(mode, "recovery") && !strings.Contains(notices.String(), "Current identity is advisory") {
				t.Fatal("recovery overstated inspection")
			}
		})
	}
}
