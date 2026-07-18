package target_config

import (
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func TestResolveProtocolOptions_ProviderManifestProductOrder(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    readmodel.ModelDeploymentReadModel
		want     []string
	}{
		{
			name:     "openai",
			provider: "openai",
			model:    readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1"},
			want: []string{
				"responses",
				"responses_stream",
				"chat_completions",
				"chat_completions_stream",
			},
		},
		{
			name:     "anthropic",
			provider: "anthropic",
			model:    readmodel.ModelDeploymentReadModel{ID: "claude-sonnet-4-5", ModelName: "claude-sonnet-4-5"},
			want:     []string{"messages", "messages_stream"},
		},
		{
			name:     "chatgpt",
			provider: "chatgpt",
			model:    readmodel.ModelDeploymentReadModel{ID: "chatgpt-4o-latest", ModelName: "chatgpt-4o-latest"},
			want:     []string{"responses_stream"},
		},
		{
			name:     "ollama",
			provider: "ollama",
			model:    readmodel.ModelDeploymentReadModel{ID: "llama3.2", ModelName: "llama3.2"},
			want:     []string{"chat_completions", "chat_completions_stream"},
		},
		{
			name:     "azure sparse catalog uses provider manifest",
			provider: "azure",
			model:    readmodel.ModelDeploymentReadModel{ID: "prod-claude-sonnet", ModelName: "prod-claude-sonnet"},
			want: []string{
				"responses",
				"responses_stream",
				"chat_completions",
				"chat_completions_stream",
				"messages",
				"messages_stream",
			},
		},
		{
			name:     "bedrock sparse catalog uses provider manifest",
			provider: "bedrock",
			model:    readmodel.ModelDeploymentReadModel{ID: "anthropic.claude-3-5-sonnet", ModelName: "anthropic.claude-3-5-sonnet"},
			want:     []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream", "messages", "messages_stream"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := protocolOptionIDs(resolveProtocolOptions(tt.provider, tt.model))
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("protocol options = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveProtocolOptions_DeploymentMetadataNarrows(t *testing.T) {
	got := protocolOptionIDs(resolveProtocolOptions("azure", readmodel.ModelDeploymentReadModel{
		ID:                         "Kimi-K2.6",
		ModelName:                  "Kimi-K2.6",
		ModelPublisher:             "MoonshotAI",
		Family:                     "openai",
		SupportedProviderProtocols: []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream"},
		DefaultProviderProtocol:    "responses",
	}))
	want := []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("protocol options = %v, want %v", got, want)
	}
}

func TestResolveProtocolOptions_DefaultProtocolNarrowsSparseReadModel(t *testing.T) {
	got := protocolOptionIDs(resolveProtocolOptions("openai", readmodel.ModelDeploymentReadModel{
		ID:                      "gpt-4.1",
		ModelName:               "gpt-4.1",
		DefaultProviderProtocol: "chat_completions",
	}))
	want := []string{"chat_completions"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("protocol options = %v, want %v", got, want)
	}
}

func TestResolveProtocolOptions_DeploymentMetadataCannotWidenProviderRules(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    readmodel.ModelDeploymentReadModel
		want     []string
	}{
		{
			name:     "chatgpt rejects catalog chat completions",
			provider: "chatgpt",
			model: readmodel.ModelDeploymentReadModel{
				ID:                         "chatgpt-4o-latest",
				ModelName:                  "chatgpt-4o-latest",
				SupportedProviderProtocols: []string{"chat_completions", "responses_stream"},
			},
			want: []string{"responses_stream"},
		},
		{
			name:     "ollama rejects catalog responses",
			provider: "ollama",
			model: readmodel.ModelDeploymentReadModel{
				ID:                         "llama3.2",
				ModelName:                  "llama3.2",
				SupportedProviderProtocols: []string{"responses_stream", "responses", "chat_completions"},
			},
			want: []string{"chat_completions"},
		},
		{
			name:     "anthropic rejects catalog openai protocols",
			provider: "anthropic",
			model: readmodel.ModelDeploymentReadModel{
				ID:                         "claude-sonnet-4-5",
				ModelName:                  "claude-sonnet-4-5",
				SupportedProviderProtocols: []string{"responses_stream", "messages"},
			},
			want: []string{"messages"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := protocolOptionIDs(resolveProtocolOptions(tt.provider, tt.model))
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("protocol options = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultProtocolForModelUsesResolvedDeploymentDefault(t *testing.T) {
	model := readmodel.ModelDeploymentReadModel{
		ID:                         "Kimi-K2.6",
		ModelName:                  "Kimi-K2.6",
		SupportedProviderProtocols: []string{"responses", "responses_stream", "chat_completions"},
		DefaultProviderProtocol:    "responses",
	}
	options := resolveProtocolOptions("azure", model)
	if got := defaultProtocolForModel("azure", model, options); got != "responses" {
		t.Fatalf("default protocol = %q, want responses", got)
	}
}

func TestResolveProtocolOptions_LabelsIncludeProtocolHints(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    readmodel.ModelDeploymentReadModel
		want     []string
	}{
		{
			name:     "openai uses provider first display labels",
			provider: "openai",
			model:    readmodel.ModelDeploymentReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1"},
			want: []string{
				"OpenAI · Responses",
				"OpenAI · Responses · stream",
				"OpenAI · Chat Completions",
				"OpenAI · Chat Completions · stream",
			},
		},
		{
			name:     "anthropic keeps its provider first display labels",
			provider: "anthropic",
			model:    readmodel.ModelDeploymentReadModel{ID: "claude-sonnet-4-5", ModelName: "claude-sonnet-4-5"},
			want:     []string{"Anthropic · Messages", "Anthropic · Messages · stream"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := protocolOptionLabels(resolveProtocolOptions(tt.provider, tt.model))
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("protocol labels = %v, want %v", got, tt.want)
			}
		})
	}
}

func protocolOptionIDs(options []protocolOption) []string {
	out := make([]string, len(options))
	for i, option := range options {
		out[i] = option.ID
	}
	return out
}

func protocolOptionLabels(options []protocolOption) []string {
	out := make([]string, len(options))
	for i, option := range options {
		out[i] = option.Label
	}
	return out
}
