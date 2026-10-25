package clientconnect

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Endpoint convergence must not certify the retired placeholder as a credential.
func TestPiPlaceholderIsNotConfigured(t *testing.T) {
	dir := t.TempDir()
	settings, models := filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json")
	if err := os.WriteFile(settings, []byte(`{"defaultProvider":"swobu","defaultModel":"coding"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(models, []byte(`{"providers":{"swobu":{"baseUrl":"http://127.0.0.1:7926/workspaces/work","api":"openai-responses","apiKey":"swobu","models":[{"id":"coding","name":"coding"}]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	planned, err := planPi(settings, models, testTarget(t), os.Getenv)
	if err == nil && planned.plan.AlreadyConfigured() {
		t.Fatal("placeholder credential was acknowledged as configured")
	}
}

func TestCodexCredentialPrecedenceAndManagedEnvironmentReuse(t *testing.T) {
	for _, reuse := range []bool{false, true} {
		t.Run(map[bool]string{false: "replace-stale-sources", true: "preserve-valid-environment"}[reuse], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			raw := []byte("model=\"old\"\n[model_providers.swobu]\nenv_key=\"SWOBU_TEST_KEY\" # owned binding\nexperimental_bearer_token=\"stale\"\n[model_providers.swobu.http_headers]\nAuthorization=\"Bearer stale-header\"\nX-Keep=\"unchanged\"\n")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			value := "stale-environment"
			if reuse {
				value = testTarget(t).bearer
			}
			resolver := func(string) string { return value }
			mutation, err := planCodex(path, testTarget(t), resolver)
			if err != nil {
				t.Fatal(err)
			}
			if mutation.plan.ConfiguredCredential() != value {
				t.Fatal("did not inspect effective environment source")
			}
			if err := mutation.apply(t.Context()); err != nil {
				t.Fatal(err)
			}
			next, err := planCodex(path, testTarget(t), resolver)
			if err != nil || !next.plan.AlreadyConfigured() || next.plan.ConfiguredCredential() != testTarget(t).bearer {
				t.Fatal("effective authentication did not converge", err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Contains(after, []byte(`X-Keep="unchanged"`)) {
				t.Fatal("unrelated header changed")
			}
			if bytes.Contains(after, []byte(`env_key="SWOBU_TEST_KEY"`)) != reuse {
				t.Fatal("environment source preservation did not match reuse")
			}
			public, _ := json.Marshal(mutation.plan)
			for _, secret := range []string{value, testTarget(t).bearer, "stale-header"} {
				if bytes.Contains(public, []byte(secret)) {
					t.Fatal("review disclosed credential")
				}
			}
		})
	}
}

func TestOpenCodePreservesAcceptedEnvironmentReference(t *testing.T) {
	home := t.TempDir()
	path := writeOpenCodeConfig(t, openCodeConfigDir(home, nil), "opencode.json", `{"provider":{"swobu":{"options":{"apiKey":"{env:SWOBU_TEST_KEY}"}}}}`)
	service := openCodeService(t, home, map[string]string{"SWOBU_TEST_KEY": testTarget(t).bearer}, true)
	plan, err := service.Plan(t.Context(), ClientOpenCode, testTarget(t))
	if err != nil || plan.ConfiguredCredential() != testTarget(t).bearer {
		t.Fatal("environment credential not inspected", err)
	}
	if _, err := service.Apply(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Contains(after, []byte(`{env:SWOBU_TEST_KEY}`)) || bytes.Contains(after, []byte(testTarget(t).bearer)) {
		t.Fatal("accepted user-managed reference was replaced")
	}
}

func TestPiPreservesAcceptedEnvironmentReference(t *testing.T) {
	dir := t.TempDir()
	settings, models := filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json")
	if err := os.WriteFile(models, []byte(`{"providers":{"swobu":{"apiKey":"SWOBU_TEST_KEY"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	resolver := func(key string) string {
		if key == "SWOBU_TEST_KEY" {
			return testTarget(t).bearer
		}
		return ""
	}
	mutation, err := planPi(settings, models, testTarget(t), resolver)
	if err != nil || mutation.plan.ConfiguredCredential() != testTarget(t).bearer {
		t.Fatal("environment credential not inspected", err)
	}
	if err := mutation.apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(models)
	if !bytes.Contains(after, []byte(`"apiKey":"SWOBU_TEST_KEY"`)) || bytes.Contains(after, []byte(testTarget(t).bearer)) {
		t.Fatal("accepted user-managed reference was replaced")
	}
}
