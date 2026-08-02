package toolname

import (
	"strings"
	"testing"
)

func TestGeneratedIsStableBoundedAndIdentitySensitive(t *testing.T) {
	identity := "tool:v1/crm/github/issues/function/" + strings.Repeat("create_issue", 8)
	first := Generated(identity, []string{"github", "issues"}, strings.Repeat("create_issue", 8))
	second := Generated(identity, []string{"github", "issues"}, strings.Repeat("create_issue", 8))
	if first != second || len(first) > MaxLength || !Safe(first) || !strings.HasPrefix(first, GeneratedPrefix) {
		t.Fatalf("generated names = %q and %q", first, second)
	}
	if first == Generated("other/"+identity, []string{"github", "issues"}, strings.Repeat("create_issue", 8)) {
		t.Fatal("generated digest omitted complete identity")
	}
}

func TestGeneratedDistinguishesNormalizedReadableCollision(t *testing.T) {
	left := Generated("tool:v1/a-b/function/same", []string{"a-b"}, "same")
	right := Generated("tool:v1/a_b/function/same", []string{"a_b"}, "same")
	if left == right {
		t.Fatalf("normalized collision produced %q", left)
	}
}

func TestPreservableLiteralReservesGeneratedPrefix(t *testing.T) {
	if !PreservableLiteral("lookup") {
		t.Fatal("safe literal was not preservable")
	}
	if PreservableLiteral("s__lookup") {
		t.Fatal("generated prefix was not reserved")
	}
	if PreservableLiteral("look up") {
		t.Fatal("unsafe literal was preservable")
	}
}
