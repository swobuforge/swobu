package bootstrap

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/accounting"
	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/app/operator/adminapproval"
	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/platform/config"
	modernsqlite "modernc.org/sqlite"
)

func TestDaemonPublishedTariffForecastStopsWorkspaceAcrossPrincipalsAndRestart(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "off")
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"id":"provider-response","model":"fixture","output":[],"usage":{"input_tokens":10000,"output_tokens":10000,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}`)
	}))
	defer upstream.Close()
	var secondaryCalls atomic.Int32
	secondary := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		secondaryCalls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		// The provider completes without usage. Accounting must retain its
		// pre-dispatch estimate and mark the result uncertain, never as zero.
		_, _ = io.WriteString(writer, `{"id":"secondary-response","model":"secondary-model","output":[]}`)
	}))
	defer secondary.Close()
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
INSERT INTO provider_models VALUES(1,'custom','fixture',''),(2,'custom','secondary-model','');
INSERT INTO tariffs VALUES(1,1,'standard'),(2,2,'regional');
INSERT INTO tariff_prices VALUES(1,100,0,'USD','1','2',NULL,NULL,NULL),(2,100,0,'USD','2','3',NULL,NULL,NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	pricingPath := filepath.Join(root, "pricing.json")
	settings := struct {
		Database string           `json:"models_database"`
		Bindings []pricingBinding `json:"bindings"`
	}{
		Database: databasePath, Bindings: []pricingBinding{
			{TargetID: "primary", TargetVersion: 1, Provider: "custom", Model: "fixture", BaseURL: upstream.URL + "/v1", Tariff: "standard", InputForecast: 10000, OutputForecast: 10000},
			{TargetID: "secondary", TargetVersion: 1, Provider: "custom", Model: "secondary-model", BaseURL: secondary.URL + "/v1", Tariff: "regional", InputForecast: 10000, OutputForecast: 10000},
		},
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pricingPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SWOBU_PRICING_CONFIG", pricingPath)
	configPath := filepath.Join(root, "swobu.yaml")
	writeCurrentTwoRouteFixture(t, configPath, root, "coding", upstream.URL, secondary.URL)
	start := func() *Daemon {
		t.Helper()
		daemon, err := Start(t.Context(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = daemon.Close() })
		return daemon
	}
	daemon := start()
	control := operatorclient.New(http.DefaultClient, daemon.BaseURL())
	controlJSON := func(method, path string, payload any, out any) {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(t.Context(), method, daemon.BaseURL()+path, strings.NewReader(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal("Control request failed")
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("Control %s status=%d", path, response.StatusCode)
		}
		if out != nil {
			if err := json.NewDecoder(response.Body).Decode(out); err != nil {
				t.Fatal(err)
			}
		}
	}
	limit := accounting.Limit{Currency: "USD", AmountCents: 8, Window: "day", Mode: accounting.ModeStopNewBillableWork, UnknownPrice: "reject"}
	var approval adminapproval.Approval
	controlJSON(http.MethodPost, "/api/v1/workspaces/coding/limits/preview", limit, &approval)
	if approval.Workspace != "coding" || approval.Operation != "limits.set" || approval.Digest == "" || approval.Revision == "" {
		t.Fatal("limit preview lost reviewed scope")
	}
	controlJSON(http.MethodPost, "/api/v1/workspaces/coding/limits/apply", struct {
		ApprovalID string           `json:"approval_id"`
		Limit      accounting.Limit `json:"limit"`
	}{approval.ID, limit}, nil)
	alice, _, err := control.CreatePrincipal(t.Context(), "Alice", "pricing-alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, _, err := control.CreatePrincipal(t.Context(), "Bob", "pricing-bob")
	if err != nil || alice.ID == bob.ID {
		t.Fatal("independent identities missing")
	}
	issue := func(principal, key string) accessapp.IssueResult {
		t.Helper()
		intent, err := operatorclient.RestoreCredentialIssuanceIntent(principal, "coding", key)
		if err != nil {
			t.Fatal(err)
		}
		result, err := control.ApplyCredentialIssuance(t.Context(), intent)
		if err != nil {
			t.Fatal("Control issuance failed")
		}
		return result
	}
	for _, principal := range []string{alice.ID, bob.ID} {
		preview, err := control.PreviewWorkspacePermission(t.Context(), "coding", principal, true, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		review, err := operatorclient.ReviewPermission(preview, "pricing-grant-"+principal)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := control.ApplyWorkspacePermission(t.Context(), review); err != nil {
			t.Fatal(err)
		}
	}
	aliceKey := issue(alice.ID, "pricing-alice-key")
	bobKey := issue(bob.ID, "pricing-bob-key")
	request := func(token, route string, want int) {
		t.Helper()
		payload, err := json.Marshal(map[string]any{"model": route, "input": "hello", "stream": false})
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, daemon.BaseURL()+"/workspaces/coding/v1/responses", strings.NewReader(string(payload)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal("Workspace request failed")
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || response.StatusCode != want {
			t.Fatalf("Workspace status=%d want=%d", response.StatusCode, want)
		}
		if want == http.StatusPaymentRequired && !strings.Contains(string(body), "BUDGET_EXCEEDED") {
			t.Fatal("missing truthful budget denial")
		}
	}
	request(aliceKey.Token, "chat", http.StatusOK)
	request(bobKey.Token, "alternate", http.StatusOK)
	request(aliceKey.Token, "chat", http.StatusPaymentRequired)
	rotated := issue(bob.ID, "pricing-bob-rotation")
	revokePreview, err := control.PreviewCredentialRevocation(t.Context(), bob.ID, bobKey.Credential.ID)
	if err != nil {
		t.Fatal(err)
	}
	revokeIntent, err := operatorclient.RestoreCredentialRevocationIntent(bob.ID, bobKey.Credential.ID, revokePreview.Revision, "pricing-revoke-bob")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.ApplyCredentialRevocation(t.Context(), revokeIntent); err != nil {
		t.Fatal(err)
	}
	request(bobKey.Token, "alternate", http.StatusUnauthorized)
	request(rotated.Token, "alternate", http.StatusPaymentRequired)
	activity, err := operatorclient.New(http.DefaultClient, daemon.BaseURL()).Activity(t.Context(), "coding")
	if err != nil {
		t.Fatal(err)
	}
	actors := map[string]int{}
	for _, exchange := range activity.Exchanges {
		if exchange.HTTPStatus == http.StatusOK {
			if exchange.Workspace != "coding" || exchange.AttemptCount != 1 {
				t.Fatal("priced execution lost Workspace or attempt identity")
			}
			actors[exchange.AccessID]++
		}
	}
	if !reflect.DeepEqual(actors, map[string]int{alice.ID: 1, bob.ID: 1}) {
		t.Fatal("priced effects lost Principal attribution")
	}
	before := daemon.accountingStore.Reservations("coding")
	status, err := control.LimitsStatus(t.Context(), "coding")
	if err != nil || status.ChargedCents != "8" || status.ReservedCents != "0" || status.SettledCents != "3" || status.UncertainCents != "5" || status.UnknownPriceOperations != 0 || status.PartialQuantityOperations != 2 || status.UnquotedOperations != 0 || status.RemainingCents == nil || *status.RemainingCents != "0" {
		t.Fatalf("Control accounting status lost synchronous coverage: %#v err=%v", status, err)
	}
	if len(before) != 2 || calls.Load() != 1 || secondaryCalls.Load() != 1 {
		t.Fatal("aggregate budget did not bind real upstream effects")
	}
	for index, reservation := range before {
		wantAmount, wantTariff := int64(3), "standard"
		if index == 1 {
			wantAmount, wantTariff = 5, "regional"
		}
		wantState := "settled"
		if index == 1 {
			wantState = "uncertain"
		}
		if reservation.AmountCents != wantAmount || reservation.State != wantState || reservation.Quote == nil || reservation.Quote.Price.Tariff != wantTariff || reservation.Quote.QuantityCoverage != "partial" {
			t.Fatal("production daemon lost published quote provenance")
		}
	}
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	daemon = start()
	restoredStatus, err := operatorclient.New(http.DefaultClient, daemon.BaseURL()).LimitsStatus(t.Context(), "coding")
	if err != nil || !reflect.DeepEqual(status, restoredStatus) {
		t.Fatal("restart changed synchronous Limits status")
	}
	deniedRequest, err := http.NewRequestWithContext(t.Context(), http.MethodGet, daemon.BaseURL()+"/api/v1/workspaces/coding/limits/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	deniedRequest.Header.Set("Authorization", "Bearer "+rotated.Token)
	deniedResponse, err := http.DefaultClient.Do(deniedRequest)
	if err != nil {
		t.Fatal("Limits authority probe failed")
	}
	_ = deniedResponse.Body.Close()
	if deniedResponse.StatusCode != http.StatusForbidden {
		t.Fatal("Workspace credential read Control accounting status")
	}
	var restoredLimit accounting.Limit
	controlJSON(http.MethodGet, "/api/v1/workspaces/coding/limits", nil, &restoredLimit)
	if restoredLimit != limit {
		t.Fatal("Control readback lost durable Workspace threshold")
	}
	if !reflect.DeepEqual(before, daemon.accountingStore.Reservations("coding")) {
		t.Fatal("restart reset economic authority")
	}
	request(rotated.Token, "alternate", http.StatusPaymentRequired)
	request(aliceKey.Token, "chat", http.StatusPaymentRequired)
	request(bobKey.Token, "alternate", http.StatusUnauthorized)
	if calls.Load() != 1 || secondaryCalls.Load() != 1 || daemon.ledgerWriter != nil {
		t.Fatal("restart/disabled Ledger bypassed accounting")
	}
}

func TestDaemonPricingRejectsInvalidSettingsBeforeOpeningCatalogue(t *testing.T) {
	for _, payload := range []string{
		`{"models_database":"relative.sqlite","bindings":[]}`,
		`{"models_database":"/missing.sqlite","bindings":[],"unknown":true}`,
		`{"models_database":"/missing.sqlite","bindings":[]} {}`,
		`{"models_database":"/missing.sqlite","bindings":[{"target_id":"primary","target_version":0}]}`,
	} {
		t.Run(payload, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pricing.json")
			if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SWOBU_PRICING_CONFIG", path)
			if pricing, err := openDaemonPricing(t.Context()); err == nil || pricing != nil {
				t.Fatal("invalid pricing settings admitted")
			}
		})
	}
}
