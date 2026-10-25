package exchange

import (
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"testing"
)

func TestModelInvocationResourceCapturesFrozenConnection(t *testing.T) {
	route, _ := routing.ParseRouteName("primary")
	id, _ := connectionidentity.Parse("cn_shared")
	snapshot := provider.TargetSnapshot{TargetID: "target", TargetVersion: 7, ProviderSpec: "openai", Model: "model", ProtocolKind: protocolkind.Responses}
	for _, revision := range []uint64{1, 2} {
		bound, err := snapshot.BindConnection(id, revision)
		if err != nil {
			t.Fatal(err)
		}
		call := callProviderCommand{route: route}
		call.backend.Target = bound
		resource, err := modelInvocationResource(call)
		if err != nil {
			t.Fatal(err)
		}
		gotID, gotRevision, ok := resource.ConnectionIdentity()
		if !ok || gotID != id || gotRevision != revision || resource.Version() != 7 {
			t.Fatal("attempt evidence lost frozen identity")
		}
	}
}
