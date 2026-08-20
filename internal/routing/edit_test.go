package routing

import "testing"

func TestRenameDefaultRoutePreservesDefaultIdentity(t *testing.T) {
	config := testConfig(t, testTarget(t, "a"))
	slug, _ := ParseWorkspaceSlug("dev")
	from, _ := ParseRouteName("chat")
	to, _ := ParseRouteName("renamed")
	next, err := config.RenameRoute(slug, from, to)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := next.Workspace(slug)
	if workspace.DefaultRoute() != to {
		t.Fatalf("default route = %q, want %q", workspace.DefaultRoute().String(), to.String())
	}
}

func TestTargetRejectsProtocolFromDifferentProvider(t *testing.T) {
	id, _ := ParseTargetID("a")
	model, _ := ParseUpstreamModel("model")
	openAI, _ := ParseProvider("openai", func(string) bool { return true })
	anthropic, _ := ParseProvider("anthropic", func(string) bool { return true })
	protocol, _ := ParseProtocol("responses", openAI, func(Provider, string) bool { return true })
	connection, _ := NewStandardConnection(anthropic, "", "env:KEY")
	if _, err := NewTarget(id, model, protocol, connection); err == nil {
		t.Fatal("provider/protocol contradiction unexpectedly succeeded")
	}
}
