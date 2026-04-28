package protocols

import "io"

type WireRequest struct {
	Method  string
	Path    string
	Body    io.Reader
	HasBody bool
}
