package operatorclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestActivityStreamValidatedObservationAndTerminalFailures(t *testing.T) {
	const valid = `{"mode":"now_history_observation","complete":false,"activity":{"scope":{"kind":"workspace","workspace":"research"},"exchanges":[],"mcp_operations":[{"operation_id":"mcp_exact","workspace":"research","execution_possibility":"may_have_occurred"}]}}`
	for _, payload := range []string{valid, strings.Replace(valid, `false`, `true`, 1), strings.Replace(valid, `"complete":false,`, ``, 1), strings.Replace(valid, `"workspace":"research"`, `"workspace":"other"`, 1), strings.Replace(valid, `"exchanges":[]`, `"exchanges":null`, 1), valid + `{}`, strings.Repeat("x", (1<<20)+1)} {
		var calls atomic.Int32
		node := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			calls.Add(1)
			if request.URL.Path != "/api/v1/activity/stream" || request.URL.Query().Get("workspace") != "research" || request.URL.Query().Get("kind") != "mcp_operation" {
				t.Error("stream escaped canonical scope")
			}
			writer.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(writer, "event: activity\ndata: %s\n\n", payload)
		}))
		client := New(node.Client(), node.URL)
		stream, err := client.OpenActivityStream(t.Context(), "research", "mcp_operation")
		if err != nil {
			t.Fatal(err)
		}
		projection, err := stream.Next(t.Context())
		if payload == valid {
			if err != nil || len(projection.MCPOperations) != 1 || projection.MCPOperations[0].Occurrence != "may_have_occurred" {
				t.Fatal("valid observation lost uncertainty", err)
			}
		} else if err == nil {
			t.Fatal("invalid observation accepted")
		}
		if _, err := stream.Next(t.Context()); err != io.EOF || calls.Load() != 1 {
			t.Fatal("terminal stream retried or reconnected")
		}
		stream.Close()
		node.Close()
	}
}

func TestActivityStreamCancellationAndPrecontactValidation(t *testing.T) {
	var calls atomic.Int32
	done := make(chan struct{})
	node := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		defer close(done)
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		writer.(http.Flusher).Flush()
		<-request.Context().Done()
	}))
	defer node.Close()
	client := New(node.Client(), node.URL)
	for _, input := range []struct{ workspace, kind string }{{"Research", ""}, {"research", "unknown"}, {"../research", "exchange"}} {
		if _, err := client.OpenActivityStream(t.Context(), input.workspace, input.kind); err == nil || calls.Load() != 0 {
			t.Fatal("invalid stream input contacted Node")
		}
	}
	stream, err := client.OpenActivityStream(t.Context(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := stream.Next(ctx); err == nil {
		t.Fatal("cancelled observation succeeded")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not release remote handler")
	}
	if _, err := stream.Next(t.Context()); err != io.EOF || calls.Load() != 1 {
		t.Fatal("cancelled stream reconnected")
	}
}

func TestActivityStreamDeniedWrongMediaAndTerminalErrorArePrivate(t *testing.T) {
	for _, fixture := range []struct {
		status      int
		media, body string
	}{
		{http.StatusForbidden, "application/json", "private-fixture"},
		{http.StatusOK, "application/json", "private-fixture"},
		{http.StatusOK, "text/event-stream", "event: error\ndata: {\"secret\":\"private-fixture\"}\n\n"},
	} {
		var calls atomic.Int32
		node := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			calls.Add(1)
			writer.Header().Set("Content-Type", fixture.media)
			writer.WriteHeader(fixture.status)
			fmt.Fprint(writer, fixture.body)
		}))
		client := New(node.Client(), node.URL)
		stream, err := client.OpenActivityStream(t.Context(), "", "")
		if stream != nil {
			_, err = stream.Next(t.Context())
			stream.Close()
		}
		if err == nil || strings.Contains(err.Error(), "private-fixture") || calls.Load() != 1 {
			t.Fatal("failed stream disclosed body or retried")
		}
		node.Close()
	}
}
