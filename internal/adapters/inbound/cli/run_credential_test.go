package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestCredentialIssuanceConsentAndRecovery(t *testing.T) {
	const principal = "acc_AAAAAAAAAAAAAAAAAAAAAA"
	for _, mode := range []string{"fresh", "replay", "lost", "decline", "noninteractive", "changed-intent"} {
		t.Run(mode, func(t *testing.T) {
			writes := 0
			var sentKey string
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/status":
					fmt.Fprint(w, `{"state":"healthy","control_plane_protocol":11}`)
				case "/api/v1/workspaces/research/permissions/" + principal:
					w.Header().Set("ETag", `"rev-fixture"`)
					fmt.Fprint(w, `{"principal":"`+principal+`","workspace":"research","revision":"rev-fixture","permission":{"workspace":"research","action":"use"},"allowed":true}`)
				case "/api/v1/principals/" + principal + "/credentials":
					mu.Lock()
					writes++
					sentKey = r.Header.Get("Idempotency-Key")
					mu.Unlock()
					body, _ := io.ReadAll(r.Body)
					if string(body) != `{"workspace":"research"}` {
						t.Error("altered audience")
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
					if mode == "changed-intent" {
						w.WriteHeader(409)
						fmt.Fprint(w, `{"code":"CONFLICT","message":"idempotency_conflict"}`)
						return
					}
					if mode == "replay" {
						fmt.Fprint(w, `{"access":{"id":"`+principal+`"},"credential":{"id":"acred_AAAAAAAAAAAAAAAAAAAAAA","workspace":"research"},"secret_not_recoverable":true}`)
						return
					}
					w.WriteHeader(201)
					fmt.Fprint(w, `{"access":{"id":"`+principal+`"},"credential":{"id":"acred_AAAAAAAAAAAAAAAAAAAAAA","workspace":"research"},"token":"fixture-one-shot-secret"}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			address, _ := url.Parse(server.URL)
			args := []string{"issue", "--principal", principal, "--workspace", "research", "--addr", address.Host}
			if mode == "replay" || mode == "changed-intent" {
				args = append(args, "--retry-key", "original-retained-key")
			}
			input := "ISSUE " + principal + " research\n"
			if mode == "decline" {
				input = "no\n"
			}
			var out, notices strings.Builder
			runner := Runner{Stdin: strings.NewReader(input), IsInteractive: func() bool { return mode != "noninteractive" }}
			code := runCredential(context.Background(), server.Client(), &out, &notices, args, runner)
			mu.Lock()
			defer mu.Unlock()
			if mode == "decline" || mode == "noninteractive" {
				if writes != 0 || code != ExitDown {
					t.Fatal("denied consent issued credential")
				}
				return
			}
			if writes != 1 || sentKey == "" {
				t.Fatal("issuance missing key or retried automatically")
			}
			if mode == "replay" || mode == "changed-intent" {
				if sentKey != "original-retained-key" {
					t.Fatal("recovery replaced original key")
				}
			}
			if mode == "replay" {
				if code != ExitHealthy || !strings.Contains(out.String(), "secret_not_recoverable=true") || strings.Contains(out.String(), "Access key:") || !strings.Contains(notices.String(), "may issue the first credential") {
					t.Fatal("recovery lied about secret delivery or omitted risk")
				}
			}
			if mode == "lost" {
				if code != ExitDown || !strings.Contains(notices.String(), "--retry-key "+sentKey) || !strings.Contains(notices.String(), "do not infer rollback") || out.Len() != 0 {
					t.Fatal("lost response discarded retained intent or claimed delivery")
				}
			}
			if mode == "changed-intent" && (code != ExitDown || !strings.Contains(notices.String(), "idempotency_conflict")) {
				t.Fatal("changed intent conflict hidden")
			}
			if mode == "fresh" && (code != ExitHealthy || !strings.Contains(out.String(), "fixture-one-shot-secret")) {
				t.Fatal("first issuance omitted handoff")
			}
		})
	}
}
