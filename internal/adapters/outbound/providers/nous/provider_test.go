package nous

import (
	"strings"
	"testing"

	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestProjectModelAuthorsNousAsChatOnly(t *testing.T) {
	rows, err := modelcatalogopenai.DecodeModelRows(strings.NewReader(`{"data":[{"id":"poolside/laguna-s-2.1:free"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	option, include, err := projectModel(profile.ProviderSpecNous, rows[0])
	if err != nil || !include {
		t.Fatalf("include=%v err=%v", include, err)
	}
	if option.DefaultProviderProtocol != "chat_completions" || len(option.SupportedProviderProtocols) != 2 {
		t.Fatalf("Nous option = %#v", option)
	}
	for _, protocol := range option.SupportedProviderProtocols {
		if protocol != "chat_completions" && protocol != "chat_completions_stream" {
			t.Fatalf("Nous exposed non-Chat protocol %q", protocol)
		}
	}
}
