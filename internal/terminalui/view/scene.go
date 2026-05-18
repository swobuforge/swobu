package view

type SceneSnapshot struct {
	Durable   []string
	Ephemeral []string
}

func Project(root ViewSpec) SceneSnapshot {
	return SceneSnapshot{
		Durable:   DurableLines(root),
		Ephemeral: EphemeralLines(root),
	}
}
