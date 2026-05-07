package view

type Scene struct {
	Durable   []string
	Ephemeral []string
}

func Project(root ViewSpec) Scene {
	return Scene{
		Durable:   DurableLines(root),
		Ephemeral: EphemeralLines(root),
	}
}

