package bootstrap

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	evidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/execution"
	domain "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

// This assembled proof compares HTTP filtering and export continuation with
// direct SQLite truth, without substituting live memory or transport fixtures.
func TestActivityExportMatchesRetainedSQLiteAndLocalAdmission(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "sqlite")
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "swobu.yaml")
	seedTestWorkspaces(t, path, "research", "other")
	daemon, err := Start(t.Context(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	research, _ := routing.ParseWorkspaceSlug("research")
	other, _ := routing.ParseWorkspaceSlug("other")
	now := time.Now().UTC().Truncate(time.Second)
	managed := accessidentity.NewManaged([16]byte{1})
	var completed []domain.CompletedExchange
	for i := 0; i < 610; i++ {
		id, err := execution.NewExchangeID()
		if err != nil {
			t.Fatal(err)
		}
		workspace, principal, result, status := research, accessidentity.Local, delivery.Succeeded, 200
		if i%10 == 0 {
			workspace = other
		}
		if i%2 == 1 {
			principal = managed
			result = delivery.ExchangeFailed
			status = 502
		}
		settled := now.Add(-time.Duration(i) * time.Second)
		started, err := domain.NewExchangeStarted(id, principal, workspace, settled.Add(-time.Second), clientfamily.Codex, protocolkind.Responses, canonical.NormalizedPathResponses)
		if err != nil {
			t.Fatal(err)
		}
		finish, err := domain.NewExchangeSettled(id, settled, result, status, "")
		if err != nil {
			t.Fatal(err)
		}
		exchange, err := domain.NewCompletedExchange(started, nil, finish)
		if err != nil {
			t.Fatal(err)
		}
		completed = append(completed, exchange)
	}
	if err := daemon.ledgerStore.WriteBatch(t.Context(), completed); err != nil {
		t.Fatal(err)
	}
	var mcp []domain.CompletedMCPOperation
	for i := 0; i < 206; i++ {
		workspace := research
		if i == 205 {
			workspace = other
		}
		settled := now.Add(-time.Duration(i) * time.Second)
		operation, err := domain.NewCompletedMCPOperation(fmt.Sprintf("mcp_export_%03d", i), accessidentity.Local, workspace, "@catalog,\"quoted\"", "=SUM(1)\nsecond", settled.Add(-time.Second), settled, execution.Succeeded, execution.Occurred)
		if err != nil {
			t.Fatal(err)
		}
		mcp = append(mcp, operation)
	}
	if err := daemon.ledgerStore.WriteMCPOperations(t.Context(), mcp); err != nil {
		t.Fatal(err)
	}
	from, to := now.Add(-600*time.Second), now.Add(time.Second)
	success := delivery.Succeeded
	filter := ledger.ExchangeFilter{Workspace: &research, Access: &accessidentity.Local, DeliveryResult: &success, Settled: ledger.TimeRange{Start: from, End: to}}
	var expected []string
	var cursor ledger.Cursor
	for {
		page, err := daemon.ledgerStore.ListRecentSummaries(t.Context(), filter, cursor, ledger.MaximumPageSize)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range page.Exchanges {
			expected = append(expected, row.ID.String())
		}
		if !page.Next.Valid() {
			break
		}
		cursor = page.Next
	}
	if len(expected) <= 201 {
		t.Fatalf("fixture did not cross internal and caller page boundaries: %d", len(expected))
	}
	values := url.Values{"workspace": {"research"}, "kind": {"exchange"}, "principal": {"local"}, "result": {"succeeded"}, "from": {from.Format(time.RFC3339Nano)}, "to": {to.Format(time.RFC3339Nano)}}
	list := url.Values{}
	for key, entries := range values {
		list[key] = slices.Clone(entries)
	}
	list.Set("limit", "50")
	response := activityExportGET(t, daemon.BaseURL()+"/api/v1/activity?"+list.Encode(), http.StatusOK)
	var projection evidence.ActivityProjection
	if err := json.NewDecoder(response.Body).Decode(&projection); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	var listed []string
	for _, row := range projection.Exchanges {
		listed = append(listed, row.ExchangeID)
	}
	if !slices.Equal(listed, expected[:50]) {
		t.Fatal("HTTP filter differs from direct Ledger truth")
	}
	values.Set("max_rows", "201")
	var actual []string
	for {
		response = activityExportGET(t, daemon.BaseURL()+"/api/v1/activity/export?"+values.Encode(), http.StatusOK)
		decoder := json.NewDecoder(response.Body)
		count := 0
		for {
			var row struct {
				Kind     string                    `json:"kind"`
				Exchange evidence.ActivityExchange `json:"exchange"`
				MCP      json.RawMessage           `json:"mcp_operation"`
			}
			err := decoder.Decode(&row)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if row.Kind != "exchange" || row.Exchange.Workspace != "research" || row.Exchange.AccessID != "local" || row.MCP != nil {
				t.Fatal("export crossed evidence kind/scope/Principal")
			}
			actual = append(actual, row.Exchange.ExchangeID)
			count++
		}
		response.Body.Close()
		if response.Header.Get("X-Swobu-Export-Rows") != fmt.Sprint(count) || response.Header.Get("X-Swobu-Export-Mode") != "retained-page" {
			t.Fatal("export metadata disagrees with body")
		}
		next := response.Header.Get("X-Swobu-Next-Cursor")
		if next == "" {
			break
		}
		values.Set("cursor", next)
	}
	if !slices.Equal(actual, expected) {
		t.Fatal("export omitted, duplicated, reordered or included filtered records")
	}
	response = activityExportGET(t, daemon.BaseURL()+"/api/v1/activity/export?workspace=research&kind=mcp_operation&format=csv&max_rows=201", http.StatusOK)
	rows, err := csv.NewReader(response.Body).ReadAll()
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 202 || rows[1][12] != "'@catalog,\"quoted\"" || rows[1][13] != "'=SUM(1)\nsecond" {
		t.Fatal("real retained MCP CSV lost quoting or formula protection")
	}
	next := response.Header.Get("X-Swobu-Next-Cursor")
	if next == "" {
		t.Fatal("MCP continuation missing")
	}
	response = activityExportGET(t, daemon.BaseURL()+"/api/v1/activity/export?workspace=research&kind=mcp_operation&format=csv&max_rows=201&cursor="+url.QueryEscape(next), http.StatusOK)
	tail, err := csv.NewReader(response.Body).ReadAll()
	response.Body.Close()
	if err != nil || len(tail) != 5 || response.Header.Get("X-Swobu-Next-Cursor") != "" {
		t.Fatalf("MCP continuation rows=%d err=%v", len(tail), err)
	}
	for _, suffix := range []string{"kind=mcp_operation&principal=local", "kind=exchange&result=unsupported"} {
		r := activityExportGET(t, daemon.BaseURL()+"/api/v1/activity/export?"+suffix, http.StatusBadRequest)
		r.Body.Close()
	}
	_, issued, err := daemon.accessStore.Create("export-consumer", research)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, key, value string }{
		{"Workspace bearer", "Authorization", "Bearer " + issued.Token},
		{"external origin", "Origin", "https://attacker.example"},
		{"forged browser session", "Cookie", "swobu_operator_session=" + strings.Repeat("x", 43)},
		{"browser without session", "User-Agent", "Mozilla/5.0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, daemon.BaseURL()+"/api/v1/activity/export?kind=exchange", nil)
			req.Header.Set(test.key, test.value)
			if test.name == "forged browser session" {
				req.Header.Set("User-Agent", "Mozilla/5.0")
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != http.StatusForbidden && res.StatusCode != http.StatusUnauthorized {
				t.Fatalf("export admission allowed %s: %d", test.name, res.StatusCode)
			}
			if res.Header.Get("Content-Disposition") != "" {
				t.Fatal("denied request received download")
			}
		})
	}
	for _, credential := range []string{"", "Bearer " + issued.Token} {
		req := httptest.NewRequest(http.MethodGet, "https://public.example/api/v1/activity/export?kind=exchange", nil)
		if credential != "" {
			req.Header.Set("Authorization", credential)
		}
		rec := httptest.NewRecorder()
		daemon.publicHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusUnauthorized {
			t.Fatalf("public ingress exposed export: %d", rec.Code)
		}
	}
}

func activityExportGET(t *testing.T, endpoint string, want int) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != want {
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("Activity status=%d want=%d: %s", response.StatusCode, want, body)
	}
	return response
}
