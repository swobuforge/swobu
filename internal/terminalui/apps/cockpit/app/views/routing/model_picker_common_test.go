package routing

import "testing"

func TestBuildModelPickerItems_ReturnsOnlyCatalogOptions(t *testing.T) {
	t.Parallel()

	items := buildModelPickerItems([]modelPickerOption{{Key: "gpt-5.5", Label: "gpt-5.5"}, {Key: "gpt-5.4-mini", Label: "gpt-5.4-mini"}})
	if len(items) != 2 {
		t.Fatalf("items len=%d want 2", len(items))
	}
	if got := items[0].Label; got != "gpt-5.5" {
		t.Fatalf("items[0] label=%q want gpt-5.5", got)
	}
	if got := items[1].Label; got != "gpt-5.4-mini" {
		t.Fatalf("items[1] label=%q want gpt-5.4-mini", got)
	}
	if got := items[0].Key; got != "gpt-5.5" {
		t.Fatalf("items[0] key=%q want gpt-5.5", got)
	}
	if got := items[1].Key; got != "gpt-5.4-mini" {
		t.Fatalf("items[1] key=%q want gpt-5.4-mini", got)
	}
}

func TestModelPickerFirstFocusKey_UsesFirstStableModelID(t *testing.T) {
	t.Parallel()

	items := []modelPickerOption{
		{Key: "gpt-5.5", Label: "gpt-5.5"},
		{Key: "gpt-5.4-mini", Label: "gpt-5.4-mini"},
	}
	if got := modelPickerFirstFocusKey(items, "provider-model-option"); got != "gpt-5.5" {
		t.Fatalf("first focus key=%q want gpt-5.5", got)
	}
}
