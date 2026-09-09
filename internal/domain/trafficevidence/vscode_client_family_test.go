package trafficevidence

import "testing"

func TestClassifyVSCodeClientFamily(t *testing.T) {
	if got := ClassifyClientFamily("swobu-vscode/0.1.0"); got != ClientFamilyVSCode {
		t.Fatalf("ClassifyClientFamily() = %q, want %q", got, ClientFamilyVSCode)
	}
}
