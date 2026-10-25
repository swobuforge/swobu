package operatorclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/authplane"
)

func TestClientGetAuthSessionStatus(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/auth-sessions/sess-1" {
			t.Fatalf("request method/path = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"provider_spec":"chatgpt","session_id":"sess-1","state":"succeeded","credential_ref":"secret:chatgpt/default","error":""}`))
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	out, err := c.GetAuthSessionStatus(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetAuthSessionStatus returned error: %v", err)
	}
	if out.ProviderSpec != "chatgpt" || out.CredentialRef != "secret:chatgpt/default" || out.State != "succeeded" {
		t.Fatalf("unexpected output: %#v", out)
	}
}

func TestClientCancelAuthSession(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/auth-sessions/sess-1" {
			t.Fatalf("request method/path = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"session_id":"sess-1","state":"canceled"}`))
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	if err := c.CancelAuthSession(context.Background(), "sess-1"); err != nil {
		t.Fatalf("CancelAuthSession returned error: %v", err)
	}
}

func TestAuthHandoffRequestsCannotBeRewoundForTransportReplay(t *testing.T) {
	intent, err := NewAuthStartIntent(authplane.StartInput{ProviderSpec: "chatgpt", DraftSubject: "draft", AuthMode: "device"}, "original-start-key")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	c := New(&http.Client{Transport: principalCreationTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.GetBody != nil || request.URL.Path != "/api/v1/auth-sessions" || request.Header.Get("Idempotency-Key") != "original-start-key" {
			t.Error("canonical retained auth handoff permitted replay or lost original attribution")
		}
		return nil, context.DeadlineExceeded
	})}, "http://127.0.0.1:7926")
	if _, err := c.ApplyAuthStartIntent(context.Background(), intent); err == nil || calls != 1 {
		t.Fatalf("uncertain handoff: calls=%d error=%v", calls, err)
	}
}

func TestAuthObservationInvalidIDsNeverContactNode(t *testing.T) {
	calls := 0
	client := New(&http.Client{Transport: principalCreationTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, context.DeadlineExceeded })}, "http://127.0.0.1:7926")
	for _, id := range []string{"", " sess-1", "sess-1 ", "a/b", "a?query", "a#fragment", "%73ess", "..", "é"} {
		if _, err := client.GetAuthSessionStatus(t.Context(), id); err == nil {
			t.Fatalf("GET accepted %q", id)
		}
		if err := client.CancelAuthSession(t.Context(), id); err == nil {
			t.Fatalf("DELETE accepted %q", id)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid IDs contacted Node %d times", calls)
	}
}

func TestAuthObservationRejectsChangedAcknowledgements(t *testing.T) {
	for _, body := range []string{`{"session_id":"other","state":"pending"}`, `{"session_id":"sess-1","state":"future"}`, `{"session_id":"sess-1","state":"succeeded"}`, `{"session_id":"sess-1","state":"pending","credential_ref":"opaque"}`} {
		client := New(&http.Client{Transport: principalCreationTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}, "http://127.0.0.1:7926")
		if _, err := client.GetAuthSessionStatus(t.Context(), "sess-1"); err == nil {
			t.Fatalf("accepted status %s", body)
		}
		if err := client.CancelAuthSession(t.Context(), "sess-1"); err == nil {
			t.Fatalf("accepted cancellation %s", body)
		}
	}
}
