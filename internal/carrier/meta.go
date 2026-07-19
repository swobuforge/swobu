package carrier

// Meta carries optional boundary metadata that is useful for tracing and
// diagnostics but not part of semantic request/response meaning.
type Meta struct {
	Endpoint string
	Opaque   map[string]string
}

func (m Meta) Clone() Meta {
	out := m
	if len(m.Opaque) > 0 {
		out.Opaque = make(map[string]string, len(m.Opaque))
		for k, v := range m.Opaque {
			out.Opaque[k] = v
		}
	}
	return out
}
