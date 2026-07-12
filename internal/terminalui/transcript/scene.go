package transcript

// SceneSnapshot captures durable and ephemeral transcript lines for one frame.
type SceneSnapshot struct {
	Durable   []string
	Ephemeral []string
}

// Project converts one transcript tree into a render-ready scene snapshot.
func Project(root ViewSpec) SceneSnapshot {
	return SceneSnapshot{
		Durable:   DurableLines(root),
		Ephemeral: EphemeralLines(root),
	}
}
