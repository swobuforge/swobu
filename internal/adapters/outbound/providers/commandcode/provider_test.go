package commandcode

import (
	"strings"
	"testing"

	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestProjectModelSelectsExactCommandCodeProtocol(t *testing.T) {
	tests := []struct {
		name string
		row  string
		want []string
	}{
		{name: "Claude fallback", row: `{"id":"claude-opus-4-8"}`, want: []string{"messages", "messages_stream"}},
		{name: "metadata does not override model rule", row: `{"id":"opaque-chat","provider":"anthropic","protocol":"messages"}`, want: []string{"chat_completions", "chat_completions_stream"}},
		{name: "non Anthropic fallback", row: `{"id":"glm-5"}`, want: []string{"chat_completions", "chat_completions_stream"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows, err := modelcatalogopenai.DecodeModelRows(strings.NewReader(`{"data":[` + test.row + `]}`))
			if err != nil {
				t.Fatal(err)
			}
			option, include, err := projectModel(profile.ProviderSpecCommandCode, rows[0])
			if err != nil {
				t.Fatal(err)
			}
			if !include {
				t.Fatal("model was omitted")
			}
			if len(option.SupportedProviderProtocols) != len(test.want) || option.SupportedProviderProtocols[0] != test.want[0] || option.SupportedProviderProtocols[1] != test.want[1] {
				t.Fatalf("protocols = %v, want %v", option.SupportedProviderProtocols, test.want)
			}
		})
	}
}
