package connectionidentity

import "testing"

func TestIdentityGrammarAndGeneratedReferences(t *testing.T) {
	for _, raw := range []string{"legacy-mcp", "cn_0123", "0", "a_b-c"} {
		id, err := Parse(raw)
		if err != nil || id.String() != raw {
			t.Fatalf("identity changed: %q %v", raw, err)
		}
	}
	for _, raw := range []string{"", "Upper", " a", "a ", "a/b", "a.b", "é", "a\n"} {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("invalid reference accepted: %q", raw)
		}
	}
	seen := map[ID]bool{}
	for range 128 {
		id, err := New()
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := Parse(id.String())
		if err != nil || parsed != id || seen[id] || len(id.String()) != 35 {
			t.Fatal("invalid or duplicate generated reference")
		}
		seen[id] = true
	}
	if (ID{}).String() != "" {
		t.Fatal("zero ID acquired identity")
	}
}
