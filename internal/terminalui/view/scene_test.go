package view

import "testing"

func TestProject_SplitsDurableAndEphemeral(t *testing.T) {
	t.Parallel()

	root := FlowColumn("root", 0,
		DurableText("a"),
		EphemeralText("s"),
	)
	scene := Project(root)
	if len(scene.Durable) != 1 || scene.Durable[0] != "a" {
		t.Fatalf("durable=%#v", scene.Durable)
	}
	if len(scene.Ephemeral) != 1 || scene.Ephemeral[0] != "s" {
		t.Fatalf("ephemeral=%#v", scene.Ephemeral)
	}
}

