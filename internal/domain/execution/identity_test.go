package execution

import (
	"bytes"
	"testing"
)

func TestExchangeIDRejectsEntropyFailure(t *testing.T) {
	if _, err := newExchangeID(bytes.NewReader(nil)); err == nil {
		t.Fatal("expected entropy failure")
	}
}
func TestExchangeIDAndMixedInvocationOrder(t *testing.T) {
	id, err := newExchangeID(bytes.NewReader(bytes.Repeat([]byte{7}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewInvocationID(id, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewInvocationID(id, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first.String() == second.String() || first.Ordinal() != 1 || second.Ordinal() != 2 {
		t.Fatalf("ids = %q, %q", first, second)
	}
}
