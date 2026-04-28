package endpointintent

import (
	"fmt"
)

type EndpointName struct {
	value string
}

// ParseEndpointName enforces the public endpoint naming contract.
// It rejects implicit aliases instead of coercing them.
func ParseEndpointName(raw string) (EndpointName, error) {
	if raw == "" {
		return EndpointName{}, fmt.Errorf("%w: name must not be empty", ErrInvalidEndpointName)
	}
	if raw == "default" {
		return EndpointName{}, fmt.Errorf("%w: implicit default alias is forbidden", ErrInvalidEndpointName)
	}
	if raw == "_" {
		return EndpointName{}, fmt.Errorf("%w: underscore alias is forbidden", ErrInvalidEndpointName)
	}
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return EndpointName{}, fmt.Errorf("%w: only lowercase letters, digits, and dashes are allowed", ErrInvalidEndpointName)
		}
	}
	return EndpointName{value: raw}, nil
}

func (n EndpointName) String() string {
	return n.value
}

func (n EndpointName) IsZero() bool {
	return n.value == ""
}
