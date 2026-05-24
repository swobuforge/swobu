package routing

import "testing"

func TestBuildModelPickerItems_ReturnsOnlyCatalogOptions(t *testing.T) {
	t.Parallel()

	items := buildModelPickerItems([]modelPickerOption{{Label: "gpt-5.5"}, {Label: "gpt-5.4-mini"}})
	if len(items) != 2 {
		t.Fatalf("items len=%d want 2", len(items))
	}
	if got := items[0].Label; got != "gpt-5.5" {
		t.Fatalf("items[0] label=%q want gpt-5.5", got)
	}
	if got := items[1].Label; got != "gpt-5.4-mini" {
		t.Fatalf("items[1] label=%q want gpt-5.4-mini", got)
	}
}
