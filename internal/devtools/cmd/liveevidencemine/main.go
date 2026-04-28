package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/metrofun/swobu/internal/devtools/livematrix"
)

type clientVersionInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Found   bool   `json:"found"`
}

type minedEvidenceRecord struct {
	CapturedAtUTC string              `json:"captured_at_utc"`
	ProviderRun   string              `json:"provider_run"`
	ClientRun     string              `json:"client_run"`
	Clients       []clientVersionInfo `json:"clients"`
}

func main() {
	var (
		casesPath       = flag.String("cases", "test/fixtures/live_matrix/scenario_cases.smoke.json", "path to live scenario-case matrix (default: credentialed remote-provider smoke lane)")
		recordsDir      = flag.String("records-out", "test/fixtures/live_matrix/records", "directory for provider live records")
		clientArtifact  = flag.String("client-artifact-out", "test/artifacts/live/client_integration/openrouter_latest.json", "client integration evidence artifact path")
		keyFile         = flag.String("openrouter-key-file", "", "path to OpenRouter key file (optional when OPENROUTER_API_KEY is set)")
		openAIKey       = flag.String("openai-key-file", "", "path to OpenAI key file (optional when OPENAI_API_KEY is set)")
		anthropicKey    = flag.String("anthropic-key-file", "", "path to anthropic key file (optional when ANTHROPIC_API_KEY is set)")
		providerTimeout = flag.Duration("provider-timeout", 90*time.Second, "per-provider scenario timeout")
	)
	flag.Parse()

	scenarioCases, err := livematrix.LoadScenarioCases(*casesPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load scenario cases: %v\n", err)
		os.Exit(1)
	}

	requiredEnvs := requiredCredentialEnvKeys(scenarioCases)
	// OpenRouter remains mandatory for client integration evidence.
	requiredEnvs["OPENROUTER_API_KEY"] = true

	if err := resolveAndExportCredential(credentialResolverInput{
		Label:              "openrouter",
		APIKeyEnv:          "OPENROUTER_API_KEY",
		KeyFileEnv:         "SWOBU_OPENROUTER_KEY_FILE",
		ExplicitKeyFile:    *keyFile,
		FallbackCandidates: []string{".secrets/openrouter.key"},
		Required:           requiredEnvs["OPENROUTER_API_KEY"],
	}); err != nil {
		fmt.Fprintf(os.Stderr, "openrouter credentials invalid: %v\n", err)
		os.Exit(1)
	}
	if err := resolveAndExportCredential(credentialResolverInput{
		Label:              "openai",
		APIKeyEnv:          "OPENAI_API_KEY",
		KeyFileEnv:         "SWOBU_OPENAI_KEY_FILE",
		ExplicitKeyFile:    *openAIKey,
		FallbackCandidates: []string{".secrets/openai.key"},
		Required:           requiredEnvs["OPENAI_API_KEY"],
	}); err != nil {
		fmt.Fprintf(os.Stderr, "openai credentials invalid: %v\n", err)
		os.Exit(1)
	}
	if err := resolveAndExportCredential(credentialResolverInput{
		Label:              "anthropic",
		APIKeyEnv:          "ANTHROPIC_API_KEY",
		KeyFileEnv:         "SWOBU_ANTHROPIC_KEY_FILE",
		ExplicitKeyFile:    *anthropicKey,
		FallbackCandidates: []string{".secrets/anthropic.key", ".secrets/claude.key"},
		Required:           requiredEnvs["ANTHROPIC_API_KEY"],
	}); err != nil {
		fmt.Fprintf(os.Stderr, "anthropic credentials invalid: %v\n", err)
		os.Exit(1)
	}

	providerRunAt := time.Now().UTC()
	if err := mineProviderEvidence(*casesPath, *recordsDir, *providerTimeout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	clientRunAt := time.Now().UTC()
	if err := requireClientBinary("aider"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := runClientEvidenceE2E(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	artifact := minedEvidenceRecord{
		CapturedAtUTC: time.Now().UTC().Format(time.RFC3339),
		ProviderRun:   providerRunAt.Format(time.RFC3339),
		ClientRun:     clientRunAt.Format(time.RFC3339),
		Clients: []clientVersionInfo{
			probeClientVersion("aider", []string{"--version"}),
		},
	}
	if err := os.MkdirAll(filepath.Dir(*clientArtifact), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create artifact directory: %v\n", err)
		os.Exit(1)
	}
	raw, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal client artifact: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*clientArtifact, append(raw, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write client artifact: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("live evidence mining complete: %s\n", *clientArtifact)
}

func mineProviderEvidence(casesPath string, outDir string, timeout time.Duration) error {
	cmd := exec.Command(
		"go", "run", "./internal/devtools/cmd/livematrix",
		"-mode", "swobu_session",
		"-cases", casesPath,
		"-out", outDir,
		"-timeout", timeout.String(),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("live provider mining failed: %w", err)
	}
	return nil
}

func runClientEvidenceE2E() error {
	cmd := exec.Command("go", "test", "./test/e2e", "-run", "TestLiveClientIntegrationEvidence_OpenRouter_AiderOnly", "-count=1")
	cmd.Env = append(os.Environ(), "SWOBU_LIVE_EVIDENCE=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("live client integration run failed: %w", err)
	}
	return nil
}

func probeClientVersion(binary string, args []string) clientVersionInfo {
	path, err := exec.LookPath(binary)
	if err != nil {
		return clientVersionInfo{Name: binary, Found: false, Version: "not found"}
	}
	cmd := exec.Command(path, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return clientVersionInfo{Name: binary, Found: true, Version: strings.TrimSpace(string(out))}
	}
	version := strings.TrimSpace(string(out))
	if version == "" {
		version = "ok"
	}
	return clientVersionInfo{Name: binary, Found: true, Version: version}
}

func requireClientBinary(binary string) error {
	if _, err := exec.LookPath(binary); err != nil {
		return fmt.Errorf("required client binary %q not found in PATH; install it before running make mine-live-evidence", binary)
	}
	return nil
}

func ensureReadableNonEmptyFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("path must not be empty")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return errors.New("file is empty")
	}
	return nil
}

type credentialResolverInput struct {
	Label              string
	APIKeyEnv          string
	KeyFileEnv         string
	ExplicitKeyFile    string
	FallbackCandidates []string
	Required           bool
}

func requiredCredentialEnvKeys(scenarioCases []livematrix.ScenarioCase) map[string]bool {
	required := map[string]bool{}
	for _, scenarioCase := range scenarioCases {
		envKey := strings.TrimSpace(scenarioCase.APIKeyEnv)
		if envKey == "" {
			continue
		}
		required[envKey] = true
	}
	return required
}

func resolveAndExportCredential(in credentialResolverInput) error {
	token, source, ok := resolveCredential(in.APIKeyEnv, in.KeyFileEnv, in.ExplicitKeyFile, in.FallbackCandidates)
	if !ok {
		if !in.Required {
			return nil
		}
		candidates := append([]string{}, in.FallbackCandidates...)
		candidates = slices.DeleteFunc(candidates, func(path string) bool {
			return strings.TrimSpace(path) == ""
		})
		return fmt.Errorf(
			"set %s or provide a readable key file via %s or -%s-key-file (fallback %s)",
			in.APIKeyEnv,
			in.KeyFileEnv,
			in.Label,
			strings.Join(candidates, ", "),
		)
	}
	if err := os.Setenv(in.APIKeyEnv, token); err != nil {
		return fmt.Errorf("set %s: %w", in.APIKeyEnv, err)
	}
	if strings.TrimSpace(source) != "" {
		if err := os.Setenv(in.KeyFileEnv, source); err != nil {
			return fmt.Errorf("set %s: %w", in.KeyFileEnv, err)
		}
	}
	return nil
}

func resolveCredential(envKey string, keyFileEnv string, explicitKeyFile string, fallbackCandidates []string) (string, string, bool) {
	if token := strings.TrimSpace(os.Getenv(envKey)); token != "" {
		return token, "", true
	}
	candidates := []string{
		strings.TrimSpace(explicitKeyFile),
		strings.TrimSpace(os.Getenv(keyFileEnv)),
	}
	candidates = append(candidates, fallbackCandidates...)
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if err := ensureReadableNonEmptyFile(candidate); err != nil {
			continue
		}
		raw, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		if token := strings.TrimSpace(string(raw)); token != "" {
			return token, candidate, true
		}
	}
	return "", "", false
}
