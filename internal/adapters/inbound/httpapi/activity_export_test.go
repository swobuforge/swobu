package httpapi

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	evidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/ledger"
)

func exportTestCursor(t *testing.T, workspace, kind string) string {
	t.Helper()
	identity := "mcp_test"
	if kind == "exchange" {
		exchange, err := execution.NewExchangeID()
		if err != nil {
			t.Fatal(err)
		}
		identity = exchange.String()
	}
	return ledger.EncodeActivityCursor(workspace, kind, time.Date(2026, 10, 20, 20, 0, 0, 0, time.UTC), identity)
}

func TestActivityExportRetainedPagesAndContinuation(t *testing.T) {
	next := exportTestCursor(t, "engineering", "exchange")
	calls := 0
	handler := NewActivityExportHandler(func(_ *http.Request, scope evidence.ProjectionScope, page ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
		calls++
		if scope.Workspace != "engineering" || page.Kind != "exchange" || page.Limit != 200 && page.Limit != 1 {
			t.Fatalf("unexpected query: %+v %+v", scope, page)
		}
		result := evidence.ActivityProjection{Scope: scope, Page: &ledger.ActivityPage{Mode: "retained", Limit: page.Limit}}
		if calls == 1 {
			for i := 0; i < 200; i++ {
				result.Exchanges = append(result.Exchanges, evidence.ActivityExchange{ExchangeID: "ex_" + strconv.Itoa(i), Workspace: "engineering", State: "succeeded"})
			}
			result.Page.ExchangeNext = next
		} else {
			if !page.Exchange.Valid() || page.Exchange.ExchangeID().String() != decodeExportTestExchange(t, next) {
				t.Fatal("continuation cursor lost")
			}
			result.Exchanges = []evidence.ActivityExchange{{ExchangeID: "ex_last", Workspace: "engineering", State: "failed"}}
		}
		return result, nil
	})
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/activity/export?workspace=engineering&kind=exchange&max_rows=201", nil))
	if writer.Code != http.StatusOK || calls != 2 || writer.Header().Get("X-Swobu-Export-Rows") != "201" || writer.Header().Get("X-Swobu-Next-Cursor") != "" || writer.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("bad export: status=%d calls=%d header=%v", writer.Code, calls, writer.Header())
	}
	if writer.Header().Get("Content-Type") != "application/x-ndjson; charset=utf-8" || !strings.Contains(writer.Header().Get("Content-Disposition"), "engineering.ndjson") {
		t.Fatalf("bad download headers: %v", writer.Header())
	}
	scanner := bufio.NewScanner(strings.NewReader(writer.Body.String()))
	count := 0
	for scanner.Scan() {
		var row activityExportRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil || row.Kind != "exchange" || row.Exchange == nil || row.MCPOperation != nil {
			t.Fatalf("untyped export record: %s (%v)", scanner.Bytes(), err)
		}
		count++
	}
	if err := scanner.Err(); err != nil || count != 201 {
		t.Fatalf("rows=%d err=%v", count, err)
	}
}

func decodeExportTestExchange(t *testing.T, cursor string) string {
	t.Helper()
	_, id, err := ledger.DecodeActivityCursor(cursor, "engineering", "exchange")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestActivityExportBoundedSinglePageAndResume(t *testing.T) {
	next := exportTestCursor(t, "engineering", "mcp_operation")
	calls := 0
	handler := NewActivityExportHandler(func(_ *http.Request, scope evidence.ProjectionScope, page ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
		calls++
		result := evidence.ActivityProjection{Scope: scope, Page: &ledger.ActivityPage{Mode: "retained", Limit: page.Limit}}
		result.MCPOperations = []evidence.MCPOperation{{OperationID: "mcp_one", Workspace: "engineering"}}
		if calls == 1 {
			result.Page.MCPNext = next
		} else if !page.MCP.Valid() {
			t.Fatal("MCP continuation was not decoded")
		}
		return result, nil
	})
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/activity/export?workspace=engineering&kind=mcp_operation&max_rows=1", nil))
	if writer.Code != http.StatusOK || calls != 1 || writer.Header().Get("X-Swobu-Next-Cursor") != next {
		t.Fatalf("single page lost next cursor: %d %v", writer.Code, writer.Header())
	}
	writer = httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/activity/export?workspace=engineering&kind=mcp_operation&cursor="+url.QueryEscape(next)+"&max_rows=1", nil))
	if writer.Code != http.StatusOK || calls != 2 || writer.Header().Get("X-Swobu-Next-Cursor") != "" {
		t.Fatalf("MCP resume failed: %d %v", writer.Code, writer.Header())
	}
}

func TestActivityExportCSVIsSpreadsheetSafeAndWellTyped(t *testing.T) {
	handler := NewActivityExportHandler(func(_ *http.Request, scope evidence.ProjectionScope, _ ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
		return evidence.ActivityProjection{Scope: scope, Page: &ledger.ActivityPage{Mode: "retained"}, Exchanges: []evidence.ActivityExchange{
			{ExchangeID: "ex_safe", Provider: "  =HYPERLINK(\"https://evil.example\")", Workspace: "research", State: "failed", HTTPStatus: 429},
		}}, nil
	})
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/activity/export?kind=exchange&format=csv", nil))
	if writer.Code != http.StatusOK || writer.Header().Get("Content-Type") != "text/csv; charset=utf-8" {
		t.Fatalf("bad CSV export: %d %s", writer.Code, writer.Body.String())
	}
	rows, err := csv.NewReader(strings.NewReader(writer.Body.String())).ReadAll()
	if err != nil || len(rows) != 2 || len(rows[0]) != len(rows[1]) || rows[1][10] != "'  =HYPERLINK(\"https://evil.example\")" || rows[1][14] != "429" {
		t.Fatalf("CSV compromised: rows=%v err=%v", rows, err)
	}
	for _, dangerous := range []string{"=SUM(1)", "+cmd", "-1+2", "@SUM(1)", " \t@a", "\n=cmd"} {
		if !strings.HasPrefix(safeActivityCSVCell(dangerous), "'") {
			t.Fatalf("CSV formula escape missing for %q", dangerous)
		}
	}
}

func TestActivityExportRejectsInvalidBeforeReading(t *testing.T) {
	otherScopeCursor := exportTestCursor(t, "other", "exchange")
	for _, query := range []string{
		"", "kind=", "kind=models", "kind=exchange&kind=mcp_operation", "kind=exchange&format=xml", "kind=exchange&format=",
		"kind=exchange&max_rows=0", "kind=exchange&max_rows=2001", "kind=exchange&max_rows=01", "kind=exchange&max_rows=-1",
		"kind=exchange&admin=true", "kind=mcp_operation&principal=local", "kind=exchange&principal=nonexistent", "kind=exchange&result=ok",
		"kind=exchange&from=not-time", "kind=exchange&to=0001-01-01T00:00:00Z", "kind=exchange&from=0001-01-01T00:00:00Z", "kind=exchange&from=2026-10-21T00:00:00Z&to=2026-10-20T00:00:00Z",
		"kind=exchange&workspace=Bad", "kind=exchange&workspace=engineering&workspace=other",
		"kind=exchange&workspace=engineering&cursor=" + url.QueryEscape(otherScopeCursor),
		"kind=mcp_operation&cursor=" + url.QueryEscape(otherScopeCursor), "kind=exchange&cursor=",
	} {
		t.Run(query, func(t *testing.T) {
			calls := 0
			handler := NewActivityExportHandler(func(*http.Request, evidence.ProjectionScope, ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
				calls++
				return evidence.ActivityProjection{}, nil
			})
			writer := httptest.NewRecorder()
			handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/activity/export?"+query, nil))
			if writer.Code != http.StatusBadRequest || calls != 0 {
				t.Fatalf("invalid export query reached Ledger: %d calls=%d", writer.Code, calls)
			}
		})
	}
}

func TestActivityExportFailsClosedAndBoundsResources(t *testing.T) {
	secrets := "sensitive-backend-path"
	handler := NewActivityExportHandler(func(_ *http.Request, scope evidence.ProjectionScope, _ ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
		return evidence.ActivityProjection{}, errors.New(secrets)
	})
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/activity/export?kind=exchange", nil))
	if writer.Code != http.StatusServiceUnavailable || strings.Contains(writer.Body.String(), secrets) || writer.Header().Get("Content-Disposition") != "" {
		t.Fatalf("backend failure acknowledged/downloaded: %d %s", writer.Code, writer.Body.String())
	}
	// Export concurrency has a fixed cap; a busy server must not spawn an
	// unbounded number of expensive retained Ledger reads.
	for i := 0; i < cap(handler.slots); i++ {
		handler.slots <- struct{}{}
	}
	writer = httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/activity/export?kind=exchange", nil))
	if writer.Code != http.StatusServiceUnavailable {
		t.Fatalf("capacity exhaustion admitted: %d", writer.Code)
	}
	oversize := NewActivityExportHandler(func(_ *http.Request, scope evidence.ProjectionScope, _ ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
		return evidence.ActivityProjection{Scope: scope, Page: &ledger.ActivityPage{Mode: "retained"}, Exchanges: []evidence.ActivityExchange{{Provider: strings.Repeat("a", activityExportMaxBytes)}}}, nil
	})
	writer = httptest.NewRecorder()
	oversize.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/activity/export?kind=exchange", nil))
	if writer.Code != http.StatusRequestEntityTooLarge || writer.Header().Get("Content-Disposition") != "" {
		t.Fatalf("oversized export returned success: %d %v", writer.Code, writer.Header())
	}
}

func TestActivityRetainedExchangeFiltersArePushedToLedger(t *testing.T) {
	from := "2026-10-20T12:00:00Z"
	to := "2026-10-21T12:00:00Z"
	for _, export := range []bool{false, true} {
		called := 0
		read := func(_ *http.Request, scope evidence.ProjectionScope, page ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
			called++
			filter := page.ExchangeFilter
			if filter.Access == nil || filter.Access.String() != "local" || filter.DeliveryResult == nil || *filter.DeliveryResult != delivery.Succeeded || filter.Settled.Start.IsZero() || filter.Settled.End.IsZero() || filter.Settled.End.Sub(filter.Settled.Start) != 24*time.Hour {
				t.Fatalf("filters ignored: %+v", filter)
			}
			return evidence.ActivityProjection{Scope: scope, Page: &ledger.ActivityPage{Mode: "retained"}, Exchanges: make([]evidence.ActivityExchange, 0)}, nil
		}
		uri := "/api/v1/activity?kind=exchange&limit=50"
		var handler http.Handler = NewActivityHandler(read)
		if export {
			uri = "/api/v1/activity/export?kind=exchange"
			handler = NewActivityExportHandler(read)
		}
		uri += "&principal=local&result=succeeded&from=" + url.QueryEscape(from) + "&to=" + url.QueryEscape(to)
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, uri, nil))
		if writer.Code != http.StatusOK || called != 1 {
			t.Fatalf("filter transport failed export=%v status=%d called=%d: %s", export, writer.Code, called, writer.Body.String())
		}
	}
	for _, suffix := range []string{
		"kind=exchange&principal=local",                // Live observation doesn't support retained filtering.
		"kind=mcp_operation&limit=50&result=succeeded", // MCP kind not silently filtered.
	} {
		handler := NewActivityHandler(func(*http.Request, evidence.ProjectionScope, ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
			t.Fatal("rejected Activity filter reached Ledger")
			return evidence.ActivityProjection{}, nil
		})
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/activity?"+suffix, nil))
		if writer.Code != http.StatusBadRequest {
			t.Fatalf("unsupported filter accepted: %s -> %d", suffix, writer.Code)
		}
	}
}

func TestActivityExportConcurrentSessionsAndCancellation(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	handler := NewActivityExportHandler(func(req *http.Request, scope evidence.ProjectionScope, page ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
		deadline, ok := req.Context().Deadline()
		if !ok || time.Until(deadline) > 30*time.Second {
			return evidence.ActivityProjection{}, errors.New("missing export deadline")
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-req.Context().Done():
			return evidence.ActivityProjection{}, req.Context().Err()
		}
		return evidence.ActivityProjection{Scope: scope, Page: &ledger.ActivityPage{Mode: "retained"}}, nil
	})
	finished := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/activity/export?kind=exchange", nil))
			finished <- rec.Code
		}()
	}
	<-entered
	<-entered
	third := httptest.NewRecorder()
	handler.ServeHTTP(third, httptest.NewRequest(http.MethodGet, "/api/v1/activity/export?kind=exchange", nil))
	if third.Code != http.StatusServiceUnavailable {
		t.Fatalf("concurrent third scan admitted: %d", third.Code)
	}
	close(release)
	for i := 0; i < 2; i++ {
		if status := <-finished; status != http.StatusOK {
			t.Fatalf("admitted export failed: %d", status)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	canceled := httptest.NewRecorder()
	handler.ServeHTTP(canceled, httptest.NewRequest(http.MethodGet, "/api/v1/activity/export?kind=exchange", nil).WithContext(ctx))
	if canceled.Code != http.StatusServiceUnavailable || canceled.Header().Get("Content-Disposition") != "" {
		t.Fatal("canceled export acknowledged success")
	}
}

func TestActivityExportDiscardsAlreadyRenderedRowsOnLaterReadFailure(t *testing.T) {
	calls := 0
	next := exportTestCursor(t, "", "exchange")
	handler := NewActivityExportHandler(func(_ *http.Request, scope evidence.ProjectionScope, page ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
		calls++
		if calls == 2 {
			return evidence.ActivityProjection{}, errors.New("private-storage-error")
		}
		return evidence.ActivityProjection{Scope: scope, Page: &ledger.ActivityPage{Mode: "retained", ExchangeNext: next}, Exchanges: []evidence.ActivityExchange{{ExchangeID: "ex_partial"}}}, nil
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/activity/export?kind=exchange", nil))
	if calls != 2 || rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "ex_partial") || strings.Contains(rec.Body.String(), "private-storage-error") || rec.Header().Get("Content-Disposition") != "" {
		t.Fatal("failed continuation leaked a partial successful download")
	}
}

func TestActivityExportDeadlineInterruptsRetainedRead(t *testing.T) {
	handler := NewActivityExportHandler(func(req *http.Request, _ evidence.ProjectionScope, _ ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
		<-req.Context().Done()
		return evidence.ActivityProjection{}, req.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/activity/export?kind=exchange", nil).WithContext(ctx))
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Content-Disposition") != "" || rec.Header().Get("X-Swobu-Export-Mode") != "" {
		t.Fatalf("expired retained read produced a download: %d %v", rec.Code, rec.Header())
	}
}
