package bootstrap

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	evidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
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

func TestCanonicalRetainedActivityPagesSurviveRestartWithoutLiveRows(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "sqlite")
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	input := StartInput{ConfigPath: filepath.Join(root, "swobu.yaml"), StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}}
	daemon, err := Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	workspace, _ := routing.ParseWorkspaceSlug("research")
	settled := time.Now().UTC()
	var exchangeIDs []string
	for index := 0; index < 3; index++ {
		identity, err := execution.NewExchangeID()
		if err != nil {
			t.Fatal(err)
		}
		end := settled.Add(-time.Duration(index) * time.Second)
		start, err := domain.NewExchangeStarted(identity, accessidentity.Local, workspace, settled.Add(-time.Duration(10-index)*time.Second), clientfamily.Codex, protocolkind.Responses, canonical.NormalizedPathResponses)
		if err != nil {
			t.Fatal(err)
		}
		finish, err := domain.NewExchangeSettled(identity, end, delivery.Succeeded, 200, "")
		if err != nil {
			t.Fatal(err)
		}
		completed, err := domain.NewCompletedExchange(start, nil, finish)
		if err != nil {
			t.Fatal(err)
		}
		if err := daemon.ledgerStore.WriteBatch(t.Context(), []domain.CompletedExchange{completed}); err != nil {
			t.Fatal(err)
		}
		exchangeIDs = append(exchangeIDs, identity.String())
	}
	for _, identity := range []string{"mcp_a", "mcp_z", "mcp_A"} {
		operation, err := domain.NewCompletedMCPOperation(identity, accessidentity.Local, workspace, "docs", "write", settled.Add(-time.Second), settled, execution.Failed, execution.MayHaveOccurred)
		if err != nil {
			t.Fatal(err)
		}
		if err := daemon.ledgerStore.WriteMCPOperations(t.Context(), []domain.CompletedMCPOperation{operation}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := daemon.executionEvidenceStore.BeginMCP(accessidentity.Local, workspace, "docs", "live"); err != nil {
		t.Fatal(err)
	}
	api := operatorclient.New(http.DefaultClient, daemon.BaseURL())
	activeOperations := daemon.executionEvidenceStore.ProjectActivity(evidence.ProjectionInput{}).MCPOperations
	if len(activeOperations) != 1 {
		t.Fatal("live detail fixture unavailable")
	}
	activeDetail, err := api.ActivityOperation(t.Context(), activeOperations[0].OperationID)
	if err != nil || activeDetail.Evidence != "memory" || activeDetail.MCP == nil || activeDetail.MCP.Outcome != "" || activeDetail.MCP.SettledAt != "" {
		t.Fatal("live detail invented settlement", err)
	}
	exchanges, err := api.ActivityPage(t.Context(), "research", operatorclient.ActivityPageRequest{Limit: 2, Kind: "exchange"})
	if err != nil || len(exchanges.Exchanges) != 2 || exchanges.Exchanges[0].ExchangeID != exchangeIDs[0] || exchanges.Exchanges[1].ExchangeID != exchangeIDs[1] || exchanges.Page.ExchangeNext == "" {
		t.Fatalf("Exchange page reordered by start time: %#v, %v", exchanges, err)
	}
	first, err := api.ActivityPage(t.Context(), "research", operatorclient.ActivityPageRequest{Limit: 1, Kind: "mcp_operation"})
	if err != nil || first.Page == nil || first.Page.MCPNext == "" || len(first.MCPOperations) != 1 || first.MCPOperations[0].OperationID != "mcp_z" || first.History != nil {
		t.Fatalf("first retained page: %#v, %v", first, err)
	}
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	api = operatorclient.New(http.DefaultClient, restarted.BaseURL())
	missing, err := http.Get(restarted.BaseURL() + "/api/v1/activity/operations/mcp_missing")
	if err != nil {
		t.Fatal(err)
	}
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatal("missing detail fabricated execution truth")
	}
	for _, identity := range []string{exchangeIDs[0], "mcp_a"} {
		detail, found, err := restarted.activityDetail(t.Context(), identity)
		if err != nil || !found || detail.Evidence != "retained" {
			t.Fatalf("exact detail after restart: %#v, %v, %v", detail, found, err)
		}
		if identity == "mcp_a" && (detail.Exchange != nil || detail.MCP == nil || detail.MCP.Occurrence != execution.MayHaveOccurred) {
			t.Fatal("standalone detail invented Exchange or lost uncertainty")
		}
		typed, err := api.ActivityOperation(t.Context(), identity)
		if err != nil || typed.Evidence != "retained" || typed.Kind != detail.Kind {
			t.Fatal("typed detail disagrees with exact owner", err)
		}
		response, err := http.Get(restarted.BaseURL() + "/api/v1/activity/operations/" + identity)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatal("canonical detail is not mounted")
		}
	}
	lastExchange, err := api.ActivityPage(t.Context(), "research", operatorclient.ActivityPageRequest{Limit: 2, Kind: "exchange", ExchangeCursor: exchanges.Page.ExchangeNext})
	if err != nil || len(lastExchange.Exchanges) != 1 || lastExchange.Exchanges[0].ExchangeID != exchangeIDs[2] || lastExchange.Page.ExchangeNext != "" {
		t.Fatalf("Exchange restart continuation: %#v, %v", lastExchange, err)
	}
	page, err := api.ActivityPage(t.Context(), "research", operatorclient.ActivityPageRequest{Limit: 2, Kind: "mcp_operation", MCPCursor: first.Page.MCPNext})
	if err != nil || len(page.MCPOperations) != 2 || page.MCPOperations[0].OperationID != "mcp_a" || page.MCPOperations[1].OperationID != "mcp_A" || page.Page.MCPNext != "" || page.MCPOperations[0].Occurrence != execution.MayHaveOccurred || len(page.Exchanges) != 0 {
		t.Fatalf("restart continuation: %#v, %v", page, err)
	}
	all, err := api.ActivityPage(t.Context(), "", operatorclient.ActivityPageRequest{Limit: 2})
	if err != nil || all.History == nil || all.Page == nil || all.Page.Mode != "retained" || len(all.MCPOperations) != 2 {
		t.Fatalf("Node page capture qualifiers: %#v, %v", all, err)
	}
	for _, query := range []string{"limit=1&mcp_cursor=" + first.Page.MCPNext, "workspace=research&limit=1&kind=exchange&mcp_cursor=" + first.Page.MCPNext, "workspace=research&limit=1&mcp_cursor=invalid"} {
		typedContext, typedCancel := context.WithTimeout(t.Context(), 2*time.Second)
		typedStream, err := api.OpenActivityStream(typedContext, "research", "mcp_operation")
		if err != nil {
			typedCancel()
			t.Fatal(err)
		}
		observation, err := typedStream.Next(typedContext)
		typedStream.Close()
		typedCancel()
		if err != nil || len(observation.MCPOperations) != 3 || len(observation.Exchanges) != 0 || observation.History != nil || observation.MCPOperations[0].Occurrence != execution.MayHaveOccurred {
			t.Fatal("typed real-Node stream lost scope/uncertainty", err)
		}
		streamContext, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		request, _ := http.NewRequestWithContext(streamContext, http.MethodGet, restarted.BaseURL()+"/api/v1/activity/stream?workspace=research&kind=mcp_operation", nil)
		stream, err := http.DefaultClient.Do(request)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		reader := bufio.NewReader(stream.Body)
		event, err := reader.ReadString('\n')
		if err != nil || event != "event: activity\n" {
			cancel()
			stream.Body.Close()
			t.Fatal("canonical stream mount missing", err)
		}
		data, err := reader.ReadString('\n')
		cancel()
		stream.Body.Close()
		if err != nil || !strings.Contains(data, "mcp_a") || !strings.Contains(data, `"complete":false`) || strings.Contains(data, "exchange_id") || strings.Contains(data, `"history"`) {
			t.Fatal("real stream lost scoped standalone observation")
		}
		response, err := http.Get(restarted.BaseURL() + "/api/v1/activity?" + query)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatal("invalid retained cursor binding admitted")
		}
	}
	if _, _, err := ledger.DecodeActivityCursor(first.Page.MCPNext, "coding", "mcp_operation"); err == nil {
		t.Fatal("cursor changed Workspace scope")
	}
}

func TestCanonicalRetainedActivityCannotSubstituteDisabledLedger(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "off")
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(t.Context(), StartInput{ConfigPath: filepath.Join(root, "swobu.yaml"), StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	response, err := http.Get(daemon.BaseURL() + "/api/v1/activity?limit=1")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatal("disabled Ledger fabricated retained page")
	}
	export := activityExportGET(t, daemon.BaseURL()+"/api/v1/activity/export?kind=exchange", http.StatusServiceUnavailable)
	defer export.Body.Close()
	if export.Header.Get("Content-Disposition") != "" {
		t.Fatal("disabled Ledger acknowledged an empty export")
	}
}
