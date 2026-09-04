package opencodezen

import (
	"errors"
	"net/http"
	"strings"

	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/thread"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	policy := openaifamily.BearerWithMessagesAPIKeyPolicy(profile.ProviderSpecOpenCodeZen).WithModelCatalogProject(projectModel)
	runtime := openaifamily.NewRuntime(client, credentials, policy)
	runtime.BackendResolver = openCodeBackendResolver{base: runtime.BackendResolver}
	return runtime
}

type openCodeBackendResolver struct {
	base provider.BackendResolver
}

func (r openCodeBackendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.base.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	codec, ok := backend.Codec.(protocolcodec.Codec)
	if !ok {
		return provider.Backend{}, errors.New("OpenCode backend requires protocol codec")
	}
	codec.ProjectRequestHeaders = projectOpenCodeRequestHeaders
	backend.Codec = codec
	return backend, backend.Validate()
}

func projectOpenCodeRequestHeaders(attempt provider.AttemptContext, header http.Header) error {
	if attempt.ThreadID.IsZero() {
		return errors.New("OpenCode request requires thread identity")
	}
	value, err := thread.Project("provider/opencode-session/v1", attempt.ThreadID)
	if err != nil {
		return err
	}
	header.Set("X-Opencode-Session", value)
	return nil
}

func projectModel(providerID profile.ProviderID, row modelcatalogopenai.ModelRow) (profile.ModelAuthoringOption, bool, error) {
	protocol, ok, err := zenProtocol(row)
	if err != nil || !ok {
		return profile.ModelAuthoringOption{}, false, err
	}
	return profile.NewModelAuthoringOption(row.ID(), row.ID(), string(providerID), "", "", protocolContracts(protocol), protocol), true, nil
}

func zenProtocol(row modelcatalogopenai.ModelRow) (string, bool, error) {
	id := strings.ToLower(strings.TrimSpace(row.ID()))
	switch {
	case id == "x-preview-f-free",
		strings.HasPrefix(id, "deepseek-"),
		strings.HasPrefix(id, "glm-"),
		strings.HasPrefix(id, "minimax-"),
		strings.HasPrefix(id, "kimi-"),
		strings.HasPrefix(id, "mimo-"),
		strings.HasPrefix(id, "hy3-"),
		strings.HasPrefix(id, "nemotron-"),
		strings.HasPrefix(id, "big-pickle"):
		return "chat_completions", true, nil
	case strings.HasPrefix(id, "gpt-"), strings.HasPrefix(id, "grok-"), strings.HasPrefix(id, "muse-"):
		return "responses", true, nil
	case strings.HasPrefix(id, "claude-"), strings.HasPrefix(id, "qwen"):
		return "messages", true, nil
	default:
		return "", false, nil
	}
}

func protocolContracts(buffered string) []string {
	return []string{buffered, buffered + "_stream"}
}
