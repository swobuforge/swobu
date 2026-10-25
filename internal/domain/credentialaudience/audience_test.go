package credentialaudience

import "testing"

func TestAudienceIsClosedAndScopeBound(t *testing.T) {
	for _, test := range []struct {
		audience  Audience
		workspace bool
		node      bool
	}{
		{Audience{}, false, false}, {Workspace(""), false, false},
		{Workspace("research"), true, false}, {Workspace("other"), false, false},
		{Node(), false, true},
	} {
		if test.audience.AllowsWorkspace("research") != test.workspace || test.audience.AllowsNodeAdministration() != test.node {
			t.Fatalf("audience=%+v", test.audience)
		}
		if test.audience.AllowsWorkspace("") {
			t.Fatal("empty Workspace admits credential")
		}
	}
}
