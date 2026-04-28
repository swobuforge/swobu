package providers_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/metrofun/swobu/internal/devtools/livematrix"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/internal/domain/providercatalog"
)

func TestLiveMatrixCaseCoverage_FullMatrixAlignsWithProviderCatalog(t *testing.T) {
	t.Parallel()

	cases := mustLoadLiveMatrixCases(t, "../../fixtures/live_matrix/scenario_cases.json")
	assertCaseRowsAlignToCatalog(t, cases)

	providersInCases := providerSet(cases)
	for _, profile := range providercatalog.All() {
		if profile.Spec == "custom" {
			continue // custom requires operator-specified base URL; not live-mined as a fixed fixture row
		}
		if !providersInCases[profile.Spec] {
			t.Fatalf("missing full-matrix scenario case coverage for provider=%q", profile.Spec)
		}
	}
}

func TestLiveMatrixCaseCoverage_SmokeMatrixCoversCredentialedRemoteProviders(t *testing.T) {
	t.Parallel()

	cases := mustLoadLiveMatrixCases(t, "../../fixtures/live_matrix/scenario_cases.smoke.json")
	assertCaseRowsAlignToCatalog(t, cases)

	wantProviders := map[string]bool{}
	for _, profile := range providercatalog.All() {
		if profile.Spec == "custom" {
			continue
		}
		if strings.TrimSpace(providercatalog.DefaultEnvKeyForSpec(profile.Spec)) == "" {
			continue
		}
		wantProviders[profile.Spec] = true
	}

	gotProviders := providerSet(cases)
	if len(gotProviders) != len(wantProviders) {
		t.Fatalf("smoke provider coverage mismatch: got=%v want=%v", sortedKeys(gotProviders), sortedKeys(wantProviders))
	}
	for provider := range wantProviders {
		if !gotProviders[provider] {
			t.Fatalf("smoke matrix missing provider=%q", provider)
		}
	}
}

func assertCaseRowsAlignToCatalog(t *testing.T, cases []livematrix.ScenarioCase) {
	t.Helper()
	for _, scenarioCase := range cases {
		provider := strings.TrimSpace(strings.ToLower(scenarioCase.Provider))
		if !providercatalog.SupportsSpec(provider) {
			t.Fatalf("scenario_case %q uses unknown provider=%q", scenarioCase.ID, scenarioCase.Provider)
		}
		if !providercatalog.SupportsRoute(provider, normalizeProtocolKind(scenarioCase.Protocol)) {
			t.Fatalf("scenario_case %q uses unsupported provider/protocol pair=%q/%q", scenarioCase.ID, provider, scenarioCase.Protocol)
		}
		wantEnvKey := strings.TrimSpace(providercatalog.DefaultEnvKeyForSpec(provider))
		gotEnvKey := strings.TrimSpace(scenarioCase.APIKeyEnv)
		switch {
		case wantEnvKey == "" && gotEnvKey != "":
			t.Fatalf("scenario_case %q provider=%q should not declare api_key_env, got=%q", scenarioCase.ID, provider, scenarioCase.APIKeyEnv)
		case wantEnvKey != "" && gotEnvKey != wantEnvKey:
			t.Fatalf("scenario_case %q provider=%q api_key_env=%q want=%q", scenarioCase.ID, provider, scenarioCase.APIKeyEnv, wantEnvKey)
		}
	}
}

func mustLoadLiveMatrixCases(t *testing.T, path string) []livematrix.ScenarioCase {
	t.Helper()
	cases, err := livematrix.LoadScenarioCases(path)
	if err != nil {
		t.Fatalf("load scenario cases %q: %v", path, err)
	}
	if len(cases) == 0 {
		t.Fatalf("scenario cases %q empty", path)
	}
	return cases
}

func providerSet(cases []livematrix.ScenarioCase) map[string]bool {
	out := map[string]bool{}
	for _, scenarioCase := range cases {
		out[strings.TrimSpace(strings.ToLower(scenarioCase.Provider))] = true
	}
	return out
}

func normalizeProtocolKind(raw string) protocolsurface.Kind {
	return protocolsurface.Kind(strings.TrimSpace(strings.ToLower(raw)))
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func TestLiveMatrixCaseCoverage_DefaultScenarioCaseIDsStayStable(t *testing.T) {
	t.Parallel()

	cases := mustLoadLiveMatrixCases(t, "../../fixtures/live_matrix/scenario_cases.smoke.json")
	got := make([]string, 0, len(cases))
	for _, scenarioCase := range cases {
		got = append(got, scenarioCase.ID)
	}

	want := []string{
		"openai-chat-tool-sse-smoke",
		"openrouter-chat-tool-sse-smoke",
		"anthropic-messages-text-smoke",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("smoke scenario ids drifted: got=%v want=%v", got, want)
	}
}
