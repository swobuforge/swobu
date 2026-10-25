package httpapi

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	evidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/ledger"
)

func TestActivityStreamObservationFilteringAndCancellation(t *testing.T) {
	var calls atomic.Int32
	returned := make(chan struct{})
	handler := NewActivityStreamHandler(func(request *http.Request, scope evidence.ProjectionScope, _ ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
		calls.Add(1)
		return evidence.ActivityProjection{Scope: scope, Exchanges: []evidence.ActivityExchange{{ExchangeID: "excluded"}}, MCPOperations: []evidence.MCPOperation{{OperationID: "mcp_exact"}}, History: &ledger.Health{State: ledger.StateDegraded, DroppedMCPOperationsTotal: 2}}, nil
	})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer close(returned)
		handler.ServeHTTP(writer, request)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"?kind=mcp_operation", nil)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	event, err := reader.ReadString('\n')
	if err != nil || event != "event: activity\n" {
		t.Fatal("Activity event missing", err)
	}
	data, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(data, `"complete":false`) || !strings.Contains(data, `"mcp_exact"`) || !strings.Contains(data, `"dropped_mcp_operations_total":2`) || strings.Contains(data, "excluded") || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("stream lost observation/filter/gap semantics")
	}
	cancel()
	response.Body.Close()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("cancelled stream retained handler")
	}
	if calls.Load() != 1 {
		t.Fatal("cancellation continued polling")
	}
}

func TestActivityStreamRejectsPaginationResumeAndReadFailure(t *testing.T) {
	for _, query := range []string{"limit=1", "kind=unknown", "workspace=research&workspace=other", "mcp_cursor=invalid", "workspace=%zz", "from=2026-10-20T12:00:00Z", "result=succeeded", "principal=local"} {
		calls := 0
		handler := NewActivityStreamHandler(func(*http.Request, evidence.ProjectionScope, ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
			calls++
			return evidence.ActivityProjection{}, nil
		})
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/activity/stream?"+query, nil))
		if writer.Code != http.StatusBadRequest || calls != 0 {
			t.Fatal("invalid stream query reached evidence", query)
		}
	}
	handler := NewActivityStreamHandler(func(*http.Request, evidence.ProjectionScope, ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
		return evidence.ActivityProjection{}, errors.New("private-fixture")
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatal("failed initial evidence became empty successful stream")
	}
}

func TestActivityStreamCapacityAndResumeRejectBeforeRead(t *testing.T) {
	calls := 0
	handler := NewActivityStreamHandler(func(*http.Request, evidence.ProjectionScope, ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
		calls++
		return evidence.ActivityProjection{}, nil
	})
	for index := 0; index < cap(handler.sessions); index++ {
		handler.sessions <- struct{}{}
	}
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/activity/stream", nil))
	if writer.Code != http.StatusServiceUnavailable || calls != 0 {
		t.Fatal("exhausted observation capacity read evidence")
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/activity/stream", nil)
	request.Header.Set("Last-Event-ID", "forged-resume")
	writer = httptest.NewRecorder()
	handler.ServeHTTP(writer, request)
	if writer.Code != http.StatusBadRequest || calls != 0 {
		t.Fatal("unsupported stream replay admitted")
	}
}

func TestActivityStreamFailureAfterFirstFrameIsTerminalAndRedacted(t *testing.T) {
	var calls atomic.Int32
	handler := NewActivityStreamHandler(func(request *http.Request, scope evidence.ProjectionScope, _ ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
		if calls.Add(1) > 1 {
			return evidence.ActivityProjection{}, errors.New("private-fixture")
		}
		return evidence.ActivityProjection{Scope: scope, Exchanges: []evidence.ActivityExchange{}}, nil
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewScanner(response.Body)
	var events []string
	for reader.Scan() {
		events = append(events, reader.Text())
	}
	joined := strings.Join(events, "\n")
	if reader.Err() != nil || calls.Load() != 2 || !strings.Contains(joined, "event: error") || strings.Contains(joined, "private-fixture") {
		t.Fatal("post-header failure retried or disclosed private evidence")
	}
}

func TestActivityStreamOversizedFrameDoesNotPublishPartialObservation(t *testing.T) {
	handler := NewActivityStreamHandler(func(request *http.Request, scope evidence.ProjectionScope, _ ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
		return evidence.ActivityProjection{Scope: scope, Exchanges: []evidence.ActivityExchange{{ExchangeID: strings.Repeat("x", 1<<20)}}}, nil
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusInternalServerError || response.Header.Get("Content-Type") == "text/event-stream" {
		t.Fatal("oversized observation acknowledged as complete frame")
	}
}
