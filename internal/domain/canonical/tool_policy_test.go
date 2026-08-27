package canonical

import "testing"

func TestToolPolicyPermitsExactDeclaration(t *testing.T) {
	search := WebSearchToolKey()
	other, err := NewRequestToolKey(ToolKindFunction, "lookup")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		policy ToolPolicy
		want   bool
	}{
		{name: "none", policy: NewToolPolicy(ToolPolicyNone, nil)},
		{name: "auto", policy: NewToolPolicy(ToolPolicyAuto, nil), want: true},
		{name: "required", policy: NewToolPolicy(ToolPolicyRequired, nil), want: true},
		{name: "specific search", policy: NewToolPolicy(ToolPolicySpecific, &search), want: true},
		{name: "specific other", policy: NewToolPolicy(ToolPolicySpecific, &other)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.policy.Permits(search); got != test.want {
				t.Fatalf("Permits(web_search) = %v, want %v", got, test.want)
			}
		})
	}
}
