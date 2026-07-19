package compat

import (
	"testing"
)

func TestResponseReferenceFeaturesMirrorCanonicalPaths(t *testing.T) {
	t.Parallel()

	for feature, want := range map[Feature]string{
		RequestPreviousResponse:          "request.previous_response",
		RequestPreviousResponseResponses: "request.previous_response.responses",
		ResponseID:                       "response.id",
		ResponseIDResponses:              "response.id.responses",
	} {
		if string(feature) != want {
			t.Fatalf("feature %q = %q, want canonical path %q", feature, feature, want)
		}
	}
}

func TestValidateSubject(t *testing.T) {
	t.Parallel()

	valid := []Subject{
		"wire:/messages/2/tool_calls/0/id",
		"wire:/input/0/call_id",
		"canonical:items[3].tool_call.id",
		"route:provider/openrouter/protocol/chat_completions",
	}
	for _, subject := range valid {
		if err := ValidateSubject(subject); err != nil {
			t.Fatalf("ValidateSubject(%q) = %v", subject, err)
		}
	}

	invalid := []Subject{
		"messages[2].tool_calls[0].id",
		"because missing id",
		"wire:messages.2.id",
	}
	for _, subject := range invalid {
		if err := ValidateSubject(subject); err == nil {
			t.Fatalf("ValidateSubject(%q) expected error", subject)
		}
	}
}
