package deepinfra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
)

const failFastCarrierMarker = "deepinfra.fail_fast"

// NewRuntime combines DeepInfra's one Chat endpoint with a catalog that is
// intentionally advisory: catalog metadata never selects inference behavior.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecDeepInfra))
	bundle.BackendResolver = backendResolver{standard: bundle.BackendResolver}
	bundle.Discovery = discovery{client: client, credentials: credentials}
	return bundle
}

type backendResolver struct{ standard provider.BackendResolver }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	if target.ProtocolKind != protocolkind.ChatCompletions {
		return provider.Backend{}, fmt.Errorf("DeepInfra backend protocol %q is not Chat Completions", target.ProtocolKind)
	}
	standard, ok := backend.Codec.(protocolcodec.Codec)
	if !ok {
		return provider.Backend{}, fmt.Errorf("DeepInfra Chat backend has codec %T, want protocolcodec.Codec", backend.Codec)
	}
	backend.Codec = failFastCodec{standard: standard}
	backend.Transport = overloadTransport{standard: backend.Transport}
	return backend, backend.Validate()
}

// failFastCodec adds DeepInfra's queue-avoidance switch after shared typed
// Chat lowering and before its one JSON serialization boundary.
type failFastCodec struct{ standard protocolcodec.Codec }

func (c failFastCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	document, changes, err := protocolcodec.LowerChatCompletionsRequest(req)
	if err != nil {
		return carrier.Document{}, changes, err
	}
	if req.EncodeContext.HasNextRouteCandidate {
		document.Payload["fail_fast"] = true
	}
	encoded, err := chatcompletions.EncodeProviderRequestDocument(document)
	if err == nil && req.EncodeContext.HasNextRouteCandidate {
		encoded.Meta.Opaque = map[string]string{failFastCarrierMarker: "true"}
	}
	return encoded, changes, err
}

func (c failFastCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return c.standard.Decode(ctx, req, ingress)
}

// overloadTransport can make the narrow, documented execution claim only when
// this adapter emitted fail_fast and the backend returns engine_overloaded.
// Every other backend result retains the shared conservative classification.
type overloadTransport struct{ standard provider.Transport }

func (t overloadTransport) Send(ctx context.Context, document carrier.Document) (provider.Ingress, error) {
	ingress, err := t.standard.Send(ctx, document)
	if err == nil || !isMarkedFailFastDocument(document) || !isEngineOverloaded(err) {
		return ingress, err
	}
	return ingress, provider.AttemptRejectedBeforeExecution(provider.Rejected(attemptCause(err)))
}

func isMarkedFailFastDocument(document carrier.Document) bool {
	if document.Meta.Opaque[failFastCarrierMarker] != "true" {
		return false
	}
	var payload struct {
		FailFast bool `json:"fail_fast"`
	}
	return json.Unmarshal(document.RawBytes(), &payload) == nil && payload.FailFast
}

func isEngineOverloaded(err error) bool {
	var backend canonical.BackendError
	if !errors.As(err, &backend) || backend.StatusCode != http.StatusTooManyRequests {
		return false
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	return json.Unmarshal([]byte(backend.Message), &envelope) == nil && envelope.Error.Code == "engine_overloaded"
}

func attemptCause(err error) error {
	if failure, ok := provider.AsAttemptFailure(err); ok {
		return failure.Cause()
	}
	return err
}

type discovery struct {
	client      *http.Client
	credentials providersruntime.CredentialProvider
}

func (d discovery) ProbeTarget(ctx context.Context, target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	if strings.TrimSpace(target.CredentialRef) == "" || d.credentials == nil {
		return provider.TargetProbeResult{}, canonical.BadEndpoint("DeepInfra credential reference is required")
	}
	token, err := d.credentials.ResolveCredential(ctx, target.ProviderID(), target.CredentialRef)
	if err != nil || strings.TrimSpace(token) == "" {
		return provider.TargetProbeResult{}, canonical.BadEndpoint("DeepInfra credential reference could not be resolved")
	}
	client := d.client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.deepinfra.com/models/list", nil)
	if err != nil {
		return provider.TargetProbeResult{}, canonical.InternalError("DeepInfra catalog request could not be built")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	resp, err := client.Do(req)
	if err != nil {
		return provider.TargetProbeResult{}, canonical.BadEndpoint("DeepInfra catalog request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		_ = resp.Body.Close()
		return provider.TargetProbeResult{}, canonical.InternalError("DeepInfra catalog response content encoding is unsupported or invalid")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		return provider.TargetProbeResult{}, httpedge.ReadBackendHTTPError(resp, target.TargetID)
	}
	var rows []struct {
		ID           string `json:"id"`
		ModelName    string `json:"model_name"`
		ReportedType string `json:"reported_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return provider.TargetProbeResult{}, canonical.InternalError("DeepInfra catalog response could not be decoded")
	}
	deployments := make([]profile.ModelAuthoringOption, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.ReportedType) != "text-generation" {
			continue
		}
		id := strings.TrimSpace(row.ID)
		if id == "" {
			id = strings.TrimSpace(row.ModelName)
		}
		if id == "" {
			continue
		}
		deployments = append(deployments, profile.NewModelAuthoringOption(id, id, "DeepInfra", "", row.ReportedType, nil, ""))
	}
	return provider.TargetProbeResult{Options: deployments}, nil
}

var _ provider.Codec = failFastCodec{}
var _ provider.Transport = overloadTransport{}
var _ providersruntime.Discovery = discovery{}
