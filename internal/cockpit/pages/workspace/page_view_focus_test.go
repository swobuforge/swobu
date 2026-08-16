package workspace

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestPage_FirstFrameSelectsWorkspaceDisclosure(t *testing.T) {
	page := Page(readmodel.WorkspaceReadModel{
		ID:           "dev",
		Slug:         "dev",
		State:        readmodel.WorkspaceExisting,
		WorkspaceURL: "http://127.0.0.1:7926/c/dev",
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

func TestPage_ConventionalFirstWorkspaceRendersAuthoringAndEmptyActivity(t *testing.T) {
	page := Page(
		readmodel.NewConventionalFirstWorkspace("http://127.0.0.1:7926/c/default", nil),
		nil, nil, nil, context.Background(), nil, nil,
	)
	for _, width := range []int{80, 100, 120} {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			frame := testkit.RenderMountedTrimmed(t, page, width, 16)
			for _, want := range []string{"workspace", "endpoint", "/c/default", "model routes", "add model route", "activity", "no requests yet"} {
				if !strings.Contains(frame, want) {
					t.Fatalf("bootstrap page width %d missing %q:\n%s", width, want, frame)
				}
			}
			for _, forbidden := range []string{"derived from slug", "delete ↵", "discard ↵"} {
				if strings.Contains(frame, forbidden) {
					t.Fatalf("bootstrap page width %d exposes %q:\n%s", width, forbidden, frame)
				}
			}
		})
	}
}
