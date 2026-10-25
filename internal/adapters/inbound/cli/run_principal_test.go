package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPrincipalCreationRequiresFreshConsentAndRetainsRecovery(t *testing.T) {
	for _, mode := range []string{"fresh", "replay", "decline", "noninteractive", "lost"} {
		t.Run(mode, func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/status" {
					fmt.Fprint(w, `{"state":"healthy","control_plane_protocol":11}`)
					return
				}
				if r.URL.Path != "/api/v1/principals" || r.Method != "POST" || r.Header.Get("Idempotency-Key") != "original-key" {
					t.Error("creation used wrong path, key or method")
				}
				writes.Add(1)
				if mode == "lost" {
					conn, _, _ := w.(http.Hijacker).Hijack()
					_ = conn.Close()
					return
				}
				if mode == "fresh" {
					w.WriteHeader(201)
				}
				fmt.Fprint(w, `{"id":"acc_AAAAAAAAAAAAAAAAAAAAAA","label":"Independent","kind":"managed","status":"active","permissions":[]}`)
			}))
			defer server.Close()
			address, _ := url.Parse(server.URL)
			input := "CREATE Independent\n"
			if mode == "decline" {
				input = "no\n"
			}
			var out, notices strings.Builder
			code := runPrincipal(t.Context(), server.Client(), &out, &notices, []string{"create", "--label", "Independent", "--retry-key", "original-key", "--addr", address.Host}, Runner{Stdin: strings.NewReader(input), IsInteractive: func() bool { return mode != "noninteractive" }})
			if mode == "decline" || mode == "noninteractive" {
				if writes.Load() != 0 || code != ExitDown {
					t.Fatal("creation bypassed consent")
				}
			} else if mode == "lost" {
				if writes.Load() != 1 || code != ExitDown || out.Len() != 0 || !strings.Contains(notices.String(), "do not infer rollback") || !strings.Contains(notices.String(), "original-key") {
					t.Fatal("uncertainty lost intent or retried")
				}
			} else if writes.Load() != 1 || code != ExitHealthy || !strings.Contains(out.String(), "No permission or credential") {
				t.Fatal("creation outcome was not truthful")
			}
		})
	}
}
