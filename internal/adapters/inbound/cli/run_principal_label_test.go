package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPrincipalLabelRequiresFreshConsentAndNeverRefreshesStaleRevision(t *testing.T) {
	const id = "acc_AAAAAAAAAAAAAAAAAAAAAA"
	for _, mode := range []string{"success", "decline", "eof", "noninteractive", "stale", "lost", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			var reads, writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/status" {
					fmt.Fprint(w, `{"state":"healthy","control_plane_protocol":11}`)
					return
				}
				if r.URL.Path != "/api/v1/principals/"+id {
					t.Error("noncanonical Principal path")
				}
				if r.Method == http.MethodGet {
					reads.Add(1)
					if mode == "unavailable" {
						http.Error(w, "unavailable", 503)
						return
					}
					fmt.Fprint(w, `{"id":"`+id+`","label":"Before","revision":"reviewed","kind":"managed","status":"active","permissions":[]}`)
					return
				}
				writes.Add(1)
				if r.Method != http.MethodPatch || r.Header.Get("If-Match") != `"reviewed"` {
					t.Error("consent revision changed")
				}
				if mode == "stale" {
					http.Error(w, "stale revision", 412)
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
				fmt.Fprint(w, `{"id":"`+id+`","label":"After","revision":"committed","kind":"managed","status":"active","permissions":[]}`)
			}))
			defer server.Close()
			input := "LABEL " + id + "\n"
			if mode == "decline" {
				input = "no\n"
			}
			if mode == "eof" {
				input = "LABEL " + id
			}
			var out, notices strings.Builder
			code := runPrincipal(t.Context(), server.Client(), &out, &notices, []string{"label", "--principal", id, "--label", "After", "--addr", strings.TrimPrefix(server.URL, "http://")}, Runner{Stdin: strings.NewReader(input), IsInteractive: func() bool { return mode != "noninteractive" }})
			wantWrites := int32(0)
			if mode == "success" || mode == "stale" || mode == "lost" {
				wantWrites = 1
			}
			if writes.Load() != wantWrites || reads.Load() > 1 {
				t.Fatal("consent bypass, automatic retry or refreshed revision")
			}
			if mode == "success" {
				if code != ExitHealthy || !strings.Contains(out.String(), "No permission or credential changed") {
					t.Fatal("successful update not acknowledged")
				}
			} else if code != ExitDown || out.Len() != 0 {
				t.Fatal("failed update reported success")
			}
			if mode == "lost" && (!strings.Contains(notices.String(), "do not infer rollback") || !strings.Contains(notices.String(), `reviewed revision="reviewed"`)) {
				t.Fatal("uncertainty discarded reviewed intent")
			}
		})
	}
}
