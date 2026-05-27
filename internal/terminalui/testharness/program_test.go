package testharness

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
)

func TestProgram_Run_DoUntilAndEnsure(t *testing.T) {
	t.Parallel()

	type fake struct {
		out string
	}
	f := &fake{out: "row: workspace"}
	s := &Session{
		Render: func() string { return f.out },
		SendKey: func(key interaction.Key) {
			switch key {
			case interaction.KeyDown:
				f.out = "> routing"
			case interaction.KeyEnter:
				f.out = "picker\n> AWS Bedrock"
			}
		},
	}

	program := Plan(
		Do(interaction.KeyDown).Until(FocusedRowContains("routing")).Within(2),
		Do(interaction.KeyEnter).Until(TextContains("AWS Bedrock")).Within(2),
		Ensure(TextContains("picker")).Eventually(1),
	)

	if err := Run(s, program); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestProgram_Run_ReturnsStepContextOnFailure(t *testing.T) {
	t.Parallel()

	s := &Session{
		Render:  func() string { return "workspace" },
		SendKey: func(interaction.Key) {},
	}
	program := Plan(Do(interaction.KeyDown).Until(TextContains("routing")).Within(1))
	err := Run(s, program)
	if err == nil {
		t.Fatal("Run returned nil error, want failure")
	}
	if !strings.Contains(err.Error(), "step 1") {
		t.Fatalf("error missing step context: %v", err)
	}
}
