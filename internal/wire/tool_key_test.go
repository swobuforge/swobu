package wire

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type toolNamesStub struct {
	byKindAndName map[struct {
		kind canonical.ToolKind
		name string
	}]canonical.ToolKey
}

func (s toolNamesStub) WireName(canonical.ToolKey) (string, error) { return "", nil }

func (s toolNamesStub) CanonicalKey(kind canonical.ToolKind, name string) (canonical.ToolKey, bool) {
	key, ok := s.byKindAndName[struct {
		kind canonical.ToolKind
		name string
	}{kind: kind, name: name}]
	return key, ok
}

func TestDecodeToolKeyRejectsProviderNameAbsentFromAttemptDictionary(t *testing.T) {
	_, err := DecodeToolKey(toolNamesStub{}, canonical.ToolEnvironment{}, canonical.ToolKindFunction, "unknown")
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("error = %T %v, want backend error", err, err)
	}
}

func TestDecodeToolKeyRejectsHistoricalAliasWhenDeclarationIsNoLongerEffective(t *testing.T) {
	historical, _ := canonical.NewToolKey("history/tools", canonical.ToolKindFunction, "lookup")
	names := toolNamesStub{byKindAndName: map[struct {
		kind canonical.ToolKind
		name string
	}]canonical.ToolKey{
		{kind: canonical.ToolKindFunction, name: "s__history__lookup"}: historical,
	}}
	current := canonicaltest.MustFunctionTool(
		canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "current"),
		"", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool](),
	)
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{current})
	item, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	environment, err := canonical.EffectiveTools(canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item}}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecodeToolKey(names, environment, canonical.ToolKindFunction, "s__history__lookup")
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("error = %T %v, want backend error", err, err)
	}
}
