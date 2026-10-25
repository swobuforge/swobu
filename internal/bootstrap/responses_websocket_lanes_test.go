package bootstrap

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
	"golang.org/x/net/websocket"
)

// The real daemon, Exchange and Responses codec must all preserve the socket
// contract; an adapter-only fake cannot prove delivery mode or provider ingress.
func TestWorkspaceResponsesWebSocketNamedLanes(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body["stream_id"] != nil || body["generate"] != nil {
			t.Error("transport metadata reached provider")
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
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
	_, issued, err := daemon.accessStore.Create("lane-client", slug)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := websocket.NewConfig("ws"+strings.TrimPrefix(daemon.BaseURL(), "http")+"/workspaces/socket/v1/responses", daemon.BaseURL())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Header.Set("Authorization", "Bearer "+issued.Token)
	conn, err := websocket.DialConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	for _, lane := range []string{"planner", "research"} {
		raw, _ := json.Marshal(map[string]any{"type": "response.create", "stream_id": lane, "model": "chat", "input": "hello"})
		if err := websocket.Message.Send(conn, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("two independent lanes did not reach provider concurrently")
		}
	}
	close(release)
	completed := map[string]bool{}
	for len(completed) < 2 {
		var raw string
		if err := websocket.Message.Receive(conn, &raw); err != nil {
			t.Fatal(err)
		}
		var event struct {
			Type string `json:"type"`
			Lane string `json:"stream_id"`
		}
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "error" {
			t.Fatalf("daemon lane failed: %s", raw)
		}
		if event.Lane != "planner" && event.Lane != "research" {
			t.Fatalf("untagged event: %s", raw)
		}
		if event.Type == "response.completed" {
			completed[event.Lane] = true
		}
	}
}

func TestWorkspaceResponsesWebSocketQueuedCreateSeesDisabledModels(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body["stream_id"] != nil || body["generate"] != nil {
			t.Error("transport metadata reached provider")
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
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
	_, issued, err := daemon.accessStore.Create("lane-client", slug)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := websocket.NewConfig("ws"+strings.TrimPrefix(daemon.BaseURL(), "http")+"/workspaces/socket/v1/responses", daemon.BaseURL())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Header.Set("Authorization", "Bearer "+issued.Token)
	conn, err := websocket.DialConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	create := `{"type":"response.create","stream_id":"main","model":"chat","input":"hello"}`
	if err := websocket.Message.Send(conn, create); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first create did not start")
	}
	if err := websocket.Message.Send(conn, create); err != nil {
		t.Fatal(err)
	}
	if err := websocket.Message.Send(conn, `{"type":"response.steer","stream_id":"barrier"}`); err != nil {
		t.Fatal(err)
	}
	var barrier string
	if err := websocket.Message.Receive(conn, &barrier); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(barrier, `"type":"error"`) || !strings.Contains(barrier, `"stream_id":"barrier"`) {
		t.Fatal(barrier)
	}
	_, err = daemon.configStore.Update(t.Context(), func(current routing.Config) (routing.Config, error) {
		route, err := routing.ParseRouteName("chat")
		if err != nil {
			return routing.Config{}, err
		}
		next, err := current.DeleteRoute(slug, route)
		if err != nil {
			return routing.Config{}, err
		}
		return next.SetServiceEnabled(slug, routing.ServiceModels, false)
	})
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	completed := false
	for {
		var raw string
		if err := websocket.Message.Receive(conn, &raw); err != nil {
			t.Fatal(err)
		}
		var event struct {
			Type string `json:"type"`
			Lane string `json:"stream_id"`
		}
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			t.Fatal(err)
		}
		if event.Lane != "main" {
			t.Fatal("lost queued lane identity")
		}
		if event.Type == "response.completed" {
			completed = true
		}
		if event.Type == "error" {
			if !completed {
				t.Fatalf("active request failed before queued rejection: %s", raw)
			}
			break
		}
	}
	select {
	case <-started:
		t.Fatal("disabled Models dispatched queued create to provider")
	default:
	}
}
