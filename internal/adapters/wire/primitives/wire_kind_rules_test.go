package core

import "testing"

func TestWireKindSupportMatrix(t *testing.T) {
	cases := []struct {
		kind WireKind
		op   WireOperation
		ok   bool
	}{
		{WireKindRequest, WireOpEncode, true},
		{WireKindRequest, WireOpDecode, false},
		{WireKindResponse, WireOpDecode, true},
		{WireKindResponse, WireOpEncode, false},
		{WireKindResponseStream, WireOpDecode, true},
		{WireKindUsage, WireOpDecode, true},
		{WireKindError, WireOpDecode, true},
		{WireKindUnknown, WireOpDecode, false},
	}
	for _, tc := range cases {
		if got := tc.kind.Supports(tc.op); got != tc.ok {
			t.Fatalf("kind=%q op=%q supports=%v want=%v", tc.kind, tc.op, got, tc.ok)
		}
	}
}

func TestWirePacketValidateFor(t *testing.T) {
	if err := (WirePacket{Kind: WireKindRequest}).ValidateFor(WireOpEncode); err != nil {
		t.Fatalf("request encode should validate: %v", err)
	}
	if err := (WirePacket{Kind: WireKindRequest}).ValidateFor(WireOpDecode); err == nil {
		t.Fatalf("request decode must fail")
	}
	if err := (WirePacket{Kind: WireKindUnknown}).ValidateFor(WireOpPatch); err == nil {
		t.Fatalf("unknown kind must fail")
	}
}
