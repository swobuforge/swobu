package workspace

import (
	"context"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestPage_FirstFrameSelectsWorkspaceDisclosure(t *testing.T) {
	page := Page(readmodel.WorkspaceReadModel{
		ID:            "dev",
		Slug:          "dev",
		State:         readmodel.WorkspaceExisting,
		ClientBaseURL: "http://127.0.0.1:7926/c/dev",
		Routes: []readmodel.RouteReadModel{{
			ID:        "zai",
			ModelName: "zai",
			Enabled:   true,
		}},
	}, nil, nil, nil, context.Background(), nil, nil)

	h, err := testkit.NewHarnessAt(page, 120, 40)
	if err != nil {
		t.Fatalf("NewHarnessAt: %v", err)
	}
	defer h.Close()
	h.Open()

	frame := h.FrameTrimmed()
	if !strings.Contains(frame, "> workspace ▾") {
		t.Fatalf("first frame must select workspace disclosure:\n%s", frame)
	}
	if got := strings.Count(frame, ">"); got != 1 {
		t.Fatalf("first frame selection markers = %d, want 1:\n%s", got, frame)
	}
}
