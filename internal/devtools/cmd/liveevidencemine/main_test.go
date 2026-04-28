package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/metrofun/swobu/internal/devtools/livematrix"
)

func TestRequiredCredentialEnvKeys_DerivesRequiredSetFromScenarioCases(t *testing.T) {
	required := requiredCredentialEnvKeys([]livematrix.ScenarioCase{
		{ID: "a", APIKeyEnv: "OPENROUTER_API_KEY"},
		{ID: "b", APIKeyEnv: " OPENAI_API_KEY "},
		{ID: "c", APIKeyEnv: ""},
		{ID: "d", APIKeyEnv: "OPENROUTER_API_KEY"},
	})

	if len(required) != 2 {
		t.Fatalf("required set len=%d want=2", len(required))
	}
	if !required["OPENROUTER_API_KEY"] {
		t.Fatal("OPENROUTER_API_KEY missing from required set")
	}
	if !required["OPENAI_API_KEY"] {
		t.Fatal("OPENAI_API_KEY missing from required set")
	}
}

func TestResolveCredential_PrefersAPIKeyEnvOverFileSources(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "token-env")
	t.Setenv("SWOBU_OPENAI_KEY_FILE", "")
	token, source, ok := resolveCredential("OPENAI_API_KEY", "SWOBU_OPENAI_KEY_FILE", "", []string{".secrets/openai.key"})
	if !ok {
		t.Fatal("resolveCredential returned ok=false, want true")
	}
	if token != "token-env" {
		t.Fatalf("token=%q want token-env", token)
	}
	if source != "" {
		t.Fatalf("source=%q want empty (env token)", source)
	}
}

func TestResolveCredential_UsesExplicitThenEnvKeyFileThenFallback(t *testing.T) {
	tempDir := t.TempDir()
	explicit := filepath.Join(tempDir, "explicit.key")
	envFile := filepath.Join(tempDir, "env.key")
	fallback := filepath.Join(tempDir, "fallback.key")

	mustWriteKeyFile(t, explicit, "token-explicit")
	mustWriteKeyFile(t, envFile, "token-env-file")
	mustWriteKeyFile(t, fallback, "token-fallback")

	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("SWOBU_OPENAI_KEY_FILE", envFile)

	token, source, ok := resolveCredential("OPENAI_API_KEY", "SWOBU_OPENAI_KEY_FILE", explicit, []string{fallback})
	if !ok {
		t.Fatal("resolveCredential returned ok=false, want true")
	}
	if token != "token-explicit" || source != explicit {
		t.Fatalf("explicit precedence mismatch: token=%q source=%q", token, source)
	}

	token, source, ok = resolveCredential("OPENAI_API_KEY", "SWOBU_OPENAI_KEY_FILE", "", []string{fallback})
	if !ok {
		t.Fatal("resolveCredential returned ok=false for env key file source")
	}
	if token != "token-env-file" || source != envFile {
		t.Fatalf("env key file precedence mismatch: token=%q source=%q", token, source)
	}

	t.Setenv("SWOBU_OPENAI_KEY_FILE", "")
	token, source, ok = resolveCredential("OPENAI_API_KEY", "SWOBU_OPENAI_KEY_FILE", "", []string{fallback})
	if !ok {
		t.Fatal("resolveCredential returned ok=false for fallback source")
	}
	if token != "token-fallback" || source != fallback {
		t.Fatalf("fallback mismatch: token=%q source=%q", token, source)
	}
}

func TestResolveAndExportCredential_RequiredFailsWhenMissing(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("SWOBU_OPENAI_KEY_FILE", "")
	err := resolveAndExportCredential(credentialResolverInput{
		Label:              "openai",
		APIKeyEnv:          "OPENAI_API_KEY",
		KeyFileEnv:         "SWOBU_OPENAI_KEY_FILE",
		FallbackCandidates: []string{filepath.Join(t.TempDir(), "missing.key")},
		Required:           true,
	})
	if err == nil {
		t.Fatal("resolveAndExportCredential returned nil error for required missing credential")
	}
}

func TestResolveAndExportCredential_ExportsTokenAndResolvedFileEnv(t *testing.T) {
	tempDir := t.TempDir()
	keyFile := filepath.Join(tempDir, "openai.key")
	mustWriteKeyFile(t, keyFile, "token-file")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("SWOBU_OPENAI_KEY_FILE", "")

	if err := resolveAndExportCredential(credentialResolverInput{
		Label:              "openai",
		APIKeyEnv:          "OPENAI_API_KEY",
		KeyFileEnv:         "SWOBU_OPENAI_KEY_FILE",
		FallbackCandidates: []string{keyFile},
		Required:           true,
	}); err != nil {
		t.Fatalf("resolveAndExportCredential returned error: %v", err)
	}

	if got := os.Getenv("OPENAI_API_KEY"); got != "token-file" {
		t.Fatalf("OPENAI_API_KEY=%q want token-file", got)
	}
	if got := os.Getenv("SWOBU_OPENAI_KEY_FILE"); got != keyFile {
		t.Fatalf("SWOBU_OPENAI_KEY_FILE=%q want %q", got, keyFile)
	}
}

func mustWriteKeyFile(t *testing.T, path string, token string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(token+"\n"), 0o644); err != nil {
		t.Fatalf("write key file %q: %v", path, err)
	}
}
