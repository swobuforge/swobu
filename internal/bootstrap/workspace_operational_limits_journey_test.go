package bootstrap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

// Real Search execution crosses both local bearer admission and encrypted relay
// transport. Distinct Principals share one process bucket, not one per ingress.
func TestWorkspaceSearchOperationalTotalAcrossLocalRelayPrincipalsAndRestart(t *testing.T) {
	t.Setenv("SWOBU_RELAY_ADDR", "127.0.0.1:1")
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces:\n  research:\n    routes: []\n    search:\n      connection_id: cn_fixture_search\n  other:\n    routes: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"url":"https://docs.example.test","title":"Docs","content":"quota proof"}]}`)
	}))
	defer upstream.Close()
	id, _ := connectionidentity.Parse("cn_fixture_search")
	endpoint, err := connections.ParseEndpoint(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := connections.NewSearchSpec("fixture", endpoint, credentialref.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := connections.New(id, "Operational Search fixture", spec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	publishCurrentConnectionFixtures(t, path, connection)
	input := StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}}
	daemon, err := Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	workspace, _ := routing.ParseWorkspaceSlug("research")
	unlock := daemon.publicationStore.LockLifecycle()
	err = daemon.publicationStore.Set(workspace, true, daemon.publicationStore.Revision(), func() error { return nil })
	unlock()
	if err != nil {
		t.Fatal(err)
	}
	_, first, err := daemon.accessStore.Create("local-search", workspace)
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := daemon.accessStore.Create("relay-search", workspace)
	if err != nil {
		t.Fatal(err)
	}
	denied, third, err := daemon.accessStore.Create("models-only", workspace)
	if err != nil {
		t.Fatal(err)
	}
	permission, err := daemon.accessStore.PermissionRevision(denied.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	_, err = daemon.accessStore.ApplyPermissionServicesWithRatesChecked(t.Context(), denied.ID, workspace, permission, "models-only", func() error { return nil }, []routing.ServiceKind{routing.ServiceModels}, nil)
	if err != nil {
		t.Fatal(err)
	}
	control := func(method, slug, etag string, body []byte, want int) ([]byte, string) {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), method, daemon.BaseURL()+"/api/v1/workspaces/"+slug+"/service-limits", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		if etag != "" {
			request.Header.Set("If-Match", etag)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, slug, response.StatusCode, want, raw)
		}
		return raw, response.Header.Get("ETag")
	}
	_, revision := control("GET", "research", "", nil, 200)
	policy := []byte(`{"models":{"requests_per_minute":0},"mcp":{"requests_per_minute":0},"search":{"requests_per_minute":2}}`)
	control("PUT", "research", "", policy, 428)
	_, saved := control("PUT", "research", revision, policy, 200)
	control("PUT", "research", revision, policy, 412)
	control("PUT", "research", saved, []byte(`{"search":{"concurrent":1}}`), 400)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, noop := control("PUT", "research", saved, policy, 200)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if noop != saved || !bytes.Equal(before, after) {
		t.Fatal("no-op rewrote policy or revision")
	}
	other, _ := control("GET", "other", "", nil, 200)
	var otherLimits routing.ServiceLimits
	if err := json.Unmarshal(other, &otherLimits); err != nil || otherLimits != (routing.ServiceLimits{}) {
		t.Fatalf("other Workspace changed: %s %v", other, err)
	}
	send := func(client *http.Client, base, token, origin string, want int) {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), "POST", base+"/workspaces/research/search", strings.NewReader(`{"query":"limits","limit":1}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != want {
			t.Fatalf("Search status=%d want=%d: %s", response.StatusCode, want, body)
		}
		if want == 429 {
			seconds, err := strconv.Atoi(response.Header.Get("Retry-After"))
			if err != nil || seconds < 1 {
				t.Fatalf("missing retry interval: %q", response.Header.Get("Retry-After"))
			}
		}
	}
	relay := ownerRelaySearchClient(t, daemon.publicHandler)
	send(relay, "https://owner.example", third.Token, "", 403)
	send(http.DefaultClient, daemon.BaseURL(), first.Token, "https://external.example", 403)
	if calls.Load() != 0 {
		t.Fatal("authority denial reached Search")
	}
	send(http.DefaultClient, daemon.BaseURL(), first.Token, "", 200)
	send(relay, "https://owner.example", second.Token, "", 200)
	send(http.DefaultClient, daemon.BaseURL(), second.Token, "", 429)
	send(relay, "https://owner.example", first.Token, "", 429)
	send(relay, "https://owner.example", third.Token, "", 403)
	if calls.Load() != 2 {
		t.Fatalf("denied calls reached upstream: %d", calls.Load())
	}
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	daemon, err = Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	restored, reopened := control("GET", "research", "", nil, 200)
	var limits routing.ServiceLimits
	if err := json.Unmarshal(restored, &limits); err != nil || limits.Search.RequestsPerMinute != 2 || reopened != saved {
		t.Fatalf("restart changed policy/revision: %s %s %v", restored, reopened, err)
	}
	send(http.DefaultClient, daemon.BaseURL(), first.Token, "", 200)
	if calls.Load() != 3 {
		t.Fatal("restart did not reset rate credit")
	}
}

func TestWorkspaceModelOperationalSlotsAcrossStreamCompletionCancelAndDisconnect(t *testing.T) {
	for _, terminal := range []string{"complete", "cancel", "disconnect"} {
		t.Run(terminal, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			finish := make(chan struct{})
			ended := make(chan struct{})
			chunk := "data: " + `{"id":"quota","object":"chat.completion.chunk","model":"fixture","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}` + "\n\n"
			done := "data: " + `{"id":"quota","object":"chat.completion.chunk","model":"fixture","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, chunk)
				w.(http.Flusher).Flush()
				if n == 1 {
					defer close(ended)
					select {
					case <-finish:
					case <-r.Context().Done():
						return
					}
				}
				_, _ = io.WriteString(w, done)
			}))
			defer upstream.Close()
			path := filepath.Join(root, "swobu.yaml")
			writeCurrentWorkspaceFixture(t, path, root, "quota", upstream.URL, "chat_completions_stream", false)
			daemon, err := Start(t.Context(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
			if err != nil {
				t.Fatal(err)
			}
			defer daemon.Close()
			slug, _ := routing.ParseWorkspaceSlug("quota")
			policy := routing.ServiceLimits{Models: routing.ServiceQuota{RequestsPerMinute: 2, Concurrent: 1}}
			publish := func() {
				t.Helper()
				current, err := daemon.configStore.GetWorkspace(t.Context(), slug)
				if err != nil {
					t.Fatal(err)
				}
				etag := fmt.Sprintf("%s-%d", current.ManagementIncarnation(), current.ManagementRevision())
				if _, err := daemon.configStore.SetServiceLimitsChecked(t.Context(), slug, policy, etag, func(context.Context) error { return nil }); err != nil {
					t.Fatal(err)
				}
			}
			publish()
			_, issued, err := daemon.accessStore.Create("stream quota", slug)
			if err != nil {
				t.Fatal(err)
			}
			send := func(ctx context.Context) *http.Response {
				t.Helper()
				request, err := http.NewRequestWithContext(ctx, "POST", daemon.BaseURL()+"/workspaces/quota/chat/completions", strings.NewReader(`{"model":"chat","messages":[{"role":"user","content":"limits"}],"stream":true}`))
				if err != nil {
					t.Fatal(err)
				}
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Authorization", "Bearer "+issued.Token)
				response, err := http.DefaultClient.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				return response
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			first := send(ctx)
			if first.StatusCode != 200 {
				body, _ := io.ReadAll(first.Body)
				t.Fatalf("first status=%d %s", first.StatusCode, body)
			}
			reader := bufio.NewReader(first.Body)
			for {
				line, err := reader.ReadString('\n')
				if strings.Contains(line, "partial") {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			denied := send(t.Context())
			_, _ = io.Copy(io.Discard, denied.Body)
			_ = denied.Body.Close()
			if denied.StatusCode != 429 || denied.Header.Get("Retry-After") == "" || calls.Load() != 1 {
				t.Fatalf("slot did not cover delivery: status=%d upstream=%d", denied.StatusCode, calls.Load())
			}
			switch terminal {
			case "complete":
				close(finish)
				if _, err := io.Copy(io.Discard, first.Body); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
			case "disconnect":
				_ = first.Body.Close()
			}
			_ = first.Body.Close()
			select {
			case <-ended:
			case <-time.After(3 * time.Second):
				t.Fatal("upstream did not finish")
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				next := send(t.Context())
				raw, err := io.ReadAll(next.Body)
				_ = next.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if next.StatusCode == 200 {
					if !strings.Contains(string(raw), "partial") {
						t.Fatal("returned slot did not deliver")
					}
					break
				}
				if next.StatusCode != 429 || time.Now().After(deadline) {
					t.Fatalf("slot did not return: %d %s", next.StatusCode, raw)
				}
				time.Sleep(5 * time.Millisecond)
			}
			policy.Models.Concurrent = 2
			publish()
			exhausted := send(t.Context())
			_, _ = io.Copy(io.Discard, exhausted.Body)
			_ = exhausted.Body.Close()
			if exhausted.StatusCode != 429 || calls.Load() != 2 {
				t.Fatalf("policy revision reset RPM: status=%d upstream=%d", exhausted.StatusCode, calls.Load())
			}
		})
	}
}

func TestWorkspaceResponsesWebSocketChargesCreatesNotConnections(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, workspaceToolRoundStream("responses", "", true))
	}))
	defer upstream.Close()
	path := filepath.Join(root, "swobu.yaml")
	writeCurrentWorkspaceFixture(t, path, root, "socket", upstream.URL, "responses_stream", false)
	daemon, err := Start(t.Context(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	slug, _ := routing.ParseWorkspaceSlug("socket")
	current, err := daemon.configStore.GetWorkspace(t.Context(), slug)
	if err != nil {
		t.Fatal(err)
	}
	etag := fmt.Sprintf("%s-%d", current.ManagementIncarnation(), current.ManagementRevision())
	_, err = daemon.configStore.SetServiceLimitsChecked(t.Context(), slug, routing.ServiceLimits{Models: routing.ServiceQuota{RequestsPerMinute: 3, Concurrent: 1}}, etag, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	_, issued, err := daemon.accessStore.Create("socket consumer", slug)
	if err != nil {
		t.Fatal(err)
	}
	connect := func() *websocket.Conn {
		t.Helper()
		cfg, err := websocket.NewConfig("ws"+strings.TrimPrefix(daemon.BaseURL(), "http")+"/workspaces/socket/responses", daemon.BaseURL())
		if err != nil {
			t.Fatal(err)
		}
		cfg.Header.Set("Authorization", "Bearer "+issued.Token)
		conn, err := websocket.DialConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		return conn
	}
	unlock := daemon.publicationStore.LockLifecycle()
	err = daemon.publicationStore.Set(slug, true, daemon.publicationStore.Revision(), func() error { return nil })
	unlock()
	if err != nil {
		t.Fatal(err)
	}
	_, publicCredential, err := daemon.accessStore.Create("relay model consumer", slug)
	if err != nil {
		t.Fatal(err)
	}
	relay := ownerRelaySearchClient(t, daemon.publicHandler)
	publicRequest := func(want int) {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), "POST", "https://owner.example/workspaces/socket/responses", strings.NewReader(`{"model":"chat","input":"public limits","stream":true}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+publicCredential.Token)
		request.Header.Set("Content-Type", "application/json")
		response, err := relay.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || response.StatusCode != want {
			t.Fatalf("public Models status=%d want=%d body=%s err=%v", response.StatusCode, want, body, err)
		}
		if want == 429 && response.Header.Get("Retry-After") == "" {
			t.Fatal("public exhaustion omitted Retry-After")
		}
	}
	publicRequest(200)
	first := connect()
	second := connect()
	create := func(conn *websocket.Conn, allowed bool) {
		t.Helper()
		if err := websocket.Message.Send(conn, `{"type":"response.create","model":"chat","input":"limits","stream":true}`); err != nil {
			t.Fatal(err)
		}
		for {
			var frame string
			if err := websocket.Message.Receive(conn, &frame); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(frame, `"type":"error"`) {
				if allowed || !strings.Contains(frame, `429`) {
					t.Fatalf("unexpected WebSocket error: %s", frame)
				}
				return
			}
			if strings.Contains(frame, `"type":"response.completed"`) {
				if !allowed {
					t.Fatal("exhausted operation completed")
				}
				return
			}
		}
	}
	create(first, true)
	create(second, true)
	create(first, false)
	_ = connect() // New upgrades remain free even with no rate credit.
	publicRequest(429)
	if calls.Load() != 3 {
		t.Fatalf("WebSocket admission reached provider %d times", calls.Load())
	}
	request, err := http.NewRequestWithContext(t.Context(), "POST", daemon.BaseURL()+"/workspaces/socket/responses", strings.NewReader(`{"model":"chat","input":"limits","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+issued.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != 429 || calls.Load() != 3 {
		t.Fatalf("HTTP bypassed WS total: %d upstream=%d", response.StatusCode, calls.Load())
	}
}
