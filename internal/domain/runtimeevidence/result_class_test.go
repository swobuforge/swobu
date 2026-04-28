package runtimeevidence

import "testing"

func TestParseResultClass_RejectsUnknown(t *testing.T) {
	if _, err := ParseResultClass("mystery"); err == nil {
		t.Fatal("ParseResultClass should reject unknown values")
	}
}

func TestResultClass_IsTerminal(t *testing.T) {
	if ResultClassInProgress.IsTerminal() {
		t.Fatal("in_progress should not be terminal")
	}
	if !ResultClassSuccess.IsTerminal() {
		t.Fatal("success should be terminal")
	}
}
