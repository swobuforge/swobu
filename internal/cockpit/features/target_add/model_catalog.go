package target_add

import (
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// ---------------------------------------------------------------------------
// Static model catalogs — Tier 1-2 providers (RFC §5)
//
// These are hardcoded snapshots of known models shipped with swobucli.
// The operator client does not yet expose a model-catalog endpoint at the
// daemon level, so cockpit relies on static projection for the add-target
// workflow.
//
// When the daemon gains catalog probing, replace StaticCatalogRegistry with
// the ProbeProviderModels port.
// ---------------------------------------------------------------------------

// staticCatalogs maps provider spec → known deployments.
// Each entry is a curated snapshot (name, model-name, protocols).
var staticCatalogs = map[string][]readmodel.ModelDeploymentReadModel{
	"openai": {
		{ID: "gpt-4.1", Name: "GPT-4.1", ModelName: "gpt-4.1", DefaultProviderProtocol: "chat_completions"},
		{ID: "gpt-4.1-mini", Name: "GPT-4.1 Mini", ModelName: "gpt-4.1-mini", DefaultProviderProtocol: "chat_completions"},
		{ID: "gpt-4o", Name: "GPT-4o", ModelName: "gpt-4o", DefaultProviderProtocol: "chat_completions"},
		{ID: "gpt-4o-mini", Name: "GPT-4o Mini", ModelName: "gpt-4o-mini", DefaultProviderProtocol: "chat_completions"},
		{ID: "o3", Name: "o3", ModelName: "o3", DefaultProviderProtocol: "chat_completions"},
		{ID: "o4-mini", Name: "o4-mini", ModelName: "o4-mini", DefaultProviderProtocol: "chat_completions"},
	},
	"anthropic": {
		{ID: "claude-opus-4", Name: "Claude Opus 4", ModelName: "claude-opus-4", DefaultProviderProtocol: "messages"},
		{ID: "claude-sonnet-4", Name: "Claude Sonnet 4", ModelName: "claude-sonnet-4", DefaultProviderProtocol: "messages"},
		{ID: "claude-haiku-4", Name: "Claude Haiku 4", ModelName: "claude-haiku-4", DefaultProviderProtocol: "messages"},
		{ID: "claude-3-7-sonnet", Name: "Claude 3.7 Sonnet", ModelName: "claude-3-7-sonnet-latest", DefaultProviderProtocol: "messages"},
		{ID: "claude-3-5-haiku", Name: "Claude 3.5 Haiku", ModelName: "claude-3-5-haiku-latest", DefaultProviderProtocol: "messages"},
	},
	"openrouter": {
		{ID: "openai/gpt-4o", Name: "OpenAI GPT-4o (OR)", ModelName: "openai/gpt-4o", DefaultProviderProtocol: "chat_completions"},
		{ID: "anthropic/claude-3.7-sonnet", Name: "Claude 3.7 Sonnet (OR)", ModelName: "anthropic/claude-3.7-sonnet", DefaultProviderProtocol: "chat_completions"},
		{ID: "meta-llama/llama-4-maverick", Name: "Llama 4 Maverick (OR)", ModelName: "meta-llama/llama-4-maverick", DefaultProviderProtocol: "chat_completions"},
		{ID: "google/gemini-2.5-pro", Name: "Gemini 2.5 Pro (OR)", ModelName: "google/gemini-2.5-pro", DefaultProviderProtocol: "chat_completions"},
		{ID: "deepseek/deepseek-chat", Name: "DeepSeek V3 (OR)", ModelName: "deepseek/deepseek-chat", DefaultProviderProtocol: "chat_completions"},
	},
	"chatgpt": {
		{ID: "chatgpt-4o-latest", Name: "ChatGPT 4o", ModelName: "chatgpt-4o-latest", DefaultProviderProtocol: "responses_stream"},
		{ID: "o1-pro", Name: "ChatGPT o1 Pro", ModelName: "o1-pro", DefaultProviderProtocol: "responses_stream"},
	},
	"ollama": {
		{ID: "llama3.2", Name: "Llama 3.2 3B", ModelName: "llama3.2", DefaultProviderProtocol: "chat_completions"},
		{ID: "qwen2.5:72b", Name: "Qwen 2.5 72B", ModelName: "qwen2.5:72b", DefaultProviderProtocol: "chat_completions"},
		{ID: "deepseek-r1:32b", Name: "DeepSeek R1 32B", ModelName: "deepseek-r1:32b", DefaultProviderProtocol: "chat_completions"},
	},
}

// staticCatalogFallback returns the static deployments for a provider, if
// known. If the provider has no static catalog, the caller may fall back to
// a manual-entry flow (RFC §5 escape-hatch).
func staticCatalogFallback(providerSpec string) []readmodel.ModelDeploymentReadModel {
	deployments, ok := staticCatalogs[providerSpec]
	if !ok {
		return nil
	}
	out := make([]readmodel.ModelDeploymentReadModel, len(deployments))
	copy(out, deployments)
	return out
}

// staticCatalogProviderSpecs returns the list of provider specs that have
// static catalogs. Used by tests and by the section to determine whether
// a provider supports catalog selection.
func staticCatalogProviderSpecs() []string {
	out := make([]string, 0, len(staticCatalogs))
	for spec := range staticCatalogs {
		out = append(out, spec)
	}
	return out
}
