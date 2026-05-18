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
	if strings.TrimSpace(raw) == "" { // swobu:io-string source=domain
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
