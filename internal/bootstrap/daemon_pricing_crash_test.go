package bootstrap

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
	modernsqlite "modernc.org/sqlite"
)

const (
	pricingCrashConfigEnv = "SWOBU_PRICING_CRASH_CONFIG"
	pricingCrashTokenEnv  = "SWOBU_PRICING_CRASH_TOKEN"
)

func TestDaemonProviderSendCrashHelper(t *testing.T) {
	configPath := os.Getenv(pricingCrashConfigEnv)
	if configPath == "" {
		return
	}
	daemon, err := Start(t.Context(), StartInput{
		ConfigPath:    configPath,
		StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	payload := `{"model":"chat","input":"crash after provider receives request","stream":false}`
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, daemon.BaseURL()+"/workspaces/coding/v1/responses", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+os.Getenv(pricingCrashTokenEnv))
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
}

func TestDaemonProcessDeathAfterProviderSendKeepsPricedReservation(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "off")
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	var providerCalls atomic.Int32
	providerReceived := make(chan struct{}, 1)
	providerRelease := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		providerCalls.Add(1)
		select {
		case providerReceived <- struct{}{}:
		default:
		}
		select {
		case <-providerRelease:
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"id":"provider-response","model":"fixture","output":[],"usage":{"input_tokens":10000,"output_tokens":10000}}`)
		case <-request.Context().Done():
		}
	}))
	defer upstream.Close()
	defer close(providerRelease)

	databasePath := filepath.Join(root, "models.sqlite")
	connector, err := modernsqlite.NewConnector(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	database := sql.OpenDB(connector)
	defer database.Close()
	_, err = database.Exec(`CREATE TABLE provider_models(id INTEGER PRIMARY KEY,provider_id TEXT,external_id TEXT,external_version TEXT);
CREATE TABLE tariffs(id INTEGER PRIMARY KEY,provider_model_id INTEGER,name TEXT);
CREATE TABLE tariff_prices(tariff_id INTEGER,effective_at INTEGER,min_input_tokens INTEGER,currency TEXT,input_per_million TEXT,output_per_million TEXT,cache_read_per_million TEXT,cache_write_per_million TEXT,reasoning_per_million TEXT);
INSERT INTO provider_models VALUES(1,'custom','fixture','');
INSERT INTO tariffs VALUES(1,1,'standard');
INSERT INTO tariff_prices VALUES(1,100,0,'USD','1','2',NULL,NULL,NULL);`)
	if err != nil {
		t.Fatal(err)
	}

	pricingPath := filepath.Join(root, "pricing.json")
	pricing, err := json.Marshal(struct {
		Database string           `json:"models_database"`
		Bindings []pricingBinding `json:"bindings"`
	}{
		Database: databasePath,
		Bindings: []pricingBinding{{
			TargetID: "primary", TargetVersion: 1, Provider: "custom", Model: "fixture",
			BaseURL: upstream.URL + "/v1", Tariff: "standard", InputForecast: 10000, OutputForecast: 10000,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pricingPath, pricing, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SWOBU_PRICING_CONFIG", pricingPath)

	configPath := filepath.Join(root, "swobu.yaml")
	writeCurrentWorkspaceFixture(t, configPath, root, "coding", upstream.URL, "responses", false)
	node, err := nodefile.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	access, err := accessstate.OpenInline(nil, node.PublishPrincipals)
	if err != nil {
		t.Fatal(err)
	}
	coding, err := routing.ParseWorkspaceSlug("coding")
	if err != nil {
		t.Fatal(err)
	}
	_, credential, err := access.Create("crash-fixture", coding)
	if err != nil {
		t.Fatal(err)
	}
	accountingPath := filepath.Join(root, "accounting.json")
	accountingStore, err := accounting.OpenPolicyInline(accountingPath, nil, node.PublishAccounting)
	if err != nil {
		t.Fatal(err)
	}
	if err := accountingStore.SetLimit("coding", accounting.Limit{
		Currency: "USD", AmountCents: 3, Window: "day", Mode: accounting.ModeStopNewBillableWork, UnknownPrice: "reject",
	}); err != nil {
		t.Fatal(err)
	}

	child := exec.Command(os.Args[0], "-test.run=^TestDaemonProviderSendCrashHelper$", "-test.count=1")
	child.Env = append(os.Environ(), pricingCrashConfigEnv+"="+configPath, pricingCrashTokenEnv+"="+credential.Token)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if child.ProcessState == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	select {
	case <-providerReceived:
	case <-time.After(30 * time.Second):
		t.Fatal("provider did not receive the child daemon request")
	}
	if providerCalls.Load() != 1 {
		t.Fatalf("provider calls before process death=%d, want 1", providerCalls.Load())
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("daemon child exited normally before provider settlement")
	}

	inlinePolicy, err := node.AccountingDocument()
	if err != nil {
		t.Fatal(err)
	}
	reopenedAccounting, err := accounting.OpenPolicyInline(accountingPath, inlinePolicy, node.PublishAccounting)
	if err != nil {
		t.Fatal(err)
	}
	reservations := reopenedAccounting.Reservations("coding")
	if len(reservations) != 1 || reservations[0].State != "reserved" || reservations[0].AmountCents != 3 || reservations[0].Coverage != accounting.CoverageKnown {
		t.Fatalf("reservation after provider-send process death=%#v", reservations)
	}
	if quote := reservations[0].Quote; quote == nil || quote.TargetID != "primary" || quote.Price.ExternalID != "fixture" || quote.QuantityCoverage != "partial" {
		t.Fatalf("crash recovery lost the target-pinned partial quote: %#v", quote)
	}

	restarted, err := Start(t.Context(), StartInput{
		ConfigPath:    configPath,
		StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, restarted.BaseURL()+"/workspaces/coding/v1/responses", strings.NewReader(`{"model":"chat","input":"must remain denied","stream":false}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusPaymentRequired || !strings.Contains(string(body), "BUDGET_EXCEEDED") {
		t.Fatalf("restart admission status=%d body=%q err=%v", response.StatusCode, body, readErr)
	}
	if providerCalls.Load() != 1 {
		t.Fatalf("restart redispatched an already in-doubt provider effect: calls=%d", providerCalls.Load())
	}
	if got := restarted.accountingStore.Reservations("coding"); len(got) != 1 || got[0].State != "reserved" {
		t.Fatalf("restart changed the in-doubt reservation: %#v", got)
	}
	if err := restarted.Close(); err != nil {
		t.Fatal(err)
	}
}
