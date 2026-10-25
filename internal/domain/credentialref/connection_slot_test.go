package credentialref

import "testing"

func TestConnectionSlotRequiresExactOwnerAndGeneratedIdentity(t *testing.T) {
	owner, _ := NewConnectionScope("cn_owner")
	foreign, _ := NewConnectionScope("cn_other")
	provider, _ := NewProviderScope("openai")
	name := "connections/cn_owner/cred_0123456789abcdef0123456789abcdef"
	for _, prefix := range []string{"secret:", "secretfile:"} {
		ref := Parse(prefix + name)
		if slot, ok := ConnectionSlot(owner, ref); !ok || slot != name {
			t.Fatal("owned slot rejected")
		}
		for _, scope := range []Scope{foreign, provider, {}} {
			if _, ok := ConnectionSlot(scope, ref); ok {
				t.Fatal("foreign scope accepted")
			}
		}
	}
	for _, ref := range []string{"env:FIXTURE", "secret:" + name + "0", "secret:" + name[:len(name)-1], "secret:connections/cn_owner/cred_0123456789ABCDEF0123456789ABCDEF", "secret:connections/cn_owner/cred_../../escape"} {
		if _, ok := ConnectionSlot(owner, Parse(ref)); ok {
			t.Fatal("invalid generated slot accepted")
		}
	}
}
