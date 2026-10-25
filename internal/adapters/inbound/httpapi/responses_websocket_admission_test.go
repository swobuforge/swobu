package httpapi

import (
	"encoding/json"
	"github.com/swobuforge/swobu/internal/admission"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/routing"
	"golang.org/x/net/websocket"
)

// Exercise effect-time authority through a real socket and credential store,
// after a second create has entered the connection's lane queue.
func TestResponsesWebsocket_QueuedCreateRechecksPrincipalAdmission(t *testing.T) {
	for _, change := range []string{"revoke", "remove_models", "rate"} {
		t.Run(change, func(t *testing.T) {
			store, err := openTestAccessState(filepath.Join(t.TempDir(), "access.json"))
			if err != nil {
				t.Fatal(err)
			}
			workspace, _ := routing.ParseWorkspaceSlug("alpha")
			principal, issued, err := store.Create("socket-client", workspace)
			if err != nil {
				t.Fatal(err)
			}
			if change == "rate" {
				revision, _ := store.PermissionRevision(principal.ID, workspace)
				_, err = store.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, workspace, revision, "rate", func() error { return nil }, []routing.ServiceKind{routing.ServiceModels}, []accessstate.RequestRateLimit{{Service: routing.ServiceModels, RequestsPerMinute: 1}})
				if err != nil {
					t.Fatal(err)
				}
			}
			ingress := &namedLaneWebsocketIngress{started: make(chan struct{}, 2), release: make(chan struct{}, 1)}
			server := httptest.NewServer(NewLocalAdmissionHandler(NewWorkspaceDataHandler(ingress), store, nil))
			defer server.Close()
			config, err := websocket.NewConfig("ws"+strings.TrimPrefix(server.URL, "http")+"/workspaces/alpha/v1/responses", server.URL)
			if err != nil {
				t.Fatal(err)
			}
			config.Header.Set("Authorization", "Bearer "+issued.Token)
			conn, err := websocket.DialConfig(config)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			create := `{"type":"response.create","stream_id":"main","model":"m","input":"hi"}`
			if err := websocket.Message.Send(conn, create); err != nil {
				t.Fatal(err)
			}
			awaitStartedNamedLane(t, ingress)
			if err := websocket.Message.Send(conn, create); err != nil {
				t.Fatal(err)
			}
			// This error is a read-loop barrier: the preceding create is queued.
			if err := websocket.Message.Send(conn, `{"type":"response.steer","stream_id":"barrier"}`); err != nil {
				t.Fatal(err)
			}
			assertWebsocketLaneError(t, conn, "barrier", 501)
			switch change {
			case "revoke":
				_, err = store.RevokeAccess(principal.ID)
			case "remove_models":
				revision, _ := store.PermissionRevision(principal.ID, workspace)
				_, err = store.ApplyPermissionServicesChecked(t.Context(), principal.ID, workspace, revision, "remove", true, func() error { return nil }, []routing.ServiceKind{routing.ServiceMCP})
			}
			if err != nil {
				t.Fatal(err)
			}
			ingress.release <- struct{}{}
			readNamedLaneCompletions(t, conn, 1)
			status := 400
			if change == "rate" {
				status = 429
			}
			assertWebsocketLaneError(t, conn, "main", status)
			select {
			case <-ingress.started:
				t.Fatal("denied queued create reached Exchange")
			default:
			}
		})
	}
}

func assertWebsocketLaneError(t *testing.T, conn *websocket.Conn, lane string, status int) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var raw string
	if err := websocket.Message.Receive(conn, &raw); err != nil {
		t.Fatal(err)
	}
	var event struct {
		Type   string `json:"type"`
		Lane   string `json:"stream_id"`
		Status int    `json:"status_code"`
	}
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "error" || event.Lane != lane || event.Status != status {
		t.Fatalf("unexpected error: %s", raw)
	}
}

func TestResponsesWebsocket_WorkspaceConcurrencyReleasedOnDisconnect(t *testing.T) {
	limiter := admission.New(func(routing.WorkspaceSlug) (string, routing.ServiceLimits, error) {
		return "workspace-life", routing.ServiceLimits{Models: routing.ServiceQuota{Concurrent: 1}}, nil
	})
	ingress := &namedLaneWebsocketIngress{started: make(chan struct{}, 2), release: make(chan struct{})}
	exited := make(chan struct{}, 1)
	data := NewLocalAdmissionHandler(NewWorkspaceDataHandler(ingress), nil, limiter.Admit)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { exited <- struct{}{} }()
		data.ServeHTTP(w, r)
	}))
	defer server.Close()
	dial := func() *websocket.Conn {
		t.Helper()
		conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/workspaces/alpha/v1/responses", "", server.URL)
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}
	conn := dial()
	defer conn.Close()
	if err := websocket.Message.Send(conn, `{"type":"response.create","stream_id":"one","model":"m","input":"hi"}`); err != nil {
		t.Fatal(err)
	}
	awaitStartedNamedLane(t, ingress)
	if err := websocket.Message.Send(conn, `{"type":"response.create","stream_id":"two","model":"m","input":"hi"}`); err != nil {
		t.Fatal(err)
	}
	assertWebsocketLaneError(t, conn, "two", 429)
	_ = conn.Close()
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("socket handler retained active workers")
	}
	// A fresh socket must be able to spend the slot released by cancellation.
	next := dial()
	defer next.Close()
	if err := websocket.Message.Send(next, `{"type":"response.create","model":"m","input":"hi"}`); err != nil {
		t.Fatal(err)
	}
	awaitStartedNamedLane(t, ingress)
	_ = next.Close()
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("replacement socket retained workers")
	}
}
