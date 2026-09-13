package routes

import (
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

func TestTargetDeleteConfirmationIsDangerOnlyWhileArmed(t *testing.T) {
	section := Section(readmodel.WorkspaceReadModel{ID: "dev"}, nil)
	row := TargetDeleteConfirmRow(section, readmodel.RouteReadModel{ID: "chat"}, readmodel.TargetReadModel{ID: "target", Model: "gpt"})
	if row.ValueTone != ui.ToneFailure {
		t.Fatalf("armed target-delete tone = %v, want failure", row.ValueTone)
	}
	if idle := AddTargetRowComponent(section, readmodel.RouteReadModel{ID: "chat"}); idle.ValueTone == ui.ToneFailure {
		t.Fatal("ordinary route action inherited destructive tone")
	}
}
