package accessstate

import (
	"github.com/swobuforge/swobu/internal/routing"
	"testing"
)

func TestResolutionAudienceCannotPromoteWorkspaceToNode(t *testing.T) {
	research, err := routing.ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	audience := (Resolution{Workspace: research}).Audience()
	if !audience.AllowsWorkspace("research") || audience.AllowsWorkspace("other") || audience.AllowsNodeAdministration() {
		t.Fatal("credential audience widened beyond issued Workspace")
	}
	if (Resolution{}).Audience().AllowsWorkspace("") || (Resolution{}).Audience().AllowsNodeAdministration() {
		t.Fatal("zero resolution grants authority")
	}
}
