package model

type OpKind string

const (
	OpAppendLine   OpKind = "append_line"
	OpUpdateStatus OpKind = "update_status"
	OpPaintFrame   OpKind = "paint_frame"
)

type OpRecord struct {
	Kind  OpKind
	Line  string
	Lines []string
}
