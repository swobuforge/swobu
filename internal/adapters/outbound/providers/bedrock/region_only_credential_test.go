package bedrock

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

// TestRegionOnlyBedrockTargetWithoutCredentialTerminatesAtSigV4AmbientAuth
// pins the authentication fork for a region-only Bedrock target whose
// credential reference is empty (the durable {region} config shape). The fork:
//
//   - valid ambient SigV4 chain  -> Send dispatches to the backend.
//   - no retrievable SigV4 chain -> Send terminates pre-dispatch with
//     AttemptNotDispatched wrapping canonical.Error{Code: BAD_ENDPOINT}, which
//     TerminalErrorCode surfaces as error_code=BAD_ENDPOINT, error_origin=swobu.
//
// AWS_BEARER_TOKEN_BEDROCK is never consulted on this arm: it is catalog
// metadata (profile/catalog.go) only, and resolveBedrockAuth selects bearer
// strictly from a non-empty target CredentialRef. The second arm is therefore
// the Swobu-origin terminal for any region-only target whose process cannot
// retrieve AWS credentials. This is a regression guard: a future change that
// made a credential-less target succeed or fail differently would break it.
func TestRegionOnlyBedrockTargetWithoutCredentialTerminatesAtSigV4AmbientAuth(t *testing.T) {
	dispatched := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatched++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer upstream.Close()

	regionOnlyTarget := provider.NewBedrockTargetSnapshot(
		"tgt_region_only",
		upstream.URL,
		"", // empty credential reference — forces the SigV4 ambient arm
		protocolkind.Responses,
		"",
		"responses_stream",
		"us-east-1",
	)
	doc := carrier.NewDocument(
		protocolkind.Responses,
		"application/json",
		nil,
		[]byte(`{"model":"xai.grok-4.3","input":[]}`),
		carrier.Meta{},
	)
	exec := NewExecutor(http.DefaultClient)

	t.Run("valid_SigV4_chain_dispatches", func(t *testing.T) {
		setStaticAWSCredentials(t, "AKIAEXAMPLE019283746", "static-secret-for-reproduction-only", "")
		_, err := exec.Send(context.Background(), regionOnlyTarget, doc)
		if err != nil {
			t.Fatalf("valid SigV4 chain must dispatch; got error = %v", err)
		}
		if dispatched != 1 {
			t.Fatalf("dispatched=%d want 1", dispatched)
		}
	})

	t.Run("no_SigV4_chain_terminates_BadEndpoint_before_dispatch", func(t *testing.T) {
		stripAWSCredentials(t)
		_, err := exec.Send(context.Background(), regionOnlyTarget, doc)
		if err == nil {
			t.Fatal("empty SigV4 chain must terminate before dispatch")
		}
		failure, ok := provider.AsAttemptFailure(err)
		if !ok {
			t.Fatalf("error = %T %v, want an AttemptFailure", err, err)
		}
		if failure.Execution() != provider.ExecutionNotDispatched {
			t.Fatalf("execution = %v want ExecutionNotDispatched", failure.Execution())
		}
		var badEndpoint canonical.Error
		if !errors.As(err, &badEndpoint) {
			t.Fatalf("cause = %T %v, want a canonical.Error BadEndpoint", err, err)
		}
		if badEndpoint.Code != canonical.ErrorCodeBadEndpoint {
			t.Fatalf("error code = %v want %v", badEndpoint.Code, canonical.ErrorCodeBadEndpoint)
		}
		// TerminalErrorCode is the exact classifier that stamps request_outcome's
		// error_code log field.
		if got := canonical.TerminalErrorCode(err); got != canonical.ErrorCodeBadEndpoint {
			t.Fatalf("TerminalErrorCode = %v want %v", got, canonical.ErrorCodeBadEndpoint)
		}
		if dispatched != 1 {
			t.Fatalf("dispatched must stay 1 (no-chain arm must NOT dispatch); got %d", dispatched)
		}
	})
}

// stripAWSCredentials removes every SigV4 chain source so
// loadBedrockAmbientConfig has nothing to retrieve; AWS_EC2_METADATA_DISABLED
// blocks the IMDS fallback.
func stripAWSCredentials(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
		"AWS_PROFILE", "AWS_DEFAULT_PROFILE",
	} {
		t.Setenv(key, "")
	}
	tmp := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", tmp+"/config-absent")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", tmp+"/credentials-absent")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
}
