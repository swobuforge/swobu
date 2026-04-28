package runtimeevidence

import "testing"

func TestNewTokenUsageWithOptional_RejectsNegativeValues(t *testing.T) {
	negative := -1
	if _, err := NewTokenUsageWithOptional(&negative, nil, nil, nil); err == nil {
		t.Fatal("NewTokenUsageWithOptional should reject negative input tokens")
	}
	if _, err := NewTokenUsageWithOptional(nil, &negative, nil, nil); err == nil {
		t.Fatal("NewTokenUsageWithOptional should reject negative output tokens")
	}
	if _, err := NewTokenUsageWithOptional(nil, nil, &negative, nil); err == nil {
		t.Fatal("NewTokenUsageWithOptional should reject negative cache read tokens")
	}
	if _, err := NewTokenUsageWithOptional(nil, nil, nil, &negative); err == nil {
		t.Fatal("NewTokenUsageWithOptional should reject negative cache write tokens")
	}
}
