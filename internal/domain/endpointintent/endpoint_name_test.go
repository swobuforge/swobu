package endpointintent

import (
	"errors"
	"testing"
)

func TestEndpointName_AcceptsCanonicalLowercaseDigitsAndDashes(t *testing.T) {
	name, err := ParseEndpointName("alpha-1")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	if got, want := name.String(), "alpha-1"; got != want {
		t.Fatalf("name.String() = %q, want %q", got, want)
	}
}

func TestEndpointName_RejectsUppercase(t *testing.T) {
	_, err := ParseEndpointName("Alpha")
	if !errors.Is(err, ErrInvalidEndpointName) {
		t.Fatalf("expected ErrInvalidEndpointName, got %v", err)
	}
}

func TestEndpointName_RejectsImplicitDefaultAndUnderscoreAlias(t *testing.T) {
	for _, raw := range []string{"default", "_"} {
		t.Run(raw, func(t *testing.T) {
			_, err := ParseEndpointName(raw)
			if !errors.Is(err, ErrInvalidEndpointName) {
				t.Fatalf("expected ErrInvalidEndpointName, got %v", err)
			}
		})
	}
}
