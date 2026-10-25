package execution

import (
	"bytes"
	"strings"
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

func TestPersistedExecutionIdentitiesRoundTripAndRejectMalformedValues(t *testing.T) {
	exchange, err := NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	parsedExchange, err := ParseExchangeID(exchange.String())
	if err != nil || parsedExchange != exchange {
		t.Fatalf("exchange parse = %#v, %v", parsedExchange, err)
	}
	invocation, err := NewInvocationID(exchange, 7)
	if err != nil {
		t.Fatal(err)
	}
	parsedInvocation, err := ParseInvocationID(invocation.String())
	if err != nil || parsedInvocation != invocation {
		t.Fatalf("invocation parse = %#v, %v", parsedInvocation, err)
	}
	for _, raw := range []string{"", "ex_bad", "ex_", "inv_bad", "inv_bad_0", "inv_" + exchange.String()[3:] + "_x", "inv_" + exchange.String()[3:] + "_1_extra"} {
		if strings.HasPrefix(raw, "inv_") {
			if _, err := ParseInvocationID(raw); err == nil {
				t.Fatalf("accepted malformed invocation %q", raw)
			}
		} else if _, err := ParseExchangeID(raw); err == nil {
			t.Fatalf("accepted malformed exchange %q", raw)
		}
	}
}
