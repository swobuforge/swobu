package bedrock

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestCheckCredentialProcessChild(t *testing.T) {
	if os.Getenv("SWOBU_SYNTHETIC_PROCESS") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	fmt.Fprint(os.Stderr, "synthetic-private-stderr")
	switch mode {
	case "success":
		fmt.Print(`{"Version":1,"AccessKeyId":"synthetic-access","SecretAccessKey":"synthetic-secret","SessionToken":"synthetic-session"}`)
	case "oversized":
		fmt.Print(strings.Repeat("x", checkProcessOutputLimit+1))
	case "stall":
		time.Sleep(time.Minute)
	default:
		fmt.Print(`private-invalid-output`)
	}
	os.Exit(0)
}

func processCommand(t *testing.T, mode string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The SDK shell contract is intentional for credential_process, unlike MCP
	// executable/argv. Quote the synthetic executable as one shell operand.
	return "'" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "' -test.run=^TestCheckCredentialProcessChild$ -- " + mode
}

func TestBoundedCredentialProcessSuccessOversizeAndCancellation(t *testing.T) {
	t.Setenv("SWOBU_SYNTHETIC_PROCESS", "1")
	value, err := (boundedCheckProcess{command: processCommand(t, "success")}).Retrieve(t.Context())
	if err != nil || value.AccessKeyID != "synthetic-access" || value.SessionToken != "synthetic-session" {
		t.Fatal("synthetic process failed", err)
	}
	for _, mode := range []string{"oversized", "invalid", "stall"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, err := (boundedCheckProcess{command: processCommand(t, mode)}).Retrieve(ctx)
			if err == nil || strings.Contains(err.Error(), "private") || time.Since(start) > 2*time.Second {
				t.Fatal("unbounded or unsanitized process", err)
			}
		})
	}
}

func TestSelectedCredentialProcessPreservesSourceProfileRoleChain(t *testing.T) {
	t.Setenv("SWOBU_SYNTHETIC_PROCESS", "1")
	root := t.TempDir()
	path := filepath.Join(root, "config")
	text := "[profile selected]\nrole_arn = arn:aws:iam::123456789012:role/synthetic\nsource_profile = source\nrole_session_name = synthetic-session\nexternal_id = synthetic-external\nduration_seconds = 1800\n[source]\ncredential_process = " + processCommand(t, "success") + "\n"
	// Shared config non-default profiles require the profile prefix.
	text = strings.Replace(text, "[source]", "[profile source]", 1)
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_PROFILE", "selected")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", path)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(root, "missing"))
	for _, name := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_ROLE_ARN"} {
		t.Setenv(name, "")
	}
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "Action=AssumeRole") || !strings.Contains(string(body), "ExternalId=synthetic-external") || !strings.Contains(string(body), "DurationSeconds=1800") || !strings.Contains(r.Header.Get("Authorization"), "synthetic-access/") {
			t.Error("role chain options or source lost")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`<AssumeRoleResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><AssumeRoleResult><Credentials><AccessKeyId>synthetic-role</AccessKeyId><SecretAccessKey>synthetic-role-secret</SecretAccessKey><SessionToken>synthetic-role-session</SessionToken><Expiration>2099-01-01T00:00:00Z</Expiration></Credentials></AssumeRoleResult></AssumeRoleResponse>`))}, nil
	})}
	cfg, err := config.LoadDefaultConfig(t.Context(), config.WithRegion("us-east-1"), config.WithHTTPClient(client))
	if err != nil {
		t.Fatal(err)
	}
	if err := boundSelectedProcess(&cfg); err != nil {
		t.Fatal(err)
	}
	credentials, err := cfg.Credentials.Retrieve(t.Context())
	if err != nil || credentials.AccessKeyID != "synthetic-role" || requests != 1 {
		t.Fatal("bounded role chain failed", err, requests)
	}
}

func TestUnselectedCredentialProcessDoesNotOverrideAmbientKeys(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config")
	if err := os.WriteFile(path, []byte("[default]\ncredential_process = must-not-execute\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "synthetic-env")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "synthetic-env-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_CONFIG_FILE", path)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(root, "missing"))
	cfg, err := config.LoadDefaultConfig(t.Context(), config.WithRegion("us-east-1"))
	if err != nil {
		t.Fatal(err)
	}
	original := cfg.Credentials
	if err := boundSelectedProcess(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Credentials != original {
		t.Fatal("replaced SDK ambient selection")
	}
	credentials, err := cfg.Credentials.Retrieve(t.Context())
	if err != nil || credentials.AccessKeyID != "synthetic-env" {
		t.Fatal("ambient precedence lost", err)
	}
}

func TestConnectionCatalogUsesSelectedBoundedProcess(t *testing.T) {
	t.Setenv("SWOBU_SYNTHETIC_PROCESS", "1")
	root := t.TempDir()
	path := filepath.Join(root, "config")
	if err := os.WriteFile(path, []byte("[default]\ncredential_process = "+processCommand(t, "success")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_ROLE_ARN"} {
		t.Setenv(name, "")
	}
	t.Setenv("AWS_CONFIG_FILE", path)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(root, "missing"))
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if !strings.Contains(r.Header.Get("Authorization"), "synthetic-access/") {
			t.Error("wrong process signing key")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"synthetic-model"}]}`))}, nil
	})}
	target := provider.NewBedrockTargetSnapshot("check", "", "", protocolkind.Responses, "responses", "us-east-1", delivery.BufferedDelivery())
	result, err := CheckConnectionCatalog(t.Context(), target, client, nil)
	if err != nil || len(result.Options) != 1 || requests != 1 {
		t.Fatal("bounded process catalog failed", err)
	}
}
