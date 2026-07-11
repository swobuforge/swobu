package carrier

import (
	"io"
	"net/http"
)

// HTTPEnvelope is one HTTP transport boundary carrier.
type HTTPEnvelope struct {
	Leg    Leg
	Method string
	Path   string
	Status int
	Header http.Header
	Body   io.ReadCloser
	Meta   Meta
}
