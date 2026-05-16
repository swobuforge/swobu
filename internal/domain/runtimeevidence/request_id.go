package runtimeevidence

import (
	"fmt"
	"strings"
)

// RequestID is the stable evidence correlation key for one request lifecycle.
type RequestID struct {
	value string
}

func ParseRequestID(raw string) (RequestID, error) {
	if strings.TrimSpace(raw) == "" { // trimlowerlint:allow domain canonicalization
		return RequestID{}, fmt.Errorf("request id must not be empty")
	}
	return RequestID{value: raw}, nil
}

func (id RequestID) IsZero() bool {
	return id.value == ""
}

func (id RequestID) String() string {
	return id.value
}
