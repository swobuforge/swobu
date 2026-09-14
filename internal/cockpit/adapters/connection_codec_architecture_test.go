package adapters

import (
	"os"
	"strings"
	"testing"
)

func TestCockpitDelegatesCredentialPersistenceToDaemon(t *testing.T) {
	content, err := os.ReadFile("live_operator.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	for _, forbidden := range []string{
		"adapters/outbound/credentials",
		"ResolveAuthCredentialWritePolicy",
		"StoreMaterializedCredential",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("Cockpit adapter owns daemon credential persistence through %q", forbidden)
		}
	}
	if !strings.Contains(source, "a.client.StorePastedCredential") {
		t.Fatal("Cockpit adapter does not delegate pasted credentials to the daemon operator client")
	}
}
