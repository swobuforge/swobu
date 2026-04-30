package model

type Mode string

const (
	ModeAppend     Mode = "append"
	ModeLive       Mode = "live"
	ModeFullscreen Mode = "fullscreen"
)

type Durability string

const (
	Durable   Durability = "durable"
	Ephemeral Durability = "ephemeral"
)

type Node struct {
	Key        string
	Kind       string
	Text       string
	Durability Durability
	Children   []Node
}
