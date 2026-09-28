package exchange

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/productprojection"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestProductObservationUsesDeliveryStatusAndExcludesRequestIdentity(t *testing.T) {
	evidence := &ProductObservationInputData{
		clientProduct:  clientfamily.Codex,
		clientProtocol: canonical.ClientFamilyResponses,
		requestPath:    canonical.NormalizedPathResponses,
		provider:       "openai",
		targetProtocol: protocolkind.Responses.String(),
		requestedModel: "default",
		resolvedModel:  "configured",
	}
	observation := BuildProductObservation(evidence, delivery.Result{Kind: delivery.Succeeded})
	if observation == nil || observation.StatusCode != 200 || observation.Result != "success" {
		t.Fatalf("observation = %#v", observation)
	}
	if observation.RequestedModel != "default" || observation.ResolvedModel != "configured" {
		t.Fatalf("bounded model categories lost: %#v", observation)
	}
	for _, forbidden := range []string{"arbitrary-client-request-id", "prompt-secret", "https://", "token"} {
		if containsObservation(observation, forbidden) {
			t.Fatalf("observation contains forbidden value %q: %#v", forbidden, observation)
		}
	}
}

func TestProductObservationClassifiesTerminalCauseInProductionBuilder(t *testing.T) {
	evidence := &ProductObservationInputData{}
	for _, test := range []struct {
		name   string
		result delivery.Result
		want   productprojection.ResultClass
	}{
		{"success", delivery.Result{Kind: delivery.Succeeded}, productprojection.ResultSuccess},
		{"cancelled", delivery.Result{Kind: delivery.ClientCancelled}, productprojection.ResultCancelled},
		{"backend", delivery.Result{Kind: delivery.ExchangeFailed, Err: canonical.NewBackendError("provider", 503, "untrusted", "")}, productprojection.ResultBackendError},
		{"unsupported operation", delivery.Result{Kind: delivery.ExchangeFailed, Err: canonical.ClientUnsupportedOperation("unsupported", "change operation")}, productprojection.ResultUnsupportedOperation},
		{"unsupported delivery", delivery.Result{Kind: delivery.ExchangeFailed, Err: canonical.ClientUnsupportedDelivery("unsupported", "change delivery")}, productprojection.ResultUnsupportedDeliveryVariant},
		{"not implemented", delivery.Result{Kind: delivery.ExchangeFailed, Err: canonical.NotImplemented("pending")}, productprojection.ResultNotImplemented},
		{"other Swobu error", delivery.Result{Kind: delivery.ExchangeFailed, Err: canonical.BadRequest("invalid")}, productprojection.ResultSwobuError},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := BuildProductObservation(evidence, test.result)
			if got == nil || got.Result != string(test.want) {
				t.Fatalf("terminal result = %#v, want %s", got, test.want)
			}
		})
	}
}

func TestTelemetryModelCategoriesDoNotExposeRawModelNames(t *testing.T) {
	if got := telemetryRequestedModel("private-customer-alias", "route-a"); got != "custom" {
		t.Fatalf("requested class = %q", got)
	}
	if got := telemetryRequestedModel("route-a", "route-a"); got != "configured" {
		t.Fatalf("requested class = %q", got)
	}
	if got := telemetryResolvedModel("route-a", "private-upstream-model"); got != "configured" {
		t.Fatalf("resolved class = %q", got)
	}
	if got := telemetryResolvedModel("", ""); got != "unknown" {
		t.Fatalf("missing resolved class = %q", got)
	}
}

func containsObservation(observation *productprojection.Observation, value string) bool {
	for _, field := range []string{observation.ClientFamily, observation.ClientProtocol, observation.Operation, observation.Provider, observation.TargetProtocol, observation.Result, observation.DeliveryKind, observation.ErrorCode} {
		if strings.Contains(field, value) {
			return true
		}
	}
	return false
}
