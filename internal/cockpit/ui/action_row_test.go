package ui

import (
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

type actionRowFixture struct {
	label  string
	value  string
	action string
}

func (f actionRowFixture) Render(*tui.App) *tui.Element {
	return ActionRow(">", f.label, f.value, f.action)
}

func TestActionRowLayoutContract(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		width      int
		value      string
		action     string
		wantValue  string
		wantAction string
		forbidJoin string
	}{
		{"short value", 100, "ready", "edit ↵", "ready", "edit ↵", "readyedit ↵"},
		{"exact-fit value", 60, strings.Repeat("x", 24), "edit ↵", strings.Repeat("x", 24), "edit ↵", strings.Repeat("x", 24) + "edit ↵"},
		{"overflowing value", 60, "https://bedrock-mantle.eu-west-2.api.aws/openai/v1", "edit ↵", "https://bedrock", "edit ↵", "v1edit ↵"},
		{"no action", 60, "https://example.test/value", "", "https://example.test/value", "", ""},
		{"80 column viewport", 80, "https://bedrock-mantle.eu-west-2.api.aws/openai/v1", "edit ↵", "https://bedrock-mantle", "edit ↵", "v1edit ↵"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rendered := testkit.RenderMountedTrimmed(t, actionRowFixture{label: "API URL", value: tc.value, action: tc.action}, tc.width, 2)
			if !strings.Contains(rendered, tc.wantValue) {
				t.Fatalf("frame missing value witness %q:\n%s", tc.wantValue, rendered)
			}
			if tc.wantAction != "" && !strings.Contains(rendered, tc.wantAction) {
				t.Fatalf("frame missing action %q:\n%s", tc.wantAction, rendered)
			}
			if tc.action == "" && strings.Contains(rendered, "edit ↵") {
				t.Fatalf("no-action row rendered an action:\n%s", rendered)
			}
			if tc.forbidJoin != "" && strings.Contains(rendered, tc.forbidJoin) {
				t.Fatalf("value and action lost separation:\n%s", rendered)
			}
		})
	}
}
