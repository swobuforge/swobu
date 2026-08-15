package bedrock

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestProbeTargetCatalogSuccessSurvivesSTSIdentityFailure(t *testing.T) {
	setStaticAWSCredentials(t, "access", "secret", "session")
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"model-1"}]}`)), Request: req}, nil
	})}
	exec := NewExecutor(client)
	exec.callerIdentity = func(context.Context, aws.Config) (*sts.GetCallerIdentityOutput, error) {
		return nil, errors.New("STS unavailable")
	}
	result, err := exec.ProbeTarget(context.Background(), newBedrockTarget("https://bedrock-mantle.eu-west-2.api.aws/v1", "", protocolkind.Responses))
	if err != nil {
		t.Fatalf("catalog-success probe failed because STS failed: %v", err)
	}
	if len(result.Options) != 1 || !strings.Contains(string(result.Diagnostics), "identity_probe_failed") {
		t.Fatalf("result = %#v diagnostics=%s", result, result.Diagnostics)
	}
}

func TestProbeTargetReportsTruthfulFailureStage(t *testing.T) {
	t.Run("authentication", func(t *testing.T) {
		t.Setenv("MISSING_BEDROCK_TOKEN", "")
		exec := NewExecutor(http.DefaultClient)
		result, err := exec.ProbeTarget(context.Background(), newBedrockTarget("https://bedrock-mantle.eu-west-2.api.aws/v1", "env:MISSING_BEDROCK_TOKEN", protocolkind.Responses))
		diagnostics := string(result.Diagnostics)
		if err == nil || !strings.Contains(diagnostics, `"authentication":"explicit_api_key"`) || !strings.Contains(diagnostics, `"failure_stage":"authentication"`) {
			t.Fatalf("err=%v diagnostics=%s", err, result.Diagnostics)
		}
		if strings.Contains(diagnostics, `"aws_identity"`) || strings.Contains(diagnostics, `"authentication":"unavailable"`) {
			t.Fatalf("API-key failure invented AWS identity or unavailable strategy: %s", diagnostics)
		}
	})
	t.Run("AWS identity authentication", func(t *testing.T) {
		t.Setenv("AWS_ACCESS_KEY_ID", "")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "")
		t.Setenv("AWS_SESSION_TOKEN", "")
		t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/missing-credentials")
		t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/missing-config")
		t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
		exec := NewExecutor(http.DefaultClient)
		result, err := exec.ProbeTarget(context.Background(), newBedrockTarget("https://bedrock-mantle.eu-west-2.api.aws/v1", "", protocolkind.Responses))
		diagnostics := string(result.Diagnostics)
		if err == nil || !strings.Contains(diagnostics, `"authentication":"aws_identity"`) || !strings.Contains(diagnostics, `"failure_stage":"authentication"`) || !strings.Contains(diagnostics, `"error":`) {
			t.Fatalf("err=%v diagnostics=%s", err, result.Diagnostics)
		}
		if strings.Contains(diagnostics, `"aws_identity":{`) || strings.Contains(diagnostics, `"authentication":"unavailable"`) {
			t.Fatalf("AWS authentication failure invented identity evidence or unavailable strategy: %s", diagnostics)
		}
	})
	t.Run("catalog", func(t *testing.T) {
		t.Setenv("BEDROCK_TOKEN", "token")
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("network timeout") })}
		exec := NewExecutor(client)
		exec.credentials = resolutionCredentialProvider{value: "token"}
		result, err := exec.ProbeTarget(context.Background(), newBedrockTarget("https://bedrock-mantle.eu-west-2.api.aws/v1", "env:BEDROCK_TOKEN", protocolkind.Responses))
		if err == nil || !strings.Contains(string(result.Diagnostics), `"failure_stage":"catalog"`) {
			t.Fatalf("err=%v diagnostics=%s", err, result.Diagnostics)
		}
	})
}

func TestProbeTargetCatalogFailureIncludesRefreshedAWSIdentity(t *testing.T) {
	setStaticAWSCredentials(t, "access", "secret", "session")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("catalog timeout")
	})}
	exec := NewExecutor(client)
	exec.callerIdentity = func(context.Context, aws.Config) (*sts.GetCallerIdentityOutput, error) {
		return &sts.GetCallerIdentityOutput{Account: aws.String("222222222222"), Arn: aws.String("arn:aws:sts::222222222222:assumed-role/changed/session")}, nil
	}
	result, err := exec.ProbeTarget(context.Background(), newBedrockTarget("https://bedrock-mantle.eu-west-2.api.aws/v1", "", protocolkind.Responses))
	if err == nil {
		t.Fatal("catalog failure returned nil error")
	}
	diagnostics := string(result.Diagnostics)
	for _, want := range []string{`"authentication":"aws_identity"`, `"failure_stage":"catalog"`, `"state":"resolved"`, `"account":"222222222222"`, `"arn":"arn:aws:sts::222222222222:assumed-role/changed/session"`} {
		if !strings.Contains(diagnostics, want) {
			t.Fatalf("diagnostics %s do not contain %s", diagnostics, want)
		}
	}
}
