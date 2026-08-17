package trafficevidence

import "testing"

func TestReusablePrefixEvidenceClosedStates(t *testing.T) {
	changed, err := NewChangedReusablePrefix(ReusablePrefixTool)
	if err != nil || changed.State() != ReusablePrefixChanged {
		t.Fatalf("changed = %#v, %v", changed, err)
	}
	if kind, ok := changed.ChangeKind(); !ok || kind != ReusablePrefixTool {
		t.Fatalf("change kind = %q, %t", kind, ok)
	}
	for _, evidence := range []ReusablePrefixEvidence{UnknownReusablePrefix(), PreservedReusablePrefix(), NativeReusablePrefix()} {
		if _, ok := evidence.ChangeKind(); ok {
			t.Fatalf("state %q carried a change kind", evidence.State())
		}
	}
	if UnknownReusablePrefix().State() != ReusablePrefixUnknown || PreservedReusablePrefix().State() != ReusablePrefixPreserved || NativeReusablePrefix().State() != ReusablePrefixNative {
		t.Fatal("closed states changed")
	}
}

func TestChangedReusablePrefixRejectsInvalidKind(t *testing.T) {
	if _, err := NewChangedReusablePrefix(""); err == nil {
		t.Fatal("empty change kind accepted")
	}
}
