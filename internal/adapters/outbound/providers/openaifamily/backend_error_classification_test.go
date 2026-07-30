package openaifamily

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestClassifyBackendError_StrictToolModeUnsupported(t *testing.T) {
	base := canonical.NewBackendError("backend-a", 400, `{"error":{"message":"tool_choice required is unsupported","param":"tool_choice","code":"unsupported_parameter"}}`, "")
	err := classifyBackendError(base)
	if !canonical.IsBackendErrorClass(err, canonical.BackendErrorClassToolChoiceUnsupported) {
		t.Fatal("expected strict tool mode unsupported capability classification")
	}
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) {
		t.Fatal("expected wrapped backend error to be preserved")
	}
}

func TestClassifyBackendError_LiveCaptureToolChoiceUnsupported(t *testing.T) {
	t.Parallel()
	const liveCapturedBody = `{"error":{"message":"Provider does not support tool_choice='required'. Update request to use tool_choice='auto'.","param":"tool_choice","code":"unsupported_parameter"}}`
	base := canonical.NewBackendError("backend-a", 404, liveCapturedBody, "")
	classified := classifyBackendError(base)
	if !canonical.IsBackendErrorClass(classified, canonical.BackendErrorClassToolChoiceUnsupported) {
		t.Fatalf("fixture error was not classified as tool-choice unsupported: %#v", classified)
	}
}

func TestClassifyBackendError_ContinuationReferenceRequiresOwnedFields(t *testing.T) {
	classified := classifyBackendError(canonical.NewBackendError(
		"backend-a",
		404,
		`{"error":{"message":"previous_response_id was not found","param":"previous_response_id","code":"not_found"}}`,
		"",
	))
	if !canonical.IsBackendErrorClass(classified, canonical.BackendErrorClassContinuationReferenceInvalid) {
		t.Fatalf("continuation error was not classified: %#v", classified)
	}
	unstructured := classifyBackendError(canonical.NewBackendError(
		"backend-a",
		404,
		`{"error":{"message":"not found"}}`,
		"",
	))
	if canonical.IsBackendErrorClass(unstructured, canonical.BackendErrorClassContinuationReferenceInvalid) {
		t.Fatalf("unstructured status became continuation fact: %#v", unstructured)
	}
}
