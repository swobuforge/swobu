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
		model    readmodel.ModelAuthoringOptionReadModel
		want     []string
	}{
		{
			name:     "openai",
			provider: "openai",
			model:    readmodel.ModelAuthoringOptionReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1"},
			want:     []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream"},
		},
		{
			name:     "anthropic",
			provider: "anthropic",
			model:    readmodel.ModelAuthoringOptionReadModel{ID: "claude-sonnet-4-5", ModelName: "claude-sonnet-4-5"},
			want:     []string{"messages", "messages_stream"},
		},
		{
			name:     "chatgpt",
			provider: "chatgpt",
			model:    readmodel.ModelAuthoringOptionReadModel{ID: "chatgpt-4o-latest", ModelName: "chatgpt-4o-latest"},
			want:     []string{"responses_stream"},
		},
		{
			name:     "ollama",
			provider: "ollama",
			model:    readmodel.ModelAuthoringOptionReadModel{ID: "llama3.2", ModelName: "llama3.2"},
			want:     []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream", "messages", "messages_stream"},
		},
		{
			name:     "lm studio",
			provider: "lmstudio",
			model:    readmodel.ModelAuthoringOptionReadModel{ID: "local-model", ModelName: "local-model"},
			want:     []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream", "messages", "messages_stream"},
		},
		{
			name:     "vllm",
			provider: "vllm",
			model:    readmodel.ModelAuthoringOptionReadModel{ID: "served-model", ModelName: "served-model"},
			want:     []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream", "messages", "messages_stream"},
		},
		{
			name:     "azure sparse catalog uses provider manifest",
			provider: "azure",
			model:    readmodel.ModelAuthoringOptionReadModel{ID: "prod-claude-sonnet", ModelName: "prod-claude-sonnet"},
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
			model:    readmodel.ModelAuthoringOptionReadModel{ID: "anthropic.claude-3-5-sonnet", ModelName: "anthropic.claude-3-5-sonnet"},
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
	got := protocolOptionIDs(resolveProtocolOptions("azure", readmodel.ModelAuthoringOptionReadModel{
		ID:                         "Kimi-K2.6",
		ModelName:                  "Kimi-K2.6",
		ModelPublisher:             "MoonshotAI",
		Family:                     "openai",
		SupportedProviderProtocols: []string{"responses", "chat_completions"},
		DefaultProviderProtocol:    "responses",
	}))
	want := []string{"responses", "chat_completions"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("protocol options = %v, want %v", got, want)
	}
}

func TestResolveProtocolOptions_DefaultProtocolDoesNotNarrowSparseReadModel(t *testing.T) {
	got := protocolOptionIDs(resolveProtocolOptions("openai", readmodel.ModelAuthoringOptionReadModel{
		ID:                      "gpt-4.1",
		ModelName:               "gpt-4.1",
		DefaultProviderProtocol: "chat_completions",
	}))
	want := []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("protocol options = %v, want %v", got, want)
	}
}

func TestResolveProtocolOptions_DeploymentMetadataCannotWidenProviderRules(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    readmodel.ModelAuthoringOptionReadModel
		want     []string
	}{
		{
			name:     "chatgpt rejects catalog chat completions",
			provider: "chatgpt",
			model: readmodel.ModelAuthoringOptionReadModel{
				ID:                         "chatgpt-4o-latest",
				ModelName:                  "chatgpt-4o-latest",
				SupportedProviderProtocols: []string{"chat_completions", "responses"},
			},
			want: []string{"responses_stream"},
		},
		{
			name:     "ollama intersects catalog protocols",
			provider: "ollama",
			model: readmodel.ModelAuthoringOptionReadModel{
				ID:                         "llama3.2",
				ModelName:                  "llama3.2",
				SupportedProviderProtocols: []string{"responses", "chat_completions"},
			},
			want: []string{"responses", "chat_completions"},
		},
		{
			name:     "anthropic rejects catalog openai protocols",
			provider: "anthropic",
			model: readmodel.ModelAuthoringOptionReadModel{
				ID:                         "claude-sonnet-4-5",
				ModelName:                  "claude-sonnet-4-5",
				SupportedProviderProtocols: []string{"responses", "messages"},
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

func TestResolveProtocolOptions_LabelsIncludeProtocolHints(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    readmodel.ModelAuthoringOptionReadModel
		want     []string
	}{
		{
			name:     "openai uses provider first display labels",
			provider: "openai",
			model:    readmodel.ModelAuthoringOptionReadModel{ID: "gpt-4.1", ModelName: "gpt-4.1"},
			want: []string{
				"OpenAI · Responses · buffered",
				"OpenAI · Responses · streaming",
				"OpenAI · Chat Completions · buffered",
				"OpenAI · Chat Completions · streaming",
			},
		},
		{
			name:     "anthropic keeps its provider first display labels",
			provider: "anthropic",
			model:    readmodel.ModelAuthoringOptionReadModel{ID: "claude-sonnet-4-5", ModelName: "claude-sonnet-4-5"},
			want:     []string{"Anthropic · Messages · buffered", "Anthropic · Messages · streaming"},
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
