package modelcatalog

import (
	"context"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
)

// PreviewRequest captures one first-run provider draft route for model-catalog
// preview.
type PreviewRequest struct {
	ProviderSpec  string
	BaseURL       string
	CredentialRef string
	ProtocolKind  string
}

// PreviewSnapshot is the provider-backed model catalog result for one draft
// route. Error stays local so callers can render an anchored inline message.
type PreviewSnapshot struct {
	ModelIDs []string `json:"model_ids,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// PreviewLoader owns model-catalog preview read for first-run draft routing.
type PreviewLoader struct {
	providers ports.ProviderModelCatalog
}

func NewPreviewLoader(providers ports.ProviderModelCatalog) PreviewLoader {
	return PreviewLoader{providers: providers}
}

func (l PreviewLoader) Load(ctx context.Context, req PreviewRequest) (PreviewSnapshot, error) {
	if l.providers == nil {
		return PreviewSnapshot{}, compatibility.InternalError("provider model catalog is not configured")
	}
	spec := strings.TrimSpace(strings.ToLower(req.ProviderSpec))
	if spec == "" {
		return PreviewSnapshot{}, compatibility.BadEndpoint("provider spec is required")
	}
	kind := protocolsurface.ChatCompletions
	if rawKind := strings.TrimSpace(req.ProtocolKind); rawKind != "" {
		parsed, err := protocolsurface.Parse(rawKind)
		if err != nil {
			return PreviewSnapshot{}, compatibility.BadEndpoint(err.Error())
		}
		kind = parsed
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(providercatalog.DefaultBaseURL(spec))
	}
	credentialRef := strings.TrimSpace(req.CredentialRef)
	models, errText := probeRouteModels(ctx, l.providers, modelCatalogProbeInput{
		ProviderConfigRef: "draft",
		ProviderSpec:      spec,
		BaseURL:           baseURL,
		CredentialRef:     credentialRef,
		ProtocolKind:      kind,
	})
	if errText != nil {
		return PreviewSnapshot{Error: normalizePreviewError(errText.Error(), credentialRef)}, nil
	}
	return PreviewSnapshot{ModelIDs: models}, nil
}

func normalizePreviewError(message string, credentialRef string) string {
	message = strings.TrimSpace(message)
	if !strings.Contains(strings.ToLower(message), "credential reference could not be resolved") {
		return message
	}
	if !isFileCredentialRef(credentialRef) {
		return message
	}
	return "BAD_ENDPOINT: credential file could not be resolved (check file path, read permission, and non-empty token)"
}

func isFileCredentialRef(credentialRef string) bool {
	ref := strings.TrimSpace(strings.ToLower(credentialRef))
	return ref == "file" ||
		ref == "file:" ||
		strings.HasPrefix(ref, "file:") ||
		strings.HasPrefix(ref, "/") ||
		strings.HasPrefix(ref, "~/")
}
