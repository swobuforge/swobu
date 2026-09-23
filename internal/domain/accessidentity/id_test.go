package accessidentity

import "testing"

func TestBuiltInAndManagedAccessIDs(t *testing.T) {
	if Local.String() != "local" || Share.String() != "share" {
		t.Fatalf("built-ins = %q, %q", Local, Share)
	}
	managed := NewManaged([16]byte{1, 2, 3})
	parsed, err := Parse(managed.String())
	if err != nil || parsed != managed || !parsed.Managed() {
		t.Fatalf("Parse(%q) = %v, %v", managed, parsed, err)
	}
}

func TestAccessIDRejectsEmptyMalformedAndReservedManagedRestore(t *testing.T) {
	for _, raw := range []string{"", "anonymous", "acc_", "acc_not+base64"} {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("Parse(%q) unexpectedly succeeded", raw)
		}
	}
	for _, raw := range []string{"local", "share"} {
		if _, err := RestoreManaged(raw); err == nil {
			t.Fatalf("RestoreManaged(%q) unexpectedly succeeded", raw)
		}
	}
}
