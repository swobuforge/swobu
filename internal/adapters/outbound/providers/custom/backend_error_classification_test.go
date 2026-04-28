package custom

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/metrofun/swobu/internal/domain/compatibility"
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

	type fixture struct {
		Response struct {
			Body string `json:"body"`
		} `json:"response"`
	}
	path := filepath.Join(
		"..", "..", "..", "..", "..",
		"test", "fixtures", "live_matrix", "records",
		"openrouter-chat-tool-sse-nvidia-nemotron-3-super-120b-a12b.json",
	)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error: %v", path, err)
	}
	var live fixture
	if err := json.Unmarshal(raw, &live); err != nil {
		t.Fatalf("Unmarshal fixture returned error: %v", err)
	}

	base := compatibility.NewBackendError("backend-a", 404, live.Response.Body, "")
	classified := classifyBackendError(base)
	if !compatibility.IsBackendErrorClass(classified, compatibility.BackendErrorClassToolChoiceUnsupported) {
		t.Fatalf("fixture error was not classified as tool-choice unsupported: %#v", classified)
	}
}
