package routing

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func TestBuildModelPickerItems_PrependsRawIDChoiceWhenQueryNotInOptions(t *testing.T) {
	t.Parallel()

	var chosen string
	items := buildModelPickerItems(
		[]modelPickerOption{{Label: "gpt-5.5"}},
		"gpt-5.6-preview",
		func(rawID string) []update.Action {
			chosen = rawID
			return nil
		},
	)
	if len(items) < 2 {
		t.Fatalf("items len=%d want >=2", len(items))
	}
	if got := items[0].Label; got != "use: gpt-5.6-preview" {
		t.Fatalf("first label=%q want raw use option", got)
	}
	_ = items[0].OnChoose()
	if chosen != "gpt-5.6-preview" {
		t.Fatalf("chosen raw id=%q want gpt-5.6-preview", chosen)
	}
}

func TestBuildModelPickerItems_DoesNotDuplicateRawIDWhenAlreadyPresent(t *testing.T) {
	t.Parallel()

	items := buildModelPickerItems(
		[]modelPickerOption{{Label: "gpt-5.5"}},
		"gpt-5.5",
		func(string) []update.Action { return nil },
	)
	if len(items) != 1 {
		t.Fatalf("items len=%d want 1", len(items))
	}
	if got := items[0].Label; got != "gpt-5.5" {
		t.Fatalf("label=%q want gpt-5.5", got)
	}
}
