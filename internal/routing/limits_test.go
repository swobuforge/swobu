package routing

import "testing"

func TestServiceLimitsHaveOneWorkspaceOwner(t *testing.T) {
	slug, _ := ParseWorkspaceSlug("engineering")
	c, _ := NewConfig(nil)
	c, err := c.CreateEmptyWorkspace(EmptyWorkspaceSeed{Slug: slug})
	if err != nil {
		t.Fatal(err)
	}
	desired := ServiceLimits{Models: ServiceQuota{RequestsPerMinute: 100, Concurrent: 4}, MCP: ServiceQuota{RequestsPerMinute: 20}, Search: ServiceQuota{RequestsPerMinute: 10}}
	modified, err := c.SetServiceLimits(slug, desired)
	if err != nil {
		t.Fatal(err)
	}
	w, _ := modified.Workspace(slug)
	if w.ServiceLimits() != desired {
		t.Fatalf("round trip: %+v", w.ServiceLimits())
	}
	original, _ := c.Workspace(slug)
	if original.ServiceLimits() != (ServiceLimits{}) {
		t.Fatal("editing changed original immutable snapshot")
	}
	for _, bad := range []ServiceLimits{
		{Models: ServiceQuota{RequestsPerMinute: 100001}},
		{Models: ServiceQuota{Concurrent: 10001}},
		{MCP: ServiceQuota{Concurrent: 1}},
		{Search: ServiceQuota{Concurrent: 1}},
	} {
		if _, err := c.SetServiceLimits(slug, bad); err == nil {
			t.Fatalf("accepted impossible Service limit %+v", bad)
		}
	}
}
