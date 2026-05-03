package custom

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

func TestClassifyBackendError_StrictToolModeUnsupported(t *testing.T) {
	base := compatibility.NewBackendError("backend-a", 400, `{"error":{"message":"tool_choice required is unsupported","param":"tool_choice","code":"unsupported_parameter"}}`, "")
	err := classifyBackendError(base)
	if !compatibility.IsBackendErrorClass(err, compatibility.BackendErrorClassToolChoiceUnsupported) {
		t.Fatal("expected strict tool mode unsupported capability classification")
	}
	var backendErr compatibility.BackendError
	if !errors.As(err, &backendErr) {
		t.Fatal("expected wrapped backend error to be preserved")
	}
}

func TestClassifyBackendError_LiveCaptureToolChoiceUnsupported(t *testing.T) {
	t.Parallel()
	const liveCapturedBody = `{"error":{"message":"Provider does not support tool_choice='required'. Update request to use tool_choice='auto'.","param":"tool_choice","code":"unsupported_parameter"}}`
	base := compatibility.NewBackendError("backend-a", 404, liveCapturedBody, "")
	classified := classifyBackendError(base)
	if !compatibility.IsBackendErrorClass(classified, compatibility.BackendErrorClassToolChoiceUnsupported) {
		t.Fatalf("fixture error was not classified as tool-choice unsupported: %#v", classified)
	}
}
