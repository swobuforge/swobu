package workspaces

import (
	"encoding/json"
	"reflect"
	"runtime"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestConnectionDraftConversionsAreStructuralAndIndependent(t *testing.T) {
	tests := []routing.ConnectionDraft{
		{Provider: "openai", Standard: &routing.StandardConnectionDraft{Locator: "https://example.test/v1", Credential: "file:/foreign/path"}},
		{Provider: "zai", ZAI: &routing.ZAIConnectionDraft{Access: "coding_plan", Credential: "env:ZAI_KEY"}},
		{Provider: "bedrock", Bedrock: &routing.BedrockConnectionDraft{Region: "eu-west-2", Endpoint: "https://example.test/v1", Credential: "env:BEDROCK_KEY"}},
		{Provider: "custom", Custom: &routing.CustomConnectionDraft{BaseURL: "https://example.test/v1", Header: &routing.CustomHeaderDraft{Name: "X-Key", Credential: "env:CUSTOM_KEY"}}},
	}
	for _, input := range tests {
		t.Run(input.Provider, func(t *testing.T) {
			want := cloneTestConnectionDraft(input)
			document := ConnectionFromDraft(input)
			mutateTestConnectionDraft(input)
			first := document.Draft()
			if !reflect.DeepEqual(first, want) {
				t.Fatalf("draft = %#v, want %#v", first, want)
			}
			mutateTestConnectionDraft(first)
			if got := document.Draft(); !reflect.DeepEqual(got, want) {
				t.Fatalf("connection aliased returned draft: %#v, want %#v", got, want)
			}
		})
	}
}

func TestOperatorConnectionCodecTreatsForeignFileCredentialAsOpaque(t *testing.T) {
	foreign := `file:C:\Users\operator\key`
	if runtime.GOOS == "windows" {
		foreign = "file:/home/operator/key"
	}
	original := ConnectionFromDraft(routing.ConnectionDraft{
		Provider: "gemini",
		Standard: &routing.StandardConnectionDraft{Credential: foreign},
	})
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Connection
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	draft := decoded.Draft()
	if draft.Standard == nil || draft.Standard.Credential != foreign {
		t.Fatalf("credential = %#v, want %q", draft.Standard, foreign)
	}
}

func cloneTestConnectionDraft(d routing.ConnectionDraft) routing.ConnectionDraft {
	return ConnectionFromDraft(d).Draft()
}

func mutateTestConnectionDraft(d routing.ConnectionDraft) {
	d.Provider = "changed"
	if d.Standard != nil {
		d.Standard.Locator, d.Standard.Credential = "changed", "changed"
	}
	if d.ZAI != nil {
		d.ZAI.Access, d.ZAI.Credential = "changed", "changed"
	}
	if d.Bedrock != nil {
		d.Bedrock.Region, d.Bedrock.Endpoint, d.Bedrock.Credential = "changed", "changed", "changed"
	}
	if d.Custom != nil {
		d.Custom.BaseURL = "changed"
		if d.Custom.Header != nil {
			d.Custom.Header.Name, d.Custom.Header.Credential = "changed", "changed"
		}
	}
}
