// Package providertest provides checked provider-attempt fixtures.
package providertest

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

func ProjectedToolName(t testing.TB, declaration canonical.ToolDeclaration) string {
	t.Helper()
	tools, err := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item}})
	projected, _, _, err := provider.ProjectAttemptTools(request)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := canonical.ToolEnvironmentAt(projected.Items(), len(projected.Items()))
	if err != nil {
		t.Fatal(err)
	}
	return environment.Declarations()[0].Key().Name()
}
